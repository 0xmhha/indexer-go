package graphql

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/0xmhha/indexer-go/internal/constants"
	"github.com/0xmhha/indexer-go/storage"
	"github.com/ethereum/go-ethereum/common"
	"github.com/graphql-go/graphql"
	"go.uber.org/zap"
)

// ========== Contract Creation Resolvers ==========

// resolveContractCreation resolves contract creation information by contract address
func (s *Schema) resolveContractCreation(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context
	addressStr, ok := p.Args["address"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid address")
	}

	address := common.HexToAddress(addressStr)

	// Check if storage implements AddressIndexReader
	addressReader, ok := s.storage.(storage.AddressIndexReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support address indexing")
	}

	creation, err := addressReader.GetContractCreation(ctx, address)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, nil
		}
		s.logger.Error("failed to get contract creation",
			zap.String("address", addressStr),
			zap.Error(err))
		return nil, err
	}

	return s.contractCreationToMap(creation), nil
}

// resolveContractsByCreator resolves contracts created by a specific address
func (s *Schema) resolveContractsByCreator(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context
	creatorStr, ok := p.Args["creator"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid creator address")
	}

	creator := common.HexToAddress(creatorStr)

	// Get pagination parameters
	limit := constants.DefaultPaginationLimit
	offset := 0
	if pagination, ok := p.Args["pagination"].(map[string]interface{}); ok {
		if l, ok := pagination["limit"].(int); ok && l > 0 {
			if l > constants.DefaultMaxPaginationLimit {
				limit = constants.DefaultMaxPaginationLimit
			} else {
				limit = l
			}
		}
		if o, ok := pagination["offset"].(int); ok && o >= 0 {
			offset = o
		}
	}

	// Check if storage implements AddressIndexReader
	addressReader, ok := s.storage.(storage.AddressIndexReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support address indexing")
	}

	contracts, err := addressReader.GetContractsByCreator(ctx, creator, limit, offset)
	if err != nil {
		s.logger.Error("failed to get contracts by creator",
			zap.String("creator", creatorStr),
			zap.Error(err))
		return nil, err
	}

	// Get full contract creation info for each contract
	nodes := make([]interface{}, 0, len(contracts))
	for _, contractAddr := range contracts {
		creation, err := addressReader.GetContractCreation(ctx, contractAddr)
		if err != nil {
			s.logger.Warn("failed to get contract creation details",
				zap.String("contract", contractAddr.Hex()),
				zap.Error(err))
			continue
		}
		nodes = append(nodes, s.contractCreationToMap(creation))
	}

	return map[string]interface{}{
		"nodes":      nodes,
		"totalCount": len(nodes),
		"pageInfo": map[string]interface{}{
			"hasNextPage":     len(nodes) == limit,
			"hasPreviousPage": offset > 0,
		},
	}, nil
}

// ========== Internal Transaction Resolvers ==========

// resolveInternalTransactions resolves internal transactions for a transaction hash
func (s *Schema) resolveInternalTransactions(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context
	txHashStr, ok := p.Args["txHash"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid transaction hash")
	}

	txHash := common.HexToHash(txHashStr)

	// Check if storage implements AddressIndexReader
	addressReader, ok := s.storage.(storage.AddressIndexReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support address indexing")
	}

	internals, err := addressReader.GetInternalTransactions(ctx, txHash)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return []interface{}{}, nil
		}
		s.logger.Error("failed to get internal transactions",
			zap.String("txHash", txHashStr),
			zap.Error(err))
		return nil, err
	}

	nodes := make([]interface{}, len(internals))
	for i, internal := range internals {
		nodes[i] = s.internalTransactionToMap(internal)
	}

	return nodes, nil
}

