package execwire

import "fmt"

const (
	MaxCallFrames     = 4096
	MaxCallDepth      = 64
	MaxCallFieldBytes = 64 << 10
	MaxCallBytes      = 4 << 20
	MaxCallErrorBytes = 4096
)

// CallTraces is observation from the SAME verified recording execution. Nil
// means not collected. Frame completeness is a producer contract, not a proof
// implied by a receipt/state root. System journal steps are not user calls.
type CallTraces struct {
	Version uint32
	Profile string
	Frames  []CallFrame
}

// Parent is a prior frame's global ordinal, or -1 for a transaction root.
// To/Value are optional. In particular delegate value is context, not transfer.
type CallFrame struct {
	TransactionIndex uint32
	TransactionHash  Hash
	Parent           int32
	Type             string
	From             Address
	To               *Address
	Value            *Word
	Gas              uint64
	GasUsed          uint64
	Input            []byte
	Output           []byte
	Error            string
}

func ValidateCallTraces(r Result) error {
	t := r.CallTraces
	if t == nil {
		return nil
	}
	if t.Version != 1 || t.Frames == nil || len(t.Profile) == 0 || len(t.Profile) > 64 {
		return ErrProtocol
	}
	for _, c := range t.Profile {
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-' {
			continue
		}
		return ErrProtocol
	}
	if len(t.Frames) > MaxCallFrames || r.TxCount > MaxCallFrames {
		return ErrLimit
	}
	hashes := make([]Hash, 0, r.TxCount)
	for _, s := range r.Steps {
		if s.Cause == Transaction {
			hashes = append(hashes, s.TxHash)
		}
	}
	if len(hashes) != int(r.TxCount) {
		return ErrProtocol
	}
	var stack []int32
	roots, size := uint32(0), 0
	for i, f := range t.Frames {
		if f.TransactionIndex >= r.TxCount || f.TransactionHash != hashes[f.TransactionIndex] || f.Input == nil || f.Output == nil || f.GasUsed > f.Gas {
			return ErrProtocol
		}
		if len(f.Input) > MaxCallFieldBytes || len(f.Output) > MaxCallFieldBytes || len(f.Error) > MaxCallErrorBytes {
			return ErrLimit
		}
		size += len(f.Input) + len(f.Output) + len(f.Error)
		if size > MaxCallBytes {
			return ErrLimit
		}
		switch f.Type {
		case "CALL", "CALLCODE", "DELEGATECALL", "STATICCALL", "CREATE", "CREATE2", "SELFDESTRUCT":
		default:
			return ErrProtocol
		}
		if f.To == nil && ((f.Type != "CREATE" && f.Type != "CREATE2") || f.Error == "") {
			return ErrProtocol
		}
		if f.Type == "STATICCALL" && f.Value != nil {
			return ErrProtocol
		}
		if f.Parent == -1 {
			if f.TransactionIndex != roots || f.Value == nil || f.Type != "CALL" && f.Type != "CREATE" {
				return ErrProtocol
			}
			roots++
			stack = []int32{int32(i)}
			continue
		}
		if f.Parent < 0 || int(f.Parent) >= i || roots == 0 || f.TransactionIndex != roots-1 {
			return ErrProtocol
		}
		for len(stack) > 0 && stack[len(stack)-1] != f.Parent {
			stack = stack[:len(stack)-1]
		}
		if len(stack) == 0 || len(stack) > MaxCallDepth || t.Frames[f.Parent].Type == "SELFDESTRUCT" {
			return fmt.Errorf("%w: call preorder/depth", ErrProtocol)
		}
		stack = append(stack, int32(i))
	}
	if roots != r.TxCount {
		return fmt.Errorf("%w: missing call roots", ErrIncomplete)
	}
	return nil
}
