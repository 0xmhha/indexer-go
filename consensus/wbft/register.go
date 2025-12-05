package wbft

import (
	"github.com/0xmhha/indexer-go/consensus"
	"github.com/0xmhha/indexer-go/types/chain"
	"go.uber.org/zap"
)

func init() {
	// Register WBFT parser with the global registry
	consensus.MustRegister(
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
	)
}

// Factory creates a new WBFT parser instance
func Factory(config *consensus.Config, logger *zap.Logger) (chain.ConsensusParser, error) {
	epochLength := config.EpochLength
	if epochLength == 0 {
		epochLength = 10 // Default epoch length
	}

	return NewParser(epochLength, logger), nil
}
