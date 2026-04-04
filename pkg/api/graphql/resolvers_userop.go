package graphql

import (
	"fmt"
	"strconv"

	"github.com/0xmhha/indexer-go/internal/constants"
	storagepkg "github.com/0xmhha/indexer-go/pkg/storage"
	"github.com/0xmhha/indexer-go/pkg/userop"
	"github.com/ethereum/go-ethereum/common"
	"github.com/graphql-go/graphql"
	"go.uber.org/zap"
)

// resolveUserOperation resolves a single UserOperation by its hash
func (s *Schema) resolveUserOperation(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	hashStr, ok := p.Args["hash"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid hash")
	}

	opHash := common.HexToHash(hashStr)

	userOpReader, ok := s.storage.(storagepkg.UserOpIndexReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support UserOp queries")
	}

	op, err := userOpReader.GetUserOp(ctx, opHash)
	if err != nil {
		if err == storagepkg.ErrNotFound {
			return nil, nil
		}
		s.logger.Error("failed to get UserOperation",
			zap.String("hash", hashStr),
			zap.Error(err))
		return nil, err
	}

	return s.userOpToMap(op), nil
}

// resolveUserOperations resolves a paginated list of UserOperations with optional sender filter
func (s *Schema) resolveUserOperations(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

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

	userOpReader, ok := s.storage.(storagepkg.UserOpIndexReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support UserOp queries")
	}

	// If sender filter is provided, use GetUserOpsBySender
	if senderStr, ok := p.Args["sender"].(string); ok && senderStr != "" {
		sender := common.HexToAddress(senderStr)
		ops, err := userOpReader.GetUserOpsBySender(ctx, sender, limit, offset)
		if err != nil {
			s.logger.Error("failed to get UserOperations by sender",
				zap.String("sender", senderStr),
				zap.Error(err))
			return nil, err
		}
		return s.userOpConnectionResult(ops, limit, offset), nil
	}

	// Otherwise return recent UserOps
	ops, err := userOpReader.GetRecentUserOps(ctx, limit)
	if err != nil {
		s.logger.Error("failed to get recent UserOperations",
			zap.Error(err))
		return nil, err
	}

	// Apply offset manually for recent ops
	if offset > 0 && offset < len(ops) {
		ops = ops[offset:]
	} else if offset >= len(ops) {
		ops = nil
	}
	if len(ops) > limit {
		ops = ops[:limit]
	}

	return s.userOpConnectionResult(ops, limit, offset), nil
}

// resolveUserOperationsByAddress resolves UserOperations by sender address
func (s *Schema) resolveUserOperationsByAddress(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	senderStr, ok := p.Args["sender"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid sender address")
	}

	sender := common.HexToAddress(senderStr)

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

	userOpReader, ok := s.storage.(storagepkg.UserOpIndexReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support UserOp queries")
	}

	ops, err := userOpReader.GetUserOpsBySender(ctx, sender, limit, offset)
	if err != nil {
		s.logger.Error("failed to get UserOperations by sender",
			zap.String("sender", senderStr),
			zap.Error(err))
		return nil, err
	}

	return s.userOpConnectionResult(ops, limit, offset), nil
}

// resolveBundlers resolves a paginated list of bundler stats
func (s *Schema) resolveBundlers(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

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

	userOpReader, ok := s.storage.(storagepkg.UserOpIndexReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support UserOp queries")
	}

	bundlers, err := userOpReader.ListBundlers(ctx, limit, offset)
	if err != nil {
		s.logger.Error("failed to list bundlers", zap.Error(err))
		return nil, err
	}

	nodes := make([]interface{}, len(bundlers))
	for i, b := range bundlers {
		nodes[i] = map[string]interface{}{
			"address":      b.Address.Hex(),
			"totalBundles": strconv.FormatUint(b.TotalBundles, 10),
			"totalOps":     strconv.FormatUint(b.TotalOps, 10),
		}
	}

	return map[string]interface{}{
		"nodes":      nodes,
		"totalCount": len(nodes),
		"pageInfo": map[string]interface{}{
			"hasNextPage":     len(nodes) == limit,
			"hasPreviousPage": offset > 0,
			"startCursor":     nil,
			"endCursor":       nil,
		},
	}, nil
}

