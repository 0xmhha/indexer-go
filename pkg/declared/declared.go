// Package declared is the declared ingestion of refactoring plan R6-1: a
// project names the contracts, events and start block it needs and the
// tables their logs go to, instead of indexing the whole chain.
//
//	features:
//	  records:
//	    enabled: true
//	    sources:
//	      - name: settlement
//	        address: "0x..."               # or addresses: [...]
//	        start_block: 1200
//	        events:                        # human-readable signatures ...
//	          - "PaymentSettled(address indexed merchant, bytes32 indexed orderId, address device, uint256 amount)"
//	      - name: other
//	        address: "0x..."
//	        abi: abis/other.json           # ... or an ABI file and event names
//	        events: [Deposited]
//	    tables:
//	      - name: receipts
//	        source: settlement
//	        event: PaymentSettled
//	        keys:                          # lookups: each a list of event arguments
//	          - [merchant, orderId]
//
// Compile checks a Spec and returns the Plan the ingestion follows: which
// logs are wanted (Filter), which table a log goes to (Match) and its
// decoded fields (Decode).
package declared

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"

	"github.com/0xmhha/indexer-go/pkg/core/model"
)

// Spec is the declaration, as configured.
type Spec struct {
	Sources []Source `yaml:"sources"`
	Tables  []Table  `yaml:"tables"`
}

// Source is a set of contracts and the events read from them.
type Source struct {
	Name       string   `yaml:"name"`
	Address    string   `yaml:"address"`
	Addresses  []string `yaml:"addresses"`
	StartBlock uint64   `yaml:"start_block"`
	// ABI is the path of a JSON ABI; Events are then event names.
	// Without it, Events are human-readable signatures.
	ABI    string   `yaml:"abi"`
	Events []string `yaml:"events"`
}

// Table receives the logs of one event of one source.
type Table struct {
	Name   string `yaml:"name"`
	Source string `yaml:"source"`
	Event  string `yaml:"event"`
	// Keys are the lookups the table serves, each a list of event
	// arguments whose values together find records.
	Keys [][]string `yaml:"keys"`
}

// Plan is a compiled Spec.
type Plan struct {
	Tables     []*TablePlan
	byLog      map[logKey][]*TablePlan
	addresses  []common.Address
	topics     []common.Hash
	startBlock uint64
}

type logKey struct {
	address common.Address
	topic   common.Hash
}

// TablePlan is a compiled table.
type TablePlan struct {
	Name      string
	Source    string
	Event     abi.Event
	Addresses []common.Address
	// Keys are the table's lookups, each a list of argument names in the
	// declared order; KeyID names one.
	Keys [][]string
}

// KeyID is the name of a key: its arguments joined with ",".
func KeyID(fields []string) string { return strings.Join(fields, ",") }

// Fields are the table's fields: the event's arguments, in order.
func (t *TablePlan) Fields() []string {
	out := make([]string, len(t.Event.Inputs))
	for i, a := range t.Event.Inputs {
		out[i] = a.Name
	}
	return out
}

// Key returns the declared key with exactly these fields (in any order),
// in declared order; false when the table has none.
func (t *TablePlan) Key(fields []string) ([]string, bool) {
	want := append([]string{}, fields...)
	sort.Strings(want)
	for _, k := range t.Keys {
		have := append([]string{}, k...)
		sort.Strings(have)
		if slicesEqual(have, want) {
			return k, true
		}
	}
	return nil, false
}

// Lookup finds the declared key with exactly the fields of values and
// returns its KeyID and the values in the key's order, normalized as
// records store them (FormatInput).
func (t *TablePlan) Lookup(values map[string]string) (string, []string, error) {
	names := make([]string, 0, len(values))
	for n := range values {
		names = append(names, n)
	}
	fields, ok := t.Key(names)
	if !ok {
		sort.Strings(names)
		return "", nil, fmt.Errorf("table %q declares no key %v (keys: %v)", t.Name, names, t.Keys)
	}
	out := make([]string, len(fields))
	for i, f := range fields {
		for _, arg := range t.Event.Inputs {
			if arg.Name == f {
				v, err := FormatInput(arg, values[f])
				if err != nil {
					return "", nil, err
				}
				out[i] = v
			}
		}
	}
	return KeyID(fields), out, nil
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

var namePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)

