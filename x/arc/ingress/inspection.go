package arcingress

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/metadata"
	"github.com/0xmhha/indexer-go/execwire/retention"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/execution/ingress"
	"github.com/0xmhha/indexer-go/pkg/window"
	"github.com/0xmhha/indexer-go/x/arc/nativeinput"
	"github.com/0xmhha/indexer-go/x/arc/nativestore"
	"github.com/ethereum/go-ethereum/common"
)

type NativeObservation struct {
	Identity        nativeinput.Identity
	Head            nativeinput.Head
	ReceiptObserved bool
}
type RawObservation struct {
	StoreID   execwire.Hash
	Control   rawControl
	Temporary bool
}
type MaintenanceObservation struct {
	Phase                     string
	Point                     ingress.DrainPoint
	Identity                  nativeinput.Identity
	PolicyDigest, OwnerDigest execwire.Hash
	Permit                    *retention.Permit
	Receipt                   *retention.Receipt
	RawPath, SourcePath       string `json:"-"`
}

// InspectionPath mirrors the metadata reader's documented macOS aliases.
// It resolves no symlink or unselected path.
func InspectionPath(path string) (string, error) {
	p, err := filepath.Abs(path)
	if err != nil || len(p) > 4096 {
		return "", metadata.ErrUnsafe
	}
	if runtime.GOOS == "darwin" && (strings.HasPrefix(p, "/tmp/") || strings.HasPrefix(p, "/var/")) {
		p = "/private" + p
	}
	return p, nil
}
func sealedMetadata(r *metadata.Reader, path string, target any) error {
	b, err := r.File(path)
	if err != nil {
		return err
	}
	if len(b) > maxMeta {
		return metadata.ErrUnsafe
	}
	return metadata.Sealed(b, target)
}

// InspectNativeControl checks fixed control bytes, not SDK state/full epoch
// lineage. It invokes no run/writer lock, DB opener or native command.
func InspectNativeControl(ctx context.Context, path string) (out NativeObservation, err error) {
	path, err = InspectionPath(path)
	if err != nil {
		return out, err
	}
	r := metadata.New(ctx)
	if err = sealedMetadata(r, filepath.Join(path, identityName), &out.Identity); err != nil {
		return out, err
	}
	if err = nativeinput.ValidIdentity(out.Identity); err != nil {
		return out, err
	}
	if err = sealedMetadata(r, filepath.Join(path, headName), &out.Head); err != nil {
		return out, err
	}
	i, h := out.Identity, out.Head
	if h.Version != 1 || h.Instance != i.Instance || !positive(h.ImportEpoch) || h.ImportEpoch > 128 || !positive(h.ReadEpoch) || h.RequestID >= 1<<63-1 || h.Tip.Height < i.Anchor.Height || h.Tip.Height >= 1<<63-1 || h.Tip.Hash == (execwire.Hash{}) || h.Tip.Root == (execwire.Hash{}) {
		return out, ErrNative
	}
	if h.ImportEpoch == 1 && (h.RequestID != h.Tip.Height-i.Anchor.Height || h.ReadEpoch != h.RequestID+1) {
		return out, ErrNative
	}
	if h.Tip.Height == i.Anchor.Height && h.Tip != i.Anchor {
		return out, ErrNative
	}
	b, e := r.File(filepath.Join(path, nativeinput.BootstrapName))
	if e != nil {
		return out, e
	}
	if len(b) > maxMeta || execwire.Hash(sha256.Sum256(b)) != i.BootstrapDigest {
		return out, ErrNative
	}
	if h.Tip.Height > i.Anchor.Height && h.RequestID > 0 {
		var receipt nativeinput.Receipt
		name := strconv.FormatUint(h.ImportEpoch, 10) + "-" + strconv.FormatUint(h.Tip.Height, 10) + ".json"
		if err = sealedMetadata(r, filepath.Join(path, ".arc-native-import-receipts", name), &receipt); err != nil {
			return out, err
		}
		if err = nativeinput.ValidateProof(nativeinput.Proof{Version: 1, Identity: i, Head: h, Ref: h.Tip, Receipt: &receipt}, i); err != nil {
			return out, err
		}
		out.ReceiptObserved = true
	}
	return out, r.Verify()
}

