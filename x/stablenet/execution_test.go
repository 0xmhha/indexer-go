package stablenet_test

import (
	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/chains/evm"
	"github.com/0xmhha/indexer-go/x/stablenet"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestExecutionCapabilityRequiresExplicitProducerDeclaration(t *testing.T) {
	require.False(t, chains.SupportsExecution(nil))
	require.False(t, chains.SupportsExecution(evm.New("stablenet")), "a matching name is not a verified execution producer")
	require.False(t, chains.SupportsExecution(evm.New("evm")))
	require.True(t, chains.SupportsExecution(stablenet.New()))
}
