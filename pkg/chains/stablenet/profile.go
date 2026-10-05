// Package stablenet is the chain profile for StableNet (go-stablenet,
// WBFT consensus). It extends the EVM profile with the fee delegation
// transaction type (0x16), the WBFT block hash rule and the Anzeon fee rule.
package stablenet

import (
	"strings"

	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/chains/evm"
)

// ID is the StableNet profile id.
const ID = "stablenet"

// LegacyName is the former name of the chain, kept as an alias of ID.
const LegacyName = "stableone"

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
		evm.WithBinaryTxDecoder(FeeDelegationTxType, DecodeFeeDelegationTxBinary),
		evm.WithEffectiveGasPrice(EffectiveGasPrice),
		evm.WithNativeAccounting(NewAccounting()),
		evm.WithNativeCoinContract(NativeCoinAdapterAddress),
	)
}

func init() {
	// Above the generic EVM fallback.
	chains.Register(New(), 100)
	// The chain was called StableOne; configurations and the --adapter flag
	// still use that name.
	chains.RegisterAlias(LegacyName, ID)
}
