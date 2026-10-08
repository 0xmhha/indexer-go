package testchain

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
)

// StableNet mode makes the chain follow go-stablenet's (Anzeon, WBFT) rules
// where they change what an indexer sees:
//
//   - every native value move emits Transfer(from, to, value) from the
//     NativeCoinAdapter contract (core/vm/evm.go), including internal moves,
//     mints (from the zero address) and burns (to the zero address); failed
//     transactions emit nothing;
//   - the tip is the governance gas tip in the WBFT extra data unless the
//     sender is an authorized account, whose transaction ends with an
//     AuthorizedTxExecuted log (core/state_transition.go);
//   - the tip goes to the coinbase and the base fee of the block is split
//     among the previous epoch's validators by diligence, the remainder
//     (dust) to the coinbase (consensus/wbft/engine distributeBaseFee);
//   - headers carry WBFT extra data with the previous block's seals and, on
//     epoch blocks, the next epoch's candidates and validators.
//
// Seals of the current round are left empty and the round is 0, so the WBFT
// block hash equals the plain header hash.

// StableNet addresses and event signatures (go-stablenet params).
var (
	NativeCoinAdapterAddress  = common.HexToAddress("0x0000000000000000000000000000000000001000")
	AccountManagerAddress     = common.HexToAddress("0x0000000000000000000000000000000000B00003")
	SigAuthorizedTxExecuted   = crypto.Keccak256Hash([]byte("AuthorizedTxExecuted()"))
	StableNetClientVersion    = "Gstable/v1.1.0-testchain/linux-amd64/go1.24"
	defaultStableNetGasTipWei = big.NewInt(27_600_000_000_000)
)

// Candidate is a WBFT validator candidate.
type Candidate struct {
	Address   common.Address
	Diligence uint64 // base value; epoch block E uses Diligence + E
}

// StableNetConfig configures StableNet mode.
type StableNetConfig struct {
	Coinbase    common.Address
	EpochLength uint64      // epoch blocks are multiples of it (genesis included)
	Candidates  []Candidate // all are validators, in order
	GasTip      *big.Int    // governance gas tip in the header (default 27.6 Twei)
}

// NativeTransfer is a native value move inside a transaction (internal call,
// mint from the zero address, burn to the zero address).
type NativeTransfer struct {
	From, To common.Address
	Value    *big.Int
}

// NewStableNetChain creates a chain in StableNet mode.
func NewStableNetChain(chainID int64, alloc map[common.Address]*big.Int, cfg StableNetConfig) *Chain {
	if cfg.EpochLength == 0 {
		cfg.EpochLength = 10
	}
	if cfg.GasTip == nil {
		cfg.GasTip = new(big.Int).Set(defaultStableNetGasTipWei)
	}
	return newChain(chainID, alloc, &cfg)
}

// StableNet reports whether the chain is in StableNet mode.
func (c *Chain) StableNet() bool { return c.sn != nil }

// StableNetConfig returns the StableNet mode configuration (nil otherwise).
func (c *Chain) StableNetConfig() *StableNetConfig { return c.sn }

// PrevRound is the round in which block n was finalized (recorded in block
// n+1).
func (cfg *StableNetConfig) PrevRound(n uint64) uint32 { return cfg.prevRound(n) }

// epochInfoAt returns the candidates' diligence recorded in epoch block e.
func (cfg *StableNetConfig) diligence(e uint64, i int) uint64 {
	return cfg.Candidates[i].Diligence + e
}

// lastEpochBlock returns the last epoch block at or below n.
func (cfg *StableNetConfig) lastEpochBlock(n uint64) uint64 {
	return n - n%cfg.EpochLength
}

// txPrice returns the effective gas price under the Anzeon rule.
func (c *Chain) txPrice(tx *types.Transaction, s TxSpec, baseFee *big.Int) *big.Int {
	tip := tx.GasTipCap()
	if c.sn != nil && !s.Authorized {
		tip = c.sn.GasTip
	}
	price := new(big.Int).Add(tip, baseFee)
	if price.Cmp(tx.GasFeeCap()) > 0 {
		price = new(big.Int).Set(tx.GasFeeCap())
	}
	return price
}

// nativeLogs returns the logs a successful transaction emits for its native
// value moves in StableNet mode: the top-level value first, then internal
// moves.
func (c *Chain) nativeLogs(from common.Address, to *common.Address, value *big.Int, s TxSpec) []*types.Log {
	if c.sn == nil {
		return nil
	}
	var out []*types.Log
	transfer := func(f, t common.Address, v *big.Int) {
		out = append(out, &types.Log{
			Address: NativeCoinAdapterAddress,
			Topics:  []common.Hash{SigTransfer, addrTopic(f), addrTopic(t)},
			Data:    word(v),
		})
	}
	if to != nil && value.Sign() > 0 {
		transfer(from, *to, value)
	}
	for _, nt := range s.NativeTransfers {
		transfer(nt.From, nt.To, nt.Value)
	}
	return out
}

