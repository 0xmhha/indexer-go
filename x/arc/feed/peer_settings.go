package arcfeed

import (
	"context"
	"encoding/hex"
	"fmt"
	"path/filepath"

	"github.com/0xmhha/indexer-go/pkg/chainfeed"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/ethpeer"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/feed"
	"github.com/0xmhha/indexer-go/pkg/source"
)

// ValidatePeer is pure. Native ingress has no capture consumer/projection or
// finality policy; only the shared transport settings are accepted here.
func ValidatePeer(p chainfeed.PeerSettings) error {
	if !filepath.IsAbs(p.KeyFile) || p.NetworkID == 0 || len(p.Peers) == 0 || len(p.Peers) > 8 || len(p.ForkHash) != 8 {
		return model.ErrCaptureInvalid
	}
	fork, err := hex.DecodeString(p.ForkHash)
	if err != nil || len(fork) != 4 || string(fork) == "\x00\x00\x00\x00" {
		return model.ErrCaptureInvalid
	}
	return validateConfig(peerFeedConfig(p))
}
func peerFeedConfig(p chainfeed.PeerSettings) feed.Config {
	return feed.Config{ChainID: "5042002", NetworkID: p.NetworkID, ForkHash: p.ForkHash, ForkNext: p.ForkNext, KeyFile: p.KeyFile, Peers: p.Peers, ListenAddr: p.ListenAddr}
}
func OpenPeer(ctx context.Context, p chainfeed.PeerSettings, anchor model.FeedBlock, fresh source.CanonicalReader) (ethpeer.Config, error) {
	if err := ValidatePeer(p); err != nil {
		return ethpeer.Config{}, err
	}
	if anchor.Validate() != nil || anchor.Chain.ID != "5042002" || fresh == nil {
		return ethpeer.Config{}, fmt.Errorf("arc.eth68: audited prepared anchor and shared canonical reader required")
	}
	return openPeer(ctx, peerFeedConfig(p), anchor, fresh)
}
