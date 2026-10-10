package app

import (
	"context"
	"math/big"
	"path/filepath"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/features/aa"
	"github.com/0xmhha/indexer-go/pkg/testchain"
)

var (
	// entryPointA is poc-contract's EntryPoint v0.9 (chain 8283 deployment).
	entryPointA = common.HexToAddress("0xEf6817fe73741A8F10088f9511c64b666a338A14")
	entryPointB = common.HexToAddress("0x00000000000000000000000000000000000E9009")
)

// entryPointScenario is the reference scenario with one bundle through
// each of entryPointA and entryPointB, neither of them a known EntryPoint.
type entryPointScenario struct {
	*testchain.Scenario
	txA, txB common.Hash
}

func buildEntryPointScenario() *entryPointScenario {
	sc := testchain.BuildDefault()
	bundle := func(ep common.Address, op string) common.Hash {
		word := func(v int64) []byte { return common.LeftPadBytes(big.NewInt(v).Bytes(), 32) }
		data := append(append(append(word(7), word(1)...), word(150_000)...), word(1_000)...)
		b := sc.Chain.AddBlock(testchain.TxSpec{From: sc.Accounts[5], Tx: &types.LegacyTx{To: &ep, Gas: 300000, GasPrice: big.NewInt(1_000_000_000)}, GasUsed: 210000,
			Logs: []*types.Log{{
				Address: ep,
				Topics: []common.Hash{
					testchain.SigUserOperationEvent,
					crypto.Keccak256Hash([]byte(op)),
					common.BytesToHash(sc.Account.Bytes()),
					{}, // no paymaster
				},
				Data: data,
			}}})
		return b.Block.Transactions()[0].Hash()
	}
	return &entryPointScenario{Scenario: sc, txA: bundle(entryPointA, "op-a"), txB: bundle(entryPointB, "op-b")}
}

// entryPointFeatures is a features section configuring aa.erc4337's
// entry points.
func entryPointFeatures(t *testing.T, eps ...aa.EntryPointSetting) map[string]config.FeatureConfig {
	t.Helper()
	var holder config.Config
	require.NoError(t, holder.SetFeatureSettings(aa.ERC4337, aa.ERC4337Settings{EntryPoints: eps}))
	return holder.Features
}

// requireOps checks the UserOperations stored for tx: one through ep with
// version, or none when ep is the zero address.
func requireOps(t *testing.T, s port.UserOpIndexReader, tx common.Hash, ep common.Address, version string) {
	t.Helper()
	ops, err := s.GetUserOpsByTx(context.Background(), tx)
	require.NoError(t, err)
	if ep == (common.Address{}) {
		assert.Empty(t, ops, "tx %s goes through an EntryPoint that is not indexed", tx.Hex())
		return
	}
	require.Len(t, ops, 1, "tx %s", tx.Hex())
	assert.Equal(t, ep, ops[0].EntryPoint)
	assert.Equal(t, version, ops[0].EntryPointVersion)
	assert.Equal(t, "7", ops[0].Nonce)
	assert.True(t, ops[0].Status)
}

// TestConfiguredEntryPoints: features.aa.erc4337.entry_points adds an
// EntryPoint with its version to the known ones; an EntryPoint left out is
// still not indexed, and the known v0.7 EntryPoint still is.
func TestConfiguredEntryPoints(t *testing.T) {
	sc := buildEntryPointScenario()
	srv := testchain.NewServer(sc.Chain)
	t.Cleanup(srv.Close)

	cfg := config.NewConfig()
	cfg.RPC.Endpoint = srv.URL()
	cfg.RPC.Timeout = 5 * time.Second
	setTestDatabase(t, cfg, filepath.Join(t.TempDir(), "db"))
	cfg.API.Enabled = false
	enableTestChainFeatures(cfg)
	cfg.Features[aa.ERC4337] = entryPointFeatures(t, aa.EntryPointSetting{Address: entryPointA.Hex(), Version: "v0.9"})[aa.ERC4337]
	app, err := NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, err)
	t.Cleanup(app.Shutdown)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	require.NoError(t, app.fetcher.FetchRange(ctx, 0, sc.Chain.Head()))

	s := app.storage.(port.UserOpIndexReader)
	requireOps(t, s, sc.txA, entryPointA, "v0.9")
	requireOps(t, s, sc.txB, common.Address{}, "")
	known, _, err := s.GetUserOpsBySender(ctx, sc.Account, port.FirstPage(10))
	require.NoError(t, err)
	versions := map[string]int{}
	for _, op := range known {
		versions[op.EntryPointVersion]++
	}
	assert.Equal(t, map[string]int{"v0.7": 1, "v0.9": 1}, versions, "the scenario's v0.7 bundle is still indexed")
}

