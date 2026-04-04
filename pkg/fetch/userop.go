package fetch

import (
	"context"
	"encoding/hex"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"

	storagepkg "github.com/0xmhha/indexer-go/pkg/storage"
	"github.com/0xmhha/indexer-go/pkg/userop"
)

// UserOpIndexer defines the interface for indexing ERC-4337 UserOperations
type UserOpIndexer interface {
	storagepkg.UserOpIndexWriter
	storagepkg.UserOpIndexReader
}

// UserOpProcessor handles processing of ERC-4337 UserOperations from blocks
type UserOpProcessor struct {
	logger  *zap.Logger
	storage UserOpIndexer
}

// NewUserOpProcessor creates a new UserOp processor
func NewUserOpProcessor(logger *zap.Logger, storage UserOpIndexer) *UserOpProcessor {
	return &UserOpProcessor{
		logger:  logger.Named("userop"),
		storage: storage,
	}
}

// ProcessUserOpsFromBlock processes all ERC-4337 UserOperations from a block's receipts.
// It scans all transaction receipts for known EntryPoint event logs and extracts UserOp data.
func (p *UserOpProcessor) ProcessUserOpsFromBlock(
	ctx context.Context,
	block *types.Block,
	receipts types.Receipts,
) error {
	blockNumber := block.NumberU64()
	blockHash := block.Hash()
	blockTime := time.Unix(int64(block.Time()), 0)

	transactions := block.Transactions()
	receiptMap := make(map[common.Hash]*types.Receipt, len(receipts))
	for _, receipt := range receipts {
		if receipt != nil {
			receiptMap[receipt.TxHash] = receipt
		}
	}

	var allOps []*userop.UserOperation
	bundlerTxCounts := make(map[common.Address]int) // Track bundles per bundler in this block

	for _, tx := range transactions {
		receipt, ok := receiptMap[tx.Hash()]
		if !ok || receipt == nil {
			continue
		}

		// Check if this transaction contains any EntryPoint events
		entryPointAddr, version := p.detectEntryPointTx(receipt)
		if entryPointAddr == (common.Address{}) {
			continue
		}

		// Extract bundler (tx.From())
		bundler := getTransactionSender(tx)

		// Track that this bundler submitted a bundle
		bundlerTxCounts[bundler]++

		// Parse UserOperationEvent logs from this receipt
		ops := p.parseUserOpsFromReceipt(receipt, entryPointAddr, version, bundler, blockNumber, blockHash, blockTime)
		allOps = append(allOps, ops...)
	}

	if len(allOps) == 0 {
		return nil
	}

	// Save all UserOps in batch
	if err := p.storage.SaveUserOps(ctx, allOps); err != nil {
		p.logger.Error("Failed to save UserOperations",
			zap.Uint64("blockNumber", blockNumber),
			zap.Int("count", len(allOps)),
			zap.Error(err))
		return err
	}

	// Update stats for bundlers, paymasters, factories, and smart accounts
	if err := p.updateStats(ctx, allOps, bundlerTxCounts); err != nil {
		p.logger.Warn("Failed to update ERC-4337 stats",
			zap.Uint64("blockNumber", blockNumber),
			zap.Error(err))
		// Don't fail block processing for stats errors
	}

	p.logger.Info("Indexed ERC-4337 UserOperations",
		zap.Uint64("blockNumber", blockNumber),
		zap.Int("userOpCount", len(allOps)))

	return nil
}

// detectEntryPointTx checks if a transaction receipt contains any known EntryPoint events.
// Returns the EntryPoint address and version if found.
func (p *UserOpProcessor) detectEntryPointTx(receipt *types.Receipt) (common.Address, string) {
	for _, log := range receipt.Logs {
		if len(log.Topics) > 0 && log.Topics[0] == userop.UserOperationEventSig {
			if v := userop.GetEntryPointVersion(log.Address); v != "" {
				return log.Address, v
			}
		}
	}
	return common.Address{}, ""
}

