package storage

import (
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// Aliases of the ports moved to pkg/core/port (refactoring plan R1-1);
// removed once every consumer uses the port package.
type (
	TransactionType        = port.TransactionType
	TransactionFilter      = port.TransactionFilter
	TransactionWithReceipt = port.TransactionWithReceipt
	BalanceSnapshot        = port.BalanceSnapshot
	MinerStats             = port.MinerStats
	TokenBalance           = port.TokenBalance
	GasStats               = port.GasStats
	AddressGasStats        = port.AddressGasStats
	NetworkMetrics         = port.NetworkMetrics
	AddressActivityStats   = port.AddressActivityStats
	AddressStats           = port.AddressStats
	HistoricalReader       = port.HistoricalReader
	HistoricalWriter       = port.HistoricalWriter
	BalanceRecordChecker   = port.BalanceRecordChecker
	HistoricalStorage      = port.HistoricalStorage
	FeeDelegationTxMeta    = port.FeeDelegationTxMeta
	FeeDelegationReader    = port.FeeDelegationReader
	FeeDelegationWriter    = port.FeeDelegationWriter
)

const (
	TxTypeAll      = port.TxTypeAll
	TxTypeSent     = port.TxTypeSent
	TxTypeReceived = port.TxTypeReceived
)

var (
	ErrNegativeBalance       = port.ErrNegativeBalance
	DefaultTransactionFilter = port.DefaultTransactionFilter
)