// TestConfiguredEntryPointsRejected: a malformed or repeated entry stops
// startup.
func TestConfiguredEntryPointsRejected(t *testing.T) {
	sc := testchain.BuildDefault()
	srv := testchain.NewServer(sc.Chain)
	t.Cleanup(srv.Close)
	v09 := aa.EntryPointSetting{Address: entryPointA.Hex(), Version: "v0.9"}
	for name, c := range map[string]struct {
		eps  []aa.EntryPointSetting
		want string
	}{
		"unknown version":      {[]aa.EntryPointSetting{{Address: entryPointA.Hex(), Version: "v1.0"}}, "entry_points[0]"},
		"no version":           {[]aa.EntryPointSetting{{Address: entryPointA.Hex()}}, "entry_points[0]"},
		"malformed address":    {[]aa.EntryPointSetting{{Address: "0x1234", Version: "v0.9"}}, "entry_points[0]"},
		"known, other version": {[]aa.EntryPointSetting{{Address: testchain.EntryPointV07.Hex(), Version: "v0.9"}}, "entry_points[0]"},
		"listed twice":         {[]aa.EntryPointSetting{v09, v09}, "entry_points[1]"},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := config.NewConfig()
			cfg.RPC.Endpoint = srv.URL()
			cfg.RPC.Timeout = 5 * time.Second
			setTestDatabase(t, cfg, filepath.Join(t.TempDir(), "db"))
			cfg.API.Enabled = false
			cfg.Features = entryPointFeatures(t, c.eps...)
			app, err := NewApp(cfg, zap.NewNop(), false, "")
			if err == nil {
				app.Shutdown()
			}
			require.ErrorContains(t, err, "features.aa.erc4337."+c.want)
		})
	}
}

// TestMultiChainEntryPoints: in multichain mode each chain indexes the
// EntryPoints of its own features section (chains[].features).
func TestMultiChainEntryPoints(t *testing.T) {
	a, b := buildEntryPointScenario(), buildEntryPointScenario()
	aSrv, bSrv := testchain.NewServer(a.Chain), testchain.NewServer(b.Chain)
	t.Cleanup(aSrv.Close)
	t.Cleanup(bSrv.Close)

	ea, eb := chainEntry("a", aSrv.URL()), chainEntry("b", bSrv.URL())
	ea.Features = entryPointFeatures(t, aa.EntryPointSetting{Address: entryPointA.Hex(), Version: "v0.9"})
	eb.Features = entryPointFeatures(t, aa.EntryPointSetting{Address: entryPointB.Hex(), Version: "v0.8"})
	cfg := multiChainConfig(t, filepath.Join(t.TempDir(), "db"), ea, eb)
	app, err := NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	runCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- app.Run(runCtx) }()
	t.Cleanup(func() {
		stop()
		<-done
		app.Shutdown()
	})

	stores := map[string]port.UserOpIndexReader{}
	for id, sc := range map[string]*entryPointScenario{"a": a, "b": b} {
		require.Eventually(t, func() bool {
			ci, err := app.multichainManager.GetChain(id)
			if err != nil {
				return false
			}
			h, ok := ci.IndexedHeight(ctx)
			return ok && h >= sc.Chain.Head()
		}, time.Minute, 20*time.Millisecond, "chain %s indexed", id)
		store, _, ok := app.multichainManager.ChainStore(id)
		require.True(t, ok)
		stores[id] = store.(port.UserOpIndexReader)
	}
	requireOps(t, stores["a"], a.txA, entryPointA, "v0.9")
	requireOps(t, stores["a"], a.txB, common.Address{}, "")
	requireOps(t, stores["b"], b.txA, common.Address{}, "")
	requireOps(t, stores["b"], b.txB, entryPointB, "v0.8")
}
