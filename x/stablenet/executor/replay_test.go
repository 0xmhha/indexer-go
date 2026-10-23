package executor

import (
	"bytes"
	"context"
	"errors"
	"math/big"
	"reflect"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/holiman/uint256"
)

type fixtureChain struct {
	config  *params.ChainConfig
	engine  consensus.Engine
	db      state.Database
	headers map[uint64]*types.Header
}

func (c *fixtureChain) Config() *params.ChainConfig { return c.config }
func (c *fixtureChain) Engine() consensus.Engine    { return c.engine }
func (c *fixtureChain) CurrentHeader() *types.Header {
	var head *types.Header
	for _, h := range c.headers {
		if head == nil || h.Number.Cmp(head.Number) > 0 {
			head = h
		}
	}
	return head
}
func (c *fixtureChain) GetHeader(hash common.Hash, n uint64) *types.Header {
	h := c.headers[n]
	if h != nil && h.Hash() == hash {
		return types.CopyHeader(h)
	}
	return nil
}
func (c *fixtureChain) GetHeaderByNumber(n uint64) *types.Header {
	if h := c.headers[n]; h != nil {
		return types.CopyHeader(h)
	}
	return nil
}
func (c *fixtureChain) GetHeaderByHash(hash common.Hash) *types.Header {
	for _, h := range c.headers {
		if h.Hash() == hash {
			return types.CopyHeader(h)
		}
	}
	return nil
}
func (c *fixtureChain) GetTd(common.Hash, uint64) *big.Int { return big.NewInt(1) }
func (c *fixtureChain) StateAt(root common.Hash) (*state.StateDB, error) {
	return state.New(root, c.db, nil)
}

func evmFixture(t *testing.T) (ethdb.Database, ReplayInput) {
	t.Helper()
	return evmFixtureWith(t, []byte{0x60, 0x03, 0x60, 0x00, 0x55, 0x60, 0x00, 0x60, 0x00, 0xa0, 0x00}, []evmTxSpec{{to: &account, value: 1}})
}

type evmTxSpec struct {
	to    *common.Address
	input []byte
	value int64
}

func evmFixtureWith(t *testing.T, code []byte, specs []evmTxSpec) (ethdb.Database, ReplayInput) {
	t.Helper()
	return evmFixtureDatabase(t, code, specs, nil)
}

