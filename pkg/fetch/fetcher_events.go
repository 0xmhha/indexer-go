package fetch

import (
	"context"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/events"
)

// ============================================================================
// Event Publishing and System Event Detection Methods
// ============================================================================

// publishBlockEvents publishes transaction and log events to the event bus
func (f *Fetcher) publishBlockEvents(block *types.Block, receipts types.Receipts, height uint64) {
	transactions := block.Transactions()

	// Build receipt map for O(1) lookup (avoids O(n²) matching)
	receiptMap := buildReceiptMap(receipts)

	// Publish transaction events
	for i, tx := range transactions {
		// O(1) receipt lookup
		receipt := receiptMap[tx.Hash()]

		// Create and publish transaction event
		txEvent := events.NewTransactionEvent(
			tx,
			block.NumberU64(),
			block.Hash(),
			uint(i),
			getTransactionSender(tx),
			receipt,
		)

		if !f.eventBus.Publish(txEvent) {
			f.logger.Warn("Failed to publish transaction event (channel full)",
				zap.String("tx_hash", tx.Hash().Hex()),
				zap.Uint64("block", height),
			)
		}
	}

	// Publish log events
	for _, receipt := range receipts {
		if receipt == nil {
			continue
		}
		for _, logEntry := range receipt.Logs {
			if logEntry == nil {
				continue
			}
			logEvent := events.NewLogEvent(logEntry)
			if !f.eventBus.Publish(logEvent) {
				f.logger.Warn("Failed to publish log event (channel full)",
					zap.String("tx_hash", logEntry.TxHash.Hex()),
					zap.Uint64("block", logEntry.BlockNumber),
					zap.Uint("log_index", uint(logEntry.Index)),
				)
			}

			// Detect system events from logs
			f.detectSystemEvents(block, logEntry)
		}
	}
}

// detectSystemEvents detects and publishes system events from logs
func (f *Fetcher) detectSystemEvents(block *types.Block, log *types.Log) {
	if f.eventBus == nil {
		return
	}

	// Use chain adapter's system contracts handler if available
	if f.chainAdapter != nil && f.chainAdapter.SystemContracts() != nil {
		f.detectSystemEventsWithAdapter(block, log)
		return
	}

	// Fallback to hardcoded logic for backward compatibility
	f.detectSystemEventsLegacy(block, log)
}

// detectSystemEventsWithAdapter uses the chain adapter to detect system events
func (f *Fetcher) detectSystemEventsWithAdapter(block *types.Block, log *types.Log) {
	systemContracts := f.chainAdapter.SystemContracts()

	// Check if this is a system contract
	if !systemContracts.IsSystemContract(log.Address) {
		return
	}

	if len(log.Topics) == 0 {
		return
	}

	// Parse the system contract event
	scEvent, err := systemContracts.ParseSystemContractEvent(log)
	if err != nil {
		f.logger.Debug("Failed to parse system contract event",
			zap.String("contract", log.Address.Hex()),
			zap.Error(err),
		)
		return
	}

	// Handle validator set changes
	if scEvent.EventName == "MemberAdded" || scEvent.EventName == "MemberRemoved" {
		var validatorAddr common.Address
		if member, ok := scEvent.Data["member"].(common.Address); ok {
			validatorAddr = member
		} else if len(log.Topics) >= 2 {
			validatorAddr = common.BytesToAddress(log.Topics[1].Bytes())
		}

		changeType := "added"
		if scEvent.EventName == "MemberRemoved" {
			changeType = "removed"
		}

		validatorEvent := events.NewValidatorSetEvent(
			block.NumberU64(),
			block.Hash(),
			changeType,
			validatorAddr,
			"",
			0,
		)

		if !f.eventBus.Publish(validatorEvent) {
			f.logger.Warn("Failed to publish validator set event (channel full)",
				zap.String("type", changeType),
				zap.String("validator", validatorAddr.Hex()),
				zap.Uint64("block", block.NumberU64()),
			)
		} else {
			f.logger.Info("Validator "+changeType,
				zap.String("validator", validatorAddr.Hex()),
				zap.Uint64("block", block.NumberU64()),
			)
		}
	}
}

