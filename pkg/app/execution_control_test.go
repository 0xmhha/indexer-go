package app

import (
	"testing"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/chains/evm"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/x/stablenet"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestExecutionControlRequiresPreparedExactAnchorAndStableNet(t *testing.T) {
	e := executionAppConfig().Source.Execution
	anchor := execwire.Ref{Generation: 1, Hash: execwire.Hash(common.HexToHash(e.AnchorHash))}
	c := port.ExecutionControl{ExecutionConfig: port.ExecutionConfig{Anchor: anchor, AnchorRoot: execwire.Hash(common.HexToHash(e.AnchorRoot))}, Generation: 1, Visible: anchor, Root: execwire.Hash(common.HexToHash(e.AnchorRoot)), BaselineReady: true}
	require.NoError(t, validateExecutionControl(e, c, stablenet.New()))
	require.ErrorIs(t, validateExecutionControl(e, c, evm.New("evm")), port.ErrInvalidData)
	require.ErrorIs(t, validateExecutionControl(e, c, evm.New("stablenet")), port.ErrInvalidData)
	for _, mutate := range []func(*port.ExecutionControl){func(c *port.ExecutionControl) { c.BaselineReady = false }, func(c *port.ExecutionControl) { c.Anchor.Hash[0]++ }, func(c *port.ExecutionControl) { c.AnchorRoot[0]++ }, func(c *port.ExecutionControl) { c.Anchor.Height++ }} {
		changed := c
		mutate(&changed)
		require.Error(t, validateExecutionControl(e, changed, stablenet.New()))
	}
	c.Rewinding = true
	require.NoError(t, validateExecutionControl(e, c, stablenet.New()), "Run recovers the durable marker before outbox/listener startup")
}
