// Package retention defines the offline maintenance protocol shared by the
// indexer and its separately versioned execution producer. It grants no access
// by itself: only the durable database fence authorizes a matching permit.
package retention

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"math"

	"github.com/0xmhha/indexer-go/execwire"
)

const (
	Version     = 1
	DefaultKeep = 86400
	ReorgWindow = 128
	MaxArtifact = 2 << 20
)

var ErrUnsafe = errors.New("retention: invalid or unsafe maintenance evidence")

// Request is an exact offline observation. Through is inclusive; the original
// anchor and every height above Through remain protected. Hashes of source
// metadata prevent applying a plan after ACK, append or generation changes.
type Request struct {
	Version                 uint16
	Chain                   execwire.Chain
	Anchor, ACK, Tail, Head execwire.Ref
	HeadRoot, SourceDigest  execwire.Hash
	KeepBlocks, Through     uint64
}

type Permit struct {
	Coverage       string
	BaselineDigest execwire.Hash
	Version        uint16
	Token          execwire.Hash
	Request        Request
	Visible        execwire.Ref
	Root           execwire.Hash
}

// Receipt is emitted only after durable file completion and postchecks.
// Its digest links the same-permit DB release, never absence alone.
type Receipt struct {
	Version      uint16
	PermitDigest execwire.Hash
	Node, Spool  bool
	Files, Bytes uint64
}

// Release is emitted only after the DB completion commit. Producer fences stay
// closed until an explicit same-permit release, including across process death.
type Release struct {
	Version                     uint16
	PermitDigest, ReceiptDigest execwire.Hash
}

func (r Release) Match(p Permit, receipt Receipt) error {
	if err := receipt.Match(p); err != nil {
		return err
	}
	pd, e := Digest(p)
	if e != nil {
		return e
	}
	rd, e := Digest(receipt)
	if e != nil {
		return e
	}
	if r.Version != Version || r.PermitDigest != pd || r.ReceiptDigest != rd {
		return ErrUnsafe
	}
	return nil
}

func Digest(v any) (execwire.Hash, error) {
	p, err := json.Marshal(v)
	if err != nil {
		return execwire.Hash{}, err
	}
	if len(p) > MaxArtifact {
		return execwire.Hash{}, ErrUnsafe
	}
	return execwire.Hash(sha256.Sum256(p)), nil
}

func (r Request) Validate() error {
	if r.Version != Version || r.KeepBlocks <= ReorgWindow || r.KeepBlocks > math.MaxInt64 || r.ACK.Height > math.MaxInt64 || r.Tail.Height > math.MaxInt64 || r.Head.Height > math.MaxInt64 || r.Anchor.Generation > r.ACK.Generation || r.ACK.Generation != r.Tail.Generation || r.ACK.Height < r.Anchor.Height || r.Tail.Height < r.ACK.Height || r.Head.Height < r.Tail.Height || r.Head.Generation != 0 || r.Head.Hash == (execwire.Hash{}) || r.HeadRoot == (execwire.Hash{}) || r.SourceDigest == (execwire.Hash{}) {
		return ErrUnsafe
	}
	if r.ACK.Height <= r.KeepBlocks || r.Through != r.ACK.Height-r.KeepBlocks || r.Through <= r.Anchor.Height {
		return ErrUnsafe
	}
	for _, ref := range []execwire.Ref{r.Anchor, r.ACK, r.Tail} {
		if err := execwire.Encode(io.Discard, execwire.Frame{Kind: execwire.Hello, Header: execwire.Header{Version: execwire.Version, Chain: r.Chain, Ref: ref}}); err != nil {
			return ErrUnsafe
		}
	}
	if r.ACK.Height == r.Tail.Height && r.ACK != r.Tail || r.Tail.Height == r.Head.Height && r.Tail.Hash != r.Head.Hash || r.Anchor.Height == 0 && r.Anchor.Hash != r.Chain.Genesis {
		return ErrUnsafe
	}
	return nil
}

func (p Permit) Validate() error {
	if err := p.Request.Validate(); err != nil {
		return err
	}
	r := p.Request
	if p.Coverage != "full" || p.BaselineDigest == (execwire.Hash{}) || p.Version != Version || p.Token == (execwire.Hash{}) || p.Root == (execwire.Hash{}) || p.Visible.Generation != r.ACK.Generation || p.Visible.Height < r.ACK.Height || p.Visible.Height > math.MaxInt64 || p.Visible.Hash == (execwire.Hash{}) {
		return ErrUnsafe
	}
	if p.Visible.Height == r.ACK.Height && p.Visible.Hash != r.ACK.Hash {
		return ErrUnsafe
	}
	return nil
}

func (r Receipt) Match(p Permit) error {
	if err := p.Validate(); err != nil {
		return err
	}
	d, err := Digest(p)
	if err != nil {
		return err
	}
	if r.Version != Version || r.PermitDigest != d || !r.Node || !r.Spool {
		return ErrUnsafe
	}
	return nil
}