// resolveInternalTransactionsByAddress resolves internal transactions involving a specific address
func (s *Schema) resolveInternalTransactionsByAddress(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context
	addressStr, ok := p.Args["address"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid address")
	}

	isFrom, ok := p.Args["isFrom"].(bool)
	if !ok {
		return nil, fmt.Errorf("invalid isFrom parameter")
	}

	address := common.HexToAddress(addressStr)

	// Get pagination parameters
	limit := constants.DefaultPaginationLimit
	offset := 0
	if pagination, ok := p.Args["pagination"].(map[string]interface{}); ok {
		if l, ok := pagination["limit"].(int); ok && l > 0 {
			if l > constants.DefaultMaxPaginationLimit {
				limit = constants.DefaultMaxPaginationLimit
			} else {
				limit = l
			}
		}
		if o, ok := pagination["offset"].(int); ok && o >= 0 {
			offset = o
		}
	}

	// Check if storage implements AddressIndexReader
	addressReader, ok := s.storage.(storage.AddressIndexReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support address indexing")
	}

	internals, err := addressReader.GetInternalTransactionsByAddress(ctx, address, isFrom, limit, offset)
	if err != nil {
		s.logger.Error("failed to get internal transactions by address",
			zap.String("address", addressStr),
			zap.Bool("isFrom", isFrom),
			zap.Error(err))
		return nil, err
	}

	nodes := make([]interface{}, len(internals))
	for i, internal := range internals {
		nodes[i] = s.internalTransactionToMap(internal)
	}

	return map[string]interface{}{
		"nodes":      nodes,
		"totalCount": len(nodes),
		"pageInfo": map[string]interface{}{
			"hasNextPage":     len(nodes) == limit,
			"hasPreviousPage": offset > 0,
		},
	}, nil
}

// ========== ERC20 Transfer Resolvers ==========

// resolveERC20Transfer resolves ERC20 transfer by transaction hash and log index
func (s *Schema) resolveERC20Transfer(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context
	txHashStr, ok := p.Args["txHash"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid transaction hash")
	}

	logIndex, ok := p.Args["logIndex"].(int)
	if !ok {
		return nil, fmt.Errorf("invalid log index")
	}

	txHash := common.HexToHash(txHashStr)

	// Check if storage implements AddressIndexReader
	addressReader, ok := s.storage.(storage.AddressIndexReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support address indexing")
	}

	transfer, err := addressReader.GetERC20Transfer(ctx, txHash, uint(logIndex))
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, nil
		}
		s.logger.Error("failed to get ERC20 transfer",
			zap.String("txHash", txHashStr),
			zap.Int("logIndex", logIndex),
			zap.Error(err))
		return nil, err
	}

	return s.erc20TransferToMap(transfer), nil
}

// resolveERC20TransfersByToken resolves ERC20 transfers for a specific token contract
func (s *Schema) resolveERC20TransfersByToken(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context
	tokenStr, ok := p.Args["token"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid token address")
	}

	token := common.HexToAddress(tokenStr)

	// Get pagination parameters
	limit := constants.DefaultPaginationLimit
	offset := 0
	if pagination, ok := p.Args["pagination"].(map[string]interface{}); ok {
		if l, ok := pagination["limit"].(int); ok && l > 0 {
			if l > constants.DefaultMaxPaginationLimit {
				limit = constants.DefaultMaxPaginationLimit
			} else {
				limit = l
			}
		}
		if o, ok := pagination["offset"].(int); ok && o >= 0 {
			offset = o
		}
	}

	// Check if storage implements AddressIndexReader
	addressReader, ok := s.storage.(storage.AddressIndexReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support address indexing")
	}

	transfers, err := addressReader.GetERC20TransfersByToken(ctx, token, limit, offset)
	if err != nil {
		s.logger.Error("failed to get ERC20 transfers by token",
			zap.String("token", tokenStr),
			zap.Error(err))
		return nil, err
	}

	nodes := make([]interface{}, len(transfers))
	for i, transfer := range transfers {
		nodes[i] = s.erc20TransferToMap(transfer)
	}

	return map[string]interface{}{
		"nodes":      nodes,
		"totalCount": len(nodes),
		"pageInfo": map[string]interface{}{
			"hasNextPage":     len(nodes) == limit,
			"hasPreviousPage": offset > 0,
		},
	}, nil
}

// resolveERC20TransfersByAddress resolves ERC20 transfers involving a specific address
func (s *Schema) resolveERC20TransfersByAddress(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context
	addressStr, ok := p.Args["address"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid address")
	}

	isFrom, ok := p.Args["isFrom"].(bool)
	if !ok {
		return nil, fmt.Errorf("invalid isFrom parameter")
	}

	address := common.HexToAddress(addressStr)

	// Get pagination parameters
	limit := constants.DefaultPaginationLimit
	offset := 0
	if pagination, ok := p.Args["pagination"].(map[string]interface{}); ok {
		if l, ok := pagination["limit"].(int); ok && l > 0 {
			if l > constants.DefaultMaxPaginationLimit {
				limit = constants.DefaultMaxPaginationLimit
			} else {
				limit = l
			}
		}
		if o, ok := pagination["offset"].(int); ok && o >= 0 {
			offset = o
		}
	}

	// Check if storage implements AddressIndexReader
	addressReader, ok := s.storage.(storage.AddressIndexReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support address indexing")
	}

	transfers, err := addressReader.GetERC20TransfersByAddress(ctx, address, isFrom, limit, offset)
	if err != nil {
		s.logger.Error("failed to get ERC20 transfers by address",
			zap.String("address", addressStr),
			zap.Bool("isFrom", isFrom),
			zap.Error(err))
		return nil, err
	}

	nodes := make([]interface{}, len(transfers))
	for i, transfer := range transfers {
		nodes[i] = s.erc20TransferToMap(transfer)
	}

	return map[string]interface{}{
		"nodes":      nodes,
		"totalCount": len(nodes),
		"pageInfo": map[string]interface{}{
			"hasNextPage":     len(nodes) == limit,
			"hasPreviousPage": offset > 0,
		},
	}, nil
}

