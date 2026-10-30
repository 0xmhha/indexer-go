package nativeinput

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"github.com/0xmhha/indexer-go/execwire"
	"math"
)

var ErrProof = errors.New("arc native input: inconsistent or unavailable managed read proof")

func nonzero(h execwire.Hash) bool { return h != (execwire.Hash{}) }
func ValidIdentity(i Identity) error {
	if i.Version != 1 || !nonzero(i.Instance) || i.ChainID != 5042002 || !nonzero(i.Genesis) || i.ArcRevision != ArcRevision || i.RethRevision != RethRevision || i.Layout != "reth-native-v2" || !nonzero(i.BootstrapDigest) || i.Anchor.Height > math.MaxInt64 || !nonzero(i.Anchor.Hash) || !nonzero(i.Anchor.Root) || i.Anchor.Height == 0 && i.Anchor.Hash != i.Genesis {
		return ErrProof
	}
	return nil
}

// ValidateProof checks bounded structure and exact immutable bindings.
// Callers also verify current sealed controls, canonical native state and the
// operation payload and immutable epoch lineage under the same read view.
// VerifyCurrent supplies that full disk binding; this is never an E1 ACK.
func ValidateProof(p Proof, expected Identity) error {
	i, h := p.Identity, p.Head
	if ValidIdentity(i) != nil || i != expected || p.Version != 1 || !validHead(h, i) || h.ImportEpoch > 128 || p.Ref.Height > h.Tip.Height || !nonzero(p.Ref.Hash) || !nonzero(p.Ref.Root) {
		return ErrProof
	}
	if h.ImportEpoch == 1 && (h.RequestID != h.Tip.Height-i.Anchor.Height || h.ReadEpoch != h.RequestID+1) {
		return ErrProof
	}
	if h.Tip.Height == i.Anchor.Height && h.Tip != i.Anchor {
		return ErrProof
	}
	if p.Ref.Height == h.Tip.Height && p.Ref != h.Tip {
		return ErrProof
	}
	if p.Ref.Height <= i.Anchor.Height {
		if p.Receipt != nil || p.Ref.Height == i.Anchor.Height && p.Ref != i.Anchor || p.Ref.Height == 0 && p.Ref.Hash != i.Genesis {
			return ErrProof
		}
		return nil
	}
	r := p.Receipt
	if r == nil || r.Version != 1 || r.Identity != i || !signedPositive(r.ImportEpoch) || r.ImportEpoch > h.ImportEpoch || r.Height != p.Ref.Height || r.Hash != p.Ref.Hash || r.Root != p.Ref.Root || !signedPositive(r.RequestID) || !signedPositive(r.ReadEpoch) || r.ReadEpoch > h.ReadEpoch || !nonzero(r.Parent) || !nonzero(r.ParentRoot) || !nonzero(r.ReceiptsRoot) || !nonzero(r.InputDigest) {
		return ErrProof
	}
	if r.ImportEpoch == 1 && (r.RequestID != r.Height-i.Anchor.Height || r.ReadEpoch != r.RequestID+1) {
		return ErrProof
	}
	if r.Height == i.Anchor.Height+1 && (r.Parent != i.Anchor.Hash || r.ParentRoot != i.Anchor.Root) {
		return ErrProof
	}
	return nil
}
func Decode(p []byte, out any) error {
	if len(p) == 0 || len(p) > MaxMeta {
		return ErrProof
	}
	d := json.NewDecoder(bytes.NewReader(p))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil {
		return ErrProof
	}
	canonical, err := json.Marshal(out)
	if err != nil || !bytes.Equal(canonical, p) {
		return ErrProof
	}
	return nil
}
func Digest(m Meta, block, receipts []byte) (execwire.Hash, error) {
	if m.Version != 1 || m.ChainID != 5042002 || !nonzero(m.Genesis) || m.ArcRevision != ArcRevision || m.RethRevision != RethRevision || m.RequestID == 0 || m.RequestID > math.MaxInt64 || m.ImportEpoch == 0 || m.ImportEpoch > math.MaxInt64 || m.Height == 0 || m.Height > math.MaxInt64 || !nonzero(m.Hash) || !nonzero(m.Parent) || !nonzero(m.ParentRoot) || len(block) == 0 || len(block) > 10<<20 || len(receipts) == 0 || len(receipts) > 10<<20 {
		return execwire.Hash{}, ErrProof
	}
	meta, err := json.Marshal(m)
	if err != nil || len(meta) > MaxMeta {
		return execwire.Hash{}, ErrProof
	}
	var header [16]byte
	copy(header[:], "AIN1")
	for i, n := range []int{len(meta), len(block), len(receipts)} {
		binary.BigEndian.PutUint32(header[4+i*4:], uint32(n))
	}
	h := sha256.New()
	for _, p := range [][]byte{header[:], meta, block, receipts} {
		_, _ = h.Write(p)
	}
	var digest execwire.Hash
	copy(digest[:], h.Sum(nil))
	return digest, nil
}
func ReceiptMeta(r Receipt) Meta {
	return Meta{Version: 1, RequestID: r.RequestID, ChainID: r.Identity.ChainID, Genesis: r.Identity.Genesis, ArcRevision: r.Identity.ArcRevision, RethRevision: r.Identity.RethRevision, ImportEpoch: r.ImportEpoch, Height: r.Height, Hash: r.Hash, Parent: r.Parent, ParentRoot: r.ParentRoot}
}

// ValidatePayload binds exact immutable inputs; it never assigns E1 generation.
func ValidatePayload(p Proof, r execwire.Result) error {
	if ValidateProof(p, p.Identity) != nil || p.Receipt == nil || r.Ref.Height != p.Ref.Height || r.Ref.Hash != p.Ref.Hash || r.StateRoot != p.Ref.Root || r.ReceiptsRoot != p.Receipt.ReceiptsRoot || r.Parent != p.Receipt.Parent {
		return ErrProof
	}
	digest, err := Digest(ReceiptMeta(*p.Receipt), r.BlockRLP, r.ReceiptsRLP)
	if err != nil || digest != p.Receipt.InputDigest {
		return ErrProof
	}
	return nil
}
