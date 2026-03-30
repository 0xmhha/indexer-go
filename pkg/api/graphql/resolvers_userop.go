package graphql

import (
	"encoding/hex"
	"fmt"
	"strconv"

	"github.com/0xmhha/indexer-go/internal/constants"
	storagepkg "github.com/0xmhha/indexer-go/pkg/storage"
	"github.com/ethereum/go-ethereum/common"
	"github.com/graphql-go/graphql"
	"go.uber.org/zap"
)

// resolveUserOp resolves a UserOperation by its hash
func (s *Schema) resolveUserOp(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	hashStr, ok := p.Args["userOpHash"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid userOpHash")
	}

	userOpReader, ok := s.storage.(storagepkg.UserOpIndexReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support UserOp queries")
	}

	record, err := userOpReader.GetUserOp(ctx, common.HexToHash(hashStr))
	if err != nil {
		if err == storagepkg.ErrNotFound {
			return nil, nil
		}
		s.logger.Error("failed to get UserOp", zap.String("hash", hashStr), zap.Error(err))
		return nil, err
	}

	return userOpToMap(record), nil
}

// resolveUserOpsByTx resolves all UserOps in a transaction
func (s *Schema) resolveUserOpsByTx(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	txHashStr, ok := p.Args["txHash"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid txHash")
	}

	reader, ok := s.storage.(storagepkg.UserOpIndexReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support UserOp queries")
	}

	records, err := reader.GetUserOpsByTx(ctx, common.HexToHash(txHashStr))
	if err != nil {
		s.logger.Error("failed to get UserOps by tx", zap.String("txHash", txHashStr), zap.Error(err))
		return nil, err
	}

	return userOpsToSlice(records), nil
}

// resolveUserOpsBySender resolves UserOps by sender address with pagination
func (s *Schema) resolveUserOpsBySender(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	addrStr, ok := p.Args["sender"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid sender address")
	}

	limit, offset := extractPagination(p)

	reader, ok := s.storage.(storagepkg.UserOpIndexReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support UserOp queries")
	}

	sender := common.HexToAddress(addrStr)
	records, err := reader.GetUserOpsBySender(ctx, sender, limit, offset)
	if err != nil {
		s.logger.Error("failed to get UserOps by sender", zap.String("sender", addrStr), zap.Error(err))
		return nil, err
	}

	totalCount, _ := reader.GetUserOpsCountBySender(ctx, sender)

	return map[string]interface{}{
		"nodes":      userOpsToSlice(records),
		"totalCount": totalCount,
		"pageInfo": map[string]interface{}{
			"hasNextPage": offset+limit < totalCount,
			"offset":      offset,
			"limit":       limit,
		},
	}, nil
}

// resolveUserOpsByBundler resolves UserOps by bundler address with pagination
func (s *Schema) resolveUserOpsByBundler(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	addrStr, ok := p.Args["bundler"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid bundler address")
	}

	limit, offset := extractPagination(p)

	reader, ok := s.storage.(storagepkg.UserOpIndexReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support UserOp queries")
	}

	bundler := common.HexToAddress(addrStr)
	records, err := reader.GetUserOpsByBundler(ctx, bundler, limit, offset)
	if err != nil {
		s.logger.Error("failed to get UserOps by bundler", zap.String("bundler", addrStr), zap.Error(err))
		return nil, err
	}

	totalCount, _ := reader.GetUserOpsCountByBundler(ctx, bundler)

	return map[string]interface{}{
		"nodes":      userOpsToSlice(records),
		"totalCount": totalCount,
		"pageInfo": map[string]interface{}{
			"hasNextPage": offset+limit < totalCount,
			"offset":      offset,
			"limit":       limit,
		},
	}, nil
}

