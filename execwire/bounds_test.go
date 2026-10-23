package execwire

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

func TestResultPreservesRawSegments(t *testing.T) {
	_, _, v := fixture()
	v.BlockRLP = []byte{0xff, 0, 0xfe, 0x81, 0x82, 0x80}
	v.ReceiptsRLP = []byte{0xf0, 0, 0xa0, 0x86, 0x98}
	p, err := MarshalResult(v)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(p, v.BlockRLP) || !bytes.HasSuffix(p, v.ReceiptsRLP) {
		t.Fatal("source RLP was not preserved as raw segments")
	}
	got, err := UnmarshalResult(p)
	if err != nil || !bytes.Equal(got.BlockRLP, v.BlockRLP) || !bytes.Equal(got.ReceiptsRLP, v.ReceiptsRLP) {
		t.Fatalf("raw round trip: %v", err)
	}
}

func TestResultRejectsMalformedSegmentLengthsAndJSON(t *testing.T) {
	_, _, v := fixture()
	p, err := MarshalResult(v)
	if err != nil {
		t.Fatal(err)
	}
	for n := 0; n < len(p); n++ {
		if _, err := UnmarshalResult(p[:n]); err == nil {
			t.Fatalf("accepted result truncation %d", n)
		}
	}
	for _, offset := range []int{4, 8, 12} {
		bad := bytes.Clone(p)
		binary.BigEndian.PutUint32(bad[offset:offset+4], ^uint32(0))
		if _, err := UnmarshalResult(bad); err == nil {
			t.Fatal("accepted overflow length")
		}
	}
	for _, meta := range [][]byte{
		[]byte(`{"Time":1,"Time":2}`),
		append(bytes.Repeat([]byte("["), 17), bytes.Repeat([]byte("]"), 17)...),
		append(append([]byte("["), bytes.Repeat([]byte("0,"), 262144)...), []byte("0]")...),
	} {
		bad := make([]byte, 16)
		copy(bad, "EXR1")
		binary.BigEndian.PutUint32(bad[4:8], uint32(len(meta)))
		binary.BigEndian.PutUint32(bad[8:12], 1)
		binary.BigEndian.PutUint32(bad[12:16], 1)
		bad = append(bad, meta...)
		bad = append(bad, 0xc0, 0xc0)
		if _, err := UnmarshalResult(bad); err == nil {
			t.Fatal("accepted ambiguous or excessive JSON")
		}
	}
}

func FuzzResult(f *testing.F) {
	_, _, v := fixture()
	p, _ := MarshalResult(v)
	f.Add(p)
	f.Add([]byte("EXR1"))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, p []byte) {
		if len(p) > MaxBlockBytes {
			t.Skip()
		}
		r, err := UnmarshalResult(p)
		if err == nil {
			got, err := MarshalResult(r)
			if err != nil || !bytes.Equal(got, p) {
				t.Fatal("accepted noncanonical result")
			}
		}
	})
}

func TestAssemblerRejectsOversizeFrame(t *testing.T) {
	c, r, _ := fixture()
	data := bytes.Repeat([]byte{1}, MaxFrameBytes)
	f := Frame{Kind: ResultPart, Header: Header{Version: Version, Chain: c, Ref: r, Total: uint64(len(data)), Count: 1, Digest: Hash(sha256.Sum256(data)), PartDigest: Hash(sha256.Sum256(data))}, Payload: data}
	a, _ := NewAssembler(Limits{Blocks: 1, Bytes: MaxBlockBytes, TTL: time.Second})
	if _, _, err := a.Add(time.Unix(1, 0), f); !errors.Is(err, ErrLimit) {
		t.Fatalf("oversize frame: %v", err)
	}
}

func TestConnPartialReadCancellationPoisonsStream(t *testing.T) {
	left, right := net.Pipe()
	c := NewConn(left)
	defer func() { _ = c.Close() }()
	defer func() { _ = right.Close() }()
	chain, ref, _ := fixture()
	var raw bytes.Buffer
	if err := Encode(&raw, Frame{Kind: Hello, Header: Header{Version: Version, Chain: chain, Ref: ref}}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	read := make(chan error, 1)
	go func() { _, err := c.Read(ctx); read <- err }()
	written := make(chan error, 1)
	go func() { _, err := right.Write(raw.Bytes()[:6]); written <- err }()
	select {
	case err := <-written:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("partial read did not begin")
	}
	cancel()
	select {
	case err := <-read:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("partial read did not cancel")
	}
	if _, err := c.Read(context.Background()); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("partial stream reused: %v", err)
	}
}

type enteredConn struct {
	net.Conn
	entered chan struct{}
	once    sync.Once
}

type enteredWriter struct {
	net.Conn
	entered chan struct{}
	once    sync.Once
}

func (c *enteredWriter) Write(p []byte) (int, error) {
	c.once.Do(func() { close(c.entered) })
	return c.Conn.Write(p)
}

func TestConnBlockedWriteCancellationAndReuse(t *testing.T) {
	left, right := net.Pipe()
	entered := &enteredWriter{Conn: left, entered: make(chan struct{})}
	a, b := NewConn(entered), NewConn(right)
	defer func() { _ = a.Close(); _ = b.Close() }()
	chain, ref, _ := fixture()
	frame := Frame{Kind: Hello, Header: Header{Version: Version, Chain: chain, Ref: ref}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- a.Write(ctx, frame) }()
	select {
	case <-entered.entered:
	case <-time.After(time.Second):
		t.Fatal("write did not begin")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("write did not cancel")
	}
	go func() { done <- a.Write(context.Background(), frame) }()
	if _, err := b.Read(context.Background()); err != nil {
		t.Fatalf("idle write cancellation poisoned stream: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestConnPartialWriteCancellationPoisonsStream(t *testing.T) {
	left, right := net.Pipe()
	a := NewConn(left)
	defer func() { _ = a.Close(); _ = right.Close() }()
	chain, ref, _ := fixture()
	frame := Frame{Kind: Hello, Header: Header{Version: Version, Chain: chain, Ref: ref}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- a.Write(ctx, frame) }()
	var prefix [6]byte
	if _, err := io.ReadFull(right, prefix[:]); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("partial write did not cancel")
	}
	if err := a.Write(context.Background(), frame); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("partial write stream reused: %v", err)
	}
}

func (c *enteredConn) Read(p []byte) (int, error) {
	c.once.Do(func() { close(c.entered) })
	return c.Conn.Read(p)
}

func TestConnWaiterCancellationAndDeadlineReset(t *testing.T) {
	left, right := net.Pipe()
	entered := &enteredConn{Conn: left, entered: make(chan struct{})}
	a, b := NewConn(entered), NewConn(right)
	defer func() { _ = a.Close() }()
	defer func() { _ = b.Close() }()
	first, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := a.Read(first); done <- err }()
	select {
	case <-entered.entered:
	case <-time.After(time.Second):
		t.Fatal("reader did not acquire gate")
	}
	queued, stop := context.WithCancel(context.Background())
	queuedDone := make(chan error, 1)
	go func() { _, err := a.Read(queued); queuedDone <- err }()
	stop()
	select {
	case err := <-queuedDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("waiter did not cancel")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	deadline, end := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer end()
	if _, err := a.Read(deadline); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline error: %v", err)
	}
	chain, ref, _ := fixture()
	want := Frame{Kind: Hello, Header: Header{Version: Version, Chain: chain, Ref: ref}}
	go func() { done <- b.Write(context.Background(), want) }()
	if _, err := a.Read(context.Background()); err != nil {
		t.Fatalf("deadline was not reset: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