// detectSystemEventsLegacy uses hardcoded logic for backward compatibility
func (f *Fetcher) detectSystemEventsLegacy(block *types.Block, log *types.Log) {
	// Check if this is a GovValidator contract event
	if log.Address != events.GovValidatorAddress {
		return
	}

	if len(log.Topics) == 0 {
		return
	}

	eventSig := log.Topics[0]

	// Detect validator set changes
	switch eventSig {
	case events.EventSigMemberAdded:
		// MemberAdded(address,uint256,uint32)
		if len(log.Topics) >= 2 {
			validatorAddr := common.BytesToAddress(log.Topics[1].Bytes())

			validatorEvent := events.NewValidatorSetEvent(
				block.NumberU64(),
				block.Hash(),
				"added",
				validatorAddr,
				"", // validator info from data field if needed
				0,  // set size would need to be tracked separately
			)

			if !f.eventBus.Publish(validatorEvent) {
				f.logger.Warn("Failed to publish validator set event (channel full)",
					zap.String("type", "added"),
					zap.String("validator", validatorAddr.Hex()),
					zap.Uint64("block", block.NumberU64()),
				)
			} else {
				f.logger.Info("Validator added",
					zap.String("validator", validatorAddr.Hex()),
					zap.Uint64("block", block.NumberU64()),
				)
			}
		}

	case events.EventSigMemberRemoved:
		// MemberRemoved(address,uint256,uint32)
		if len(log.Topics) >= 2 {
			validatorAddr := common.BytesToAddress(log.Topics[1].Bytes())

			validatorEvent := events.NewValidatorSetEvent(
				block.NumberU64(),
				block.Hash(),
				"removed",
				validatorAddr,
				"", // validator info from data field if needed
				0,  // set size would need to be tracked separately
			)

			if !f.eventBus.Publish(validatorEvent) {
				f.logger.Warn("Failed to publish validator set event (channel full)",
					zap.String("type", "removed"),
					zap.String("validator", validatorAddr.Hex()),
					zap.Uint64("block", block.NumberU64()),
				)
			} else {
				f.logger.Info("Validator removed",
					zap.String("validator", validatorAddr.Hex()),
					zap.Uint64("block", block.NumberU64()),
				)
			}
		}
	}
}

// StartPendingTxSubscription starts subscribing to pending transactions
// and publishes them to the EventBus. Returns an error channel that receives
// subscription errors. Should be run in a separate goroutine.
func (f *Fetcher) StartPendingTxSubscription(ctx context.Context) (<-chan error, error) {
	if f.eventBus == nil {
		return nil, fmt.Errorf("EventBus is not configured")
	}

	// Check if client supports pending transaction subscription
	pendingClient, ok := f.client.(PendingTxClient)
	if !ok {
		return nil, fmt.Errorf("client does not support pending transaction subscription")
	}

	f.logger.Info("starting pending transaction subscription")

	// Subscribe to pending transactions
	txHashCh, sub, err := pendingClient.SubscribePendingTransactions(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to subscribe to pending transactions: %w", err)
	}

	errCh := make(chan error, 1)

	// Start goroutine to process pending transactions
	go func() {
		defer sub.Unsubscribe()
		defer close(errCh)

		for {
			select {
			case <-ctx.Done():
				f.logger.Info("pending transaction subscription stopped")
				return

			case err := <-sub.Err():
				if err != nil {
					f.logger.Error("pending transaction subscription error", zap.Error(err))
					errCh <- err
					return
				}

			case txHash := <-txHashCh:
				// Fetch full transaction details
				tx, isPending, err := f.client.GetTransactionByHash(ctx, txHash)
				if err != nil {
					f.logger.Warn("failed to fetch pending transaction",
						zap.String("hash", txHash.Hex()),
						zap.Error(err),
					)
					continue
				}

				// Only process if still pending
				if !isPending {
					continue
				}

				// Extract sender address
				signer := types.LatestSignerForChainID(tx.ChainId())
				from, err := signer.Sender(tx)
				if err != nil {
					f.logger.Warn("failed to extract sender",
						zap.String("hash", txHash.Hex()),
						zap.Error(err),
					)
					continue
				}

				// Create transaction event
				txEvent := events.NewTransactionEvent(
					tx,
					0,             // No block number for pending tx
					common.Hash{}, // No block hash for pending tx
					0,             // No index for pending tx
					from,
					nil, // No receipt for pending tx
				)

				// Publish to EventBus
				if !f.eventBus.Publish(txEvent) {
					f.logger.Warn("EventBus channel full, pending transaction dropped",
						zap.String("hash", txHash.Hex()),
					)
				} else {
					f.logger.Debug("published pending transaction event",
						zap.String("hash", txHash.Hex()),
						zap.String("from", from.Hex()),
					)
				}
			}
		}
	}()

	return errCh, nil
}