// parseUserOpsFromReceipt extracts UserOperation records from a receipt's logs.
func (p *UserOpProcessor) parseUserOpsFromReceipt(
	receipt *types.Receipt,
	entryPoint common.Address,
	epVersion string,
	bundler common.Address,
	blockNumber uint64,
	blockHash common.Hash,
	blockTime time.Time,
) []*userop.UserOperation {
	var ops []*userop.UserOperation

	// Collect AccountDeployed events for factory detection
	accountDeployedMap := make(map[common.Hash]*accountDeployedInfo)
	// Collect revert reasons
	revertReasonMap := make(map[common.Hash][]byte)

	for _, log := range receipt.Logs {
		if log.Address != entryPoint || len(log.Topics) == 0 {
			continue
		}

		switch log.Topics[0] {
		case userop.AccountDeployedSig:
			if len(log.Topics) >= 3 && len(log.Data) >= 64 {
				opHash := log.Topics[1]
				info := &accountDeployedInfo{}
				// factory is first 32-byte word in data (padded address)
				factoryAddr := common.BytesToAddress(log.Data[12:32])
				if factoryAddr != (common.Address{}) {
					info.factory = &factoryAddr
				}
				// paymaster is second 32-byte word in data
				paymasterAddr := common.BytesToAddress(log.Data[44:64])
				if paymasterAddr != (common.Address{}) {
					info.paymaster = &paymasterAddr
				}
				accountDeployedMap[opHash] = info
			}

		case userop.UserOperationRevertReasonSig:
			if len(log.Topics) >= 3 && len(log.Data) >= 64 {
				opHash := log.Topics[1]
				// Data layout: nonce (32 bytes) + offset (32 bytes) + length (32 bytes) + revert reason bytes
				if len(log.Data) >= 96 {
					// ABI decode: skip nonce (32) + offset (32) + length (32), then read bytes
					length := new(big.Int).SetBytes(log.Data[64:96]).Uint64()
					if uint64(len(log.Data)) >= 96+length {
						revertReasonMap[opHash] = log.Data[96 : 96+length]
					}
				}
			}
		}
	}

	// Now parse UserOperationEvent logs
	var bundleIndex uint32
	for _, log := range receipt.Logs {
		if log.Address != entryPoint || len(log.Topics) == 0 {
			continue
		}

		if log.Topics[0] != userop.UserOperationEventSig {
			continue
		}

		// UserOperationEvent(bytes32 indexed userOpHash, address indexed sender, address indexed paymaster, uint256 nonce, bool success, uint256 actualGasCost, uint256 actualGasUsed)
		if len(log.Topics) < 4 || len(log.Data) < 128 {
			p.logger.Warn("Invalid UserOperationEvent log",
				zap.String("txHash", receipt.TxHash.Hex()),
				zap.Int("logIndex", int(log.Index)))
			continue
		}

		opHash := log.Topics[1]
		sender := common.BytesToAddress(log.Topics[2].Bytes())
		paymasterAddr := common.BytesToAddress(log.Topics[3].Bytes())

		// Decode data: nonce (32) + success (32) + actualGasCost (32) + actualGasUsed (32)
		nonce := new(big.Int).SetBytes(log.Data[0:32])
		success := new(big.Int).SetBytes(log.Data[32:64]).Uint64() != 0
		actualGasCost := new(big.Int).SetBytes(log.Data[64:96])
		actualGasUsed := new(big.Int).SetBytes(log.Data[96:128])

		// Determine paymaster
		var paymaster *common.Address
		if paymasterAddr != (common.Address{}) {
			paymaster = &paymasterAddr
		}

		// Determine factory from AccountDeployed event
		var factory *common.Address
		if info, ok := accountDeployedMap[opHash]; ok {
			factory = info.factory
		}

		// Determine sponsor type
		sponsorType := userop.DetermineSponsorType(paymaster)

		op := &userop.UserOperation{
			Hash:              common.Hash(opHash),
			Sender:            sender,
			Nonce:             nonce.String(),
			CallData:          nil, // Not available from events
			CallGasLimit:      "0",
			VerificationGasLimit: "0",
			PreVerificationGas:   "0",
			MaxFeePerGas:         "0",
			MaxPriorityFeePerGas: "0",
			Signature:            nil,
			EntryPoint:           entryPoint,
			EntryPointVersion:    epVersion,
			TransactionHash:      receipt.TxHash,
			BlockNumber:          blockNumber,
			BlockHash:            blockHash,
			BundleIndex:          bundleIndex,
			Bundler:              bundler,
			Factory:              factory,
			Paymaster:            paymaster,
			Status:               success,
			GasUsed:              actualGasUsed.String(),
			ActualGasCost:        actualGasCost.String(),
			SponsorType:          sponsorType,
			UserLogsStartIndex:   uint32(log.Index),
			UserLogsCount:        1,
			Timestamp:            blockTime,
		}

		// Attach revert reason if present
		if reason, ok := revertReasonMap[opHash]; ok {
			op.RevertReason = reason
		}

		ops = append(ops, op)
		bundleIndex++
	}

	return ops
}

