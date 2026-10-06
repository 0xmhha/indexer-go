package storage

import (
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// Aliases of the ports moved to pkg/core/port (refactoring plan R1-1);
// removed once every consumer uses the port package.
type (
	TokenStandard        = port.TokenStandard
	TokenMetadata        = port.TokenMetadata
	TokenMetadataReader  = port.TokenMetadataReader
	TokenMetadataWriter  = port.TokenMetadataWriter
	TokenMetadataFetcher = port.TokenMetadataFetcher
)

const (
	TokenStandardUnknown = port.TokenStandardUnknown
	TokenStandardERC20   = port.TokenStandardERC20
	TokenStandardERC721  = port.TokenStandardERC721
	TokenStandardERC1155 = port.TokenStandardERC1155
)
