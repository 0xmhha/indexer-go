package testchain

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// DEX event signatures (Uniswap V3 and V2, and the perpetual engine and
// order manager, whose enums are uint8 in signatures).
var (
	SigV3PoolCreated   = crypto.Keccak256Hash([]byte("PoolCreated(address,address,uint24,int24,address)"))
	SigV3Initialize    = crypto.Keccak256Hash([]byte("Initialize(uint160,int24)"))
	SigV3Swap          = crypto.Keccak256Hash([]byte("Swap(address,address,int256,int256,uint160,uint128,int24)"))
	SigV3Mint          = crypto.Keccak256Hash([]byte("Mint(address,address,int24,int24,uint128,uint256,uint256)"))
	SigV2PairCreated   = crypto.Keccak256Hash([]byte("PairCreated(address,address,address,uint256)"))
	SigV2Swap          = crypto.Keccak256Hash([]byte("Swap(address,uint256,uint256,uint256,uint256,address)"))
	SigV2Sync          = crypto.Keccak256Hash([]byte("Sync(uint112,uint112)"))
	SigV2Mint          = crypto.Keccak256Hash([]byte("Mint(address,uint256,uint256)"))
	SigPerpMarket      = crypto.Keccak256Hash([]byte("MarketCreated(uint32,address,address,uint32)"))
	SigPerpOrder       = crypto.Keccak256Hash([]byte("OrderCreated(bytes32,address,uint32,uint8,uint8,uint256,uint256)"))
	SigPerpPartialFill = crypto.Keccak256Hash([]byte("OrderPartiallyFilled(bytes32,uint256,uint256,uint256,uint256)"))
	SigPerpMatched     = crypto.Keccak256Hash([]byte("OrdersMatched(bytes32,bytes32,uint128,uint128)"))
	SigPerpMarketOrder = crypto.Keccak256Hash([]byte("MarketOrderExecuted(address,uint32,uint8,uint128,uint128)"))
)

// DEXTrade is a trade the DEX scenario makes, as the indexer should record
// it: the taker's side, raw amounts and the price as quote per base times
// 1e18.
type DEXTrade struct {
	Block        uint64
	Market       common.Address
	MarketID     uint64
	Venue        string // uniswap_v2, uniswap_v3, perp_orderbook
	Buy          bool
	Base, Quote  *big.Int
	Price        *big.Int
	Taker, Maker common.Address
}

// DEXScenario is a chain with a Uniswap V3 pool, a Uniswap V2 pair and a
// perpetual order book market trading, plus a foreign factory and pool
// emitting the same events, which the indexer must ignore.
type DEXScenario struct {
	Scenario
	V3Factory, V2Factory, Engine, OrderManager common.Address
	V3Pool, V2Pair                             common.Address
	FakeFactory, FakePool                      common.Address
	TokenA, TokenB                             common.Address
	PerpMarket                                 uint64
	// Trades are every trade, in chain order.
	Trades []DEXTrade
}

// e18 returns n * 1e18.
func e18(n int64) *big.Int { return new(big.Int).Mul(big.NewInt(n), big.NewInt(1e18)) }

// signedWord encodes a two's complement int256.
func signedWord(v int64) []byte {
	b := big.NewInt(v)
	if v < 0 {
		b.Add(b, new(big.Int).Lsh(big.NewInt(1), 256))
	}
	return word(b)
}

func n(v int64) *big.Int { return big.NewInt(v) }

