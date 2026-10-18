package feed

import (
	"context"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/evmstate"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/evmtrace"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestStateProofRequiresModuleCapability(t *testing.T) {
	const name = "fixture.no-mpt"
	Register(name, Module{Validate: func(Config) error { return nil }, Open: func(context.Context, Config, Fresh) (Program, error) {
		t.Fatal("pure validation opened a module")
		return Program{}, nil
	}})
	t.Cleanup(func() { registry.Lock(); delete(registry.modules, name); registry.Unlock() })
	c := Config{Module: name, Window: "fixture", Consumer: "fixture", ChainID: "1", GenesisHash: common.Hash{1}.Hex(), AnchorHash: common.Hash{1}.Hex(), NetworkID: 1, ForkHash: "01020304", KeyFile: "/not-opened/key", Peers: []string{"module-validates-this"}}
	require.NoError(t, c.Validate())
	c.StateProofs = &evmstate.Config{Name: "tracked", StartHeight: 1, Accounts: []evmstate.AccountConfig{{Address: common.Address{1}.Hex()}}}
	require.ErrorContains(t, c.Validate(), "no audited MPT")
	c.StateProofs = nil
	c.CallTraces = &evmtrace.Config{Name: "call", Provider: "fixture", StartHeight: 1}
	require.ErrorContains(t, c.Validate(), "no audited call trace")
}
