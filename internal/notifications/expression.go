package notifications

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"sync"

	"github.com/ethereum/go-ethereum/accounts/abi"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/checker"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
	"github.com/google/cel-go/interpreter"

	"github.com/0xmhha/indexer-go/pkg/events"
)

// A setting's Condition and Payload are CEL expressions
// (https://cel.dev) over the event being notified:
//
//   - block.number, block.time (uint), block.hash (string)
//   - chain.id (uint)
//   - tx.hash, tx.from, tx.to, tx.value (string; a transaction event)
//   - log.address, log.txHash (string), log.index (uint) (a log event)
//   - event.<argument> for each argument of the filter's event
//     (filter.event): uint8..uint64 as uint, int8..int64 as int, bool as
//     bool, everything else (larger integers, addresses, bytes, strings)
//     as a string the way decoded arguments are written
//
// Variables of another kind of event are empty ("" or 0). Integers that
// may exceed 64 bits are strings; compare them with bigCmp(a, b), which
// takes decimal or 0x strings (or an int or uint as b) and returns -1, 0
// or 1. Addresses and hashes are lower-case 0x hex.
//
// The condition must be a bool: false (or an error) creates no
// notification. The payload's value is sent as the notification's
// payload.result. Both are type-checked when the setting is registered,
// so a misspelled variable or argument, or a condition that is not a
// bool, is refused there. A registered expression whose estimated cost
// can exceed expressionCostLimit is refused, and evaluation stops at that
// cost.

const (
	maxExpressionLength = 2048
	// expressionCostLimit bounds an evaluation (cel.CostLimit units, about
	// one per operation and per character of string work).
	expressionCostLimit = 100_000
	// expressionStringSize bounds the strings an expression reads, for the
	// registration-time cost estimate: decoded arguments are at most a
	// few hundred characters except dynamic bytes and strings.
	expressionStringSize = 4096
)

// expressions are a setting's compiled condition and payload.
type expressions struct {
	condition cel.Program // nil when the setting has none
	payload   cel.Program
	// args are the filter event's argument types by name.
	args map[string]abi.Type
}

// expressionCache holds compiled expressions by their sources.
var expressionCache sync.Map // expressionKey -> *expressions

type expressionKey struct{ event, condition, payload string }

// argType is the CEL type of an event argument of type t.
func argType(t abi.Type) *cel.Type {
	switch t.T {
	case abi.UintTy:
		if t.Size <= 64 {
			return cel.UintType
		}
	case abi.IntTy:
		if t.Size <= 64 {
			return cel.IntType
		}
	case abi.BoolTy:
		return cel.BoolType
	}
	return cel.StringType
}

// argValue converts a decoded argument (declared.Format) to its CEL value.
func argValue(t abi.Type, s string) (any, error) {
	switch argType(t) {
	case cel.UintType:
		return strconv.ParseUint(s, 10, 64)
	case cel.IntType:
		return strconv.ParseInt(s, 10, 64)
	case cel.BoolType:
		return s == "true", nil
	}
	return s, nil
}

// bigInt parses a decimal or 0x string, or an int or uint, as an integer.
func bigInt(v ref.Val) (*big.Int, error) {
	switch x := v.(type) {
	case types.String:
		n, ok := new(big.Int).SetString(strings.TrimSpace(string(x)), 0)
		if !ok {
			return nil, fmt.Errorf("bigCmp: %q is not an integer", string(x))
		}
		return n, nil
	case types.Int:
		return big.NewInt(int64(x)), nil
	case types.Uint:
		return new(big.Int).SetUint64(uint64(x)), nil
	}
	return nil, fmt.Errorf("bigCmp: %s is not an integer", v.Type().TypeName())
}

func bigCmp(a, b ref.Val) ref.Val {
	x, err := bigInt(a)
	if err != nil {
		return types.NewErr("%v", err)
	}
	y, err := bigInt(b)
	if err != nil {
		return types.NewErr("%v", err)
	}
	return types.Int(x.Cmp(y))
}

