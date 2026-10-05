package consensus

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/storage"
)

func TestKeysBelongToTheWBFTKeyspace(t *testing.T) {
	v := common.HexToAddress("0x1")
	for _, k := range [][]byte{
		WBFTBlockExtraKey(1), WBFTEpochKey(1), LatestEpochKey(),
		WBFTValidatorStatsKey(v, 1, 2), WBFTValidatorActivityKey(v, 1),
		WBFTSignerPrepareIndexKey(1, v), WBFTSignerCommitIndexKey(1, v),
	} {
		require.Equal(t, KeyspaceOwner, storage.KeyOwner(string(k)), string(k))
	}
}
