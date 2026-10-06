package porttest

import (
	"context"
	"fmt"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// Accounts of the fixture chain.
var (
	addrA    = common.HexToAddress("0x00000000000000000000000000000000000000a1")
	addrB    = common.HexToAddress("0x00000000000000000000000000000000000000b2")
	addrC    = common.HexToAddress("0x00000000000000000000000000000000000000c3") // token contract
	minerX   = common.HexToAddress("0x00000000000000000000000000000000000000e1")
	minerY   = common.HexToAddress("0x00000000000000000000000000000000000000e2")
	created  = common.HexToAddress("0x00000000000000000000000000000000000000d4") // created at height 3
	unknown  = common.HexToAddress("0x00000000000000000000000000000000000000ff")
	baseTime = uint64(1_700_000_000)
)

// transferTopic is the ERC-20/721 Transfer event signature.
var transferTopic = crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)"))

// fixtureHash returns a deterministic hash for a label and number.
func fixtureHash(label string, n uint64) common.Hash {
	return crypto.Keccak256Hash([]byte(fmt.Sprintf("%s/%d", label, n)))
}

// chain is a small deterministic chain: block 0 is empty; every later block
// h has a legacy transfer A -> B of h wei and a fee-market call B -> C
// that emits one Transfer log (C transfers h tokens from B to A). Block 2's
// call fails (status 0, no log). Block 3 also has a contract creation by A
// that creates `created`. Miners alternate between minerX (even heights)
// and minerY (odd heights). Block h's time is baseTime + 12*h.
type chain struct {
	Blocks   []*model.Block
	Receipts []*model.Receipt // in block and transaction order
}

// newChain builds the fixture chain with heights 0..n-1.
func newChain(n int) *chain {
	c := &chain{}
	for h := uint64(0); h < uint64(n); h++ {
		b := &model.Block{
			Hash:     fixtureHash("block", h),
			Number:   h,
			Time:     baseTime + 12*h,
			GasLimit: 30_000_000,
			BaseFee:  big.NewInt(1_000_000_000),
			Miner:    minerX,
		}
		if h%2 == 1 {
			b.Miner = minerY
		}
		if h > 0 {
			b.ParentHash = fixtureHash("block", h-1)
			c.addTx(b, legacyTransfer(h))
			c.addTx(b, tokenCall(h))
			if h == 3 {
				c.addTx(b, creation(h))
			}
		}
		c.Blocks = append(c.Blocks, b)
	}
	return c
}

func legacyTransfer(h uint64) *model.Transaction {
	to := addrB
	return &model.Transaction{
		Hash: fixtureHash("transfer", h), Type: 0, ChainID: big.NewInt(1), Nonce: h,
		From: addrA, To: &to, Value: new(big.Int).SetUint64(h), Gas: 21000,
		GasPrice: big.NewInt(2_000_000_000), GasFeeCap: big.NewInt(2_000_000_000), GasTipCap: big.NewInt(2_000_000_000),
		Signature: model.Signature{V: big.NewInt(37), R: big.NewInt(1), S: big.NewInt(1)},
	}
}

func tokenCall(h uint64) *model.Transaction {
	to := addrC
	return &model.Transaction{
		Hash: fixtureHash("call", h), Type: 2, ChainID: big.NewInt(1), Nonce: h,
		From: addrB, To: &to, Value: new(big.Int), Gas: 60000,
		GasPrice: big.NewInt(3_000_000_000), GasFeeCap: big.NewInt(3_000_000_000), GasTipCap: big.NewInt(1_000_000_000),
		Input:     []byte{0xa9, 0x05, 0x9c, 0xbb, byte(h)},
		Signature: model.Signature{V: big.NewInt(0), R: big.NewInt(2), S: big.NewInt(2)},
	}
}

