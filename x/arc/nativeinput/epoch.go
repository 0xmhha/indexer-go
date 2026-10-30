package nativeinput

import (
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"math"
	"path/filepath"
	"strconv"
)

type TransitionRequest struct {
	Version  uint8
	Identity Identity
	Previous Head
	Marker   port.ExecutionRewind
}
type Transition struct {
	Version  uint8
	Identity Identity
	Previous Head
	Marker   port.ExecutionRewind
	Head     Head
}

// ReceiptContext resolves the immutable epoch base for retained canonical input.
func signedPositive(n uint64) bool { return n > 0 && n < math.MaxInt64 }
func validHead(h Head, i Identity) bool {
	return h.Version == 1 && h.Instance == i.Instance && signedPositive(h.ImportEpoch) && signedPositive(h.ReadEpoch) && h.RequestID < math.MaxInt64 && h.Tip.Height >= i.Anchor.Height && h.Tip.Height < math.MaxInt64 && nonzero(h.Tip.Hash) && nonzero(h.Tip.Root)
}
func ExpectedTransition(r TransitionRequest) (Transition, error) {
	m := r.Marker
	if r.Version != 1 || ValidIdentity(r.Identity) != nil || !validHead(r.Previous, r.Identity) || r.Previous.ImportEpoch >= 128 || !signedPositive(m.RequestGeneration) || !signedPositive(m.From.Generation) || m.From.Generation > m.RequestGeneration || m.Target.Generation != m.RequestGeneration+1 || !signedPositive(m.Target.Generation) || m.From.Height >= math.MaxInt64 || m.Target.Height < r.Identity.Anchor.Height || m.Target.Height > m.From.Height || m.Target.Height > r.Previous.Tip.Height || !nonzero(m.From.Hash) || !nonzero(m.FromRoot) || !nonzero(m.Target.Hash) || !nonzero(m.Root) || r.Previous.ReadEpoch >= math.MaxInt64-1 {
		return Transition{}, ErrProof
	}
	return Transition{Version: 1, Identity: r.Identity, Previous: r.Previous, Marker: m, Head: Head{Version: 1, Instance: r.Identity.Instance, ImportEpoch: r.Previous.ImportEpoch + 1, ReadEpoch: r.Previous.ReadEpoch + 1, Tip: Ref{Height: m.Target.Height, Hash: m.Target.Hash, Root: m.Root}}}, nil
}
func epochPath(dir string, epoch uint64) string {
	return filepath.Join(dir, ".arc-native-import-epochs", strconv.FormatUint(epoch, 10)+".json")
}
func ValidateTransition(t Transition, expected Identity) error {
	want, err := ExpectedTransition(TransitionRequest{Version: 1, Identity: expected, Previous: t.Previous, Marker: t.Marker})
	if err != nil || t != want {
		return ErrProof
	}
	return nil
}
func sequence(h, base Head, i Identity) bool {
	return validHead(h, i) && h.ImportEpoch == base.ImportEpoch && h.Tip.Height >= base.Tip.Height && h.RequestID == h.Tip.Height-base.Tip.Height && h.RequestID < math.MaxInt64-base.ReadEpoch && base.ReadEpoch+h.RequestID == h.ReadEpoch && (h.RequestID != 0 || h.Tip == base.Tip)
}
func ReceiptContext(dir string, i Identity, h Head, height uint64) (Head, error) {
	if !validHead(h, i) || h.ImportEpoch > 128 || height > h.Tip.Height {
		return Head{}, ErrProof
	}
	cursor := h
	var selected *Head
	var newerRequest uint64
	for {
		if cursor.ImportEpoch == 1 {
			base := Head{Version: 1, Instance: i.Instance, ImportEpoch: 1, ReadEpoch: 1, Tip: i.Anchor}
			if !sequence(cursor, base, i) {
				return Head{}, ErrProof
			}
			if selected != nil {
				return *selected, nil
			}
			return base, nil
		}
		if err := privateArtifactDirectory(filepath.Join(dir, ".arc-native-import-epochs")); err != nil {
			return Head{}, err
		}
		var t Transition
		if ReadArtifact(epochPath(dir, cursor.ImportEpoch), &t) != nil || ValidateTransition(t, i) != nil || !sequence(cursor, t.Head, i) {
			return Head{}, ErrProof
		}
		if newerRequest != 0 && t.Marker.Target.Generation > newerRequest {
			return Head{}, ErrProof
		}
		newerRequest = t.Marker.RequestGeneration
		if selected == nil && height > t.Head.Tip.Height {
			base := t.Head
			selected = &base
		}
		cursor = t.Previous
	}
}
