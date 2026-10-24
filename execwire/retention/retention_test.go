package retention

import (
	"testing"

	"github.com/0xmhha/indexer-go/execwire"
)

func validRequest() Request {
	return Request{Version: Version, Chain: execwire.Chain{ID: "8283", Genesis: execwire.Hash{1}}, Anchor: execwire.Ref{Generation: 1, Hash: execwire.Hash{1}}, ACK: execwire.Ref{Generation: 2, Height: 90000, Hash: execwire.Hash{2}}, Tail: execwire.Ref{Generation: 2, Height: 90002, Hash: execwire.Hash{3}}, Head: execwire.Ref{Height: 90003, Hash: execwire.Hash{4}}, HeadRoot: execwire.Hash{5}, SourceDigest: execwire.Hash{6}, KeepBlocks: DefaultKeep, Through: 3600}
}

func TestRetentionWindowProtectsACKBacklogReorgAndAnchor(t *testing.T) {
	r := validRequest()
	if err := r.Validate(); err != nil {
		t.Fatal("valid acknowledged window must be collectable:", err)
	}
	for _, mutate := range []func(*Request){
		func(r *Request) { r.Through++ }, func(r *Request) { r.KeepBlocks = 128 },
		func(r *Request) { r.ACK.Generation++ }, func(r *Request) { r.ACK.Height = r.Tail.Height + 1 },
		func(r *Request) { r.Head.Height = r.Tail.Height - 1 }, func(r *Request) { r.Anchor.Height = r.Through },
		func(r *Request) { r.SourceDigest = execwire.Hash{} }, func(r *Request) { r.Chain.ID = "0" },
		func(r *Request) { r.Version++ }, func(r *Request) { r.ACK.Height = 128; r.Through = 0 },
	} {
		bad := r
		mutate(&bad)
		if bad.Validate() == nil {
			t.Fatalf("unsafe request accepted: %+v", bad)
		}
	}
	// A larger window can only move the cutoff backward, never evict backlog.
	r.KeepBlocks++
	r.Through--
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestRetentionReceiptBindsEveryPermitField(t *testing.T) {
	p := Permit{Coverage: "full", BaselineDigest: execwire.Hash{102}, Version: Version, Token: execwire.Hash{7}, Request: validRequest(), Visible: execwire.Ref{Generation: 2, Height: 90001, Hash: execwire.Hash{8}}, Root: execwire.Hash{9}}
	d, err := Digest(p)
	if err != nil {
		t.Fatal(err)
	}
	r := Receipt{Version: Version, PermitDigest: d, Node: true, Spool: true}
	if err = r.Match(p); err != nil {
		t.Fatal(err)
	}
	p.Token[0]++
	if r.Match(p) == nil {
		t.Fatal("receipt replayed for different permit")
	}
	p.Token[0]--
	r.Node = false
	if r.Match(p) == nil {
		t.Fatal("partial deletion released fence")
	}
}
