package arc

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/core/model"
)

var (
	legacyAuthority = common.HexToAddress("0x1800000000000000000000000000000000000000")
	transferTopic   = crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)"))
	legacyTransfer  = crypto.Keccak256Hash([]byte("NativeCoinTransferred(address,address,uint256)"))
	legacyMint      = crypto.Keccak256Hash([]byte("NativeCoinMinted(address,uint256)"))
	legacyBurn      = crypto.Keccak256Hash([]byte("NativeCoinBurned(address,uint256)"))
)

// Accounting reads native moves at 18 decimals from Arc's system emitter
// (the legacy authority before Zero5). It does not also apply tx.value or
// 6-decimal ERC-20 USDC logs: both would count the same movement twice.
// Arc credits the miner with all execution gas fees, including the base fee.
type Accounting struct{}

func (Accounting) ConcurrentAccounting() bool { return true }

func (Accounting) NativeDeltas(_ context.Context, b *model.Block, rs []*model.Receipt, _ chains.AccountingEnv) ([]chains.BalanceDelta, error) {
	if len(b.Withdrawals) > 0 {
		return nil, fmt.Errorf("arc: unexpected Ethereum beacon withdrawals")
	}
	var out []chains.BalanceDelta
	fees := new(big.Int)
	feeBlock := *b
	feeBlock.BaseFee = nil // use full execution fee credit without changing the stored header
	for i, tx := range b.Transactions {
		if i >= len(rs) || rs[i] == nil {
			return nil, fmt.Errorf("arc: missing receipt for transaction %d", i)
		}
		r := rs[i]
		out = append(out, chains.TxFeeDeltas(&feeBlock, tx, r, fees)...)
		if r.Status != 1 {
			continue
		}
		for _, log := range r.Logs {
			from, to, value, ok, err := nativeMove(log)
			if err != nil {
				return nil, err
			}
			if !ok || value.Sign() == 0 {
				continue
			}
			if from != (common.Address{}) {
				out = append(out, chains.BalanceDelta{Address: from, Delta: new(big.Int).Neg(value), TxHash: tx.Hash, Reason: "transfer"})
			}
			if to != (common.Address{}) {
				out = append(out, chains.BalanceDelta{Address: to, Delta: value, TxHash: tx.Hash, Reason: "transfer"})
			}
		}
	}
	if fees.Sign() > 0 {
		out = append(out, chains.BalanceDelta{Address: b.Miner, Delta: fees, Reason: "gas fees"})
	}
	return out, nil
}

func nativeMove(log *model.Log) (from, to common.Address, value *big.Int, ok bool, err error) {
	if log == nil || len(log.Topics) == 0 {
		return
	}
	topic, count := log.Topics[0], 0
	if log.Address == NativeEmitter && topic == transferTopic {
		count = 3
	} else if log.Address == legacyAuthority {
		switch topic {
		case legacyTransfer:
			count = 3
		case legacyMint, legacyBurn:
			count = 2
		}
	}
	if count == 0 {
		return
	}
	if len(log.Topics) != count || len(log.Data) != 32 {
		err = fmt.Errorf("arc: malformed native transfer event")
		return
	}
	value, ok = new(big.Int).SetBytes(log.Data), true
	if count == 3 {
		from, to = common.BytesToAddress(log.Topics[1].Bytes()), common.BytesToAddress(log.Topics[2].Bytes())
	} else if topic == legacyMint {
		to = common.BytesToAddress(log.Topics[1].Bytes())
	} else {
		from = common.BytesToAddress(log.Topics[1].Bytes())
	}
	return
}
