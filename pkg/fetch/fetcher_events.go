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
func (f *Fetcher) publishBlockEvents(fb *fetchedBlock) {
	receipts, height := fb.gethReceipts, fb.height()

	// Publish transaction events. Hashes and the sender come from the model:
	// the go-ethereum view of a fee delegation transaction has another hash.
	for _, p := range fb.transactions() {
		txEvent := events.NewTransactionEvent(
			p.gethTx,
			height,
			fb.block.Hash,
			uint(p.index),
			p.tx.From,
			p.gethReceipt,
		)
		txEvent.Hash = p.tx.Hash

		if !f.publish(txEvent) {
			f.logger.Warn("Failed to publish transaction event (channel full)",
				zap.String("tx_hash", p.tx.Hash.Hex()),
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
			if !f.publish(logEvent) {
				f.logger.Warn("Failed to publish log event (channel full)",
					zap.String("tx_hash", logEntry.TxHash.Hex()),
					zap.Uint64("block", logEntry.BlockNumber),
					zap.Uint("log_index", uint(logEntry.Index)),
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
