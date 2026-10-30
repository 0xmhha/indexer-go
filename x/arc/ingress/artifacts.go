package arcingress

import (
	"encoding/json"
	"path/filepath"
	"strconv"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/checkpoint"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/x/arc/nativeinput"
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

func privateBytes(path string) ([]byte, error) { return nativeinput.PrivateBytes(path) }
func readArtifact(path string, out any) error  { return nativeinput.ReadArtifact(path, out) }
func validateReady(r nativeReady, c port.ExecutionControl, bootDigest execwire.Hash) error {
	i, h := r.Identity, r.Head
	if r.Version != 1 || i.Version != 1 || i.ChainID != 5042002 || c.Chain.ID != "5042002" || i.Genesis != c.Chain.Genesis || i.ArcRevision != ArcRevision || i.RethRevision != RethRevision || i.Layout != "reth-native-v2" || i.Instance == (execwire.Hash{}) || i.BootstrapDigest != bootDigest || i.Anchor != (nativeRef{Height: c.Anchor.Height, Hash: c.Anchor.Hash, Root: c.AnchorRoot}) || h.Version != 1 || h.Instance != i.Instance || !positive(h.ImportEpoch) || h.ImportEpoch > 128 || !positive(h.ReadEpoch) || !positive(h.RequestID+1) || h.Tip.Height < i.Anchor.Height || h.Tip.Hash == (execwire.Hash{}) || h.Tip.Root == (execwire.Hash{}) || h.Tip.Height < c.Visible.Height {
		return ErrNative
	}
	if h.ImportEpoch == 1 && (h.RequestID != h.Tip.Height-i.Anchor.Height || h.ReadEpoch != h.RequestID+1) {
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
