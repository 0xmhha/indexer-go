package wbft

import (
	"log"

	"github.com/0xmhha/indexer-go/pkg/consensus"
	"github.com/0xmhha/indexer-go/pkg/types/chain"
	"go.uber.org/zap"
)

func init() {
	// Register WBFT parser with the global registry
	if err := consensus.Register(
		chain.ConsensusTypeWBFT,
		Factory,
		&consensus.ParserMetadata{
			Name:        "WBFT",
			Description: "Weighted Byzantine Fault Tolerance consensus parser for StableOne and compatible chains",
			Version:     "1.0.0",
			SupportedChainTypes: []chain.ChainType{
				chain.ChainTypeEVM,
			},
		},
	); err != nil {
		log.Fatalf("failed to register WBFT consensus parser: %v", err)
	}
}

// Factory creates a new WBFT parser instance
func Factory(config *consensus.Config, logger *zap.Logger) (chain.ConsensusParser, error) {
	epochLength := config.EpochLength
	if epochLength == 0 {
		epochLength = 10 // Default epoch length
	}

	return NewParser(epochLength, logger), nil
}
