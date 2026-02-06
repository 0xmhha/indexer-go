package poa

import (
	"log"

	"github.com/0xmhha/indexer-go/pkg/consensus"
	"github.com/0xmhha/indexer-go/pkg/types/chain"
	"go.uber.org/zap"
)

func init() {
	// Register PoA parser with the global registry
	if err := consensus.Register(
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
	); err != nil {
		log.Fatalf("failed to register PoA consensus parser: %v", err)
	}
}

// Factory creates a new PoA parser instance
func Factory(config *consensus.Config, logger *zap.Logger) (chain.ConsensusParser, error) {
	return NewParser(logger), nil
}
