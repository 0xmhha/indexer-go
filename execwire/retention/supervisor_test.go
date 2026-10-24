package retention

import (
	"context"
	"encoding/json"
	"github.com/0xmhha/indexer-go/execwire"
	"os"
	"testing"
	"time"
)

func TestSupervisorCancelsAndJoinsOnParentExit(t *testing.T) {
	p := Permit{Version: 1, Coverage: "full", BaselineDigest: execwire.Hash{8}, Token: execwire.Hash{7}, Request: validRequest(), Visible: validRequest().ACK, Root: execwire.Hash{9}}
	r, w, e := os.Pipe()
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = w.Close() }()
	d, e := Digest(p)
	if e != nil {
		t.Fatal(e)
	}
	if e = json.NewEncoder(w).Encode(Supervision{Version: 1, PermitDigest: d, Challenge: execwire.Hash{4}}); e != nil {
		t.Fatal(e)
	}
	ctx, hello, stop, e := Supervise(t.Context(), r, p)
	if e != nil {
		t.Fatal("live DB supervisor must open an exact lease:", e)
	}
	defer stop()
	if hello.PermitDigest != d {
		t.Fatal("wrong lease")
	}
	if e = w.Close(); e != nil {
		t.Fatal(e)
	}
	select {
	case <-ctx.Done():
	case <-t.Context().Done():
		t.Fatal("producer ignored parent exit")
	}
	stop()
}

func TestSupervisorCanceledBeforeHelloAndWrongPermit(t *testing.T) {
	p := Permit{Version: 1, Coverage: "full", BaselineDigest: execwire.Hash{8}, Token: execwire.Hash{7}, Request: validRequest(), Visible: validRequest().ACK, Root: execwire.Hash{9}}
	r, w, e := os.Pipe()
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = w.Close() }()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, _, stop, e := Supervise(ctx, r, p)
	if stop != nil {
		stop()
	}
	if e != context.Canceled {
		t.Fatal("initialization did not cancel", e)
	}
}

func TestSupervisorStopJoinsPipeWhileParentAlive(t *testing.T) {
	p := Permit{Version: 1, Coverage: "full", BaselineDigest: execwire.Hash{8}, Token: execwire.Hash{7}, Request: validRequest(), Visible: validRequest().ACK, Root: execwire.Hash{9}}
	r, w, e := os.Pipe()
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = w.Close() }()
	d, e := Digest(p)
	if e != nil {
		t.Fatal(e)
	}
	if e = json.NewEncoder(w).Encode(Supervision{Version: 1, PermitDigest: d, Challenge: execwire.Hash{4}}); e != nil {
		t.Fatal(e)
	}
	_, _, stop, e := Supervise(t.Context(), r, p)
	if e != nil {
		t.Fatal(e)
	}
	done := make(chan struct{})
	go func() { stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("owned supervisor reader did not join while parent kept pipe open")
	}
}
