package poa

import (
	"github.com/0xmhha/indexer-go/consensus"
	"github.com/0xmhha/indexer-go/types/chain"
	"go.uber.org/zap"
)

func init() {
	// Register PoA parser with the global registry
	consensus.MustRegister(
		chain.ConsensusTypePoA,
		Factory,
		&consensus.ParserMetadata{
			Name:        "PoA",
			Description: "Proof of Authority consensus parser for Clique-based chains (Geth, Anvil, etc.)",
			Version:     "1.0.0",
			SupportedChainTypes: []chain.ChainType{
				chain.ChainTypeEVM,
			},
		},
	)
}

// Factory creates a new PoA parser instance
func Factory(config *consensus.Config, logger *zap.Logger) (chain.ConsensusParser, error) {
	return NewParser(logger), nil
}