// expressionEnv is the CEL environment of a setting with the filter event
// arguments args.
func expressionEnv(args map[string]abi.Type) (*cel.Env, error) {
	opts := []cel.EnvOption{
		cel.Variable("block.number", cel.UintType),
		cel.Variable("block.time", cel.UintType),
		cel.Variable("block.hash", cel.StringType),
		cel.Variable("chain.id", cel.UintType),
		cel.Variable("tx.hash", cel.StringType),
		cel.Variable("tx.from", cel.StringType),
		cel.Variable("tx.to", cel.StringType),
		cel.Variable("tx.value", cel.StringType),
		cel.Variable("log.address", cel.StringType),
		cel.Variable("log.txHash", cel.StringType),
		cel.Variable("log.index", cel.UintType),
		cel.Function("bigCmp",
			cel.Overload("bigCmp_string_string", []*cel.Type{cel.StringType, cel.StringType}, cel.IntType, cel.BinaryBinding(bigCmp)),
			cel.Overload("bigCmp_string_int", []*cel.Type{cel.StringType, cel.IntType}, cel.IntType, cel.BinaryBinding(bigCmp)),
			cel.Overload("bigCmp_string_uint", []*cel.Type{cel.StringType, cel.UintType}, cel.IntType, cel.BinaryBinding(bigCmp)),
		),
	}
	for name, t := range args {
		opts = append(opts, cel.Variable("event."+name, argType(t)))
	}
	return cel.NewEnv(opts...)
}

// sizeBound is the registration-time cost estimator: every string or list
// an expression reads is at most expressionStringSize long.
type sizeBound struct{}

func (sizeBound) EstimateSize(checker.AstNode) *checker.SizeEstimate {
	return &checker.SizeEstimate{Min: 0, Max: expressionStringSize}
}

func (sizeBound) EstimateCallCost(string, string, *checker.AstNode, []checker.AstNode) *checker.CallEstimate {
	return nil
}

// compileExpression type-checks one expression and builds its program;
// want is the type it must have (nil for any).
func compileExpression(env *cel.Env, what, src string, want *cel.Type) (cel.Program, error) {
	if len(src) > maxExpressionLength {
		return nil, fmt.Errorf("%s is longer than %d characters", what, maxExpressionLength)
	}
	ast, iss := env.Compile(src)
	if iss.Err() != nil {
		return nil, fmt.Errorf("%s: %w", what, iss.Err())
	}
	if want != nil && !ast.OutputType().IsExactType(want) {
		return nil, fmt.Errorf("%s must be a %s, not a %s", what, want, ast.OutputType())
	}
	cost, err := env.EstimateCost(ast, sizeBound{})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	if cost.Max > expressionCostLimit {
		return nil, fmt.Errorf("%s may cost up to %d, over the limit %d", what, cost.Max, expressionCostLimit)
	}
	return env.Program(ast, cel.CostLimit(expressionCostLimit))
}

// compileExpressions returns the compiled expressions of a setting, nil
// when it has none.
func compileExpressions(st *NotificationSetting) (*expressions, error) {
	if st.Condition == "" && st.Payload == "" {
		return nil, nil
	}
	key := expressionKey{condition: st.Condition, payload: st.Payload}
	if st.Filter != nil {
		key.event = st.Filter.Event
	}
	if x, ok := expressionCache.Load(key); ok {
		return x.(*expressions), nil
	}
	x := &expressions{args: map[string]abi.Type{}}
	if key.event != "" {
		p, err := filterEvent(key.event)
		if err != nil {
			return nil, fmt.Errorf("filter event %q: %w", key.event, err)
		}
		for _, a := range p.Event.Inputs {
			x.args[a.Name] = a.Type
		}
	}
	env, err := expressionEnv(x.args)
	if err != nil {
		return nil, err
	}
	if st.Condition != "" {
		if x.condition, err = compileExpression(env, "condition", st.Condition, cel.BoolType); err != nil {
			return nil, err
		}
	}
	if st.Payload != "" {
		if x.payload, err = compileExpression(env, "payload", st.Payload, nil); err != nil {
			return nil, err
		}
	}
	expressionCache.Store(key, x)
	return x, nil
}

// validateExpressions checks a setting's expressions when it is
// registered.
func validateExpressions(st *NotificationSetting) error {
	_, err := compileExpressions(st)
	return err
}

// eventVars are the variables of one event, resolved when an expression
// reads them (an interpreter.Activation), so variables an expression does
// not read cost nothing.
type eventVars struct {
	x       *expressions
	chainID uint64
	event   events.Event
	decoded map[string]string // the filter event's arguments
}

// activation returns the variables of an event; decoded are its filter
// event's arguments (nil when there are none).
func (x *expressions) activation(chainID uint64, event events.Event, decoded map[string]string) *eventVars {
	return &eventVars{x: x, chainID: chainID, event: event, decoded: decoded}
}

func (v *eventVars) Parent() interpreter.Activation { return nil }

func lowerHex(h interface{ Hex() string }) string { return strings.ToLower(h.Hex()) }