// resolveBundler resolves a single bundler's stats by address
func (s *Schema) resolveBundler(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	addressStr, ok := p.Args["address"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid address")
	}

	userOpReader, ok := s.storage.(storagepkg.UserOpIndexReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support UserOp queries")
	}

	stats, err := userOpReader.GetBundlerStats(ctx, common.HexToAddress(addressStr))
	if err != nil {
		s.logger.Error("failed to get bundler stats", zap.String("address", addressStr), zap.Error(err))
		return nil, err
	}

	return map[string]interface{}{
		"address":      stats.Address.Hex(),
		"totalBundles": strconv.FormatUint(stats.TotalBundles, 10),
		"totalOps":     strconv.FormatUint(stats.TotalOps, 10),
	}, nil
}

// resolveFactories resolves a paginated list of factory stats
func (s *Schema) resolveFactories(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

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

	userOpReader, ok := s.storage.(storagepkg.UserOpIndexReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support UserOp queries")
	}

	factories, err := userOpReader.ListFactories(ctx, limit, offset)
	if err != nil {
		s.logger.Error("failed to list factories", zap.Error(err))
		return nil, err
	}

	nodes := make([]interface{}, len(factories))
	for i, f := range factories {
		nodes[i] = map[string]interface{}{
			"address":       f.Address.Hex(),
			"totalAccounts": strconv.FormatUint(f.TotalAccounts, 10),
		}
	}

	return map[string]interface{}{
		"nodes":      nodes,
		"totalCount": len(nodes),
		"pageInfo": map[string]interface{}{
			"hasNextPage":     len(nodes) == limit,
			"hasPreviousPage": offset > 0,
			"startCursor":     nil,
			"endCursor":       nil,
		},
	}, nil
}

// resolveFactory resolves a single factory's stats by address
func (s *Schema) resolveFactory(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	addressStr, ok := p.Args["address"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid address")
	}

	userOpReader, ok := s.storage.(storagepkg.UserOpIndexReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support UserOp queries")
	}

	stats, err := userOpReader.GetFactoryStats(ctx, common.HexToAddress(addressStr))
	if err != nil {
		s.logger.Error("failed to get factory stats", zap.String("address", addressStr), zap.Error(err))
		return nil, err
	}

	return map[string]interface{}{
		"address":       stats.Address.Hex(),
		"totalAccounts": strconv.FormatUint(stats.TotalAccounts, 10),
	}, nil
}

// resolvePaymasters resolves a paginated list of paymaster stats
func (s *Schema) resolvePaymasters(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

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

	userOpReader, ok := s.storage.(storagepkg.UserOpIndexReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support UserOp queries")
	}

	paymasters, err := userOpReader.ListPaymasters(ctx, limit, offset)
	if err != nil {
		s.logger.Error("failed to list paymasters", zap.Error(err))
		return nil, err
	}

	nodes := make([]interface{}, len(paymasters))
	for i, pm := range paymasters {
		nodes[i] = map[string]interface{}{
			"address":  pm.Address.Hex(),
			"totalOps": strconv.FormatUint(pm.TotalOps, 10),
		}
	}

	return map[string]interface{}{
		"nodes":      nodes,
		"totalCount": len(nodes),
		"pageInfo": map[string]interface{}{
			"hasNextPage":     len(nodes) == limit,
			"hasPreviousPage": offset > 0,
			"startCursor":     nil,
			"endCursor":       nil,
		},
	}, nil
}

// resolvePaymaster resolves a single paymaster's stats by address
func (s *Schema) resolvePaymaster(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	addressStr, ok := p.Args["address"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid address")
	}

	userOpReader, ok := s.storage.(storagepkg.UserOpIndexReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support UserOp queries")
	}

	stats, err := userOpReader.GetPaymasterStats(ctx, common.HexToAddress(addressStr))
	if err != nil {
		s.logger.Error("failed to get paymaster stats", zap.String("address", addressStr), zap.Error(err))
		return nil, err
	}

	return map[string]interface{}{
		"address":  stats.Address.Hex(),
		"totalOps": strconv.FormatUint(stats.TotalOps, 10),
	}, nil
}

// resolveSmartAccounts resolves a paginated list of smart accounts
func (s *Schema) resolveSmartAccounts(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

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

	userOpReader, ok := s.storage.(storagepkg.UserOpIndexReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support UserOp queries")
	}

	accounts, err := userOpReader.ListSmartAccounts(ctx, limit, offset)
	if err != nil {
		s.logger.Error("failed to list smart accounts", zap.Error(err))
		return nil, err
	}

	nodes := make([]interface{}, len(accounts))
	for i, a := range accounts {
		nodes[i] = s.smartAccountToMap(a)
	}

	return map[string]interface{}{
		"nodes":      nodes,
		"totalCount": len(nodes),
		"pageInfo": map[string]interface{}{
			"hasNextPage":     len(nodes) == limit,
			"hasPreviousPage": offset > 0,
			"startCursor":     nil,
			"endCursor":       nil,
		},
	}, nil
}