// Compile checks a Spec: names are unique lower-case identifiers, addresses
// are addresses, events parse (or are found in the ABI) and every table
// names an event of its source and keys of its event's arguments.
func Compile(spec Spec) (*Plan, error) {
	if len(spec.Tables) == 0 {
		return nil, fmt.Errorf("no tables are declared")
	}
	type compiled struct {
		src       Source
		addresses []common.Address
		events    map[string]abi.Event
	}
	sources := map[string]*compiled{}
	for i, s := range spec.Sources {
		where := fmt.Sprintf("sources[%d]", i)
		if !namePattern.MatchString(s.Name) {
			return nil, fmt.Errorf("%s: name %q is not a lower-case identifier", where, s.Name)
		}
		if sources[s.Name] != nil {
			return nil, fmt.Errorf("%s: source %q is declared twice", where, s.Name)
		}
		c := &compiled{src: s, events: map[string]abi.Event{}}
		for _, a := range append(append([]string{}, s.Addresses...), s.Address) {
			if a == "" {
				continue
			}
			if !common.IsHexAddress(a) {
				return nil, fmt.Errorf("%s: %q is not an address", where, a)
			}
			c.addresses = append(c.addresses, common.HexToAddress(a))
		}
		if len(c.addresses) == 0 {
			return nil, fmt.Errorf("%s: no address", where)
		}
		if len(s.Events) == 0 {
			return nil, fmt.Errorf("%s: no events", where)
		}
		events, err := sourceEvents(s)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", where, err)
		}
		for _, ev := range events {
			if _, dup := c.events[ev.Name]; dup {
				return nil, fmt.Errorf("%s: event %s is declared twice", where, ev.Name)
			}
			c.events[ev.Name] = ev
		}
		sources[s.Name] = c
	}

	p := &Plan{byLog: map[logKey][]*TablePlan{}}
	seenTable := map[string]bool{}
	seenAddr, seenTopic := map[common.Address]bool{}, map[common.Hash]bool{}
	first := true
	for i, t := range spec.Tables {
		where := fmt.Sprintf("tables[%d]", i)
		if !namePattern.MatchString(t.Name) {
			return nil, fmt.Errorf("%s: name %q is not a lower-case identifier", where, t.Name)
		}
		if seenTable[t.Name] {
			return nil, fmt.Errorf("%s: table %q is declared twice", where, t.Name)
		}
		seenTable[t.Name] = true
		src := sources[t.Source]
		if src == nil {
			return nil, fmt.Errorf("%s: source %q is not declared", where, t.Source)
		}
		ev, ok := src.events[t.Event]
		if !ok {
			return nil, fmt.Errorf("%s: source %q declares no event %q", where, t.Source, t.Event)
		}
		if ev.Anonymous {
			return nil, fmt.Errorf("%s: event %q is anonymous", where, t.Event)
		}
		tp := &TablePlan{Name: t.Name, Source: t.Source, Event: ev, Addresses: src.addresses}
		args := map[string]bool{}
		for _, a := range ev.Inputs {
			if a.Name == "" {
				return nil, fmt.Errorf("%s: event %q has an unnamed argument", where, t.Event)
			}
			args[a.Name] = true
		}
		seenKey := map[string]bool{}
		for _, k := range t.Keys {
			if len(k) == 0 {
				return nil, fmt.Errorf("%s: an empty key", where)
			}
			sorted := append([]string{}, k...)
			sort.Strings(sorted)
			if seenKey[KeyID(sorted)] {
				return nil, fmt.Errorf("%s: key %v is declared twice", where, k)
			}
			seenKey[KeyID(sorted)] = true
			for i, f := range k {
				if !args[f] {
					return nil, fmt.Errorf("%s: key field %q is not an argument of %s", where, f, t.Event)
				}
				if i > 0 && sorted[i] == sorted[i-1] {
					return nil, fmt.Errorf("%s: key %v names a field twice", where, k)
				}
			}
			tp.Keys = append(tp.Keys, append([]string{}, k...))
		}
		p.Tables = append(p.Tables, tp)
		for _, a := range src.addresses {
			k := logKey{a, ev.ID}
			p.byLog[k] = append(p.byLog[k], tp)
			if !seenAddr[a] {
				seenAddr[a] = true
				p.addresses = append(p.addresses, a)
			}
		}
		if !seenTopic[ev.ID] {
			seenTopic[ev.ID] = true
			p.topics = append(p.topics, ev.ID)
		}
		if first || src.src.StartBlock < p.startBlock {
			p.startBlock, first = src.src.StartBlock, false
		}
	}
	return p, nil
}

