package storage

import (
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// Aliases of the ports moved to pkg/core/port (refactoring plan R1-1);
// removed once every consumer uses the port package.
type (
	ContractVerification       = port.ContractVerification
	ContractVerificationReader = port.ContractVerificationReader
	ContractVerificationWriter = port.ContractVerificationWriter
)
