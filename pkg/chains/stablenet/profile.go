// Package stablenet is the chain profile for StableNet (go-stablenet,
// WBFT consensus). It extends the EVM profile with the fee delegation
// transaction type (0x16) and the WBFT block hash rule.
package stablenet

import (
	"strings"

	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/chains/evm"
)

// ID is the StableNet profile id.
const ID = "stablenet"

// Features StableNet enables by default (refactoring plan 5.3).
var defaultFeatures = []string{"stablenet.system_contracts", "stablenet.fee_delegation", "stablenet.wbft"}

// Detect accepts go-stablenet nodes by client version. Release binaries
// report "Gstable/v1.1.0-..."; older builds used "go-stablenet" or
// "StableOne".
func Detect(info chains.NodeInfo) bool {
	v := strings.ToLower(info.ClientVersion)
	return strings.Contains(v, "gstable") || strings.Contains(v, "stablenet") || strings.Contains(v, "stableone")
}

// New returns the StableNet profile.
func New() *evm.Profile {
	return evm.New(ID,
		evm.WithDetect(Detect),
		evm.WithFeatures(defaultFeatures...),
		evm.WithHeaderHasher(HeaderHash),
		evm.WithTxDecoder(FeeDelegationTxType, DecodeFeeDelegationTx),
	)
}

func init() {
	// Above the generic EVM fallback.
	chains.Register(New(), 100)
}
