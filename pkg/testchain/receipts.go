package testchain

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// PaymentSettledSignature is the settlement contract's event (the P07
// receipt indexer's input), as a declared source writes it.
const PaymentSettledSignature = "PaymentSettled(address indexed merchant, bytes32 indexed orderId, address indexed device, uint256 amount, uint256 nonce)"

var (
	SigPaymentSettled = crypto.Keccak256Hash([]byte("PaymentSettled(address,bytes32,address,uint256,uint256)"))
	SigRefunded       = crypto.Keccak256Hash([]byte("Refunded(bytes32,uint256)"))
)

// Payment is a PaymentSettled log of the settlement contract the receipts
// scenario emits.
type Payment struct {
	Block    uint64
	Merchant common.Address
	OrderID  common.Hash
	Device   common.Address
	Amount   int64
	Nonce    int64
}

// ReceiptsScenario is a payment settlement chain (the P07 receipt
// indexer's input): the settlement contract emits PaymentSettled, once
// twice for the same order; another contract emits the same event and the
// settlement contract another event, which a receipts table must ignore.
type ReceiptsScenario struct {
	Scenario
	Settlement, Other common.Address
	Merchants         []common.Address
	Device            common.Address
	// Payments are the settlement contract's PaymentSettled logs, in chain
	// order.
	Payments []Payment
	// Decoys are the other logs, by block.
	Decoys int
}

// BuildReceipts builds the receipts scenario.
func BuildReceipts() *ReceiptsScenario {
	accts := make([]Account, 3)
	alloc := map[common.Address]*big.Int{}
	for i := range accts {
		accts[i] = NewAccount(uint64(80 + i))
		alloc[accts[i].Address] = ether(100)
	}
	ch := NewChain(DefaultChainID, alloc)
	sc := &ReceiptsScenario{
		Scenario:   Scenario{Chain: ch, Accounts: accts},
		Settlement: common.HexToAddress("0x00000000000000000000000000000000005E771E"),
		Other:      common.HexToAddress("0x000000000000000000000000000000000000BEEF"),
		Merchants:  []common.Address{common.HexToAddress("0x00000000000000000000000000000000000000A1"), common.HexToAddress("0x00000000000000000000000000000000000000A2")},
		Device:     common.HexToAddress("0x0000000000000000000000000000000000000DE5"),
	}
	kiosk := accts[0]
	gp := big.NewInt(1_000_000_000)
	order := func(n int64) common.Hash { return common.BigToHash(big.NewInt(1000 + n)) }
	nonce := int64(0)
	payment := func(at common.Address, m common.Address, o common.Hash, amount int64) *types.Log {
		nonce++
		return &types.Log{Address: at, Topics: []common.Hash{SigPaymentSettled, addrTopic(m), o, addrTopic(sc.Device)},
			Data: concat(word(big.NewInt(amount)), word(big.NewInt(nonce)))}
	}
	pay := func(logs ...*types.Log) {
		ch.AddBlock(TxSpec{From: kiosk, Tx: &types.LegacyTx{To: &sc.Settlement, Gas: 200000, GasPrice: gp}, GasUsed: 90000, Logs: logs})
		for _, l := range logs {
			if l.Address == sc.Settlement && l.Topics[0] == SigPaymentSettled {
				sc.Payments = append(sc.Payments, Payment{Block: ch.Head(), Merchant: common.BytesToAddress(l.Topics[1].Bytes()),
					OrderID: l.Topics[2], Device: common.BytesToAddress(l.Topics[3].Bytes()),
					Amount: new(big.Int).SetBytes(l.Data[:32]).Int64(), Nonce: new(big.Int).SetBytes(l.Data[32:]).Int64()})
			} else {
				sc.Decoys++
			}
		}
	}
	m1, m2 := sc.Merchants[0], sc.Merchants[1]

	// 1: a plain transfer.
	ch.AddBlock(TxSpec{From: accts[1], Tx: &types.LegacyTx{To: &accts[2].Address, Value: ether(1), Gas: 21000, GasPrice: gp}})
	// 2: order 1 of m1 is paid; the other contract emits the same event and
	// the settlement contract a refund.
	pay(payment(sc.Settlement, m1, order(1), 2500), payment(sc.Other, m1, order(9), 1),
		&types.Log{Address: sc.Settlement, Topics: []common.Hash{SigRefunded, order(7)}, Data: word(big.NewInt(5))})
	// 3: order 2 of m1, and order 1 of m2 (the same order id, another
	// merchant).
	pay(payment(sc.Settlement, m1, order(2), 1200), payment(sc.Settlement, m2, order(1), 800))
	// 4: order 1 of m1 settles again: a duplicate.
	pay(payment(sc.Settlement, m1, order(1), 2500))
	// 5-6: plain transfers.
	for range 2 {
		ch.AddBlock(TxSpec{From: accts[2], Tx: &types.LegacyTx{To: &accts[1].Address, Value: ether(1), Gas: 21000, GasPrice: gp}})
	}
	return sc
}