// resolveUserOpsByPaymaster resolves UserOps by paymaster address with pagination
func (s *Schema) resolveUserOpsByPaymaster(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	addrStr, ok := p.Args["paymaster"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid paymaster address")
	}

	limit, offset := extractPagination(p)

	reader, ok := s.storage.(storagepkg.UserOpIndexReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support UserOp queries")
	}

	paymaster := common.HexToAddress(addrStr)
	records, err := reader.GetUserOpsByPaymaster(ctx, paymaster, limit, offset)
	if err != nil {
		s.logger.Error("failed to get UserOps by paymaster", zap.String("paymaster", addrStr), zap.Error(err))
		return nil, err
	}

	totalCount, _ := reader.GetUserOpsCountByPaymaster(ctx, paymaster)

	return map[string]interface{}{
		"nodes":      userOpsToSlice(records),
		"totalCount": totalCount,
		"pageInfo": map[string]interface{}{
			"hasNextPage": offset+limit < totalCount,
			"offset":      offset,
			"limit":       limit,
		},
	}, nil
}

// resolveUserOpsByBlock resolves all UserOps in a specific block
func (s *Schema) resolveUserOpsByBlock(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	blockNumStr, ok := p.Args["blockNumber"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid blockNumber")
	}

	blockNumber, err := strconv.ParseUint(blockNumStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid blockNumber: %w", err)
	}

	reader, ok := s.storage.(storagepkg.UserOpIndexReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support UserOp queries")
	}

	records, err := reader.GetUserOpsByBlock(ctx, blockNumber)
	if err != nil {
		s.logger.Error("failed to get UserOps by block", zap.Uint64("block", blockNumber), zap.Error(err))
		return nil, err
	}

	return userOpsToSlice(records), nil
}

// resolveAccountDeployment resolves an account deployment by userOpHash
func (s *Schema) resolveAccountDeployment(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	hashStr, ok := p.Args["userOpHash"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid userOpHash")
	}

	reader, ok := s.storage.(storagepkg.UserOpIndexReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support UserOp queries")
	}

	record, err := reader.GetAccountDeployment(ctx, common.HexToHash(hashStr))
	if err != nil {
		if err == storagepkg.ErrNotFound {
			return nil, nil
		}
		s.logger.Error("failed to get account deployment", zap.String("hash", hashStr), zap.Error(err))
		return nil, err
	}

	return accountDeployedToMap(record), nil
}

// resolveAccountDeploymentsByFactory resolves deployments by factory address
func (s *Schema) resolveAccountDeploymentsByFactory(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	addrStr, ok := p.Args["factory"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid factory address")
	}

	limit, offset := extractPagination(p)

	reader, ok := s.storage.(storagepkg.UserOpIndexReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support UserOp queries")
	}

	records, err := reader.GetAccountDeploymentsByFactory(ctx, common.HexToAddress(addrStr), limit, offset)
	if err != nil {
		s.logger.Error("failed to get deployments by factory", zap.String("factory", addrStr), zap.Error(err))
		return nil, err
	}

	result := make([]interface{}, len(records))
	for i, r := range records {
		result[i] = accountDeployedToMap(r)
	}

	return result, nil
}

// resolveUserOpRevert resolves a revert reason by userOpHash
func (s *Schema) resolveUserOpRevert(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	hashStr, ok := p.Args["userOpHash"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid userOpHash")
	}

	reader, ok := s.storage.(storagepkg.UserOpIndexReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support UserOp queries")
	}

	record, err := reader.GetUserOpRevert(ctx, common.HexToHash(hashStr))
	if err != nil {
		if err == storagepkg.ErrNotFound {
			return nil, nil
		}
		s.logger.Error("failed to get UserOp revert", zap.String("hash", hashStr), zap.Error(err))
		return nil, err
	}

	return userOpRevertToMap(record), nil
}

// resolveBundlerStats resolves aggregated stats for a bundler
func (s *Schema) resolveBundlerStats(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	addrStr, ok := p.Args["bundler"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid bundler address")
	}

	reader, ok := s.storage.(storagepkg.UserOpIndexReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support UserOp queries")
	}

	stats, err := reader.GetBundlerStats(ctx, common.HexToAddress(addrStr))
	if err != nil {
		s.logger.Error("failed to get bundler stats", zap.String("bundler", addrStr), zap.Error(err))
		return nil, err
	}

	return bundlerStatsToMap(stats), nil
}

