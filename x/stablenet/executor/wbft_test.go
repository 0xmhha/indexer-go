package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"math/big"
	"reflect"
	"testing"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/wbft"
	"github.com/ethereum/go-ethereum/consensus/wbft/backend"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/holiman/uint256"
)

func wbftFixture(t *testing.T) (ethdb.Database, ReplayInput, common.Address, common.Address, common.Address) {
	return wbftFixtureMint(t, 9)
}
func wbftFixtureMint(t *testing.T, amount byte) (ethdb.Database, ReplayInput, common.Address, common.Address, common.Address) {
	t.Helper()
	disk := rawdb.NewMemoryDatabase()
	t.Cleanup(func() { _ = disk.Close() })
	db := state.NewDatabase(disk)
	t.Cleanup(func() { _ = db.TrieDB().Close() })
	encoded, err := json.Marshal(params.TestWBFTChainConfig)
	if err != nil {
		t.Fatal(err)
	}
	var config params.ChainConfig
	if err := json.Unmarshal(encoded, &config); err != nil {
		t.Fatal(err)
	}
	config.Ethash = nil
	config.ApplepieBlock = big.NewInt(0)
	key, err := crypto.ToECDSA(bytes.Repeat([]byte{1}, 32))
	if err != nil {
		t.Fatal(err)
	}
	payerKey, err := crypto.ToECDSA(bytes.Repeat([]byte{2}, 32))
	if err != nil {
		t.Fatal(err)
	}
	sender, payer := crypto.PubkeyToAddress(key.PublicKey), crypto.PubkeyToAddress(payerKey.PublicKey)
	recipient := common.BytesToAddress([]byte{77})
	config.Anzeon.SystemContracts.NativeCoinAdapter.Params["minters"] = sender.Hex()
	config.Anzeon.SystemContracts.NativeCoinAdapter.Params["masterMinter"] = sender.Hex()
	fund := new(big.Int).Exp(big.NewInt(10), big.NewInt(24), nil)
	g := &core.Genesis{Config: &config, GasLimit: 30_000_000, Difficulty: types.WBFTDefaultDifficulty, Alloc: types.GenesisAlloc{sender: {Balance: new(big.Int).Set(fund)}, payer: {Balance: new(big.Int).Set(fund)}, recipient: {Balance: big.NewInt(100)}}}
	g.ExtraData, err = wbft.CreateInitialExtraData(config.Anzeon)
	if err != nil {
		t.Fatal(err)
	}
	if err := core.InjectContracts(g, &config); err != nil {
		t.Fatal(err)
	}
	genesis, err := g.Commit(disk, db.TrieDB())
	if err != nil {
		t.Fatal(err)
	}
	engineConfig := *wbft.DefaultConfig
	engineConfig.Epoch = config.Anzeon.WBFT.EpochLength
	engine := backend.New(&engineConfig, key, disk)
	if engine == nil {
		t.Fatal("WBFT engine initialization failed")
	}
	t.Cleanup(func() { _ = engine.Close() })
	chain := &fixtureChain{config: &config, engine: engine, db: db, headers: map[uint64]*types.Header{0: genesis.Header()}}
	extra, err := types.ExtractWBFTExtra(genesis.Header())
	if err != nil {
		t.Fatal(err)
	}
	extra.EpochInfo = nil
	extraBytes, err := rlp.EncodeToBytes(extra)
	if err != nil {
		t.Fatal(err)
	}
	h := &types.Header{ParentHash: genesis.Hash(), Number: big.NewInt(1), Time: 1, GasLimit: 30_000_000, BaseFee: new(big.Int).SetUint64(params.MinBaseFee), Difficulty: types.WBFTDefaultDifficulty, Coinbase: config.Anzeon.Init.Validators[0], Extra: extraBytes}
	to := config.Anzeon.SystemContracts.NativeCoinAdapter.Address
	data := append([]byte{}, vm.CoinManagerMintSelector[:]...)
	data = append(data, common.LeftPadBytes(recipient.Bytes(), 32)...)
	data = append(data, common.LeftPadBytes([]byte{amount}, 32)...)
	dynamic := types.DynamicFeeTx{ChainID: config.ChainID, Nonce: 0, To: &to, Gas: 500_000, GasTipCap: new(big.Int).Set(h.GasTip()), GasFeeCap: new(big.Int).Add(h.BaseFee, h.GasTip()), Value: big.NewInt(0), Data: data}
	signed, err := types.SignTx(types.NewTx(&dynamic), types.NewLondonSigner(config.ChainID), key)
	if err != nil {
		t.Fatal(err)
	}
	dynamic.V, dynamic.R, dynamic.S = signed.RawSignatureValues()
	tx, err := types.SignTx(types.NewTx(&types.FeeDelegateDynamicFeeTx{SenderTx: dynamic, FeePayer: &payer}), types.NewFeeDelegateSigner(config.ChainID), payerKey)
	if err != nil {
		t.Fatal(err)
	}
	first, err := state.New(genesis.Root(), db, nil)
	if err != nil {
		t.Fatal(err)
	}
	first.SetTxContext(tx.Hash(), 0)
	var used uint64
	receipt, err := core.ApplyTransaction(&config, chain, nil, new(core.GasPool).AddGas(h.GasLimit), first, h, tx, &used, vm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Status != types.ReceiptStatusSuccessful {
		t.Fatal("native mint fixture reverted")
	}
	h.GasUsed = used
	if err := engine.Finalize(chain, h, first, []*types.Transaction{tx}, nil, nil); err != nil {
		t.Fatal(err)
	}
	receipts := types.Receipts{receipt}
	block := types.NewBlock(h, []*types.Transaction{tx}, nil, receipts, trie.NewStackTrie(nil))
	root, err := first.Commit(1, true)
	if err != nil {
		t.Fatal(err)
	}
	if root != block.Root() {
		t.Fatal("WBFT first root mismatch")
	}
	if err := db.TrieDB().Commit(root, false); err != nil {
		t.Fatal(err)
	}
	return disk, ReplayInput{Chain: chain, Database: db, Parent: genesis.Header(), Block: block, Receipts: receipts, Generation: 1}, sender, payer, recipient
}

func TestReplayRealWBFTFeePayerNativeMintAndFinalize(t *testing.T) {
	disk, in, sender, payer, recipient := wbftFixture(t)
	before := dump(t, disk)
	result, err := Replay(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Steps) != 2 {
		t.Fatal("missing WBFT stages")
	}
	foundPayer, foundMint, foundFinalize := false, false, false
	for _, a := range result.Steps[0].Accounts {
		if a.Address == execwire.Address(payer) {
			foundPayer = a.Before.Balance != a.After.Balance && a.Before.Nonce == a.After.Nonce
		}
		if a.Address == execwire.Address(recipient) {
			foundMint = new(uint256.Int).SetBytes(a.After.Balance[:]).Uint64() == 109
		}
		if a.Address == execwire.Address(sender) && a.Before.Balance != a.After.Balance {
			t.Fatal("fee-delegating sender paid the gas")
		}
	}
	for _, a := range result.Steps[1].Accounts {
		if a.Before.Balance != a.After.Balance {
			foundFinalize = true
		}
	}
	if !foundPayer || !foundMint || !foundFinalize {
		t.Fatalf("missing payer/mint/Finalize: %v %v %v", foundPayer, foundMint, foundFinalize)
	}
	if !reflect.DeepEqual(before, dump(t, disk)) {
		t.Fatal("WBFT replay wrote node DB")
	}
}

func TestRecorderRealNativeManagerExtra(t *testing.T) {
	_, in, _, _, recipient := wbftFixture(t)
	r, err := newRecorder(in.Database, in.Parent.Root)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.beginStep(execwire.Transaction, 0, common.Hash{9}); err != nil {
		t.Fatal(err)
	}
	h := in.Block.Header()
	blockContext := core.NewEVMBlockContext(h, in.Chain, nil)
	env := vm.NewEVM(blockContext, vm.TxContext{}, r.state, in.Chain.Config(), vm.Config{})
	input := append([]byte{}, vm.AccountManagerBlacklistSelector[:]...)
	input = append(input, common.LeftPadBytes(recipient.Bytes(), 32)...)
	// Exercise the real native manager with its required council caller. This
	// fixture calls the EVM library directly; it is not a governance transaction.
	caller := in.Chain.Config().Anzeon.SystemContracts.GovCouncil.Address
	if _, _, err := env.Call(vm.AccountRef(caller), params.AccountManagerAddress, input, 500_000, uint256.NewInt(0)); err != nil {
		t.Fatal(err)
	}
	s, err := r.finishStep(true)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, a := range s.Accounts {
		if a.Address == execwire.Address(recipient) {
			found = a.Before.Extra == 0 && a.After.Extra == types.AccountExtraMaskBlacklisted
		}
	}
	if !found {
		t.Fatal("native manager Extra change missing")
	}
}

func TestReplayEmptyWBFTBlockStillHasFinalizeStep(t *testing.T) {
	_, in, _, _, _ := wbftFixture(t)
	h := in.Block.Header()
	h.GasUsed = 0
	first, err := state.New(in.Parent.Root, in.Database, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := in.Chain.Engine().Finalize(in.Chain, h, first, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	in.Block = types.NewBlock(h, nil, nil, nil, trie.NewStackTrie(nil))
	in.Receipts = nil
	out, err := Replay(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if out.TxCount != 0 || len(out.Steps) != 1 || out.Steps[0].Cause != execwire.Finalize || out.Steps[0].TxIndex != -1 || len(out.Steps[0].Accounts) != 0 || len(out.Steps[0].Storage) != 0 {
		t.Fatal("empty block lost its contiguous Finalize completion")
	}
}
