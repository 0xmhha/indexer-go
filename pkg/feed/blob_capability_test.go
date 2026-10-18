package feed

import (
	"context"
	"strings"
	"testing"

	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/evmdata"
	"github.com/0xmhha/indexer-go/pkg/chains/evm"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

type blobFresh struct{ t *testing.T }

func (f blobFresh) HashAt(context.Context, uint64) (common.Hash, error) {
	f.t.Error("unexpected node read")
	return common.Hash{}, port.ErrInvalidData
}
func (f blobFresh) FinalizedCheckpoint(context.Context) (uint64, common.Hash, error) {
	f.t.Error("unexpected finality read")
	return 0, common.Hash{}, port.ErrInvalidData
}
func blobConfig(module string) Config {
	return Config{Module: module, Window: "fixture", Consumer: "fixture", ChainID: "1", GenesisHash: common.Hash{1}.Hex(), AnchorHash: common.Hash{1}.Hex(), NetworkID: 1, ForkHash: "01020304", KeyFile: "/not-opened/key", Peers: []string{"validated-by-module"}}
}
func TestBlobModuleCapabilityPreflightAndBinding(t *testing.T) {
	for _, mode := range []string{"copy", "invalid", "mismatch"} {
		t.Run(mode, func(t *testing.T) {
			name := "blob.policy." + mode
			c := blobConfig(name)
			policy := model.BlobCapability{Version: 1, Profile: "fixture", Status: model.BlobUnknown, Reason: "not_audited"}
			if mode == "invalid" {
				policy.Version = 2
			}
			if mode == "mismatch" {
				rev := strings.Repeat("1", 40)
				policy = model.BlobCapability{Version: 1, Profile: "fixture", Status: model.BlobUnsupported, Reason: "disabled", Basis: "node_transaction_admission", Revision: rev, SourceURL: "https://example.test/repo/blob/" + rev + "/pool.rs"}
			}
			opened := false
			Register(name, Module{BlobCapability: &policy, Validate: func(Config) error { return nil }, Open: func(_ context.Context, c Config, _ Fresh) (Program, error) {
				opened = true
				anchor, e := c.Identity()
				require.NoError(t, e)
				return Program{Anchor: anchor, Chain: anchor.Chain, Plan: evmdata.BasePlan(), Profile: evm.New("fixture"), Run: func(context.Context, port.CaptureDatabase, capture.SessionConfig) error { return nil }}, nil
			}})
			t.Cleanup(func() { registry.Lock(); delete(registry.modules, name); registry.Unlock() })
			if mode == "copy" {
				policy.Version = 99
			}
			if mode == "invalid" {
				require.ErrorContains(t, c.Validate(), "invalid module blob capability")
				require.False(t, opened)
				return
			}
			require.NoError(t, c.Validate())
			require.False(t, opened, "preflight does not open module resources")
			_, e := Open(t.Context(), c, blobFresh{t: t})
			if mode == "mismatch" {
				require.ErrorIs(t, e, port.ErrInvalidData)
			} else {
				require.NoError(t, e)
			}
		})
	}
}
func TestBlobYAMLActivationIsStillRejected(t *testing.T) {
	// A codec library is not an app transport. Do not quietly accept an option
	// that would promise to run a supplier which does not exist for this module.
	var c Config
	e := yaml.Unmarshal([]byte("module: arc.eth68\nblobs:\n  name: unsupported\n"), &c)
	require.ErrorContains(t, e, "field blobs not found")
}