// resolvePaymasterStats resolves aggregated stats for a paymaster
func (s *Schema) resolvePaymasterStats(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	addrStr, ok := p.Args["paymaster"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid paymaster address")
	}

	reader, ok := s.storage.(storagepkg.UserOpIndexReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support UserOp queries")
	}

	stats, err := reader.GetPaymasterStats(ctx, common.HexToAddress(addrStr))
	if err != nil {
		s.logger.Error("failed to get paymaster stats", zap.String("paymaster", addrStr), zap.Error(err))
		return nil, err
	}

	return paymasterStatsToMap(stats), nil
}

// resolveUserOpCount resolves total UserOp count
func (s *Schema) resolveUserOpCount(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	reader, ok := s.storage.(storagepkg.UserOpIndexReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support UserOp queries")
	}

	count, err := reader.GetUserOpCount(ctx)
	if err != nil {
		s.logger.Error("failed to get UserOp count", zap.Error(err))
		return nil, err
	}

	return count, nil
}

// resolveRecentUserOps resolves most recent UserOps
func (s *Schema) resolveRecentUserOps(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	limit := constants.DefaultPaginationLimit
	if l, ok := p.Args["limit"].(int); ok && l > 0 {
		limit = l
	}

	reader, ok := s.storage.(storagepkg.UserOpIndexReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support UserOp queries")
	}

	records, err := reader.GetRecentUserOps(ctx, limit)
	if err != nil {
		s.logger.Error("failed to get recent UserOps", zap.Error(err))
		return nil, err
	}

	return userOpsToSlice(records), nil
}

// resolveAllBundlers resolves paginated list of all known bundlers
func (s *Schema) resolveAllBundlers(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	limit, offset := extractPagination(p)

	reader, ok := s.storage.(storagepkg.UserOpIndexReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support UserOp queries")
	}

	stats, err := reader.GetAllBundlerStats(ctx, limit, offset)
	if err != nil {
		s.logger.Error("failed to get all bundler stats", zap.Error(err))
		return nil, err
	}

	totalCount, _ := reader.GetAllBundlerStatsCount(ctx)

	nodes := make([]interface{}, len(stats))
	for i, stat := range stats {
		nodes[i] = bundlerStatsToMap(stat)
	}

	return map[string]interface{}{
		"nodes":      nodes,
		"totalCount": totalCount,
		"pageInfo": map[string]interface{}{
			"hasNextPage": offset+limit < totalCount,
			"offset":      offset,
			"limit":       limit,
		},
	}, nil
}

// resolveAllPaymasters resolves paginated list of all known paymasters
func (s *Schema) resolveAllPaymasters(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	limit, offset := extractPagination(p)

	reader, ok := s.storage.(storagepkg.UserOpIndexReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support UserOp queries")
	}

	stats, err := reader.GetAllPaymasterStats(ctx, limit, offset)
	if err != nil {
		s.logger.Error("failed to get all paymaster stats", zap.Error(err))
		return nil, err
	}

	totalCount, _ := reader.GetAllPaymasterStatsCount(ctx)

	nodes := make([]interface{}, len(stats))
	for i, stat := range stats {
		nodes[i] = paymasterStatsToMap(stat)
	}

	return map[string]interface{}{
		"nodes":      nodes,
		"totalCount": totalCount,
		"pageInfo": map[string]interface{}{
			"hasNextPage": offset+limit < totalCount,
			"offset":      offset,
			"limit":       limit,
		},
	}, nil
}

// ========== Helper conversion functions ==========

