package testchain

import (
	"encoding/hex"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
)

// Event signatures the indexer recognizes.
var (
	SigTransfer           = crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)"))
	SigMint               = crypto.Keccak256Hash([]byte("Mint(address,address,uint256)"))
	SigBurn               = crypto.Keccak256Hash([]byte("Burn(address,uint256)"))
	SigUserOperationEvent = crypto.Keccak256Hash([]byte("UserOperationEvent(bytes32,address,address,uint256,bool,uint256,uint256)"))
	SigModuleInstalled    = crypto.Keccak256Hash([]byte("ModuleInstalled(uint256,address)"))
	SigModuleUninstalled  = crypto.Keccak256Hash([]byte("ModuleUninstalled(uint256,address)"))
)

// Well-known addresses used by the scenario.
var (
	NativeCoinAdapter = common.HexToAddress("0x0000000000000000000000000000000000001000")
	GovMinter         = common.HexToAddress("0x0000000000000000000000000000000000001003")
	EntryPointV07     = common.HexToAddress("0x0000000071727De22E5E9d8BAf0edAc6f37da032")
)

// Scenario is a built chain plus the actors and contracts in it, so tests can
// make assertions without re-deriving addresses.
type Scenario struct {
	Chain    *Chain
	Accounts []Account
	ERC20    common.Address
	ERC721   common.Address
	Account  common.Address // ERC-4337 / ERC-7579 smart account
	Module   common.Address
	Delegate common.Address // EIP-7702 delegation target
}

// DefaultChainID is the chain id of scenario chains.
const DefaultChainID = 7777

