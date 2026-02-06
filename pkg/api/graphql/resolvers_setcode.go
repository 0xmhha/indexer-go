package graphql

import (
	"fmt"
	"strconv"

	"github.com/0xmhha/indexer-go/internal/constants"
	storagepkg "github.com/0xmhha/indexer-go/pkg/storage"
	"github.com/ethereum/go-ethereum/common"
	"github.com/graphql-go/graphql"
	"go.uber.org/zap"
)

// resolveSetCodeAuthorization resolves a specific SetCode authorization by tx hash and auth index
func (s *Schema) resolveSetCodeAuthorization(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	txHashStr, ok := p.Args["txHash"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid txHash")
	}

	authIndex, ok := p.Args["authIndex"].(int)
	if !ok {
		return nil, fmt.Errorf("invalid authIndex")
	}

	txHash := common.HexToHash(txHashStr)

	// Cast storage to SetCodeIndexReader
	setCodeReader, ok := s.storage.(storagepkg.SetCodeIndexReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support SetCode queries")
	}

	// Get all authorizations for this tx and find the one with matching index
	records, err := setCodeReader.GetSetCodeAuthorizationsByTx(ctx, txHash)
	if err != nil {
		s.logger.Error("failed to get SetCode authorization",
			zap.String("txHash", txHashStr),
			zap.Int("authIndex", authIndex),
			zap.Error(err))
		return nil, err
	}

	for _, record := range records {
		if record.AuthIndex == authIndex {
			return s.setCodeAuthorizationToMap(record), nil
		}
	}

	return nil, nil // Not found
}

// resolveSetCodeAuthorizationsByTx resolves all SetCode authorizations in a transaction
func (s *Schema) resolveSetCodeAuthorizationsByTx(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	txHashStr, ok := p.Args["txHash"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid txHash")
	}

	txHash := common.HexToHash(txHashStr)

	// Cast storage to SetCodeIndexReader
	setCodeReader, ok := s.storage.(storagepkg.SetCodeIndexReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support SetCode queries")
	}

	records, err := setCodeReader.GetSetCodeAuthorizationsByTx(ctx, txHash)
	if err != nil {
		s.logger.Error("failed to get SetCode authorizations by tx",
			zap.String("txHash", txHashStr),
			zap.Error(err))
		return nil, err
	}

	result := make([]interface{}, len(records))
	for i, record := range records {
		result[i] = s.setCodeAuthorizationToMap(record)
	}

	return result, nil
}

// resolveSetCodeAuthorizationsByTarget resolves SetCode authorizations where address is the target
func (s *Schema) resolveSetCodeAuthorizationsByTarget(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	targetStr, ok := p.Args["target"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid target address")
	}

	target := common.HexToAddress(targetStr)

	// Get pagination parameters
	limit := constants.DefaultPaginationLimit
	offset := 0
	if pagination, ok := p.Args["pagination"].(map[string]interface{}); ok {
		if l, ok := pagination["limit"].(int); ok && l > 0 {
			limit = l
			if limit > constants.DefaultMaxPaginationLimit {
				limit = constants.DefaultMaxPaginationLimit
			}
		}
		if o, ok := pagination["offset"].(int); ok && o >= 0 {
			offset = o
		}
	}

	// Cast storage to SetCodeIndexReader
	setCodeReader, ok := s.storage.(storagepkg.SetCodeIndexReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support SetCode queries")
	}

	records, err := setCodeReader.GetSetCodeAuthorizationsByTarget(ctx, target, limit, offset)
	if err != nil {
		s.logger.Error("failed to get SetCode authorizations by target",
			zap.String("target", targetStr),
			zap.Error(err))
		return nil, err
	}

	nodes := make([]interface{}, len(records))
	for i, record := range records {
		nodes[i] = s.setCodeAuthorizationToMap(record)
	}

	return map[string]interface{}{
		"nodes":      nodes,
		"totalCount": len(records), // Note: This is the count of returned records, not total
		"pageInfo": map[string]interface{}{
			"hasNextPage":     len(records) == limit,
			"hasPreviousPage": offset > 0,
			"startCursor":     nil,
			"endCursor":       nil,
		},
	}, nil
}