// ResolveName implements interpreter.Activation. Variables of another kind
// of event are empty; a filter event argument the log lacks is an error.
func (v *eventVars) ResolveName(name string) (any, bool) {
	if arg, ok := strings.CutPrefix(name, "event."); ok {
		t, ok := v.x.args[arg]
		if !ok {
			return nil, false
		}
		s, ok := v.decoded[arg]
		if !ok {
			return types.NewErr("event argument %s is missing (the log does not decode as the filter event)", arg), true
		}
		val, err := argValue(t, s)
		if err != nil {
			return types.NewErr("event argument %s: %v", arg, err), true
		}
		return val, true
	}
	var tx *events.TransactionEvent
	var log *gethtypes.Log
	var blk *events.BlockEvent
	switch e := v.event.(type) {
	case *events.TransactionEvent:
		tx = e
	case *events.LogEvent:
		log = e.Log
	case *events.BlockEvent:
		blk = e
	}
	switch name {
	case "chain.id":
		return v.chainID, true
	case "block.time":
		if ts := v.event.Timestamp(); ts.Unix() > 0 {
			return uint64(ts.Unix()), true
		}
		return uint64(0), true
	case "block.number":
		switch {
		case tx != nil:
			return tx.BlockNumber, true
		case log != nil:
			return log.BlockNumber, true
		case blk != nil:
			return blk.Number, true
		}
		return uint64(0), true
	case "block.hash":
		switch {
		case tx != nil:
			return lowerHex(tx.BlockHash), true
		case log != nil:
			return lowerHex(log.BlockHash), true
		case blk != nil:
			return lowerHex(blk.Hash), true
		}
		return "", true
	case "tx.hash", "tx.from", "tx.to", "tx.value":
		if tx == nil {
			return "", true
		}
		switch name {
		case "tx.hash":
			return lowerHex(tx.Hash), true
		case "tx.from":
			return lowerHex(tx.From), true
		case "tx.to":
			if tx.To == nil {
				return "", true
			}
			return lowerHex(tx.To), true
		}
		return tx.Value, true
	case "log.address", "log.txHash":
		if log == nil {
			return "", true
		}
		if name == "log.address" {
			return lowerHex(log.Address), true
		}
		return lowerHex(log.TxHash), true
	case "log.index":
		if log == nil {
			return uint64(0), true
		}
		return uint64(log.Index), true
	}
	return nil, false
}

// jsonValue converts a CEL value to the Go value json.Marshal writes.
func jsonValue(v ref.Val) (any, error) {
	switch x := v.(type) {
	case types.Null:
		return nil, nil
	case types.Bool, types.Int, types.Uint, types.Double, types.String:
		return x.Value(), nil
	case types.Bytes:
		return "0x" + hex.EncodeToString([]byte(x)), nil
	case traits.Mapper:
		out := map[string]any{}
		for it := x.Iterator(); it.HasNext() == types.True; {
			k := it.Next()
			key, ok := k.Value().(string)
			if !ok {
				key = fmt.Sprint(k.Value())
			}
			val, err := jsonValue(x.Get(k))
			if err != nil {
				return nil, err
			}
			out[key] = val
		}
		return out, nil
	case traits.Lister:
		var out []any
		for it := x.Iterator(); it.HasNext() == types.True; {
			val, err := jsonValue(it.Next())
			if err != nil {
				return nil, err
			}
			out = append(out, val)
		}
		return out, nil
	}
	return nil, fmt.Errorf("a %s cannot be sent", v.Type().TypeName())
}

// evaluate runs the condition and the payload over vars: whether to
// notify, and the payload's value as JSON (nil without a payload).
func (x *expressions) evaluate(vars *eventVars) (bool, json.RawMessage, error) {
	if x.condition != nil {
		out, _, err := x.condition.Eval(vars)
		if err != nil {
			return false, nil, fmt.Errorf("condition: %w", err)
		}
		if ok, isBool := out.Value().(bool); !isBool || !ok {
			return false, nil, nil
		}
	}
	if x.payload == nil {
		return true, nil, nil
	}
	out, _, err := x.payload.Eval(vars)
	if err != nil {
		return false, nil, fmt.Errorf("payload: %w", err)
	}
	val, err := jsonValue(out)
	if err != nil {
		return false, nil, fmt.Errorf("payload: %w", err)
	}
	data, err := json.Marshal(val)
	if err != nil {
		return false, nil, fmt.Errorf("payload: %w", err)
	}
	return true, data, nil
}
