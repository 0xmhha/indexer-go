package execwire

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"reflect"
	"testing"
	"time"
)

func fixture() (Chain, Ref, Result) {
	c := Chain{ID: "1001", Genesis: Hash{1}}
	r := Ref{Generation: 1, Height: 2, Hash: Hash{2}}
	v := Result{Ref: r, Parent: Hash{1}, Time: 1, StateRoot: Hash{3}, ReceiptsRoot: Hash{4}, TxCount: 1, BlockRLP: []byte{0xc0}, ReceiptsRLP: []byte{0xc0}, Steps: []Step{
		{Ordinal: 0, Cause: Transaction, TxIndex: 0, TxHash: Hash{5}, Accounts: []AccountChange{{Address: Address{1}, Before: Account{Exists: true}, After: Account{Exists: true, Nonce: 1}}}},
		{Ordinal: 1, Cause: Finalize, TxIndex: -1},
	}}
	return c, r, v
}

func TestResultDeterministicRoundTrip(t *testing.T) {
	_, _, v := fixture()
	a, err := MarshalResult(v)
	if err != nil {
		t.Fatal(err)
	}
	b, err := MarshalResult(v)
	if err != nil || !bytes.Equal(a, b) {
		t.Fatalf("non-deterministic: %v", err)
	}
	got, err := UnmarshalResult(a)
	if err != nil || !reflect.DeepEqual(v, got) {
		t.Fatalf("round trip: %v", err)
	}
	for _, change := range []func(*Result){
		func(r *Result) { r.Ref.Generation = 0 }, func(r *Result) { r.Steps[1].Ordinal = 0 },
		func(r *Result) { r.Steps = r.Steps[:1] }, func(r *Result) { r.TxCount = 2 },
		func(r *Result) { r.Steps[0].TxIndex = -1 }, func(r *Result) { r.Steps[0].TxHash = Hash{} },
		func(r *Result) { r.Steps[0].Accounts = append(r.Steps[0].Accounts, r.Steps[0].Accounts[0]) },
		func(r *Result) { r.Steps[0].Accounts[0].After = Account{Nonce: 1} },
	} {
		_, _, bad := fixture()
		change(&bad)
		if _, err := MarshalResult(bad); err == nil {
			t.Fatal("accepted invalid result")
		}
	}
	if _, err := UnmarshalResult(append(a, []byte("{}")...)); err == nil {
		t.Fatal("accepted trailing input")
	}
	if _, err := UnmarshalResult(bytes.Replace(a, []byte(`"Time":1`), []byte(`"Xime":1`), 1)); err == nil {
		t.Fatal("accepted unknown field")
	}
}

func TestFrameRoundTripAndRejectBeforeAllocation(t *testing.T) {
	c, r, _ := fixture()
	f := Frame{Kind: Committed, Header: Header{Version: Version, Chain: c, Ref: r, Digest: Hash{1}}}
	var b bytes.Buffer
	if err := Encode(&b, f); err != nil {
		t.Fatal(err)
	}
	raw := bytes.Clone(b.Bytes())
	got, err := Decode(&b)
	if err != nil || !reflect.DeepEqual(f, got) {
		t.Fatalf("round trip: %v", err)
	}
	for n := 0; n < len(raw); n++ {
		if _, err := Decode(bytes.NewReader(raw[:n])); err == nil {
			t.Fatalf("accepted truncation %d", n)
		}
	}
	for _, n := range []uint32{0, 1, MaxFrameBytes + 1, ^uint32(0)} {
		var prefix [4]byte
		binary.BigEndian.PutUint32(prefix[:], n)
		if _, err := Decode(bytes.NewReader(prefix[:])); err == nil {
			t.Fatalf("accepted size %d", n)
		}
	}
	for _, mutate := range []func(*Frame){func(f *Frame) { f.Header.Version++ }, func(f *Frame) { f.Kind = 255 }, func(f *Frame) { f.Header.Chain.ID = "1;DROP TABLE blocks" }, func(f *Frame) { f.Payload = []byte{1} }, func(f *Frame) { f.Header.Ref.Hash = Hash{} }} {
		bad := f
		mutate(&bad)
		if err := Encode(io.Discard, bad); err == nil {
			t.Fatal("accepted invalid frame")
		}
	}
	if err := Encode(shortWriter{}, f); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("short write: %v", err)
	}
}

type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) { return len(p) / 2, nil }

