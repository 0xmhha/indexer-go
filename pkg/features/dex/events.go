package dex

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/0xmhha/indexer-go/pkg/core/model"
)

// Event topics of the three venue kinds. Enums of the order manager are
// uint8 in event signatures.
var (
	// Uniswap V3
	TopicV3PoolCreated = sig("PoolCreated(address,address,uint24,int24,address)")
	TopicV3Initialize  = sig("Initialize(uint160,int24)")
	TopicV3Swap        = sig("Swap(address,address,int256,int256,uint160,uint128,int24)")
	TopicV3Mint        = sig("Mint(address,address,int24,int24,uint128,uint256,uint256)")
	TopicV3Burn        = sig("Burn(address,int24,int24,uint128,uint256,uint256)")

	// Uniswap V2
	TopicV2PairCreated = sig("PairCreated(address,address,address,uint256)")
	TopicV2Swap        = sig("Swap(address,uint256,uint256,uint256,uint256,address)")
	TopicV2Sync        = sig("Sync(uint112,uint112)")
	TopicV2Mint        = sig("Mint(address,uint256,uint256)")
	TopicV2Burn        = sig("Burn(address,uint256,uint256,address)")

	// Perpetual engine and order manager
	TopicPerpMarketCreated       = sig("MarketCreated(uint32,address,address,uint32)")
	TopicPerpOrderCreated        = sig("OrderCreated(bytes32,address,uint32,uint8,uint8,uint256,uint256)")
	TopicPerpOrderPartiallyFill  = sig("OrderPartiallyFilled(bytes32,uint256,uint256,uint256,uint256)")
	TopicPerpOrderCancelled      = sig("OrderCancelled(bytes32,address,string)")
	TopicPerpOrderExpired        = sig("OrderExpired(bytes32,address,uint256)")
	TopicPerpOrderModified       = sig("OrderModified(bytes32,address,uint256,uint256)")
	TopicPerpMarketOrderExecuted = sig("MarketOrderExecuted(address,uint32,uint8,uint128,uint128)")
	TopicPerpOrdersMatched       = sig("OrdersMatched(bytes32,bytes32,uint128,uint128)")
)

func sig(s string) common.Hash { return crypto.Keccak256Hash([]byte(s)) }

// event is a log with its words decoded on demand. A log whose topics or
// data are shorter than its event needs is not that event (ok reports it).
type event struct {
	*model.Log
}

// is reports whether the log is event topic with n topics and at least
// words data words.
func (e event) is(topic common.Hash, n, words int) bool {
	return len(e.Topics) == n && e.Topics[0] == topic && len(e.Data) >= 32*words
}

// word returns data word i.
func (e event) word(i int) []byte { return e.Data[32*i : 32*(i+1)] }

func (e event) uint(i int) *big.Int { return new(big.Int).SetBytes(e.word(i)) }

func (e event) int(i int) *big.Int { return signed(e.word(i)) }

func (e event) address(i int) common.Address { return common.BytesToAddress(e.word(i)) }

func (e event) topicAddress(i int) common.Address { return common.BytesToAddress(e.Topics[i].Bytes()) }

func (e event) topicUint(i int) *big.Int { return new(big.Int).SetBytes(e.Topics[i].Bytes()) }

func (e event) topicInt(i int) *big.Int { return signed(e.Topics[i].Bytes()) }

// signed reads a 32-byte two's complement integer.
func signed(b []byte) *big.Int {
	v := new(big.Int).SetBytes(b)
	if len(b) > 0 && b[0]&0x80 != 0 {
		v.Sub(v, new(big.Int).Lsh(big.NewInt(1), 256))
	}
	return v
}

// int32Of narrows a decoded int24 or uint32.
func int32Of(v *big.Int) int32 { return int32(v.Int64()) }
