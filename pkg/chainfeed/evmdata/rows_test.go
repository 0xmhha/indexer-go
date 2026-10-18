package evmdata

import (
	"os"
	"testing"

	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/chains/evm"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/stretchr/testify/require"
)

func TestNormalizedRowsKeepHeaderExtension(t *testing.T) {
	data, e := os.ReadFile("../../chains/evm/testdata/sepolia-header-11901474.json")
	require.NoError(t, e)
	block, e := evm.New("evm-test").DecodeBlock(data)
	require.NoError(t, e)
	fields, ok := evm.HeaderFieldsOf(block)
	require.True(t, ok)
	rows, e := normalizedRows(block, nil)
	require.NoError(t, e)
	m, e := capture.DescribeRows(rows)
	require.NoError(t, e)
	require.Equal(t, uint32(0), m.Counts[2].Count)
	decoded, e := model.DecodeBlock(rows[0].Value)
	require.NoError(t, e)
	got, ok := evm.HeaderFieldsOf(decoded)
	require.True(t, ok)
	require.Equal(t, fields, got)
}