// ========== ERC721 Transfer Resolvers ==========

// resolveERC721Transfer resolves ERC721 transfer by transaction hash and log index
func (s *Schema) resolveERC721Transfer(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context
	txHashStr, ok := p.Args["txHash"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid transaction hash")
	}

	logIndex, ok := p.Args["logIndex"].(int)
	if !ok {
		return nil, fmt.Errorf("invalid log index")
	}

	txHash := common.HexToHash(txHashStr)

	// Check if storage implements AddressIndexReader
	addressReader, ok := s.storage.(storage.AddressIndexReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support address indexing")
	}

	transfer, err := addressReader.GetERC721Transfer(ctx, txHash, uint(logIndex))
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, nil
		}
		s.logger.Error("failed to get ERC721 transfer",
			zap.String("txHash", txHashStr),
			zap.Int("logIndex", logIndex),
			zap.Error(err))
		return nil, err
	}

	return s.erc721TransferToMap(transfer), nil
}

// resolveERC721TransfersByToken resolves ERC721 transfers for a specific NFT contract
func (s *Schema) resolveERC721TransfersByToken(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context
	tokenStr, ok := p.Args["token"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid token address")
	}

	token := common.HexToAddress(tokenStr)

	// Get pagination parameters
	limit := constants.DefaultPaginationLimit
	offset := 0
	if pagination, ok := p.Args["pagination"].(map[string]interface{}); ok {
		if l, ok := pagination["limit"].(int); ok && l > 0 {
			if l > constants.DefaultMaxPaginationLimit {
				limit = constants.DefaultMaxPaginationLimit
			} else {
				limit = l
			}
		}
		if o, ok := pagination["offset"].(int); ok && o >= 0 {
			offset = o
		}
	}

	// Check if storage implements AddressIndexReader
	addressReader, ok := s.storage.(storage.AddressIndexReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support address indexing")
	}

	transfers, err := addressReader.GetERC721TransfersByToken(ctx, token, limit, offset)
	if err != nil {
		s.logger.Error("failed to get ERC721 transfers by token",
			zap.String("token", tokenStr),
			zap.Error(err))
		return nil, err
	}

	nodes := make([]interface{}, len(transfers))
	for i, transfer := range transfers {
		nodes[i] = s.erc721TransferToMap(transfer)
	}

	return map[string]interface{}{
		"nodes":      nodes,
		"totalCount": len(nodes),
		"pageInfo": map[string]interface{}{
			"hasNextPage":     len(nodes) == limit,
			"hasPreviousPage": offset > 0,
		},
	}, nil
}

// resolveERC721TransfersByAddress resolves ERC721 transfers involving a specific address
func (s *Schema) resolveERC721TransfersByAddress(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context
	addressStr, ok := p.Args["address"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid address")
	}

	isFrom, ok := p.Args["isFrom"].(bool)
	if !ok {
		return nil, fmt.Errorf("invalid isFrom parameter")
	}

	address := common.HexToAddress(addressStr)

	// Get pagination parameters
	limit := constants.DefaultPaginationLimit
	offset := 0
	if pagination, ok := p.Args["pagination"].(map[string]interface{}); ok {
		if l, ok := pagination["limit"].(int); ok && l > 0 {
			if l > constants.DefaultMaxPaginationLimit {
				limit = constants.DefaultMaxPaginationLimit
			} else {
				limit = l
			}
		}
		if o, ok := pagination["offset"].(int); ok && o >= 0 {
			offset = o
		}
	}

	// Check if storage implements AddressIndexReader
	addressReader, ok := s.storage.(storage.AddressIndexReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support address indexing")
	}

	transfers, err := addressReader.GetERC721TransfersByAddress(ctx, address, isFrom, limit, offset)
	if err != nil {
		s.logger.Error("failed to get ERC721 transfers by address",
			zap.String("address", addressStr),
			zap.Bool("isFrom", isFrom),
			zap.Error(err))
		return nil, err
	}

	nodes := make([]interface{}, len(transfers))
	for i, transfer := range transfers {
		nodes[i] = s.erc721TransferToMap(transfer)
	}

	return map[string]interface{}{
		"nodes":      nodes,
		"totalCount": len(nodes),
		"pageInfo": map[string]interface{}{
			"hasNextPage":     len(nodes) == limit,
			"hasPreviousPage": offset > 0,
		},
	}, nil
}

