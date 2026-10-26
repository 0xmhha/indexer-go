package execwire

import (
	"encoding/binary"
	"encoding/json"
	"testing"
)

func TestStateProofEnvelopeRoundTripAndLegacy(t *testing.T) {
	r := Result{Ref: Ref{Generation: 1, Height: 1, Hash: Hash{1}}, Parent: Hash{2}, StateRoot: Hash{3}, ReceiptsRoot: Hash{4}, BlockRLP: []byte{0xc0}, ReceiptsRLP: []byte{0xc0}, Steps: []Step{{Cause: Finalize, TxIndex: -1}}}
	original, e := MarshalResult(r)
	if e != nil {
		t.Fatal(e)
	}
	meta, e := json.Marshal(r)
	if e != nil {
		t.Fatal(e)
	}
	meta = append(meta[:len(meta)-1], []byte(`,"StateProofBundle":{"Version":1,"Format":"evm.state-proof.v1","Profile":"evm","Provider":"retained-state","Response":"e30="}}`)...)
	encoded := make([]byte, 16)
	copy(encoded, "EXR1")
	binary.BigEndian.PutUint32(encoded[4:8], uint32(len(meta)))
	binary.BigEndian.PutUint32(encoded[8:12], 1)
	binary.BigEndian.PutUint32(encoded[12:16], 1)
	encoded = append(encoded, meta...)
	encoded = append(encoded, 0xc0, 0xc0)
	got, e := UnmarshalResult(encoded)
	if e != nil {
		t.Fatalf("bounded optional state proof input must survive wire decoding: %v", e)
	}
	next, e := MarshalResult(got)
	if e != nil {
		t.Fatal(e)
	}
	if string(next) == string(original) {
		t.Fatal("supplied proof input was discarded")
	}
	legacy, e := UnmarshalResult(original)
	if e != nil {
		t.Fatal(e)
	}
	round, e := MarshalResult(legacy)
	if e != nil || string(round) != string(original) {
		t.Fatal("legacy input bytes changed")
	}
}

func TestStateProofEnvelopeBounds(t *testing.T) {
	valid := StateProofBundle{Version: 1, Format: StateProofFormat, Profile: "evm", Provider: "native-state", Response: []byte(`{}`)}
	for _, mutate := range []func(*StateProofBundle){func(b *StateProofBundle) { b.Version = 2 }, func(b *StateProofBundle) { b.Format = "other" }, func(b *StateProofBundle) { b.Profile = "" }, func(b *StateProofBundle) { b.Provider = "https://private" }, func(b *StateProofBundle) { b.Response = nil }, func(b *StateProofBundle) { b.Response = make([]byte, MaxStateProofBytes+1) }} {
		bad := valid
		mutate(&bad)
		if ValidateStateProofBundle(&bad) == nil {
			t.Fatal("invalid/oversized proof envelope accepted")
		}
	}
	if ValidateStateProofBundle(nil) != nil || ValidateStateProofBundle(&valid) != nil {
		t.Fatal("legacy/valid envelope rejected")
	}
}
