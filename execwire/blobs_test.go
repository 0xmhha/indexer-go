package execwire

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"testing"
)

func TestBlobBundleMetadataSurvivesRoundTrip(t *testing.T) {
	_, _, r := fixture()
	p, err := MarshalResult(r)
	if err != nil {
		t.Fatal(err)
	}
	end := 16 + int(binary.BigEndian.Uint32(p[4:8]))
	ext := `,"BlobBundle":{"Version":1,"Format":"eip4844.full-blob-proof.v1","Profile":"evm","Provider":"retained-sidecars","Response":"wA=="}}`
	meta := append(append([]byte{}, p[16:end-1]...), ext...)
	raw := append([]byte{}, p[:16]...)
	binary.BigEndian.PutUint32(raw[4:8], uint32(len(meta)))
	raw = append(raw, meta...)
	raw = append(raw, p[end:]...)
	got, err := UnmarshalResult(raw)
	if err != nil {
		t.Fatalf("bounded optional blob envelope must survive: %v", err)
	}
	p, err = MarshalResult(got)
	if err != nil {
		t.Fatal(err)
	}
	end = 16 + int(binary.BigEndian.Uint32(p[4:8]))
	var out map[string]any
	if err := json.Unmarshal(p[16:end], &out); err != nil {
		t.Fatal(err)
	}
	if out["BlobBundle"] == nil {
		t.Fatal("blob input was silently discarded")
	}
}

func TestBlobBundleBoundsAndLegacy(t *testing.T) {
	_, _, r := fixture()
	legacy, err := MarshalResult(r)
	if err != nil {
		t.Fatal(err)
	}
	b := BlobBundle{Version: 1, Format: FullBlobFormat, Profile: "evm", Provider: "retained-sidecars", Response: []byte{0xc0}}
	for _, mutate := range []func(*BlobBundle){
		func(b *BlobBundle) { b.Version = 2 }, func(b *BlobBundle) { b.Format = "cell-proof" }, func(b *BlobBundle) { b.Profile = "" }, func(b *BlobBundle) { b.Provider = "https://private/key" }, func(b *BlobBundle) { b.Response = nil }, func(b *BlobBundle) { b.Response = make([]byte, MaxBlobResponseBytes+1) },
	} {
		bad := b
		mutate(&bad)
		r.BlobBundle = &bad
		if _, err := MarshalResult(r); err == nil {
			t.Fatal("invalid/oversized blob envelope accepted")
		}
	}
	r.BlobBundle = nil
	current, err := MarshalResult(r)
	if err != nil || !bytes.Equal(legacy, current) {
		t.Fatal("legacy bytes changed", err)
	}
}