func creation(h uint64) *model.Transaction {
	return &model.Transaction{
		Hash: fixtureHash("create", h), Type: 0, ChainID: big.NewInt(1), Nonce: 100 + h,
		From: addrA, Value: new(big.Int), Gas: 100000,
		GasPrice: big.NewInt(2_000_000_000), GasFeeCap: big.NewInt(2_000_000_000), GasTipCap: big.NewInt(2_000_000_000),
		Input:     []byte{0x60, 0x80},
		Signature: model.Signature{V: big.NewInt(37), R: big.NewInt(3), S: big.NewInt(3)},
	}
}

// addTx appends tx to b with its position and a receipt.
func (c *chain) addTx(b *model.Block, tx *model.Transaction) {
	tx.BlockHash, tx.BlockNumber, tx.Index = b.Hash, b.Number, uint(len(b.Transactions))
	b.Transactions = append(b.Transactions, tx)
	b.GasUsed += tx.Gas

	r := &model.Receipt{
		Type: tx.Type, Status: model.ReceiptStatusSuccessful, GasUsed: tx.Gas,
		CumulativeGasUsed: b.GasUsed, EffectiveGasPrice: tx.GasFeeCap,
		TxHash: tx.Hash, TxIndex: tx.Index, BlockHash: b.Hash, BlockNumber: b.Number,
		Logs: []*model.Log{},
	}
	switch {
	case tx.To == nil:
		addr := created
		r.ContractAddress = &addr
	case *tx.To == addrC && b.Number == 2:
		r.Status = model.ReceiptStatusFailed
	case *tx.To == addrC:
		r.Logs = append(r.Logs, &model.Log{
			Address:     addrC,
			Topics:      []common.Hash{transferTopic, common.BytesToHash(addrB.Bytes()), common.BytesToHash(addrA.Bytes())},
			Data:        common.LeftPadBytes(new(big.Int).SetUint64(b.Number).Bytes(), 32),
			BlockNumber: b.Number, BlockHash: b.Hash, TxHash: tx.Hash, TxIndex: tx.Index,
			Index: uint(c.logsInBlock(b.Number)),
		})
	}
	c.Receipts = append(c.Receipts, r)
}

func (c *chain) logsInBlock(n uint64) int {
	count := 0
	for _, r := range c.Receipts {
		if r.BlockNumber == n {
			count += len(r.Logs)
		}
	}
	return count
}

// receipt returns the fixture receipt of a transaction.
func (c *chain) receipt(tx common.Hash) *model.Receipt {
	for _, r := range c.Receipts {
		if r.TxHash == tx {
			return r
		}
	}
	return nil
}

// logs returns the fixture logs of blocks from..to, in order.
func (c *chain) logs(from, to uint64) []*model.Log {
	var out []*model.Log
	for _, r := range c.Receipts {
		if r.BlockNumber >= from && r.BlockNumber <= to {
			out = append(out, r.Logs...)
		}
	}
	return out
}

// head returns the highest fixture height.
func (c *chain) head() uint64 { return uint64(len(c.Blocks) - 1) }

// write stores the chain the way ingest does: block, receipts, logs (when
// the store indexes logs) and the cursor.
func (c *chain) write(t *testing.T, s blockStore) {
	t.Helper()
	ctx := context.Background()
	for _, b := range c.Blocks {
		if err := s.SetBlock(ctx, b); err != nil {
			t.Fatalf("SetBlock(%d): %v", b.Number, err)
		}
	}
	for _, r := range c.Receipts {
		if err := s.SetReceipt(ctx, r); err != nil {
			t.Fatalf("SetReceipt(%s): %v", r.TxHash.Hex(), err)
		}
		if lw, ok := s.(port.LogWriter); ok && len(r.Logs) > 0 {
			if err := lw.IndexLogs(ctx, r.Logs); err != nil {
				t.Fatalf("IndexLogs(%s): %v", r.TxHash.Hex(), err)
			}
		}
	}
	if w, ok := s.(port.Writer); ok {
		if err := w.SetLatestHeight(ctx, c.head()); err != nil {
			t.Fatalf("SetLatestHeight: %v", err)
		}
	}
}
