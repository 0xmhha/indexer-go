package execwire

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"testing"
)

func TestCallTraceMetadataSurvivesRoundTrip(t *testing.T) {
	_, _, result := fixture()
	payload, err := MarshalResult(result)
	if err != nil {
		t.Fatal(err)
	}
	end := 16 + int(binary.BigEndian.Uint32(payload[4:8]))
	// Independent canonical bytes: keep old member order and append the
	// versioned extension in its defined position. A map would sort keys.
	calls := fmt.Sprintf(`,"CallTraces":{"Version":1,"Profile":"evm","Frames":[{"TransactionIndex":0,"TransactionHash":"%s","Parent":-1,"Type":"CALL","From":"%s","To":"%s","Value":"%s","Gas":21000,"GasUsed":21000,"Input":"","Output":"","Error":""}]}}`, hexText(result.Steps[0].TxHash[:]), hexText([]byte{1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}), hexText([]byte{2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}), hexText(make([]byte, 32)))
	encoded := append(append([]byte{}, payload[16:end-1]...), calls...)
	raw := append([]byte{}, payload[:16]...)
	binary.BigEndian.PutUint32(raw[4:8], uint32(len(encoded)))
	raw = append(raw, encoded...)
	raw = append(raw, payload[end:]...)
	got, err := UnmarshalResult(raw)
	if err != nil {
		t.Fatalf("versioned same-replay call trace must be accepted: %v", err)
	}
	encoded, err = MarshalResult(got)
	if err != nil {
		t.Fatal(err)
	}
	var roundTrip map[string]any
	end = 16 + int(binary.BigEndian.Uint32(encoded[4:8]))
	if err := json.Unmarshal(encoded[16:end], &roundTrip); err != nil {
		t.Fatal(err)
	}
	if roundTrip["CallTraces"] == nil {
		t.Fatal("call traces were discarded")
	}
}

func TestCallTraceGraphBoundsAndCompleteness(t *testing.T) {
	for name, change := range map[string]func(*Result){
		"version":           func(r *Result) { r.CallTraces.Version = 2 },
		"profile injection": func(r *Result) { r.CallTraces.Profile = "x';DROP TABLE blocks" },
		"missing roots":     func(r *Result) { r.CallTraces.Frames = []CallFrame{} },
		"missing frames":    func(r *Result) { r.CallTraces.Frames = nil },
		"future parent":     func(r *Result) { r.CallTraces.Frames[1].Parent = 2 },
		"self parent":       func(r *Result) { r.CallTraces.Frames[1].Parent = 1 },
		"closed parent": func(r *Result) {
			r.CallTraces.Frames = append(r.CallTraces.Frames, r.CallTraces.Frames[1], r.CallTraces.Frames[1])
			r.CallTraces.Frames[3].Parent = 1
		},
		"selfdestruct parent": func(r *Result) {
			r.CallTraces.Frames[1].Type = "SELFDESTRUCT"
			f := r.CallTraces.Frames[1]
			f.Parent = 1
			r.CallTraces.Frames = append(r.CallTraces.Frames, f)
		},
		"unknown type":       func(r *Result) { r.CallTraces.Frames[1].Type = "UNKNOWN" },
		"unknown tx":         func(r *Result) { r.CallTraces.Frames[1].TransactionIndex++ },
		"wrong tx hash":      func(r *Result) { r.CallTraces.Frames[1].TransactionHash[0]++ },
		"root value missing": func(r *Result) { r.CallTraces.Frames[0].Value = nil },
		"static value":       func(r *Result) { r.CallTraces.Frames[1].Type = "STATICCALL" },
		"missing target":     func(r *Result) { r.CallTraces.Frames[1].To = nil },
		"missing input":      func(r *Result) { r.CallTraces.Frames[1].Input = nil },
		"oversized field":    func(r *Result) { r.CallTraces.Frames[1].Input = make([]byte, MaxCallFieldBytes+1) },
		"oversized error":    func(r *Result) { r.CallTraces.Frames[1].Error = string(make([]byte, MaxCallErrorBytes+1)) },
		"gas":                func(r *Result) { r.CallTraces.Frames[1].GasUsed = r.CallTraces.Frames[1].Gas + 1 },
		"frames": func(r *Result) {
			f := r.CallTraces.Frames[1]
			for len(r.CallTraces.Frames) <= MaxCallFrames {
				r.CallTraces.Frames = append(r.CallTraces.Frames, f)
			}
		},
		"depth": func(r *Result) {
			for len(r.CallTraces.Frames) <= MaxCallDepth+1 {
				f := r.CallTraces.Frames[1]
				f.Parent = int32(len(r.CallTraces.Frames) - 1)
				r.CallTraces.Frames = append(r.CallTraces.Frames, f)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, _, r := fixture()
			to, value := Address{2}, Word{}
			f := CallFrame{TransactionHash: r.Steps[0].TxHash, Parent: -1, Type: "CALL", From: Address{1}, To: &to, Value: &value, Gas: 1, Input: []byte{}, Output: []byte{}}
			child := f
			child.Parent = 0
			r.CallTraces = &CallTraces{Version: 1, Profile: "evm", Frames: []CallFrame{f, child}}
			if _, e := MarshalResult(r); e != nil {
				t.Fatal(e)
			}
			change(&r)
			if _, e := MarshalResult(r); e == nil {
				t.Fatal("invalid trace must fail the whole result")
			}
		})
	}
}