// resolveERC721Owner resolves current owner of an NFT token
func (s *Schema) resolveERC721Owner(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context
	tokenStr, ok := p.Args["token"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid token address")
	}

	tokenIdStr, ok := p.Args["tokenId"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid token ID")
	}

	token := common.HexToAddress(tokenStr)
	tokenId, ok := new(big.Int).SetString(tokenIdStr, 10)
	if !ok {
		return nil, fmt.Errorf("invalid token ID format")
	}

	// Check if storage implements AddressIndexReader
	addressReader, ok := s.storage.(storage.AddressIndexReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support address indexing")
	}

	owner, err := addressReader.GetERC721Owner(ctx, token, tokenId)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, nil
		}
		s.logger.Error("failed to get ERC721 owner",
			zap.String("token", tokenStr),
			zap.String("tokenId", tokenIdStr),
			zap.Error(err))
		return nil, err
	}

	return owner.Hex(), nil
}

// ========== Helper mapper functions ==========

// contractCreationToMap converts ContractCreation to a map
func (s *Schema) contractCreationToMap(creation *storage.ContractCreation) map[string]interface{} {
	return map[string]interface{}{
		"contractAddress": creation.ContractAddress.Hex(),
		"creator":         creation.Creator.Hex(),
		"transactionHash": creation.TransactionHash.Hex(),
		"blockNumber":     fmt.Sprintf("%d", creation.BlockNumber),
		"timestamp":       fmt.Sprintf("%d", creation.Timestamp),
		"bytecodeSize":    creation.BytecodeSize,
	}
}

// internalTransactionToMap converts InternalTransaction to a map
func (s *Schema) internalTransactionToMap(internal *storage.InternalTransaction) map[string]interface{} {
	m := map[string]interface{}{
		"transactionHash": internal.TransactionHash.Hex(),
		"blockNumber":     fmt.Sprintf("%d", internal.BlockNumber),
		"index":           internal.Index,
		"type":            internal.Type,
		"from":            internal.From.Hex(),
		"to":              internal.To.Hex(),
		"value":           internal.Value.String(),
		"gas":             fmt.Sprintf("%d", internal.Gas),
		"gasUsed":         fmt.Sprintf("%d", internal.GasUsed),
		"input":           fmt.Sprintf("0x%x", internal.Input),
		"output":          fmt.Sprintf("0x%x", internal.Output),
		"depth":           internal.Depth,
	}

	if internal.Error != "" {
		m["error"] = internal.Error
	}

	return m
}

// erc20TransferToMap converts ERC20Transfer to a map
func (s *Schema) erc20TransferToMap(transfer *storage.ERC20Transfer) map[string]interface{} {
	return map[string]interface{}{
		"contractAddress": transfer.ContractAddress.Hex(),
		"from":            transfer.From.Hex(),
		"to":              transfer.To.Hex(),
		"value":           transfer.Value.String(),
		"transactionHash": transfer.TransactionHash.Hex(),
		"blockNumber":     fmt.Sprintf("%d", transfer.BlockNumber),
		"logIndex":        int(transfer.LogIndex),
		"timestamp":       fmt.Sprintf("%d", transfer.Timestamp),
	}
}

// erc721TransferToMap converts ERC721Transfer to a map
func (s *Schema) erc721TransferToMap(transfer *storage.ERC721Transfer) map[string]interface{} {
	return map[string]interface{}{
		"contractAddress": transfer.ContractAddress.Hex(),
		"from":            transfer.From.Hex(),
		"to":              transfer.To.Hex(),
		"tokenId":         transfer.TokenId.String(),
		"transactionHash": transfer.TransactionHash.Hex(),
		"blockNumber":     fmt.Sprintf("%d", transfer.BlockNumber),
		"logIndex":        int(transfer.LogIndex),
		"timestamp":       fmt.Sprintf("%d", transfer.Timestamp),
	}
}
