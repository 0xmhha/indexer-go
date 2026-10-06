package storage

import (
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// Aliases of the ports moved to pkg/core/port (refactoring plan R1-1);
// removed once every consumer uses the port package.
type (
	ContractCreation         = port.ContractCreation
	InternalTransaction      = port.InternalTransaction
	ERC20Transfer            = port.ERC20Transfer
	ERC721Transfer           = port.ERC721Transfer
	NFTOwnership             = port.NFTOwnership
	AddressIndexReader       = port.AddressIndexReader
	AddressIndexWriter       = port.AddressIndexWriter
	AddressIndexReaderWriter = port.AddressIndexReaderWriter
	SetCodeIndexReaderWriter = port.SetCodeIndexReaderWriter
	FullAddressIndexer       = port.FullAddressIndexer
)

const (
	ERC20TransferTopic         = port.ERC20TransferTopic
	InternalTxTypeCall         = port.InternalTxTypeCall
	InternalTxTypeDelegateCall = port.InternalTxTypeDelegateCall
	InternalTxTypeStaticCall   = port.InternalTxTypeStaticCall
	InternalTxTypeCreate       = port.InternalTxTypeCreate
	InternalTxTypeCreate2      = port.InternalTxTypeCreate2
	InternalTxTypeSelfDestruct = port.InternalTxTypeSelfDestruct
)
