package chains_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/chains/evm"
)

func TestDetectionPrefersSpecificProfiles(t *testing.T) {
	specific := evm.New("test-specific", evm.WithDetect(func(i chains.NodeInfo) bool {
		return strings.HasPrefix(i.ClientVersion, "Special/")
	}))
	chains.Register(specific, 100)

	p, err := chains.Detect(chains.NodeInfo{ClientVersion: "Special/v1"})
	require.NoError(t, err)
	require.Equal(t, "test-specific", p.ID())

	p, err = chains.Detect(chains.NodeInfo{ClientVersion: "Geth/v1.16.5"})
	require.NoError(t, err)
	require.Equal(t, evm.ID, p.ID(), "the generic EVM profile is the fallback")

	got, ok := chains.Lookup("test-specific")
	require.True(t, ok)
	require.Equal(t, specific, got)

	ids := chains.IDs()
	require.Less(t, indexOf(ids, "test-specific"), indexOf(ids, evm.ID))
}

func TestDuplicateRegistrationPanics(t *testing.T) {
	require.Panics(t, func() { chains.Register(evm.New(evm.ID), 0) })
}

func indexOf(xs []string, x string) int {
	for i, v := range xs {
		if v == x {
			return i
		}
	}
	return -1
}