func TestPartsBoundedAssemblyDuplicatesAndExpiry(t *testing.T) {
	c, r, _ := fixture()
	data := bytes.Repeat([]byte{42}, 200000)
	parts, err := Split(c, r, data, MaxHeaderBytes+1024)
	if err != nil || len(parts) < 2 {
		t.Fatalf("split: %v", err)
	}
	a, err := NewAssembler(Limits{Blocks: 1, Bytes: uint64(len(data)), TTL: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1, 0)
	if _, done, err := a.Add(now, parts[0]); err != nil || done {
		t.Fatalf("first part: %v", err)
	}
	if _, done, err := a.Add(now, parts[0]); err != nil || done {
		t.Fatalf("duplicate: %v", err)
	}
	other := parts[0]
	other.Header.Ref.Hash[0]++
	if _, _, err := a.Add(now, other); !errors.Is(err, ErrLimit) {
		t.Fatalf("capacity: %v", err)
	}
	for i := len(parts) - 1; i > 0; i-- {
		got, done, err := a.Add(now, parts[i])
		if err != nil {
			t.Fatal(err)
		}
		if i == 1 && (!done || !bytes.Equal(data, got)) {
			t.Fatal("out of order assembly differs")
		}
		if i > 1 && done {
			t.Fatal("premature completion")
		}
	}
	if n, b := a.Usage(); n != 0 || b != 0 {
		t.Fatal("completion retained reservation")
	}
	if _, _, err := a.Add(now, parts[0]); err != nil {
		t.Fatal(err)
	}
	if a.Expire(now.Add(time.Second)) != 1 {
		t.Fatal("assembly deadline did not expire")
	}
	if n, b := a.Usage(); n != 0 || b != 0 {
		t.Fatal("expiry retained reservation")
	}
	if _, _, err := a.Add(now, parts[0]); err != nil {
		t.Fatal(err)
	}
	bad := parts[1]
	bad.Payload = bytes.Clone(bad.Payload)
	bad.Payload[0]++
	if _, _, err := a.Add(now, bad); err == nil {
		t.Fatal("accepted corrupt part")
	}
	a.Reset()
	if n, b := a.Usage(); n != 0 || b != 0 {
		t.Fatal("reset retained reservation")
	}
	for _, size := range []int{0, MaxHeaderBytes, MaxFrameBytes + 1} {
		if _, err := Split(c, r, data, size); err == nil {
			t.Fatalf("accepted frame cap %d", size)
		}
	}
}

func TestAssemblyRejectsConflictingMetadataAndWholeDigest(t *testing.T) {
	c, r, _ := fixture()
	data := bytes.Repeat([]byte{9}, 2000)
	p, err := Split(c, r, data, MaxHeaderBytes+1000)
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*Frame){
		func(f *Frame) { f.Header.Total++ }, func(f *Frame) { f.Header.Count++ },
		func(f *Frame) {
			f.Payload = bytes.Repeat([]byte{8}, len(f.Payload))
			f.Header.PartDigest = sha256.Sum256(f.Payload)
		},
	} {
		a, _ := NewAssembler(Limits{Blocks: 2, Bytes: 10000, TTL: time.Second})
		now := time.Unix(1, 0)
		if _, _, err := a.Add(now, p[0]); err != nil {
			t.Fatal(err)
		}
		bad := p[1]
		mutate(&bad)
		if _, _, err := a.Add(now, bad); err == nil {
			t.Fatal("accepted conflicting metadata or result digest")
		}
	}
}

func TestConnCancellationAndSubsequentFrame(t *testing.T) {
	left, right := net.Pipe()
	a, b := NewConn(left), NewConn(right)
	defer func() { _ = a.Close() }()
	defer func() { _ = b.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.Read(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled read: %v", err)
	}
	c, r, _ := fixture()
	want := Frame{Kind: Hello, Header: Header{Version: Version, Chain: c, Ref: r}}
	if err := a.Write(ctx, want); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled write: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- b.Write(context.Background(), want) }()
	got, err := a.Read(context.Background())
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("after cancellation: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	read := make(chan error, 1)
	go func() { _, err := a.Read(ctx); read <- err }()
	cancel()
	select {
	case err := <-read:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("blocked I/O ignored cancellation")
	}
}

func FuzzDecode(f *testing.F) {
	c, r, _ := fixture()
	var b bytes.Buffer
	_ = Encode(&b, Frame{Kind: Hello, Header: Header{Version: Version, Chain: c, Ref: r}})
	f.Add(b.Bytes())
	f.Add([]byte{0, 0, 0, 1, 1})
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, p []byte) {
		if len(p) > MaxFrameBytes+4 {
			t.Skip()
		}
		v, err := Decode(bytes.NewReader(p))
		if err == nil {
			var out bytes.Buffer
			if err := Encode(&out, v); err != nil {
				t.Fatal(err)
			}
		}
	})
}

func TestJSONInputResourceBounds(t *testing.T) {
	_, _, v := fixture()
	b, _ := MarshalResult(v)
	if _, err := UnmarshalResult(b); err != nil {
		t.Fatal(err)
	}
	if _, err := UnmarshalResult(bytes.Repeat([]byte("["), 40)); err == nil {
		t.Fatal("accepted nesting")
	}
}
