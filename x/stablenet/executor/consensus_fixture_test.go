package executor

import (
	"bytes"
	"encoding/json"
	"math/big"
	"os"
	"testing"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/wbft/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/crypto/bls"
	blscommon "github.com/ethereum/go-ethereum/crypto/bls/common"
	"github.com/ethereum/go-ethereum/rlp"
)

// TestGenerateIndependentConsensusFixture starts no node and executes no EVM.
// The fixed scalar keys below are public development fixtures, never operators.
func TestGenerateIndependentConsensusFixture(t *testing.T) {
	type validator struct {
		Address   common.Address
		PublicKey []byte
	}
	sets := make([][]validator, 2)
	keys := make([][]blscommon.SecretKey, 2)
	for epoch := range sets {
		for i := 0; i < 4; i++ {
			scalar := make([]byte, 32)
			scalar[31] = byte(1 + i + epoch*4)
			key, e := bls.SecretKeyFromBytes(scalar)
			if e != nil {
				t.Fatal(e)
			}
			keys[epoch] = append(keys[epoch], key)
			proposer, e := crypto.ToECDSA(bytes.Repeat([]byte{byte(1 + i + epoch*4)}, 32))
			if e != nil {
				t.Fatal(e)
			}
			sets[epoch] = append(sets[epoch], validator{crypto.PubkeyToAddress(proposer.PublicKey), key.PublicKey().Marshal()})
		}
	}
	epochInfo := func(epoch int) *types.EpochInfo {
		info := new(types.EpochInfo)
		for i, v := range sets[epoch] {
			info.Candidates = append(info.Candidates, &types.Candidate{Addr: v.Address})
			info.Validators = append(info.Validators, uint32(i))
			info.BLSPublicKeys = append(info.BLSPublicKeys, v.PublicKey)
		}
		return info
	}
	extra := &types.WBFTExtra{EpochInfo: epochInfo(0), GasTip: big.NewInt(0)}
	raw, e := rlp.EncodeToBytes(extra)
	if e != nil {
		t.Fatal(e)
	}
	parent := &types.Header{Number: big.NewInt(0), Difficulty: big.NewInt(1), GasLimit: 30000000, UncleHash: types.EmptyUncleHash, Root: common.Hash{1}, TxHash: types.EmptyTxsHash, ReceiptHash: types.EmptyReceiptsHash, Extra: raw}
	anchor, e := rlp.EncodeToBytes(parent)
	if e != nil {
		t.Fatal(e)
	}
	var headers [][]byte
	sign := func(h *types.Header, round uint32, kind core.SealType, epoch int) *types.WBFTAggregatedSeal {
		msg := core.PrepareSeal(h, round, kind)
		sigs := make([]blscommon.Signature, 0, 3)
		pubkeys := make([][]byte, 0, 3)
		for _, key := range keys[epoch][:3] {
			sigs = append(sigs, key.Sign(msg))
			pubkeys = append(pubkeys, key.PublicKey().Marshal())
		}
		signature := bls.AggregateSignatures(sigs)
		pk, err := bls.AggregatePublicKeys(pubkeys)
		if err != nil || !signature.Verify(pk, msg) {
			t.Fatal("native cross-validation", err)
		}
		return &types.WBFTAggregatedSeal{Sealers: types.SealerSet{7}, Signature: signature.Marshal()}
	}
	for n := uint64(1); n <= 4; n++ {
		epoch := 0
		if n > 2 {
			epoch = 1
		}
		extra = &types.WBFTExtra{Round: 2, GasTip: big.NewInt(0)}
		if n%2 == 0 {
			extra.EpochInfo = epochInfo(1)
		}
		if n > 1 {
			pe, _ := types.ExtractWBFTExtra(parent)
			extra.PrevRound = pe.Round
			old := 0
			if n > 3 {
				old = 1
			}
			extra.PrevPreparedSeal = sign(parent, pe.Round, core.SealTypePrepare, old)
			extra.PrevCommittedSeal = sign(parent, pe.Round, core.SealTypeCommit, old)
		}
		proposer, _ := crypto.ToECDSA(bytes.Repeat([]byte{byte(1 + epoch*4)}, 32))
		randao := crypto.Keccak256(append(append(big.NewInt(8283).Bytes(), byte(1)), new(big.Int).SetUint64(n).Bytes()...))
		extra.RandaoReveal, e = crypto.Sign(crypto.Keccak256(randao), proposer)
		if e != nil {
			t.Fatal(e)
		}
		raw, e = rlp.EncodeToBytes(extra)
		if e != nil {
			t.Fatal(e)
		}
		mix := new(big.Int).Xor(new(big.Int).SetBytes(parent.MixDigest[:]), new(big.Int).SetBytes(crypto.Keccak256(extra.RandaoReveal)))
		h := &types.Header{ParentHash: parent.Hash(), Number: new(big.Int).SetUint64(n), Time: n, GasLimit: 30000000, Difficulty: big.NewInt(1), Coinbase: sets[epoch][0].Address, UncleHash: types.EmptyUncleHash, Root: common.Hash{byte(n + 1)}, TxHash: types.EmptyTxsHash, ReceiptHash: types.EmptyReceiptsHash, Extra: raw, MixDigest: common.BigToHash(mix)}
		extra.PreparedSeal = sign(h, extra.Round, core.SealTypePrepare, epoch)
		extra.CommittedSeal = sign(h, extra.Round, core.SealTypeCommit, epoch)
		h.Extra, e = rlp.EncodeToBytes(extra)
		if e != nil {
			t.Fatal(e)
		}
		raw, e = rlp.EncodeToBytes(h)
		if e != nil {
			t.Fatal(e)
		}
		headers = append(headers, raw)
		parent = h
	}
	policy := struct {
		Protocol string
		Chain    struct {
			ID      string
			Genesis execwire.Hash
		}
		Fork                                             string
		EpochLength, EpochOffset, BlockPeriod, MaxHeight uint64
		AnchorHeader                                     []byte
		Applied, Next                                    []validator
	}{Protocol: "stablenet-wbft-4d17eea018c8", Fork: "anzeon-v1", EpochLength: 2, BlockPeriod: 1, MaxHeight: 4, AnchorHeader: anchor, Applied: sets[0], Next: sets[0]}
	policy.Chain.ID = "8283"
	var genesis types.Header
	if e = rlp.DecodeBytes(anchor, &genesis); e != nil {
		t.Fatal(e)
	}
	policy.Chain.Genesis = execwire.Hash(genesis.Hash())
	out, e := json.MarshalIndent(struct {
		Policy  any
		Headers [][]byte
	}{policy, headers}, "", "  ")
	if e != nil {
		t.Fatal(e)
	}
	path := "../../../testdata/execution/consensus/native.json"
	if os.Getenv("UPDATE_CONSENSUS_FIXTURES") == "1" {
		if e = os.MkdirAll("../../../testdata/execution/consensus", 0755); e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(path, append(out, '\n'), 0644); e != nil {
			t.Fatal(e)
		}
	} else {
		old, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		if !bytes.Equal(old, append(out, '\n')) {
			t.Fatal("fixture differs; regenerate explicitly")
		}
	}
}