func evmFixtureDatabase(t *testing.T, code []byte, specs []evmTxSpec, cache *triedb.Config) (ethdb.Database, ReplayInput) {
	t.Helper()
	disk := rawdb.NewMemoryDatabase()
	t.Cleanup(func() { _ = disk.Close() })
	db := state.NewDatabaseWithConfig(disk, cache)
	t.Cleanup(func() { _ = db.TrieDB().Close() })
	key, err := crypto.ToECDSA(bytes.Repeat([]byte{1}, 32))
	if err != nil {
		t.Fatal(err)
	}
	sender := crypto.PubkeyToAddress(key.PublicKey)
	contract := common.BytesToAddress([]byte{42})
	s, err := state.New(types.EmptyRootHash, db, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.SetBalance(sender, uint256.NewInt(1_000_000_000_000_000_000))
	s.SetCode(contract, code)
	root, err := s.Commit(0, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.TrieDB().Commit(root, false); err != nil {
		t.Fatal(err)
	}
	config := *params.AllEthashProtocolChanges
	parent := &types.Header{Number: big.NewInt(0), Root: root, Time: 1, GasLimit: 30_000_000, BaseFee: big.NewInt(1_000_000_000), Difficulty: big.NewInt(1)}
	chain := &fixtureChain{config: &config, engine: ethash.NewFaker(), db: db, headers: map[uint64]*types.Header{0: parent}}
	h := &types.Header{ParentHash: parent.Hash(), Number: big.NewInt(1), Time: 2, GasLimit: 30_000_000, BaseFee: big.NewInt(1_000_000_000), Difficulty: big.NewInt(1), Coinbase: common.BytesToAddress([]byte{99})}
	first, err := state.New(root, db, nil)
	if err != nil {
		t.Fatal(err)
	}
	var used uint64
	gp := new(core.GasPool).AddGas(h.GasLimit)
	var receipts types.Receipts
	var txs []*types.Transaction
	for i, spec := range specs {
		tx, err := types.SignTx(types.NewTx(&types.LegacyTx{Nonce: uint64(i), To: spec.to, Gas: 300_000, GasPrice: big.NewInt(2_000_000_000), Value: big.NewInt(spec.value), Data: spec.input}), types.MakeSigner(&config, h.Number, h.Time), key)
		if err != nil {
			t.Fatal(err)
		}
		first.SetTxContext(tx.Hash(), i)
		receipt, err := core.ApplyTransaction(&config, chain, nil, gp, first, h, tx, &used, vm.Config{})
		if err != nil {
			t.Fatal(err)
		}
		receipts = append(receipts, receipt)
		txs = append(txs, tx)
	}
	h.GasUsed = used
	if err := chain.engine.Finalize(chain, h, first, txs, nil, nil); err != nil {
		t.Fatal(err)
	}
	h.Root = first.IntermediateRoot(true)
	block := types.NewBlock(h, txs, nil, receipts, trie.NewStackTrie(nil))
	committed, err := first.Commit(1, true)
	if err != nil {
		t.Fatal(err)
	}
	if committed != block.Root() {
		t.Fatal("invalid first execution fixture")
	}
	// Real path-mode imports retain the recent parent in the diff layers.
	// Forcing a disk flush here would intentionally evict that parent.
	if cache == nil || cache.PathDB == nil {
		if err := db.TrieDB().Commit(committed, false); err != nil {
			t.Fatal(err)
		}
	}
	return disk, ReplayInput{Chain: chain, Database: db, Parent: parent, Block: block, Receipts: receipts, Generation: 1}
}

func TestReplayRealEVMMatchesImportedBlockWithoutWriting(t *testing.T) {
	disk, in := evmFixture(t)
	before := dump(t, disk)
	header := in.Block.Header()
	result, err := Replay(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if result.Ref.Hash != [32]byte(in.Block.Hash()) || result.StateRoot != [32]byte(in.Block.Root()) || result.ReceiptsRoot != [32]byte(in.Block.ReceiptHash()) {
		t.Fatal("result not bound to original block")
	}
	if len(result.Steps) != 2 || result.Steps[0].TxIndex != 0 || result.Steps[1].TxIndex != -1 || len(result.Steps[0].Storage) != 1 {
		t.Fatalf("missing transaction/Finalize attribution: %+v", result.Steps)
	}
	if !reflect.DeepEqual(header, in.Block.Header()) {
		t.Fatal("replay mutated source header")
	}
	if !reflect.DeepEqual(before, dump(t, disk)) {
		t.Fatal("replay wrote durable node state")
	}
}

func TestReplayRejectsOriginalRootEvenWhenFinalizeOverwritesHeader(t *testing.T) {
	_, in := evmFixture(t)
	engine := &rootMutator{Engine: in.Chain.Engine()}
	in.Chain.(*fixtureChain).engine = engine
	bad := in.Block.Header()
	bad.Root[0] ^= 1
	in.Block = types.NewBlockWithHeader(bad).WithBody(in.Block.Transactions(), in.Block.Uncles())
	if _, err := Replay(context.Background(), in); !errors.Is(err, ErrRootMismatch) {
		t.Fatalf("replaced original root accepted: %v", err)
	}
	if !engine.called {
		t.Fatal("fixture did not exercise mutating Finalize")
	}
}

type rootMutator struct {
	consensus.Engine
	called bool
}

func (e *rootMutator) Finalize(chain consensus.ChainHeaderReader, h *types.Header, s *state.StateDB, txs []*types.Transaction, uncles []*types.Header, withdrawals []*types.Withdrawal) error {
	e.called = true
	if err := e.Engine.Finalize(chain, h, s, txs, uncles, withdrawals); err != nil {
		return err
	}
	h.Root = s.IntermediateRoot(chain.Config().IsEIP158(h.Number))
	return nil
}

func TestReplayRejectsStoredReceiptMismatch(t *testing.T) {
	_, in := evmFixture(t)
	bad := *in.Receipts[0]
	bad.CumulativeGasUsed++
	in.Receipts = types.Receipts{&bad}
	if _, err := Replay(context.Background(), in); !errors.Is(err, ErrRootMismatch) {
		t.Fatalf("wrong stored receipt accepted: %v", err)
	}
}

func TestReplayHonorsCancellation(t *testing.T) {
	_, in := evmFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Replay(ctx, in); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled replay: %v", err)
	}
}