// BuildDEX builds the DEX scenario.
func BuildDEX() *DEXScenario {
	accts := make([]Account, 4)
	alloc := map[common.Address]*big.Int{}
	for i := range accts {
		accts[i] = NewAccount(uint64(40 + i))
		alloc[accts[i].Address] = ether(1000)
	}
	op, alice, bob, lp := accts[0], accts[1], accts[2], accts[3]
	ch := NewChain(DefaultChainID, alloc)
	sc := &DEXScenario{
		Scenario:     Scenario{Chain: ch, Accounts: accts},
		V3Factory:    common.HexToAddress("0x00000000000000000000000000000000000F3001"),
		V2Factory:    common.HexToAddress("0x00000000000000000000000000000000000F2001"),
		Engine:       common.HexToAddress("0x00000000000000000000000000000000000E0001"),
		OrderManager: common.HexToAddress("0x00000000000000000000000000000000000E0002"),
		V3Pool:       common.HexToAddress("0x00000000000000000000000000000000000B3001"),
		V2Pair:       common.HexToAddress("0x00000000000000000000000000000000000B2001"),
		FakeFactory:  common.HexToAddress("0x00000000000000000000000000000000000BAD01"),
		FakePool:     common.HexToAddress("0x00000000000000000000000000000000000BAD02"),
		TokenA:       common.HexToAddress("0x000000000000000000000000000000000000A0A0"),
		TokenB:       common.HexToAddress("0x000000000000000000000000000000000000B0B0"),
		PerpMarket:   7,
	}
	router := common.HexToAddress("0x00000000000000000000000000000000000C0001")
	gp := big.NewInt(2_000_000_000)
	call := func(from Account, to common.Address, logs ...*types.Log) TxSpec {
		return TxSpec{From: from, Tx: &types.LegacyTx{To: &to, Gas: 300000, GasPrice: gp}, GasUsed: 150000, Logs: logs}
	}
	q96 := new(big.Int).Lsh(big.NewInt(1), 96)
	v3Swap := func(pool, recipient common.Address, a0, a1, tick int64) *types.Log {
		return &types.Log{Address: pool, Topics: []common.Hash{SigV3Swap, addrTopic(router), addrTopic(recipient)},
			Data: concat(signedWord(a0), signedWord(a1), word(q96), word(n(1_000_000)), signedWord(tick))}
	}
	trade := func(tr DEXTrade) {
		tr.Block = ch.Head()
		sc.Trades = append(sc.Trades, tr)
	}

	// 1: the V3 factory creates the pool, which is initialized at price 1.
	ch.AddBlock(call(op, sc.V3Factory,
		&types.Log{Address: sc.V3Factory, Topics: []common.Hash{SigV3PoolCreated, addrTopic(sc.TokenA), addrTopic(sc.TokenB), common.BigToHash(n(3000))},
			Data: concat(signedWord(60), addrWord(sc.V3Pool))},
		&types.Log{Address: sc.V3Pool, Topics: []common.Hash{SigV3Initialize}, Data: concat(word(q96), signedWord(0))},
	))

	// 2: liquidity in the V3 pool; the V2 factory creates the pair, funded.
	ch.AddBlock(
		call(lp, sc.V3Pool, &types.Log{Address: sc.V3Pool,
			Topics: []common.Hash{SigV3Mint, addrTopic(lp.Address), common.BytesToHash(signedWord(-600)), common.BytesToHash(signedWord(600))},
			Data:   concat(addrWord(router), word(n(1_000_000)), word(n(5000)), word(n(5000)))}),
		call(op, sc.V2Factory,
			&types.Log{Address: sc.V2Factory, Topics: []common.Hash{SigV2PairCreated, addrTopic(sc.TokenA), addrTopic(sc.TokenB)},
				Data: concat(addrWord(sc.V2Pair), word(n(1)))},
			&types.Log{Address: sc.V2Pair, Topics: []common.Hash{SigV2Mint, addrTopic(router)}, Data: concat(word(n(10000)), word(n(20000)))},
			&types.Log{Address: sc.V2Pair, Topics: []common.Hash{SigV2Sync}, Data: concat(word(n(10000)), word(n(20000)))},
		),
	)

	// 3: alice buys 1000 A for 1010 B on V3; bob sells 100 A for 196 B on V2.
	ch.AddBlock(
		call(alice, router, v3Swap(sc.V3Pool, alice.Address, -1000, 1010, 19)),
		call(bob, router,
			&types.Log{Address: sc.V2Pair, Topics: []common.Hash{SigV2Swap, addrTopic(router), addrTopic(bob.Address)},
				Data: concat(word(n(100)), word(n(0)), word(n(0)), word(n(196)))},
			&types.Log{Address: sc.V2Pair, Topics: []common.Hash{SigV2Sync}, Data: concat(word(n(10100)), word(n(19804)))},
		),
	)
	trade(DEXTrade{Market: sc.V3Pool, Venue: "uniswap_v3", Buy: true, Base: n(1000), Quote: n(1010),
		Price: new(big.Int).Div(e18(101), n(100)), Taker: alice.Address})
	trade(DEXTrade{Market: sc.V2Pair, Venue: "uniswap_v2", Base: n(100), Quote: n(196),
		Price: new(big.Int).Div(e18(196), n(100)), Taker: bob.Address})

	// 4: the engine creates perpetual market 7; three limit orders.
	maker, taker, solo := common.HexToHash("0x01"), common.HexToHash("0x02"), common.HexToHash("0x03")
	order := func(id common.Hash, trader common.Address, side, size, price int64) *types.Log {
		return &types.Log{Address: sc.OrderManager, Topics: []common.Hash{SigPerpOrder, id, addrTopic(trader), common.BigToHash(n(7))},
			Data: concat(word(n(side)), word(n(1)), word(n(size)), word(e18(price)))}
	}
	ch.AddBlock(
		call(op, sc.Engine, &types.Log{Address: sc.Engine,
			Topics: []common.Hash{SigPerpMarket, common.BigToHash(n(7)), {}, addrTopic(sc.TokenB)}, Data: word(n(20))}),
		call(bob, sc.OrderManager, order(maker, bob.Address, 1, 10, 50)),
		call(alice, sc.OrderManager, order(taker, alice.Address, 0, 10, 51)),
		call(alice, sc.OrderManager, order(solo, alice.Address, 0, 5, 49)),
	)

	// 5: the operator matches 4 at 50, fills solo at 48 and executes bob's
	// market order (short 2 at 47).
	fill := func(id common.Hash, size, total, remaining, price int64) *types.Log {
		return &types.Log{Address: sc.OrderManager, Topics: []common.Hash{SigPerpPartialFill, id},
			Data: concat(word(n(size)), word(n(total)), word(n(remaining)), word(e18(price)))}
	}
	ch.AddBlock(
		call(op, sc.OrderManager,
			fill(maker, 4, 4, 6, 50), fill(taker, 4, 4, 6, 50),
			&types.Log{Address: sc.OrderManager, Topics: []common.Hash{SigPerpMatched, maker, taker}, Data: concat(word(n(4)), word(e18(50)))}),
		call(op, sc.OrderManager, fill(solo, 5, 5, 0, 48)),
		call(op, sc.OrderManager, &types.Log{Address: sc.OrderManager,
			Topics: []common.Hash{SigPerpMarketOrder, addrTopic(bob.Address), common.BigToHash(n(7))},
			Data:   concat(word(n(1)), word(n(2)), word(e18(47)))}),
	)
	trade(DEXTrade{Market: sc.OrderManager, MarketID: 7, Venue: "perp_orderbook", Buy: true, Base: n(4), Quote: n(200),
		Price: e18(50), Taker: alice.Address, Maker: bob.Address})
	trade(DEXTrade{Market: sc.OrderManager, MarketID: 7, Venue: "perp_orderbook", Buy: true, Base: n(5), Quote: n(240),
		Price: e18(48), Taker: alice.Address})
	trade(DEXTrade{Market: sc.OrderManager, MarketID: 7, Venue: "perp_orderbook", Base: n(2), Quote: n(94),
		Price: e18(47), Taker: bob.Address})

	// 6: a foreign factory and pool emit the same events; alice sells 500 A
	// for 490 B on the real pool.
	ch.AddBlock(
		call(op, sc.FakeFactory,
			&types.Log{Address: sc.FakeFactory, Topics: []common.Hash{SigV3PoolCreated, addrTopic(sc.TokenA), addrTopic(sc.TokenB), common.BigToHash(n(3000))},
				Data: concat(signedWord(60), addrWord(sc.FakePool))},
			v3Swap(sc.FakePool, alice.Address, -1, 1, 0)),
		call(alice, router, v3Swap(sc.V3Pool, alice.Address, 500, -490, -3)),
	)
	trade(DEXTrade{Market: sc.V3Pool, Venue: "uniswap_v3", Base: n(500), Quote: n(490),
		Price: new(big.Int).Div(e18(98), n(100)), Taker: alice.Address})

	// 7-8: plain blocks.
	for range 2 {
		ch.AddBlock(TxSpec{From: op, Tx: &types.LegacyTx{To: &lp.Address, Value: ether(1), Gas: 21000, GasPrice: gp}})
	}
	return sc
}

