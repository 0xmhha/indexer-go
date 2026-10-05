package evm

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/rpc"

	"github.com/0xmhha/indexer-go/internal/testchain"
)

// BenchmarkDecodeTxs compares sequential and parallel decoding of the
// transactions of one 1000-transaction block (signature recovery dominates).
func BenchmarkDecodeTxs(b *testing.B) {
	sc := testchain.BuildLoad(1, 1000, 0)
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()
	c, err := rpc.Dial(srv.URL())
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	var raw json.RawMessage
	if err := c.CallContext(context.Background(), &raw, "eth_getBlockByNumber", hexutil.EncodeUint64(sc.Chain.Head()), true); err != nil {
		b.Fatal(err)
	}
	var body rpcBlock
	if err := json.Unmarshal(raw, &body); err != nil {
		b.Fatal(err)
	}
	p := New("bench")
	b.Run("sequential", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			for _, r := range body.Transactions {
				if _, err := p.decodeTx(r); err != nil {
					b.Fatal(err)
				}
			}
		}
	})
	b.Run("parallel", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := p.decodeTxs(body.Transactions); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkDecodeParts(b *testing.B) {
	sc := testchain.BuildLoad(1, 1000, 0)
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()
	c, _ := rpc.Dial(srv.URL())
	defer c.Close()
	var rawBlock, rawReceipts json.RawMessage
	n := hexutil.EncodeUint64(sc.Chain.Head())
	_ = c.CallContext(context.Background(), &rawBlock, "eth_getBlockByNumber", n, true)
	_ = c.CallContext(context.Background(), &rawReceipts, "eth_getBlockReceipts", n)
	b.Logf("block JSON %d KB, receipts JSON %d KB", len(rawBlock)/1024, len(rawReceipts)/1024)
	p := New("bench")
	b.Run("DecodeBlock", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := p.DecodeBlock(rawBlock); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("DecodeReceipts", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := p.DecodeReceipts(rawReceipts); err != nil {
				b.Fatal(err)
			}
		}
	})
}