// settleBlock applies the end-of-block fee rules to state: base fee
// distribution in StableNet mode (burned otherwise).
func (c *Chain) settleBlock(state map[common.Address]*big.Int, number, gasUsed uint64, baseFee *big.Int, coinbase common.Address) {
	if c.sn == nil || number == 0 || gasUsed == 0 {
		return
	}
	total := new(big.Int).Mul(baseFee, new(big.Int).SetUint64(gasUsed))
	e := c.sn.lastEpochBlock(number - 1)
	var sum uint64
	for i := range c.sn.Candidates {
		sum += c.sn.diligence(e, i)
	}
	dust := new(big.Int).Set(total)
	if sum != 0 {
		for i, cand := range c.sn.Candidates {
			share := new(big.Int).Mul(total, new(big.Int).SetUint64(c.sn.diligence(e, i)))
			share.Div(share, new(big.Int).SetUint64(sum))
			if share.Sign() == 0 {
				continue
			}
			dust.Sub(dust, share)
			add(state, cand.Address, share)
		}
	}
	if dust.Sign() > 0 {
		add(state, coinbase, dust)
	}
}

// RLP shapes of go-stablenet's WBFT extra data (core/types/istanbul.go).
type wbftSeal struct {
	Sealers   []byte
	Signature []byte
}

type wbftCandidate struct {
	Addr      common.Address
	Diligence uint64
}

type wbftEpochInfo struct {
	Candidates    []*wbftCandidate
	Validators    []uint32
	BLSPublicKeys [][]byte
}

type wbftExtra struct {
	VanityData        []byte
	RandaoReveal      []byte
	PrevRound         uint32
	PrevPreparedSeal  *wbftSeal
	PrevCommittedSeal *wbftSeal
	Round             uint32
	PreparedSeal      *wbftSeal
	CommittedSeal     *wbftSeal
	GasTip            *big.Int
	EpochInfo         *wbftEpochInfo
}

// PrevRound and the seal bitmaps of block n's predecessor are a pure
// function of the height, so tests can predict them: every 7th block went
// through a round change, and on every 5th block the validator at index
// (n % validators) did not commit.
func (cfg *StableNetConfig) prevRound(n uint64) uint32 {
	if n%7 == 0 {
		return 1
	}
	return 0
}

// Committers returns the validator indexes whose commit seal for block n
// appears in block n+1.
func (cfg *StableNetConfig) Committers(n uint64) []uint32 {
	var out []uint32
	for i := range cfg.Candidates {
		if n%5 == 0 && uint64(i) == n%uint64(len(cfg.Candidates)) {
			continue
		}
		out = append(out, uint32(i))
	}
	return out
}

func sealerSet(indexes []uint32) []byte {
	var s []byte
	for _, i := range indexes {
		for len(s) <= int(i/8) {
			s = append(s, 0)
		}
		s[i/8] |= 1 << (i % 8)
	}
	return s
}

// headerExtra builds block n's WBFT extra data; vanity keeps the caller's
// extra bytes so forks stay distinguishable.
func (c *Chain) headerExtra(n uint64, vanity []byte) []byte {
	cfg := c.sn
	all := make([]uint32, len(cfg.Candidates))
	for i := range all {
		all[i] = uint32(i)
	}
	x := wbftExtra{VanityData: vanity, RandaoReveal: []byte{}, GasTip: cfg.GasTip}
	if n > 0 {
		x.PrevRound = cfg.prevRound(n - 1)
		x.PrevPreparedSeal = &wbftSeal{Sealers: sealerSet(all), Signature: make([]byte, 96)}
		x.PrevCommittedSeal = &wbftSeal{Sealers: sealerSet(cfg.Committers(n - 1)), Signature: make([]byte, 96)}
	}
	if n%cfg.EpochLength == 0 {
		info := &wbftEpochInfo{Validators: all}
		for i, cand := range cfg.Candidates {
			info.Candidates = append(info.Candidates, &wbftCandidate{Addr: cand.Address, Diligence: cfg.diligence(n, i)})
			info.BLSPublicKeys = append(info.BLSPublicKeys, make([]byte, 48))
		}
		x.EpochInfo = info
	}
	enc, err := rlp.EncodeToBytes(&x)
	if err != nil {
		panic(err)
	}
	return enc
}