// SigPerpCancelled is the order manager's OrderCancelled event.
var SigPerpCancelled = crypto.Keccak256Hash([]byte("OrderCancelled(bytes32,address,string)"))

// Fork replaces the blocks after the perpetual orders (block 4) with a
// competing branch, as a reorganization does, and updates Trades:
//
//   - 5: the operator matches the two limit orders in full at 50 and alice
//     cancels her third order;
//   - 6: bob buys 50 A for 110 B on the V2 pair;
//   - 7: a new V3 position in [-60, 60], then alice sells 200 A for 195 B;
//   - 8-10: plain blocks.
//
// The new branch is longer than the old one, so the indexer meets it.
func (sc *DEXScenario) Fork() {
	const keep = 4
	ch := sc.Chain
	ch.Reorg(keep)
	kept := sc.Trades[:0:0]
	for _, tr := range sc.Trades {
		if tr.Block <= keep {
			kept = append(kept, tr)
		}
	}
	sc.Trades = kept
	op, alice, bob, lp := sc.Accounts[0], sc.Accounts[1], sc.Accounts[2], sc.Accounts[3]
	router := common.HexToAddress("0x00000000000000000000000000000000000C0001")
	gp := big.NewInt(2_000_000_000)
	call := func(from Account, to common.Address, logs ...*types.Log) TxSpec {
		return TxSpec{From: from, Tx: &types.LegacyTx{To: &to, Gas: 300000, GasPrice: gp}, GasUsed: 150000, Logs: logs}
	}
	trade := func(tr DEXTrade) {
		tr.Block = ch.Head()
		sc.Trades = append(sc.Trades, tr)
	}
	maker, taker, solo := common.HexToHash("0x01"), common.HexToHash("0x02"), common.HexToHash("0x03")
	fill := func(id common.Hash, size, total, remaining, price int64) *types.Log {
		return &types.Log{Address: sc.OrderManager, Topics: []common.Hash{SigPerpPartialFill, id},
			Data: concat(word(n(size)), word(n(total)), word(n(remaining)), word(e18(price)))}
	}

	ch.AddBlock(
		call(op, sc.OrderManager,
			fill(maker, 10, 10, 0, 50), fill(taker, 10, 10, 0, 50),
			&types.Log{Address: sc.OrderManager, Topics: []common.Hash{SigPerpMatched, maker, taker}, Data: concat(word(n(10)), word(e18(50)))}),
		call(alice, sc.OrderManager, &types.Log{Address: sc.OrderManager,
			Topics: []common.Hash{SigPerpCancelled, solo, addrTopic(alice.Address)}, Data: concat(word(n(32)), word(n(0)))}),
	)
	trade(DEXTrade{Market: sc.OrderManager, MarketID: 7, Venue: "perp_orderbook", Buy: true, Base: n(10), Quote: n(500),
		Price: e18(50), Taker: alice.Address, Maker: bob.Address})

	ch.AddBlock(call(bob, router,
		&types.Log{Address: sc.V2Pair, Topics: []common.Hash{SigV2Swap, addrTopic(router), addrTopic(bob.Address)},
			Data: concat(word(n(0)), word(n(110)), word(n(50)), word(n(0)))},
		&types.Log{Address: sc.V2Pair, Topics: []common.Hash{SigV2Sync}, Data: concat(word(n(10050)), word(n(19914)))},
	))
	trade(DEXTrade{Market: sc.V2Pair, Venue: "uniswap_v2", Buy: true, Base: n(50), Quote: n(110),
		Price: new(big.Int).Div(e18(110), n(50)), Taker: bob.Address})

	q96 := new(big.Int).Lsh(big.NewInt(1), 96)
	ch.AddBlock(
		call(lp, sc.V3Pool, &types.Log{Address: sc.V3Pool,
			Topics: []common.Hash{SigV3Mint, addrTopic(lp.Address), common.BytesToHash(signedWord(-60)), common.BytesToHash(signedWord(60))},
			Data:   concat(addrWord(router), word(n(500_000)), word(n(1500)), word(n(1500)))}),
		call(alice, router, &types.Log{Address: sc.V3Pool, Topics: []common.Hash{SigV3Swap, addrTopic(router), addrTopic(alice.Address)},
			Data: concat(signedWord(200), signedWord(-195), word(q96), word(n(1_500_000)), signedWord(-5))}),
	)
	trade(DEXTrade{Market: sc.V3Pool, Venue: "uniswap_v3", Base: n(200), Quote: n(195),
		Price: new(big.Int).Div(e18(195), n(200)), Taker: alice.Address})

	for range 3 {
		ch.AddBlock(TxSpec{From: op, Tx: &types.LegacyTx{To: &lp.Address, Value: ether(2), Gas: 21000, GasPrice: gp}})
	}
}

