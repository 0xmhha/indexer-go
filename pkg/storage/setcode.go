package storage

import (
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// Aliases of the ports moved to pkg/core/port (refactoring plan R1-1);
// removed once every consumer uses the port package.
type (
	SetCodeAuthorizationRecord = port.SetCodeAuthorizationRecord
	AddressDelegationState     = port.AddressDelegationState
	AddressSetCodeStats        = port.AddressSetCodeStats
	SetCodeIndexReader         = port.SetCodeIndexReader
	SetCodeIndexWriter         = port.SetCodeIndexWriter
)

const (
	DelegationCodeLength           = port.DelegationCodeLength
	SetCodeErrNone                 = port.SetCodeErrNone
	SetCodeErrWrongChainID         = port.SetCodeErrWrongChainID
	SetCodeErrNonceOverflow        = port.SetCodeErrNonceOverflow
	SetCodeErrInvalidSignature     = port.SetCodeErrInvalidSignature
	SetCodeErrDestinationHasCode   = port.SetCodeErrDestinationHasCode
	SetCodeErrNonceMismatch        = port.SetCodeErrNonceMismatch
	SetCodeErrAuthorityBlacklisted = port.SetCodeErrAuthorityBlacklisted
	SetCodeErrRecoveryFailed       = port.SetCodeErrRecoveryFailed
)

var (
	DelegationPrefix    = port.DelegationPrefix
	ParseDelegation     = port.ParseDelegation
	AddressToDelegation = port.AddressToDelegation
	IsDelegation        = port.IsDelegation
)
