package arcingress

import (
	"crypto/sha256"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/checkpoint"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/x/arc/nativestore"
)

const identityName = ".arc-native-import.identity.json"
const headName = ".arc-native-import.head.json"

type sealedArtifact struct {
	Data   json.RawMessage
	Digest execwire.Hash
}
type bootstrapReceipt struct {
	Version                                    uint8
	ArcRevision, RethRevision                  string
	Anchor                                     checkpoint.Manifest
	Floor, Next                                uint64
	StateDigest                                execwire.Hash
	HeadersDigest, BlockDigest, ReceiptsDigest *execwire.Hash
}

func privateBytes(path string) ([]byte, error) {
	if err := nativestore.PrivateFile(path); err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	actual, err := os.Lstat(path)
	if err != nil || !os.SameFile(info, actual) || info.Size() > maxMeta {
		return nil, ErrNative
	}
	p, err := io.ReadAll(io.LimitReader(f, maxMeta+1))
	if err != nil {
		return nil, err
	}
	if len(p) > maxMeta {
		return nil, ErrNative
	}
	return p, nil
}
func readArtifact(path string, out any) error {
	p, err := privateBytes(path)
	if err != nil {
		return err
	}
	var s sealedArtifact
	if strictJSON(p, &s) != nil || execwire.Hash(sha256.Sum256(s.Data)) != s.Digest {
		return ErrNative
	}
	return strictJSON(s.Data, out)
}
func validateReady(r nativeReady, c port.ExecutionControl, bootDigest execwire.Hash) error {
	i, h := r.Identity, r.Head
	if r.Version != 1 || i.Version != 1 || i.ChainID != 5042002 || c.Chain.ID != "5042002" || i.Genesis != c.Chain.Genesis || i.ArcRevision != ArcRevision || i.RethRevision != RethRevision || i.Layout != "reth-native-v2" || i.Instance == (execwire.Hash{}) || i.BootstrapDigest != bootDigest || i.Anchor != (nativeRef{c.Anchor.Height, c.Anchor.Hash, c.AnchorRoot}) || h.Version != 1 || h.Instance != i.Instance || h.ImportEpoch != 1 || !positive(h.ReadEpoch) || h.ReadEpoch != h.RequestID+1 || h.Tip.Height < i.Anchor.Height || h.RequestID != h.Tip.Height-i.Anchor.Height || h.Tip.Hash == (execwire.Hash{}) || h.Tip.Root == (execwire.Hash{}) || h.Tip.Height < c.Visible.Height {
		return ErrNative
	}
	if h.Tip.Height == i.Anchor.Height && h.Tip != i.Anchor {
		return ErrNative
	}
	if h.Tip.Height == c.Visible.Height && (h.Tip.Hash != c.Visible.Hash || h.Tip.Root != c.Root) {
		return ErrNative
	}
	return nil
}
func validateCompletion(i nativeIdentity, h nativeHead, m importMeta, digest, root, receiptsRoot execwire.Hash, r importReceipt) error {
	if r.Version != 1 || r.Identity != i || r.RequestID != m.RequestID || r.ImportEpoch != m.ImportEpoch || r.ReadEpoch != h.ReadEpoch+1 || r.Height != m.Height || r.Hash != m.Hash || r.Parent != m.Parent || r.ParentRoot != m.ParentRoot || r.InputDigest != digest || r.Root != root || r.ReceiptsRoot != receiptsRoot {
		return ErrNative
	}
	return nil
}
func completionPath(dir string, epoch, height uint64) string {
	return filepath.Join(dir, ".arc-native-import-receipts", strconv.FormatUint(epoch, 10)+"-"+strconv.FormatUint(height, 10)+".json")
}