// resolveSmartAccount resolves a single smart account by address
func (s *Schema) resolveSmartAccount(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	addressStr, ok := p.Args["address"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid address")
	}

	userOpReader, ok := s.storage.(storagepkg.UserOpIndexReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support UserOp queries")
	}

	account, err := userOpReader.GetSmartAccount(ctx, common.HexToAddress(addressStr))
	if err != nil {
		if err == storagepkg.ErrNotFound {
			return nil, nil
		}
		s.logger.Error("failed to get smart account", zap.String("address", addressStr), zap.Error(err))
		return nil, err
	}

	return s.smartAccountToMap(account), nil
}

// resolveUserOpCount resolves the total count of UserOperations
func (s *Schema) resolveUserOpCount(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	userOpReader, ok := s.storage.(storagepkg.UserOpIndexReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support UserOp queries")
	}

	count, err := userOpReader.GetUserOpCount(ctx)
	if err != nil {
		s.logger.Error("failed to get UserOperation count", zap.Error(err))
		return nil, err
	}

	return count, nil
}

// ========== Helper Functions ==========

// userOpToMap converts a UserOperation to a GraphQL map
func (s *Schema) userOpToMap(op *userop.UserOperation) map[string]interface{} {
	result := map[string]interface{}{
		"hash":                 op.Hash.Hex(),
		"sender":               op.Sender.Hex(),
		"nonce":                op.Nonce,
		"callData":             "0x" + common.Bytes2Hex(op.CallData),
		"callGasLimit":         op.CallGasLimit,
		"verificationGasLimit": op.VerificationGasLimit,
		"preVerificationGas":   op.PreVerificationGas,
		"maxFeePerGas":         op.MaxFeePerGas,
		"maxPriorityFeePerGas": op.MaxPriorityFeePerGas,
		"signature":            "0x" + common.Bytes2Hex(op.Signature),
		"entryPoint":           op.EntryPoint.Hex(),
		"entryPointVersion":    op.EntryPointVersion,
		"transactionHash":      op.TransactionHash.Hex(),
		"blockNumber":          strconv.FormatUint(op.BlockNumber, 10),
		"blockHash":            op.BlockHash.Hex(),
		"bundleIndex":          int(op.BundleIndex),
		"bundler":              op.Bundler.Hex(),
		"status":               op.Status,
		"gasUsed":              op.GasUsed,
		"actualGasCost":        op.ActualGasCost,
		"sponsorType":          string(op.SponsorType),
		"userLogsStartIndex":   int(op.UserLogsStartIndex),
		"userLogsCount":        int(op.UserLogsCount),
		"timestamp":            strconv.FormatInt(op.Timestamp.Unix(), 10),
	}

	if op.Factory != nil {
		result["factory"] = op.Factory.Hex()
	}
	if op.Paymaster != nil {
		result["paymaster"] = op.Paymaster.Hex()
	}
	if len(op.RevertReason) > 0 {
		result["revertReason"] = "0x" + common.Bytes2Hex(op.RevertReason)
	}

	return result
}

// smartAccountToMap converts a SmartAccount to a GraphQL map
func (s *Schema) smartAccountToMap(account *userop.SmartAccount) map[string]interface{} {
	result := map[string]interface{}{
		"address":  account.Address.Hex(),
		"totalOps": strconv.FormatUint(account.TotalOps, 10),
	}

	if account.CreationOpHash != nil {
		result["creationOpHash"] = account.CreationOpHash.Hex()
	}
	if account.CreationTxHash != nil {
		result["creationTxHash"] = account.CreationTxHash.Hex()
	}
	if account.CreationTimestamp != nil {
		result["creationTimestamp"] = strconv.FormatInt(account.CreationTimestamp.Unix(), 10)
	}
	if account.Factory != nil {
		result["factory"] = account.Factory.Hex()
	}

	return result
}

// userOpConnectionResult creates a standard connection result for UserOperations
func (s *Schema) userOpConnectionResult(ops []*userop.UserOperation, limit, offset int) map[string]interface{} {
	nodes := make([]interface{}, len(ops))
	for i, op := range ops {
		nodes[i] = s.userOpToMap(op)
	}

	return map[string]interface{}{
		"nodes":      nodes,
		"totalCount": len(nodes),
		"pageInfo": map[string]interface{}{
			"hasNextPage":     len(nodes) == limit,
			"hasPreviousPage": offset > 0,
			"startCursor":     nil,
			"endCursor":       nil,
		},
	}
}
