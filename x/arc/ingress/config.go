package arcingress

import (
	"context"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/execution/ingress"
	"github.com/0xmhha/indexer-go/pkg/window"
	arcfeed "github.com/0xmhha/indexer-go/x/arc/feed"
	"github.com/ethereum/go-ethereum/common"
	"golang.org/x/sync/semaphore"
)

const ModuleName = "arc.native"
const ownerDomain = "arc.native/ain1/v1"

func init() {
	ingress.Register(ModuleName, ingress.Module{Profile: "arc", Validate: func(c ingress.Config) error {
		if c.MaxActiveBlocks < 2 {
			return ErrNative
		}
		return arcfeed.ValidatePeer(c.Peer)
	}, Preflight: func(c ingress.Config, o window.Options) error {
		if o.MaxPayloadBytes > 8<<20 || c.MaxActiveBytes < 2*activeBytes(o) {
			return ErrNative
		}
		return nil
	}, Prepare: func(_ context.Context, c ingress.Config, d ingress.Dependencies) (ingress.Program, error) {
		if d.Control.Chain.ID != "5042002" {
			return nil, ErrNative
		}
		if err := c.Preflight(d.Layout, d.Socket); err != nil {
			return nil, err
		}
		o, err := d.Layout.OptionsFor(c.Window, model.FeedChain{ID: d.Control.Chain.ID, GenesisHash: common.Hash(d.Control.Chain.Genesis)})
		if err != nil {
			return nil, err
		}
		return &program{cfg: c, deps: d, opts: o, slots: make(chan struct{}, c.MaxActiveBlocks), bytes: semaphore.NewWeighted(c.MaxActiveBytes)}, nil
	}})
}