// BuildDEXLoad builds a chain for load tests: block 1 creates and funds the
// DEX scenario's V2 pair, and each of the next blocks holds swaps
// transactions, each one swap of the pair (alternately buying and selling a
// little). Every block is built; the visible head is block 1, so a test
// reveals blocks one at a time with SetHead.
func BuildDEXLoad(blocks, swaps int) *DEXScenario {
	accts := make([]Account, 4)
	alloc := map[common.Address]*big.Int{}
	for i := range accts {
		accts[i] = NewAccount(uint64(60 + i))
		alloc[accts[i].Address] = ether(1_000_000)
	}
	ch := NewChain(DefaultChainID, alloc)
	sc := &DEXScenario{
		Scenario:  Scenario{Chain: ch, Accounts: accts},
		V2Factory: common.HexToAddress("0x00000000000000000000000000000000000F2001"),
		V2Pair:    common.HexToAddress("0x00000000000000000000000000000000000B2001"),
		TokenA:    common.HexToAddress("0x000000000000000000000000000000000000A0A0"),
		TokenB:    common.HexToAddress("0x000000000000000000000000000000000000B0B0"),
	}
	router := common.HexToAddress("0x00000000000000000000000000000000000C0001")
	gp := big.NewInt(2_000_000_000)
	call := func(from Account, to common.Address, logs ...*types.Log) TxSpec {
		return TxSpec{From: from, Tx: &types.LegacyTx{To: &to, Gas: 300000, GasPrice: gp}, GasUsed: 120000, Logs: logs}
	}
	r0, r1 := big.NewInt(1_000_000_000), big.NewInt(2_000_000_000)
	ch.AddBlock(call(accts[0], sc.V2Factory,
		&types.Log{Address: sc.V2Factory, Topics: []common.Hash{SigV2PairCreated, addrTopic(sc.TokenA), addrTopic(sc.TokenB)},
			Data: concat(addrWord(sc.V2Pair), word(n(1)))},
		&types.Log{Address: sc.V2Pair, Topics: []common.Hash{SigV2Mint, addrTopic(router)}, Data: concat(word(r0), word(r1))},
		&types.Log{Address: sc.V2Pair, Topics: []common.Hash{SigV2Sync}, Data: concat(word(r0), word(r1))},
	))
	for b := 0; b < blocks; b++ {
		specs := make([]TxSpec, swaps)
		for i := range specs {
			trader := accts[1+(b*swaps+i)%3]
			var swap []byte
			if i%2 == 0 { // buy 100 A for 201 B
				swap = concat(word(n(0)), word(n(201)), word(n(100)), word(n(0)))
				r0, r1 = new(big.Int).Sub(r0, n(100)), new(big.Int).Add(r1, n(201))
			} else { // sell 100 A for 199 B
				swap = concat(word(n(100)), word(n(0)), word(n(0)), word(n(199)))
				r0, r1 = new(big.Int).Add(r0, n(100)), new(big.Int).Sub(r1, n(199))
			}
			specs[i] = call(trader, router,
				&types.Log{Address: sc.V2Pair, Topics: []common.Hash{SigV2Swap, addrTopic(router), addrTopic(trader.Address)}, Data: swap},
				&types.Log{Address: sc.V2Pair, Topics: []common.Hash{SigV2Sync}, Data: concat(word(r0), word(r1))})
		}
		ch.AddBlock(specs...)
	}
	ch.SetHead(1)
	return sc
}
