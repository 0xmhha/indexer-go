package nativeinput

import (
	"crypto/sha256"
	"github.com/0xmhha/indexer-go/execwire"
	"math"
	"path/filepath"
	"strconv"
)

const IdentityName = ".arc-native-import.identity.json"
const HeadName = ".arc-native-import.head.json"
const BootstrapName = ".arc-native-bootstrap.json"

// VerifyCurrent is called only while the inherited managed read view is held.
func VerifyCurrent(dir string, p Proof, expected Identity) error {
	if err := ValidateProof(p, expected); err != nil {
		return err
	}
	var i Identity
	var h Head
	if ReadArtifact(filepath.Join(dir, IdentityName), &i) != nil || i != expected || ReadArtifact(filepath.Join(dir, HeadName), &h) != nil || h != p.Head {
		return ErrProof
	}
	boot, err := PrivateBytes(filepath.Join(dir, BootstrapName))
	if err != nil {
		return err
	}
	if execwire.Hash(sha256.Sum256(boot)) != i.BootstrapDigest {
		return ErrProof
	}
	latestBase, err := ReceiptContext(dir, i, h, h.Tip.Height)
	if err != nil {
		return err
	}
	if err = privateArtifactDirectory(filepath.Join(dir, ".arc-native-import-receipts")); err != nil {
		return err
	}
	read := func(epoch, height uint64) (Receipt, error) {
		var r Receipt
		err := ReadArtifact(filepath.Join(dir, ".arc-native-import-receipts", strconv.FormatUint(epoch, 10)+"-"+strconv.FormatUint(height, 10)+".json"), &r)
		return r, err
	}
	if p.Receipt != nil {
		base, err := ReceiptContext(dir, i, h, p.Ref.Height)
		if err != nil {
			return err
		}
		if !receiptSequence(*p.Receipt, base) {
			return ErrProof
		}
		r, err := read(p.Receipt.ImportEpoch, p.Ref.Height)
		if err != nil {
			return err
		}
		if r != *p.Receipt {
			return ErrProof
		}
	}
	if h.Tip.Height > i.Anchor.Height {
		r, err := read(latestBase.ImportEpoch, h.Tip.Height)
		if err != nil {
			return err
		}
		if err = ValidateProof(Proof{Version: 1, Identity: i, Head: h, Ref: h.Tip, Receipt: &r}, expected); err != nil {
			return err
		}
		if !receiptSequence(r, latestBase) {
			return ErrProof
		}
	}
	return nil
}
func receiptSequence(r Receipt, base Head) bool {
	return r.ImportEpoch == base.ImportEpoch && r.Height > base.Tip.Height && r.RequestID == r.Height-base.Tip.Height && r.RequestID < math.MaxInt64-base.ReadEpoch && r.ReadEpoch == base.ReadEpoch+r.RequestID && (r.RequestID != 1 || r.Parent == base.Tip.Hash && r.ParentRoot == base.Tip.Root)
}

// ReadHeadProof reads control evidence only; the native child proves SDK state.
// The caller holds the managed read view throughout this function.
func ReadHeadProof(dir string, expected Identity) (Proof, error) {
	return readReferenceProof(dir, expected, nil)
}

// ReadReferenceProof binds a published prefix to its original immutable receipt.
// The caller holds the managed read view; SDK state is proved by the native child.
func ReadReferenceProof(dir string, expected Identity, ref Ref) (Proof, error) {
	return readReferenceProof(dir, expected, &ref)
}

func readReferenceProof(dir string, expected Identity, ref *Ref) (Proof, error) {
	p := Proof{Version: 1, Identity: expected}
	if err := ReadArtifact(filepath.Join(dir, HeadName), &p.Head); err != nil {
		return p, err
	}
	p.Ref = p.Head.Tip
	if ref != nil {
		p.Ref = *ref
	}
	if p.Ref.Height > expected.Anchor.Height {
		base, err := ReceiptContext(dir, expected, p.Head, p.Ref.Height)
		if err != nil {
			return p, err
		}
		var r Receipt
		if err = ReadArtifact(filepath.Join(dir, ".arc-native-import-receipts", strconv.FormatUint(base.ImportEpoch, 10)+"-"+strconv.FormatUint(p.Ref.Height, 10)+".json"), &r); err != nil {
			return p, err
		}
		p.Receipt = &r
	}
	return p, VerifyCurrent(dir, p, expected)
}