// resolveSetCodeAuthorizationsByAuthority resolves SetCode authorizations where address is the authority
func (s *Schema) resolveSetCodeAuthorizationsByAuthority(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	authorityStr, ok := p.Args["authority"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid authority address")
	}

	authority := common.HexToAddress(authorityStr)

	// Get pagination parameters
	limit := constants.DefaultPaginationLimit
	offset := 0
	if pagination, ok := p.Args["pagination"].(map[string]interface{}); ok {
		if l, ok := pagination["limit"].(int); ok && l > 0 {
			limit = l
			if limit > constants.DefaultMaxPaginationLimit {
				limit = constants.DefaultMaxPaginationLimit
			}
		}
		if o, ok := pagination["offset"].(int); ok && o >= 0 {
			offset = o
		}
	}

	// Cast storage to SetCodeIndexReader
	setCodeReader, ok := s.storage.(storagepkg.SetCodeIndexReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support SetCode queries")
	}

	records, err := setCodeReader.GetSetCodeAuthorizationsByAuthority(ctx, authority, limit, offset)
	if err != nil {
		s.logger.Error("failed to get SetCode authorizations by authority",
			zap.String("authority", authorityStr),
			zap.Error(err))
		return nil, err
	}

	nodes := make([]interface{}, len(records))
	for i, record := range records {
		nodes[i] = s.setCodeAuthorizationToMap(record)
	}

	return map[string]interface{}{
		"nodes":      nodes,
		"totalCount": len(records),
		"pageInfo": map[string]interface{}{
			"hasNextPage":     len(records) == limit,
			"hasPreviousPage": offset > 0,
			"startCursor":     nil,
			"endCursor":       nil,
		},
	}, nil
}

// resolveAddressSetCodeInfo resolves SetCode information for an address
func (s *Schema) resolveAddressSetCodeInfo(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	addressStr, ok := p.Args["address"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid address")
	}

	address := common.HexToAddress(addressStr)

	// Cast storage to SetCodeIndexReader
	setCodeReader, ok := s.storage.(storagepkg.SetCodeIndexReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support SetCode queries")
	}

	// Get delegation state
	delegationState, err := setCodeReader.GetAddressDelegationState(ctx, address)
	if err != nil {
		s.logger.Warn("failed to get delegation state",
			zap.String("address", addressStr),
			zap.Error(err))
		// Continue with default values
	}

	// Get stats
	stats, err := setCodeReader.GetAddressSetCodeStats(ctx, address)
	if err != nil {
		s.logger.Warn("failed to get SetCode stats",
			zap.String("address", addressStr),
			zap.Error(err))
		// Continue with default values
	}

	result := map[string]interface{}{
		"address":               addressStr,
		"hasDelegation":         false,
		"delegationTarget":      nil,
		"asTargetCount":         0,
		"asAuthorityCount":      0,
		"lastActivityBlock":     nil,
		"lastActivityTimestamp": nil,
	}

	if delegationState != nil {
		result["hasDelegation"] = delegationState.HasDelegation
		if delegationState.DelegationTarget != nil {
			result["delegationTarget"] = delegationState.DelegationTarget.Hex()
		}
	}

	if stats != nil {
		result["asTargetCount"] = stats.AsTargetCount
		result["asAuthorityCount"] = stats.AsAuthorityCount
		if stats.LastActivityBlock > 0 {
			result["lastActivityBlock"] = strconv.FormatUint(stats.LastActivityBlock, 10)
		}
	}

	return result, nil
}