// BuildDefault builds the reference scenario used by golden and crash tests.
// It covers every feature the ingest path indexes for a generic EVM chain:
// native transfers (legacy and dynamic fee), a failed transaction, contract
// creation, ERC-20 and ERC-721 transfers with token metadata, StableNet
// system-contract Mint/Burn events, an EIP-7702 SetCode transaction, an
// ERC-4337 UserOperationEvent, ERC-7579 module install/uninstall, and a run of
// plain blocks so that resume points fall on both busy and empty blocks.
func BuildDefault() *Scenario {
	accts := make([]Account, 6)
	alloc := map[common.Address]*big.Int{}
	for i := range accts {
		accts[i] = NewAccount(uint64(i))
		alloc[accts[i].Address] = ether(1000)
	}
	a, b, c, d, bundler := accts[0], accts[1], accts[2], accts[3], accts[4]

	ch := NewChain(DefaultChainID, alloc)
	sc := &Scenario{
		Chain:    ch,
		Accounts: accts,
		Account:  common.HexToAddress("0x00000000000000000000000000000000000AA001"),
		Module:   common.HexToAddress("0x00000000000000000000000000000000000BB001"),
		Delegate: common.HexToAddress("0x00000000000000000000000000000000000CC001"),
	}
	gp := big.NewInt(2_000_000_000)

	// 1: native transfers, legacy and dynamic fee.
	ch.AddBlock(
		TxSpec{From: a, Tx: &types.LegacyTx{To: &b.Address, Value: ether(5), Gas: 21000, GasPrice: gp}},
		TxSpec{From: a, Tx: &types.DynamicFeeTx{To: &c.Address, Value: ether(3), Gas: 21000, GasTipCap: big.NewInt(1), GasFeeCap: gp}},
	)

	// 2: ERC-20 contract creation.
	sc.ERC20 = crypto.CreateAddress(a.Address, ch.NextNonce(a.Address))
	ch.SetCode(sc.ERC20, selectorsCode("a9059cbb", "70a08231", "18160ddd", "06fdde03", "95d89b41", "313ce567", "23b872dd"))
	ch.SetContract(sc.ERC20, ContractMock{
		"06fdde03": abiString("Test Token"),
		"95d89b41": abiString("TT"),
		"313ce567": word(big.NewInt(18)),
		"18160ddd": word(ether(1_000_000)),
	})
	ch.AddBlock(TxSpec{From: a, Tx: &types.LegacyTx{Gas: 500000, GasPrice: gp, Data: []byte{0x60, 0x80}}, GasUsed: 400000, Creates: true})

	// 3: ERC-20 transfer a->b, plus native b->c in the same block.
	ch.AddBlock(
		TxSpec{From: a, Tx: &types.LegacyTx{To: &sc.ERC20, Gas: 60000, GasPrice: gp}, GasUsed: 51000,
			Logs: []*types.Log{erc20Transfer(sc.ERC20, a.Address, b.Address, big.NewInt(1000))}},
		TxSpec{From: b, Tx: &types.LegacyTx{To: &c.Address, Value: ether(1), Gas: 21000, GasPrice: gp}},
	)

	// 4: ERC-721 contract creation (ERC-165 path).
	sc.ERC721 = crypto.CreateAddress(a.Address, ch.NextNonce(a.Address))
	ch.SetCode(sc.ERC721, selectorsCode("01ffc9a7", "6352211e", "70a08231", "23b872dd", "c87b56dd"))
	ch.SetContract(sc.ERC721, ContractMock{
		"01ffc9a701ffc9a700000000000000000000000000000000000000000000000000000000": word(big.NewInt(1)), // ERC-165
		"01ffc9a780ac58cd00000000000000000000000000000000000000000000000000000000": word(big.NewInt(1)), // ERC-721
		"01ffc9a75b5e139f00000000000000000000000000000000000000000000000000000000": word(big.NewInt(1)), // metadata
		"01ffc9a7": word(big.NewInt(0)),
		"06fdde03": abiString("Test NFT"),
		"95d89b41": abiString("TNFT"),
	})
	ch.AddBlock(TxSpec{From: a, Tx: &types.LegacyTx{Gas: 800000, GasPrice: gp, Data: []byte{0x60, 0x80}}, GasUsed: 700000, Creates: true})

	// 5: ERC-721 mint to b, then b transfers token 1 to c.
	ch.AddBlock(
		TxSpec{From: a, Tx: &types.LegacyTx{To: &sc.ERC721, Gas: 90000, GasPrice: gp}, GasUsed: 80000,
			Logs: []*types.Log{erc721Transfer(sc.ERC721, common.Address{}, b.Address, big.NewInt(1))}},
		TxSpec{From: b, Tx: &types.LegacyTx{To: &sc.ERC721, Gas: 90000, GasPrice: gp}, GasUsed: 60000,
			Logs: []*types.Log{erc721Transfer(sc.ERC721, b.Address, c.Address, big.NewInt(1))}},
	)

	// 6: StableNet system contracts: Mint to d, Burn from a.
	ch.AddBlock(
		TxSpec{From: a, Tx: &types.LegacyTx{To: &GovMinter, Gas: 100000, GasPrice: gp}, GasUsed: 70000,
			Logs: []*types.Log{
				{Address: NativeCoinAdapter, Topics: []common.Hash{SigMint, addrTopic(GovMinter), addrTopic(d.Address)}, Data: word(ether(50))},
				{Address: NativeCoinAdapter, Topics: []common.Hash{SigBurn, addrTopic(a.Address)}, Data: word(ether(7))},
			}},
	)

	// 7: EIP-7702 SetCode from c, authorization signed by d.
	auth, err := types.SignSetCode(d.Key, types.SetCodeAuthorization{
		ChainID: *uint256.NewInt(DefaultChainID),
		Address: sc.Delegate,
		Nonce:   ch.NextNonce(d.Address),
	})
	if err != nil {
		panic(err)
	}
	ch.AddBlock(TxSpec{From: c, Tx: &types.SetCodeTx{
		ChainID:   uint256.NewInt(DefaultChainID),
		GasTipCap: uint256.NewInt(1),
		GasFeeCap: uint256.MustFromBig(gp),
		Gas:       100000,
		To:        d.Address,
		AuthList:  []types.SetCodeAuthorization{auth},
	}, GasUsed: 46000})

	// 8: ERC-4337 bundle through EntryPoint v0.7.
	ch.AddBlock(TxSpec{From: bundler, Tx: &types.LegacyTx{To: &EntryPointV07, Gas: 300000, GasPrice: gp}, GasUsed: 210000,
		Logs: []*types.Log{{
			Address: EntryPointV07,
			Topics: []common.Hash{
				SigUserOperationEvent,
				crypto.Keccak256Hash([]byte("userop-1")),
				addrTopic(sc.Account),
				addrTopic(common.Address{}), // no paymaster
			},
			Data: concat(word(big.NewInt(0)), word(big.NewInt(1)), word(big.NewInt(150_000_000_000_000)), word(big.NewInt(150000))),
		}}})

	// 9: ERC-7579 module install on the smart account.
	ch.AddBlock(TxSpec{From: bundler, Tx: &types.LegacyTx{To: &sc.Account, Gas: 100000, GasPrice: gp}, GasUsed: 60000,
		Logs: []*types.Log{{Address: sc.Account, Topics: []common.Hash{SigModuleInstalled}, Data: concat(word(big.NewInt(1)), addrWord(sc.Module))}}})

	// 10: failed transaction (value not moved, gas still paid).
	ch.AddBlock(TxSpec{From: a, Tx: &types.LegacyTx{To: &b.Address, Value: ether(2), Gas: 50000, GasPrice: gp}, GasUsed: 30000, Failed: true})

	// 11: module uninstall.
	ch.AddBlock(TxSpec{From: bundler, Tx: &types.LegacyTx{To: &sc.Account, Gas: 100000, GasPrice: gp}, GasUsed: 40000,
		Logs: []*types.Log{{Address: sc.Account, Topics: []common.Hash{SigModuleUninstalled}, Data: concat(word(big.NewInt(1)), addrWord(sc.Module))}}})

	// 12..20: empty and light blocks, with repeated activity on the same
	// addresses so per-address sequences grow across many blocks.
	for i := 0; i < 9; i++ {
		if i%3 == 0 {
			ch.AddBlock()
			continue
		}
		ch.AddBlock(
			TxSpec{From: b, Tx: &types.LegacyTx{To: &a.Address, Value: big.NewInt(int64(1000 + i)), Gas: 21000, GasPrice: gp}},
			TxSpec{From: a, Tx: &types.LegacyTx{To: &sc.ERC20, Gas: 60000, GasPrice: gp}, GasUsed: 51000,
				Logs: []*types.Log{erc20Transfer(sc.ERC20, a.Address, c.Address, big.NewInt(int64(10+i)))}},
		)
	}
	return sc
}