// InspectRawControl reads STORE/OWNER only, never payload/indices/volume quotas/
// GC intents. No Store, lock or recovery path is opened.
func InspectRawControl(ctx context.Context, path string) (out RawObservation, err error) {
	path, err = InspectionPath(path)
	if err != nil {
		return out, err
	}
	r := metadata.New(ctx)
	var store struct {
		Version int
		ID      common.Hash
		Chain   model.FeedChain
		Next    uint64
		Kind    string `json:",omitempty"`
	}
	b, err := r.File(filepath.Join(path, "STORE.json"))
	if err != nil {
		return out, err
	}
	if len(b) > maxMeta || strictJSON(b, &store) != nil || store.Version != 1 || store.ID == (common.Hash{}) || store.Next == 0 || store.Kind != "owned-raw-v1" {
		return out, ErrNative
	}
	var owner window.Owner
	if err = sealedMetadata(r, filepath.Join(path, "OWNER.json"), &owner); err != nil {
		return out, err
	}
	if owner.Version != 1 || owner.StoreID != store.ID || owner.Domain != ownerDomain || len(owner.State) > 4096 || strictJSON(owner.State, &out.Control) != nil {
		return out, ErrNative
	}
	c := out.Control
	if c.Version != 1 && c.Version != 2 || c.Chain != store.Chain || c.Instance == (execwire.Hash{}) || !positive(c.RawEpoch) || c.Received.Height < c.Anchor.Height || c.Received.Hash == (execwire.Hash{}) || c.Received.Root == (execwire.Hash{}) || c.Anchor.Hash == (execwire.Hash{}) || c.Anchor.Root == (execwire.Hash{}) || c.Received.Height == c.Anchor.Height && c.Received != c.Anchor {
		return out, ErrNative
	}
	if c.Version == 1 && (c.RawEpoch != 1 || c.NativeEpoch != 0 || c.Generation != 0) || c.Version == 2 && (!positive(c.NativeEpoch) || !positive(c.Generation)) {
		return out, ErrNative
	}
	if c.Anchor.Height >= 1<<63-1 || c.Received.Height >= 1<<63-1 {
		return out, ErrNative
	}
	out.StoreID = execwire.Hash(store.ID)
	// Presence is HOLD evidence; absence does not establish global GC/owner state.
	temp := filepath.Join(path, "OWNER.json.tmp")
	_, e := os.Lstat(temp)
	if e == nil {
		if nativestore.PrivateFile(temp) != nil {
			return out, metadata.ErrUnsafe
		}
		out.Temporary = true
	} else if !errors.Is(e, os.ErrNotExist) {
		return out, e
	}
	return out, r.Verify()
}

// InspectMaintenance reads only the fixed sibling journal, without .lock.
// Paths inside it are compared as text and never followed.
func InspectMaintenance(ctx context.Context, path string) (out MaintenanceObservation, err error) {
	path, err = InspectionPath(path)
	if err != nil {
		return out, err
	}
	r := metadata.New(ctx)
	var s maintenanceRecord
	if err = sealedMetadata(r, maintenancePath(path), &s); err != nil {
		return out, err
	}
	if err = s.validate(); err != nil {
		return out, err
	}
	if s.Scope.Native != path {
		return out, ErrNative
	}
	out = MaintenanceObservation{Phase: s.Phase, Point: s.Point, Identity: s.Proof.Identity, PolicyDigest: s.PolicyDigest, OwnerDigest: s.OwnerDigest, Permit: s.Permit, Receipt: s.Receipt, RawPath: s.Scope.Raw, SourcePath: s.Scope.Source}
	return out, r.Verify()
}