func userOpToMap(r *storagepkg.UserOperationRecord) map[string]interface{} {
	return map[string]interface{}{
		"userOpHash":            r.UserOpHash.Hex(),
		"txHash":                r.TxHash.Hex(),
		"blockNumber":           strconv.FormatUint(r.BlockNumber, 10),
		"blockHash":             r.BlockHash.Hex(),
		"txIndex":               int(r.TxIndex),
		"logIndex":              int(r.LogIndex),
		"sender":                r.Sender.Hex(),
		"paymaster":             r.Paymaster.Hex(),
		"nonce":                 r.Nonce.String(),
		"success":               r.Success,
		"actualGasCost":         r.ActualGasCost.String(),
		"actualUserOpFeePerGas": r.ActualUserOpFeePerGas.String(),
		"bundler":               r.Bundler.Hex(),
		"entryPoint":            r.EntryPoint.Hex(),
		"timestamp":             strconv.FormatInt(r.Timestamp.Unix(), 10),
	}
}

func userOpsToSlice(records []*storagepkg.UserOperationRecord) []interface{} {
	result := make([]interface{}, len(records))
	for i, r := range records {
		result[i] = userOpToMap(r)
	}
	return result
}

func accountDeployedToMap(r *storagepkg.AccountDeployedRecord) map[string]interface{} {
	return map[string]interface{}{
		"userOpHash":  r.UserOpHash.Hex(),
		"sender":      r.Sender.Hex(),
		"factory":     r.Factory.Hex(),
		"paymaster":   r.Paymaster.Hex(),
		"txHash":      r.TxHash.Hex(),
		"blockNumber": strconv.FormatUint(r.BlockNumber, 10),
		"logIndex":    int(r.LogIndex),
		"timestamp":   strconv.FormatInt(r.Timestamp.Unix(), 10),
	}
}

func userOpRevertToMap(r *storagepkg.UserOpRevertRecord) map[string]interface{} {
	return map[string]interface{}{
		"userOpHash":   r.UserOpHash.Hex(),
		"sender":       r.Sender.Hex(),
		"nonce":        r.Nonce.String(),
		"revertReason": "0x" + hex.EncodeToString(r.RevertReason),
		"txHash":       r.TxHash.Hex(),
		"blockNumber":  strconv.FormatUint(r.BlockNumber, 10),
		"logIndex":     int(r.LogIndex),
		"revertType":   r.RevertType,
		"timestamp":    strconv.FormatInt(r.Timestamp.Unix(), 10),
	}
}

func bundlerStatsToMap(s *storagepkg.BundlerStats) map[string]interface{} {
	gasSponsored := "0"
	if s.TotalGasSponsored != nil {
		gasSponsored = s.TotalGasSponsored.String()
	}
	return map[string]interface{}{
		"address":           s.Address.Hex(),
		"totalOps":          s.TotalOps,
		"successfulOps":     s.SuccessfulOps,
		"failedOps":         s.FailedOps,
		"totalGasSponsored": gasSponsored,
		"lastActivityBlock": strconv.FormatUint(s.LastActivityBlock, 10),
		"lastActivityTime":  strconv.FormatInt(s.LastActivityTime.Unix(), 10),
	}
}

func paymasterStatsToMap(s *storagepkg.PaymasterStats) map[string]interface{} {
	gasSponsored := "0"
	if s.TotalGasSponsored != nil {
		gasSponsored = s.TotalGasSponsored.String()
	}
	return map[string]interface{}{
		"address":           s.Address.Hex(),
		"totalOps":          s.TotalOps,
		"successfulOps":     s.SuccessfulOps,
		"failedOps":         s.FailedOps,
		"totalGasSponsored": gasSponsored,
		"lastActivityBlock": strconv.FormatUint(s.LastActivityBlock, 10),
		"lastActivityTime":  strconv.FormatInt(s.LastActivityTime.Unix(), 10),
	}
}

// extractPagination extracts limit and offset from GraphQL pagination args
func extractPagination(p graphql.ResolveParams) (int, int) {
	limit := constants.DefaultPaginationLimit
	offset := 0

	if pagination, ok := p.Args["pagination"].(map[string]interface{}); ok {
		if l, ok := pagination["limit"].(int); ok && l > 0 {
			limit = l
		}
		if o, ok := pagination["offset"].(int); ok && o > 0 {
			offset = o
		}
	}

	return limit, offset
}