// --- ABI helpers --------------------------------------------------------------

func ether(n int64) *big.Int {
	return new(big.Int).Mul(big.NewInt(n), big.NewInt(1e18))
}

func word(v *big.Int) []byte { return common.LeftPadBytes(v.Bytes(), 32) }

func addrWord(a common.Address) []byte { return common.LeftPadBytes(a.Bytes(), 32) }

func addrTopic(a common.Address) common.Hash { return common.BytesToHash(a.Bytes()) }

func concat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func abiString(s string) []byte {
	padded := common.RightPadBytes([]byte(s), (len(s)+31)/32*32)
	return concat(word(big.NewInt(32)), word(big.NewInt(int64(len(s)))), padded)
}

// selectorsCode fakes runtime bytecode that contains the given selectors, which
// is all the bytecode-based token detector looks for.
func selectorsCode(selectors ...string) []byte {
	code, err := hex.DecodeString("6080" + strings.Join(selectors, "63"))
	if err != nil {
		panic(err)
	}
	return code
}

func erc20Transfer(token, from, to common.Address, amount *big.Int) *types.Log {
	return &types.Log{Address: token, Topics: []common.Hash{SigTransfer, addrTopic(from), addrTopic(to)}, Data: word(amount)}
}

func erc721Transfer(token, from, to common.Address, id *big.Int) *types.Log {
	return &types.Log{Address: token, Topics: []common.Hash{SigTransfer, addrTopic(from), addrTopic(to), common.BigToHash(id)}}
}
