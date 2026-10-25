package executor

import (
	"errors"
	"github.com/0xmhha/indexer-go/execwire"
	"testing"
)

func TestReplayRecordsNativeCallGraphIncludingRevertedParent(t *testing.T) {
	// Successful zero-value CALL to an EOA, then revert the parent. There is
	// no surviving journal change that could reconstruct the child call.
	code := []byte{0x60, 0, 0x60, 0, 0x60, 0, 0x60, 0, 0x60, 0, 0x60, 88, 0x61, 0xff, 0xff, 0xf1, 0x50, 0x60, 0, 0x60, 0, 0xfd}
	_, in := evmFixtureWith(t, code, []evmTxSpec{{to: &account, value: 1}})
	out, e := Replay(t.Context(), in)
	if e != nil {
		t.Fatal(e)
	}
	if out.CallTraces == nil || len(out.CallTraces.Frames) != 2 {
		t.Fatal("same replay must preserve root and attempted internal call")
	}
	root, child := out.CallTraces.Frames[0], out.CallTraces.Frames[1]
	if root.Parent != -1 || root.Error == "" || child.Parent != 0 || child.Error != "" || child.Type != "CALL" {
		t.Fatal("incorrect call/revert attribution")
	}
}

func nativeCallCode(op byte) []byte {
	c := []byte{0x60, 0, 0x60, 0, 0x60, 0, 0x60, 0}
	if op == 0xf1 || op == 0xf2 {
		c = append(c, 0x60, 0)
	}
	return append(c, 0x60, 88, 0x61, 0xff, 0xff, op, 0x50)
}
func TestReplayRecordsAllNativeCallKinds(t *testing.T) {
	code := allNativeCallsCode()
	_, in := evmFixtureWith(t, code, []evmTxSpec{{to: &account, value: 1}})
	out, e := Replay(t.Context(), in)
	if e != nil {
		t.Fatal(e)
	}
	want := []string{"CALL", "CALL", "CALLCODE", "DELEGATECALL", "STATICCALL", "CREATE", "CREATE2", "SELFDESTRUCT"}
	if out.CallTraces == nil || len(out.CallTraces.Frames) != len(want) {
		t.Fatalf("missing kinds: %+v", out.CallTraces)
	}
	for i, f := range out.CallTraces.Frames {
		if f.Type != want[i] || i > 0 && f.Parent != 0 {
			t.Fatalf("frame %d: %+v", i, f)
		}
	}
	if out.CallTraces.Frames[4].Value != nil {
		t.Fatal("STATICCALL must preserve absent value")
	}
}
func allNativeCallsCode() []byte {
	var code []byte
	for _, op := range []byte{0xf1, 0xf2, 0xf4, 0xfa} {
		code = append(code, nativeCallCode(op)...)
	}
	code = append(code, 0x60, 0, 0x60, 0, 0x60, 0, 0xf0, 0x50, 0x60, 0, 0x60, 0, 0x60, 0, 0x60, 0, 0xf5, 0x50, 0x60, 88, 0xff)
	return code
}
func TestCallObserverBoundsFailWholeResult(t *testing.T) {
	for _, kind := range []string{"field", "frames", "depth", "bytes"} {
		t.Run(kind, func(t *testing.T) {
			c := &callCollector{frames: []execwire.CallFrame{}}
			logger := newCallLogger(t.Context(), c, 0, [32]byte{1})
			switch kind {
			case "field":
				logger.push("CALL", account, account, make([]byte, execwire.MaxCallFieldBytes+1), 0, nil)
			case "frames":
				for range execwire.MaxCallFrames + 1 {
					logger.push("CALL", account, account, nil, 0, nil)
					logger.finish(nil, 0, nil)
				}
			case "depth":
				for range execwire.MaxCallDepth + 2 {
					logger.push("CALL", account, account, nil, 0, nil)
				}
			case "bytes":
				for range 66 {
					logger.push("CALL", account, account, make([]byte, execwire.MaxCallFieldBytes), 0, nil)
					logger.finish(nil, 0, nil)
				}
			}
			if !errors.Is(c.err, execwire.ErrLimit) {
				t.Fatalf("must fail, never silently truncate: %v", c.err)
			}
		})
	}
}
