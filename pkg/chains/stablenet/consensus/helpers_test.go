package consensus

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/storage"
)

// testDB is a Pebble storage with the WBFT store over it.
type testDB struct {
	*storage.PebbleStorage
	*Store
}

func newTestPebble(t *testing.T) *storage.PebbleStorage {
	t.Helper()
	db, err := storage.NewPebbleStorage(storage.DefaultConfig(t.TempDir()))
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	return db
}

func setupTestStorage(t *testing.T) (*testDB, func()) {
	t.Helper()
	db := newTestPebble(t)
	return &testDB{PebbleStorage: db, Store: NewStore(db, zap.NewNop())}, func() { _ = db.Close() }
}

func createTestBlockWithMiner(height uint64, miner common.Address, gasUsed uint64, timestamp uint64) *types.Block {
	return types.NewBlockWithHeader(&types.Header{
		UncleHash:   types.EmptyUncleHash,
		Coinbase:    miner,
		TxHash:      types.EmptyTxsHash,
		ReceiptHash: types.EmptyReceiptsHash,
		Difficulty:  big.NewInt(0),
		Number:      new(big.Int).SetUint64(height),
		GasLimit:    5000000,
		GasUsed:     gasUsed,
		Time:        timestamp,
		Extra:       []byte{},
	})
}