// updateStats updates bundler, paymaster, factory, and smart account statistics.
func (p *UserOpProcessor) updateStats(
	ctx context.Context,
	ops []*userop.UserOperation,
	bundlerTxCounts map[common.Address]int,
) error {
	// Update bundler stats
	for bundler, bundleCount := range bundlerTxCounts {
		stats, err := p.storage.GetBundlerStats(ctx, bundler)
		if err != nil {
			p.logger.Warn("Failed to get bundler stats", zap.String("address", bundler.Hex()), zap.Error(err))
			stats = &userop.BundlerStats{Address: bundler}
		}
		stats.TotalBundles += uint64(bundleCount)
		// Count ops for this bundler
		for _, op := range ops {
			if op.Bundler == bundler {
				stats.TotalOps++
			}
		}
		if err := p.storage.UpdateBundlerStats(ctx, stats); err != nil {
			p.logger.Warn("Failed to update bundler stats", zap.String("address", bundler.Hex()), zap.Error(err))
		}
	}

	// Track unique paymasters and factories in this batch
	paymasterOps := make(map[common.Address]uint64)
	factoryAccounts := make(map[common.Address]map[common.Address]bool)

	for _, op := range ops {
		// Paymaster stats
		if op.Paymaster != nil && *op.Paymaster != (common.Address{}) {
			paymasterOps[*op.Paymaster]++
		}

		// Factory stats
		if op.Factory != nil && *op.Factory != (common.Address{}) {
			if factoryAccounts[*op.Factory] == nil {
				factoryAccounts[*op.Factory] = make(map[common.Address]bool)
			}
			factoryAccounts[*op.Factory][op.Sender] = true
		}

		// Update smart account
		account, err := p.storage.GetSmartAccount(ctx, op.Sender)
		if err != nil {
			// New smart account
			account = &userop.SmartAccount{
				Address:  op.Sender,
				TotalOps: 0,
			}
		}
		account.TotalOps++

		// Set creation info if this is an account deployment
		if op.Factory != nil && account.CreationOpHash == nil {
			opHash := op.Hash
			txHash := op.TransactionHash
			ts := op.Timestamp
			account.CreationOpHash = &opHash
			account.CreationTxHash = &txHash
			account.CreationTimestamp = &ts
			account.Factory = op.Factory
		}

		if err := p.storage.SaveSmartAccount(ctx, account); err != nil {
			p.logger.Warn("Failed to save smart account",
				zap.String("address", op.Sender.Hex()),
				zap.Error(err))
		}
	}

	// Update paymaster stats
	for pm, count := range paymasterOps {
		stats, err := p.storage.GetPaymasterStats(ctx, pm)
		if err != nil {
			p.logger.Warn("Failed to get paymaster stats", zap.String("address", pm.Hex()), zap.Error(err))
			stats = &userop.PaymasterStats{Address: pm}
		}
		stats.TotalOps += count
		if err := p.storage.UpdatePaymasterStats(ctx, stats); err != nil {
			p.logger.Warn("Failed to update paymaster stats", zap.String("address", pm.Hex()), zap.Error(err))
		}
	}

	// Update factory stats
	for factory, accounts := range factoryAccounts {
		stats, err := p.storage.GetFactoryStats(ctx, factory)
		if err != nil {
			p.logger.Warn("Failed to get factory stats", zap.String("address", factory.Hex()), zap.Error(err))
			stats = &userop.FactoryStats{Address: factory}
		}
		stats.TotalAccounts += uint64(len(accounts))
		if err := p.storage.UpdateFactoryStats(ctx, stats); err != nil {
			p.logger.Warn("Failed to update factory stats", zap.String("address", factory.Hex()), zap.Error(err))
		}
	}

	return nil
}

// accountDeployedInfo holds parsed AccountDeployed event data
type accountDeployedInfo struct {
	factory   *common.Address
	paymaster *common.Address
}

// Ensure hex import is used
var _ = hex.EncodeToString