// sourceEvents returns a source's events: from its ABI file by name, or
// parsed from signatures.
func sourceEvents(s Source) ([]abi.Event, error) {
	if s.ABI == "" {
		out := make([]abi.Event, 0, len(s.Events))
		for _, sig := range s.Events {
			ev, err := ParseEvent(sig)
			if err != nil {
				return nil, err
			}
			out = append(out, ev)
		}
		return out, nil
	}
	data, err := os.ReadFile(s.ABI)
	if err != nil {
		return nil, fmt.Errorf("read ABI: %w", err)
	}
	parsed, err := abi.JSON(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("parse ABI %s: %w", s.ABI, err)
	}
	out := make([]abi.Event, 0, len(s.Events))
	for _, name := range s.Events {
		ev, ok := parsed.Events[name]
		if !ok {
			return nil, fmt.Errorf("ABI %s has no event %q", s.ABI, name)
		}
		out = append(out, ev)
	}
	return out, nil
}

var (
	signaturePattern = regexp.MustCompile(`^\s*(?:event\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*\((.*)\)\s*(anonymous)?\s*;?\s*$`)
	identPattern     = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

// ParseEvent parses a human-readable event signature such as
// "PaymentSettled(address indexed merchant, uint256 amount)". Every
// argument needs a name; tuple arguments need an ABI file.
func ParseEvent(sig string) (abi.Event, error) {
	m := signaturePattern.FindStringSubmatch(sig)
	if m == nil {
		return abi.Event{}, fmt.Errorf("event %q is not <Name>(<type> [indexed] <name>, ...)", sig)
	}
	var args abi.Arguments
	if body := strings.TrimSpace(m[2]); body != "" {
		for _, part := range strings.Split(body, ",") {
			words := strings.Fields(part)
			arg := abi.Argument{}
			switch {
			case len(words) == 2:
			case len(words) == 3 && words[1] == "indexed":
				arg.Indexed = true
			default:
				return abi.Event{}, fmt.Errorf("event %q: argument %q is not <type> [indexed] <name>", sig, strings.TrimSpace(part))
			}
			typ, err := abi.NewType(words[0], "", nil)
			if err != nil {
				return abi.Event{}, fmt.Errorf("event %q: type %q: %w", sig, words[0], err)
			}
			name := words[len(words)-1]
			if !identPattern.MatchString(name) || name == "indexed" {
				return abi.Event{}, fmt.Errorf("event %q: argument name %q", sig, name)
			}
			arg.Name, arg.Type = name, typ
			args = append(args, arg)
		}
	}
	return abi.NewEvent(m[1], m[1], m[3] != "", args), nil
}

// Addresses are the contracts whose logs are wanted.
func (p *Plan) Addresses() []common.Address { return p.addresses }

// Topics are the event topics (topic0) wanted from them.
func (p *Plan) Topics() []common.Hash { return p.topics }

// StartBlock is the earliest start block of the sources with tables.
func (p *Plan) StartBlock() uint64 { return p.startBlock }

// Match returns the tables a log goes to.
func (p *Plan) Match(l *model.Log) []*TablePlan {
	if l == nil || len(l.Topics) == 0 {
		return nil
	}
	return p.byLog[logKey{l.Address, l.Topics[0]}]
}

// Table returns a table by name.
func (p *Plan) Table(name string) (*TablePlan, bool) {
	for _, t := range p.Tables {
		if t.Name == name {
			return t, true
		}
	}
	return nil, false
}

// Decode returns the event's arguments in a log as strings (Format).
// Indexed arguments of dynamic types (string, bytes, arrays) are the topic,
// the hash of the value, as the chain keeps them.
func (t *TablePlan) Decode(l *model.Log) (map[string]string, error) {
	var indexed abi.Arguments
	for _, a := range t.Event.Inputs {
		if a.Indexed {
			indexed = append(indexed, a)
		}
	}
	if len(l.Topics) != len(indexed)+1 {
		return nil, fmt.Errorf("log has %d topics, %s needs %d", len(l.Topics), t.Event.Sig, len(indexed)+1)
	}
	values := map[string]interface{}{}
	if err := t.Event.Inputs.NonIndexed().UnpackIntoMap(values, l.Data); err != nil {
		return nil, fmt.Errorf("decode %s data: %w", t.Event.Sig, err)
	}
	for i, a := range indexed {
		topic := l.Topics[i+1]
		if dynamic(a.Type) {
			values[a.Name] = topic
			continue
		}
		if err := abi.ParseTopicsIntoMap(values, abi.Arguments{a}, []common.Hash{topic}); err != nil {
			return nil, fmt.Errorf("decode %s topic %s: %w", t.Event.Sig, a.Name, err)
		}
	}
	out := make(map[string]string, len(values))
	for _, a := range t.Event.Inputs {
		s, err := Format(values[a.Name])
		if err != nil {
			return nil, fmt.Errorf("%s.%s: %w", t.Event.Name, a.Name, err)
		}
		out[a.Name] = s
	}
	return out, nil
}

// dynamic reports whether an indexed argument of the type is stored as
// the hash of its value.
func dynamic(t abi.Type) bool {
	switch t.T {
	case abi.StringTy, abi.BytesTy, abi.SliceTy, abi.ArrayTy, abi.TupleTy:
		return true
	}
	return false
}

// Format writes a decoded value as stored and compared: addresses and
// bytes as lower-case 0x hex, integers in decimal, booleans as true or
// false, strings as they are, and anything else (arrays, tuples) as JSON.
func Format(v interface{}) (string, error) {
	switch x := v.(type) {
	case common.Address:
		return strings.ToLower(x.Hex()), nil
	case common.Hash:
		return x.Hex(), nil
	case *big.Int:
		return x.String(), nil
	case []byte:
		return "0x" + hex.EncodeToString(x), nil
	case bool:
		if x {
			return "true", nil
		}
		return "false", nil
	case string:
		return x, nil
	case uint8, uint16, uint32, uint64, int8, int16, int32, int64:
		return fmt.Sprint(x), nil
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Array && rv.Type().Elem().Kind() == reflect.Uint8 {
		b := make([]byte, rv.Len())
		reflect.Copy(reflect.ValueOf(b), rv)
		return "0x" + hex.EncodeToString(b), nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("format %T: %w", v, err)
	}
	return string(data), nil
}

// FormatInput normalizes a value given for an argument (in a lookup) the
// way Decode writes it, so the same value compares equal however it was
// written (address case, leading zeros of bytes32, hex or decimal
// integers).
func FormatInput(arg abi.Argument, s string) (string, error) {
	s = strings.TrimSpace(s)
	switch arg.Type.T {
	case abi.AddressTy:
		if !common.IsHexAddress(s) {
			return "", fmt.Errorf("%s: %q is not an address", arg.Name, s)
		}
		return strings.ToLower(common.HexToAddress(s).Hex()), nil
	case abi.IntTy, abi.UintTy:
		v, ok := new(big.Int).SetString(s, 0)
		if !ok {
			return "", fmt.Errorf("%s: %q is not an integer", arg.Name, s)
		}
		return v.String(), nil
	case abi.BoolTy:
		if s != "true" && s != "false" {
			return "", fmt.Errorf("%s: %q is not true or false", arg.Name, s)
		}
		return s, nil
	case abi.FixedBytesTy:
		b, err := hex.DecodeString(strings.TrimPrefix(strings.ToLower(s), "0x"))
		if err != nil || len(b) != arg.Type.Size {
			return "", fmt.Errorf("%s: %q is not %d bytes of hex", arg.Name, s, arg.Type.Size)
		}
		return "0x" + hex.EncodeToString(b), nil
	}
	if arg.Indexed && dynamic(arg.Type) {
		if len(strings.TrimPrefix(s, "0x")) != 64 {
			return "", fmt.Errorf("%s: an indexed %s is looked up by its hash (32 bytes of hex)", arg.Name, arg.Type)
		}
		return common.HexToHash(s).Hex(), nil
	}
	return s, nil
}
