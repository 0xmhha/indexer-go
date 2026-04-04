package fetch

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"

	modulepkg "github.com/0xmhha/indexer-go/pkg/module"
	storagepkg "github.com/0xmhha/indexer-go/pkg/storage"
)

// ModuleIndexer defines the interface for indexing ERC-7579 module events
type ModuleIndexer interface {
	storagepkg.ModuleIndexWriter
	storagepkg.ModuleIndexReader
}

// ModuleProcessor handles processing of ERC-7579 module install/uninstall events
type ModuleProcessor struct {
	logger  *zap.Logger
	storage ModuleIndexer
}

// NewModuleProcessor creates a new Module processor
func NewModuleProcessor(logger *zap.Logger, storage ModuleIndexer) *ModuleProcessor {
	return &ModuleProcessor{
		logger:  logger.Named("module"),
		storage: storage,
	}
}

// ProcessModuleEventsFromBlock processes all module events from a block's receipts
func (p *ModuleProcessor) ProcessModuleEventsFromBlock(
	ctx context.Context,
	block *types.Block,
	receipts []*types.Receipt,
) error {
	if block == nil || len(receipts) == 0 {
		return nil
	}

	blockNumber := block.NumberU64()
	blockTime := time.Unix(int64(block.Time()), 0)

	installCount := 0
	uninstallCount := 0

	for _, receipt := range receipts {
		if receipt == nil || receipt.Status != types.ReceiptStatusSuccessful {
			continue
		}

		for _, log := range receipt.Logs {
			if log == nil || len(log.Topics) == 0 {
				continue
			}

			switch log.Topics[0] {
			case modulepkg.ModuleInstalledSig:
				if err := p.processModuleInstalled(ctx, log, blockNumber, blockTime); err != nil {
					p.logger.Warn("Failed to process ModuleInstalled event",
						zap.String("txHash", log.TxHash.Hex()),
						zap.Uint("logIndex", log.Index),
						zap.Error(err))
					continue
				}
				installCount++

			case modulepkg.ModuleUninstalledSig:
				if err := p.processModuleUninstalled(ctx, log, blockNumber); err != nil {
					p.logger.Warn("Failed to process ModuleUninstalled event",
						zap.String("txHash", log.TxHash.Hex()),
						zap.Uint("logIndex", log.Index),
						zap.Error(err))
					continue
				}
				uninstallCount++
			}
		}
	}

	if installCount > 0 || uninstallCount > 0 {
		p.logger.Info("Processed module events",
			zap.Uint64("blockNumber", blockNumber),
			zap.Int("installs", installCount),
			zap.Int("uninstalls", uninstallCount))
	}

	return nil
}

// processModuleInstalled handles a ModuleInstalled event
func (p *ModuleProcessor) processModuleInstalled(
	ctx context.Context,
	log *types.Log,
	blockNumber uint64,
	blockTime time.Time,
) error {
	// ModuleInstalled(uint256 moduleTypeId, address module)
	// Both parameters are non-indexed, so they are in log.Data
	// Data layout: [32 bytes moduleTypeId][32 bytes module address]
	if len(log.Data) < 64 {
		return fmt.Errorf("insufficient data length: got %d, need 64", len(log.Data))
	}

	// Extract moduleTypeId from first 32 bytes
	moduleTypeId := new(big.Int).SetBytes(log.Data[:32])
	moduleType := storagepkg.ModuleType(moduleTypeId.Uint64())

	// Extract module address from next 32 bytes (left-padded to 32 bytes)
	moduleAddr := common.BytesToAddress(log.Data[32:64])

	// Account is the log emitter
	account := log.Address

	record := &storagepkg.InstalledModule{
		Account:     account,
		Module:      moduleAddr,
		ModuleType:  moduleType,
		InstalledAt: blockNumber,
		InstalledTx: log.TxHash,
		Active:      true,
		Timestamp:   blockTime,
	}

	// Save the module record
	if err := p.storage.SaveInstalledModule(ctx, record); err != nil {
		return fmt.Errorf("failed to save installed module: %w", err)
	}

	// Update module stats (increment)
	stats, err := p.storage.GetModuleStats(ctx, moduleAddr)
	if err != nil {
		p.logger.Warn("Failed to get module stats for increment",
			zap.String("module", moduleAddr.Hex()),
			zap.Error(err))
		// Create new stats
		stats = &storagepkg.ModuleStats{
			Module:     moduleAddr,
			ModuleType: moduleType,
		}
	}
	stats.TotalInstalls++
	stats.ActiveInstalls++
	stats.ModuleType = moduleType

	if err := p.storage.UpdateModuleStats(ctx, stats); err != nil {
		p.logger.Warn("Failed to update module stats after install",
			zap.String("module", moduleAddr.Hex()),
			zap.Error(err))
	}

	p.logger.Debug("Indexed ModuleInstalled event",
		zap.String("account", account.Hex()),
		zap.String("module", moduleAddr.Hex()),
		zap.String("moduleType", moduleType.String()),
		zap.Uint64("blockNumber", blockNumber))

	return nil
}

// processModuleUninstalled handles a ModuleUninstalled event
func (p *ModuleProcessor) processModuleUninstalled(
	ctx context.Context,
	log *types.Log,
	blockNumber uint64,
) error {
	// ModuleUninstalled(uint256 moduleTypeId, address module)
	// Both parameters are non-indexed, so they are in log.Data
	// Data layout: [32 bytes moduleTypeId][32 bytes module address]
	if len(log.Data) < 64 {
		return fmt.Errorf("insufficient data length: got %d, need 64", len(log.Data))
	}

	// Extract moduleTypeId from first 32 bytes
	moduleTypeId := new(big.Int).SetBytes(log.Data[:32])
	moduleType := storagepkg.ModuleType(moduleTypeId.Uint64())

	// Extract module address from next 32 bytes (left-padded to 32 bytes)
	moduleAddr := common.BytesToAddress(log.Data[32:64])

	// Account is the log emitter
	account := log.Address

	// Remove the module (mark as inactive)
	if err := p.storage.RemoveModule(ctx, account, moduleAddr, blockNumber, log.TxHash); err != nil {
		return fmt.Errorf("failed to remove module: %w", err)
	}

	// Update module stats (decrement active)
	stats, err := p.storage.GetModuleStats(ctx, moduleAddr)
	if err != nil {
		p.logger.Warn("Failed to get module stats for decrement",
			zap.String("module", moduleAddr.Hex()),
			zap.Error(err))
	} else {
		if stats.ActiveInstalls > 0 {
			stats.ActiveInstalls--
		}
		stats.ModuleType = moduleType

		if err := p.storage.UpdateModuleStats(ctx, stats); err != nil {
			p.logger.Warn("Failed to update module stats after uninstall",
				zap.String("module", moduleAddr.Hex()),
				zap.Error(err))
		}
	}

	p.logger.Debug("Indexed ModuleUninstalled event",
		zap.String("account", account.Hex()),
		zap.String("module", moduleAddr.Hex()),
		zap.String("moduleType", moduleType.String()),
		zap.Uint64("blockNumber", blockNumber))

	return nil
}
