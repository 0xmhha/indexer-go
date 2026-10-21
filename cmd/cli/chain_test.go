package cli

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/chains/evm"
)

func TestChainCheck(t *testing.T) {
	generic := evm.New(evm.ID)
	other := evm.New("other")
	node := chains.NodeInfo{ClientVersion: "Other/v1", ChainID: 7}
	isOther := Product{Binary: "indexer-other", Matches: func(n chains.NodeInfo) bool { return n.ChainID == 7 }}

	require.Nil(t, newCLI().chainCheck(), "a binary without products accepts any chain")

	check := newCLI(WithOtherProducts(isOther)).chainCheck()
	require.NoError(t, check(chains.NodeInfo{ChainID: 1}, generic))
	require.ErrorContains(t, check(node, generic), "run indexer-other")

	check = newCLI(WithName("indexer-other"), WithChain("other")).chainCheck()
	require.NoError(t, check(node, other))
	require.ErrorContains(t, check(chains.NodeInfo{ChainID: 1}, generic), `indexer-other indexes other chains, but the node ("", chain id 1) selected profile "evm"`)
}