// resolveSetCodeTransactionsInBlock resolves SetCode transactions in a specific block
func (s *Schema) resolveSetCodeTransactionsInBlock(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	blockNumberStr, ok := p.Args["blockNumber"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid blockNumber")
	}

	blockNumber, err := strconv.ParseUint(blockNumberStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid blockNumber format: %w", err)
	}

	// Cast storage to SetCodeIndexReader
	setCodeReader, ok := s.storage.(storagepkg.SetCodeIndexReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support SetCode queries")
	}

	records, err := setCodeReader.GetSetCodeAuthorizationsByBlock(ctx, blockNumber)
	if err != nil {
		s.logger.Error("failed to get SetCode authorizations by block",
			zap.Uint64("blockNumber", blockNumber),
			zap.Error(err))
		return nil, err
	}

	// Collect unique transaction hashes
	txHashes := make(map[common.Hash]bool)
	for _, record := range records {
		txHashes[record.TxHash] = true
	}

	// Fetch transactions
	result := make([]interface{}, 0, len(txHashes))
	for txHash := range txHashes {
		tx, location, err := s.storage.GetTransaction(ctx, txHash)
		if err != nil {
			s.logger.Warn("failed to get transaction",
				zap.String("txHash", txHash.Hex()),
				zap.Error(err))
			continue
		}
		if tx != nil {
			result = append(result, s.transactionToMap(tx, location))
		}
	}

	return result, nil
}

// resolveRecentSetCodeTransactions resolves recent SetCode transactions
func (s *Schema) resolveRecentSetCodeTransactions(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	limit := 10
	if l, ok := p.Args["limit"].(int); ok && l > 0 {
		limit = l
		if limit > 100 {
			limit = 100
		}
	}

	// Cast storage to SetCodeIndexReader
	setCodeReader, ok := s.storage.(storagepkg.SetCodeIndexReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support SetCode queries")
	}

	records, err := setCodeReader.GetRecentSetCodeAuthorizations(ctx, limit*2)
	if err != nil {
		s.logger.Error("failed to get recent SetCode authorizations",
			zap.Error(err))
		return nil, err
	}

	// Collect unique transaction hashes (up to limit)
	txHashes := make([]common.Hash, 0, limit)
	seen := make(map[common.Hash]bool)
	for _, record := range records {
		if !seen[record.TxHash] {
			seen[record.TxHash] = true
			txHashes = append(txHashes, record.TxHash)
			if len(txHashes) >= limit {
				break
			}
		}
	}

	// Fetch transactions
	result := make([]interface{}, 0, len(txHashes))
	for _, txHash := range txHashes {
		tx, location, err := s.storage.GetTransaction(ctx, txHash)
		if err != nil {
			s.logger.Warn("failed to get transaction",
				zap.String("txHash", txHash.Hex()),
				zap.Error(err))
			continue
		}
		if tx != nil {
			result = append(result, s.transactionToMap(tx, location))
		}
	}

	return result, nil
}

// resolveSetCodeTransactionCount resolves the total count of SetCode transactions
func (s *Schema) resolveSetCodeTransactionCount(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	// Cast storage to SetCodeIndexReader
	setCodeReader, ok := s.storage.(storagepkg.SetCodeIndexReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support SetCode queries")
	}

	count, err := setCodeReader.GetSetCodeTransactionCount(ctx)
	if err != nil {
		s.logger.Error("failed to get SetCode transaction count",
			zap.Error(err))
		return nil, err
	}

	return count, nil
}

// setCodeAuthorizationToMap converts a SetCodeAuthorizationRecord to a GraphQL map
func (s *Schema) setCodeAuthorizationToMap(record *storagepkg.SetCodeAuthorizationRecord) map[string]interface{} {
	result := map[string]interface{}{
		"txHash":             record.TxHash.Hex(),
		"blockNumber":        strconv.FormatUint(record.BlockNumber, 10),
		"blockHash":          record.BlockHash.Hex(),
		"transactionIndex":   int(record.TxIndex),
		"authorizationIndex": record.AuthIndex,
		"chainId":            record.ChainID.String(),
		"address":            record.TargetAddress.Hex(),
		"nonce":              strconv.FormatUint(record.Nonce, 10),
		"yParity":            int(record.YParity),
		"r":                  "0x" + record.R.Text(16),
		"s":                  "0x" + record.S.Text(16),
		"applied":            record.Applied,
		"timestamp":          strconv.FormatInt(record.Timestamp.Unix(), 10),
	}

	if record.AuthorityAddress != (common.Address{}) {
		result["authority"] = record.AuthorityAddress.Hex()
	}

	if record.Error != "" && record.Error != storagepkg.SetCodeErrNone {
		result["error"] = record.Error
	}

	return result
}
