package fetch

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"

	storagepkg "github.com/0xmhha/indexer-go/pkg/storage"
)

// ============================================================================
// Address Indexing and Balance Tracking Methods
// ============================================================================

// processAddressIndexing parses and stores address indexing data from block and receipts
func (f *Fetcher) processAddressIndexing(ctx context.Context, block *types.Block, receipts types.Receipts) error {
	// Check if storage implements AddressIndexWriter
	addressWriter, ok := f.storage.(storagepkg.AddressIndexWriter)
	if !ok {
		// Storage doesn't support address indexing - skip silently
		return nil
	}

	// Check if storage implements Writer for transaction address indexing
	storageWriter, hasWriter := f.storage.(storagepkg.Writer)

	blockNumber := block.NumberU64()
	blockTime := block.Time()
	transactions := block.Transactions()

	// Build receipt map for O(1) lookup (avoids O(n²) matching)
	receiptMap := buildReceiptMap(receipts)

	// Fee Delegation transaction type constant (StableNet-specific)
	const FeeDelegateDynamicFeeTxType = 22

	// getFeePayer extracts fee payer from transaction if available
	// Returns nil for standard go-ethereum (Fee Delegation is StableNet-specific)
	getFeePayer := func(tx *types.Transaction) *common.Address {
		return nil // TODO: Implement when using go-stablenet client
	}

	// Process each transaction and its receipt
	for txIdx, tx := range transactions {
		// O(1) receipt lookup
		receipt := receiptMap[tx.Hash()]
		if receipt == nil {
			continue
		}

		// 0. Index transaction addresses (from, to, feePayer) for transactionsByAddress query
		if hasWriter {
			txHash := tx.Hash()

			// Index 'from' address
			from := getTransactionSender(tx)
			if from != (common.Address{}) {
				if err := storageWriter.AddTransactionToAddressIndex(ctx, from, txHash); err != nil {
					f.logger.Warn("Failed to index transaction for from address",
						zap.Uint64("block", blockNumber),
						zap.String("tx", txHash.Hex()),
						zap.String("from", from.Hex()),
						zap.Error(err),
					)
				}
			}

			// Index 'to' address (if not contract creation)
			if tx.To() != nil {
				to := *tx.To()
				if to != from { // Avoid duplicate indexing for self-transfers
					if err := storageWriter.AddTransactionToAddressIndex(ctx, to, txHash); err != nil {
						f.logger.Warn("Failed to index transaction for to address",
							zap.Uint64("block", blockNumber),
							zap.String("tx", txHash.Hex()),
							zap.String("to", to.Hex()),
							zap.Error(err),
						)
					}
				}
			}

			// Index 'feePayer' address for Fee Delegation transactions (type 0x16)
			if tx.Type() == FeeDelegateDynamicFeeTxType {
				if feePayer := getFeePayer(tx); feePayer != nil {
					// Avoid duplicate indexing if feePayer is same as from or to
					if *feePayer != from && (tx.To() == nil || *feePayer != *tx.To()) {
						if err := storageWriter.AddTransactionToAddressIndex(ctx, *feePayer, txHash); err != nil {
							f.logger.Warn("Failed to index transaction for feePayer address",
								zap.Uint64("block", blockNumber),
								zap.String("tx", txHash.Hex()),
								zap.String("feePayer", feePayer.Hex()),
								zap.Error(err),
							)
						}
					}
				}
			}
		}

		// 1. Contract Creation Detection
		// Contract creation is indicated by tx.To() == nil
		if tx.To() == nil && receipt.ContractAddress != (common.Address{}) {
			creation := &storagepkg.ContractCreation{
				ContractAddress: receipt.ContractAddress,
				Creator:         getTransactionSender(tx),
				TransactionHash: tx.Hash(),
				BlockNumber:     blockNumber,
				Timestamp:       blockTime,
				BytecodeSize:    len(receipt.ContractAddress.Bytes()), // This is simplified
			}

			if err := addressWriter.SaveContractCreation(ctx, creation); err != nil {
				f.logger.Warn("Failed to save contract creation",
					zap.Uint64("block", blockNumber),
					zap.String("tx", tx.Hash().Hex()),
					zap.String("contract", receipt.ContractAddress.Hex()),
					zap.Error(err),
				)
			}

			// Index token metadata if this is a token contract
			if f.tokenIndexer != nil {
				if err := f.tokenIndexer.IndexToken(ctx, receipt.ContractAddress, blockNumber); err != nil {
					f.logger.Debug("Failed to index token metadata (may not be a token contract)",
						zap.String("contract", receipt.ContractAddress.Hex()),
						zap.Error(err),
					)
				}
			}
		}

		// 2. Parse ERC20/ERC721 Transfer Events from Logs
		for _, log := range receipt.Logs {
			if log == nil || len(log.Topics) == 0 {
				continue
			}

			// Check if this is a Transfer event
			// Transfer event topic: keccak256("Transfer(address,address,uint256)")
			if log.Topics[0].Hex() != storagepkg.ERC20TransferTopic {
				continue
			}

			// ERC20: Transfer(indexed from, indexed to, uint256 value) - 3 topics
			// ERC721: Transfer(indexed from, indexed to, indexed tokenId) - 4 topics
			// Note: First topic is the event signature, so total topics are 3 or 4

			if len(log.Topics) == 3 {
				// ERC20 Transfer Event
				if len(log.Topics) < 3 || len(log.Data) < 32 {
					continue
				}

				from := common.BytesToAddress(log.Topics[1].Bytes())
				to := common.BytesToAddress(log.Topics[2].Bytes())
				value := new(big.Int).SetBytes(log.Data)

				transfer := &storagepkg.ERC20Transfer{
					ContractAddress: log.Address,
					From:            from,
					To:              to,
					Value:           value,
					TransactionHash: log.TxHash,
					BlockNumber:     log.BlockNumber,
					LogIndex:        log.Index,
					Timestamp:       blockTime,
				}

				if err := addressWriter.SaveERC20Transfer(ctx, transfer); err != nil {
					f.logger.Warn("Failed to save ERC20 transfer",
						zap.Uint64("block", blockNumber),
						zap.String("tx", tx.Hash().Hex()),
						zap.String("token", log.Address.Hex()),
						zap.Error(err),
					)
				}

			} else if len(log.Topics) == 4 {
				// ERC721 Transfer Event
				from := common.BytesToAddress(log.Topics[1].Bytes())
				to := common.BytesToAddress(log.Topics[2].Bytes())
				tokenId := new(big.Int).SetBytes(log.Topics[3].Bytes())

				transfer := &storagepkg.ERC721Transfer{
					ContractAddress: log.Address,
					From:            from,
					To:              to,
					TokenId:         tokenId,
					TransactionHash: log.TxHash,
					BlockNumber:     log.BlockNumber,
					LogIndex:        log.Index,
					Timestamp:       blockTime,
				}

				if err := addressWriter.SaveERC721Transfer(ctx, transfer); err != nil {
					f.logger.Warn("Failed to save ERC721 transfer",
						zap.Uint64("block", blockNumber),
						zap.String("tx", tx.Hash().Hex()),
						zap.String("token", log.Address.Hex()),
						zap.String("tokenId", tokenId.String()),
						zap.Error(err),
					)
				}
			}
		}

		// 3. Process EIP-7702 SetCode Transactions
		if f.setCodeProcessor != nil && tx.Type() == types.SetCodeTxType {
			if err := f.setCodeProcessor.ProcessSetCodeTransaction(ctx, tx, receipt, block, uint64(txIdx)); err != nil {
				f.logger.Warn("Failed to process SetCode transaction",
					zap.Uint64("block", blockNumber),
					zap.String("tx", tx.Hash().Hex()),
					zap.Error(err),
				)
			}
		}
	}

	f.logger.Debug("Processed address indexing",
		zap.Uint64("height", blockNumber),
		zap.Int("transactions", len(transactions)),
	)

	return nil
}

// ensureAddressBalanceInitialized checks if an address has balance history,
// and if not, fetches the current balance from RPC and initializes it
func (f *Fetcher) ensureAddressBalanceInitialized(ctx context.Context, histReader storagepkg.HistoricalReader, histWriter storagepkg.HistoricalWriter, addr common.Address, blockNumber uint64) error {
	// Check if address already has balance history
	currentBalance, err := histReader.GetAddressBalance(ctx, addr, 0)
	if err != nil {
		return fmt.Errorf("failed to check address balance: %w", err)
	}

	// If balance is non-zero, address is already initialized
	if currentBalance.Sign() != 0 {
		return nil
	}

	// Check if there's any balance history (even if balance is 0)
	history, err := histReader.GetBalanceHistory(ctx, addr, 0, blockNumber, 1, 0)
	if err != nil {
		return fmt.Errorf("failed to check balance history: %w", err)
	}

	// If there's history, address is already initialized (balance might legitimately be 0)
	if len(history) > 0 {
		return nil
	}

	// No history found - this is the first time we see this address
	// Fetch the actual balance from RPC at the block BEFORE this transaction
	var rpcBlockNumber *big.Int
	if blockNumber > 0 {
		rpcBlockNumber = new(big.Int).SetUint64(blockNumber - 1)
	} else {
		// Genesis block - use block 0
		rpcBlockNumber = big.NewInt(0)
	}

	rpcBalance, err := f.client.BalanceAt(ctx, addr, rpcBlockNumber)
	if err != nil {
		// Log warning but don't fail - balance tracking is best-effort
		f.logger.Warn("Failed to fetch initial balance from RPC, starting from 0",
			zap.String("address", addr.Hex()),
			zap.Uint64("block", blockNumber),
			zap.Error(err),
		)
		// Set initial balance to 0
		rpcBalance = big.NewInt(0)
	}

	// Initialize the balance
	if rpcBalance.Sign() > 0 {
		f.logger.Debug("Initializing address balance from RPC",
			zap.String("address", addr.Hex()),
			zap.Uint64("block", blockNumber),
			zap.String("balance", rpcBalance.String()),
		)
	}

	// Set the initial balance
	return histWriter.SetBalance(ctx, addr, blockNumber, rpcBalance)
}

// initializeGenesisBalances initializes balances for addresses in genesis allocation
// This is called only for block 0 to handle addresses that received initial balance
// but haven't participated in any transactions yet
func (f *Fetcher) initializeGenesisBalances(ctx context.Context, block *types.Block) error {
	// Check if storage supports balance tracking
	histWriter, ok := f.storage.(storagepkg.HistoricalWriter)
	if !ok {
		return nil // Storage doesn't support balance tracking - skip
	}

	histReader, ok := f.storage.(storagepkg.HistoricalReader)
	if !ok {
		return nil // Storage doesn't support balance history - skip
	}

	// Get the block miner (validator) - this is typically a genesis allocation address
	miner := block.Coinbase()

	// Check if miner balance is already initialized
	currentBalance, err := histReader.GetAddressBalance(ctx, miner, 0)
	if err != nil {
		return fmt.Errorf("failed to check miner balance: %w", err)
	}

	// If miner already has a balance recorded, skip initialization
	if currentBalance.Sign() != 0 {
		f.logger.Debug("Genesis miner balance already initialized",
			zap.String("miner", miner.Hex()),
			zap.String("balance", currentBalance.String()),
		)
		return nil
	}

	// Check if there's any balance history for miner
	history, err := histReader.GetBalanceHistory(ctx, miner, 0, 0, 1, 0)
	if err != nil {
		return fmt.Errorf("failed to check miner balance history: %w", err)
	}

	// If there's already history, skip initialization
	if len(history) > 0 {
		f.logger.Debug("Genesis miner already has balance history",
			zap.String("miner", miner.Hex()),
		)
		return nil
	}

	// Fetch the actual balance from RPC at block 0
	rpcBalance, err := f.client.BalanceAt(ctx, miner, big.NewInt(0))
	if err != nil {
		f.logger.Warn("Failed to fetch genesis miner balance from RPC",
			zap.String("miner", miner.Hex()),
			zap.Error(err),
		)
		return err
	}

	// Initialize the balance if non-zero
	if rpcBalance.Sign() > 0 {
		f.logger.Info("Initializing genesis allocation balance",
			zap.String("address", miner.Hex()),
			zap.String("balance", rpcBalance.String()),
		)

		if err := histWriter.SetBalance(ctx, miner, 0, rpcBalance); err != nil {
			return fmt.Errorf("failed to set genesis miner balance: %w", err)
		}
	}

	return nil
}

// initializeGenesisTokenMetadata indexes token metadata for genesis system contracts
// This is called only for block 0 to ensure system contracts deployed at genesis
// have their token metadata properly indexed.
func (f *Fetcher) initializeGenesisTokenMetadata(ctx context.Context) error {
	// Check if we have a chain adapter with system contracts
	if f.chainAdapter == nil {
		f.logger.Debug("No chain adapter available, skipping genesis token metadata initialization")
		return nil
	}

	systemContracts := f.chainAdapter.SystemContracts()
	if systemContracts == nil {
		f.logger.Debug("No system contracts handler available, skipping genesis token metadata initialization")
		return nil
	}

	// Check if we have a token indexer
	if f.tokenIndexer == nil {
		f.logger.Debug("No token indexer available, skipping genesis token metadata initialization")
		return nil
	}

	// Get all system contract addresses
	addresses := systemContracts.GetSystemContractAddresses()
	if len(addresses) == 0 {
		f.logger.Debug("No system contract addresses found")
		return nil
	}

	f.logger.Info("Indexing genesis system contract token metadata",
		zap.Int("contract_count", len(addresses)),
	)

	// Index token metadata for each system contract
	var indexed, skipped int
	for _, addr := range addresses {
		// Use block height 0 for genesis contracts
		if err := f.tokenIndexer.IndexToken(ctx, addr, 0); err != nil {
			f.logger.Debug("Failed to index genesis contract token metadata (may not be a token)",
				zap.String("address", addr.Hex()),
				zap.String("name", systemContracts.GetSystemContractName(addr)),
				zap.Error(err),
			)
			skipped++
		} else {
			f.logger.Info("Indexed genesis system contract token metadata",
				zap.String("address", addr.Hex()),
				zap.String("name", systemContracts.GetSystemContractName(addr)),
			)
			indexed++
		}
	}

	f.logger.Info("Completed genesis token metadata initialization",
		zap.Int("indexed", indexed),
		zap.Int("skipped", skipped),
		zap.Int("total", len(addresses)),
	)

	return nil
}

// processBalanceTracking tracks native balance changes from ETH transfers
func (f *Fetcher) processBalanceTracking(ctx context.Context, block *types.Block, receipts types.Receipts) error {
	// Check if storage implements HistoricalWriter
	histWriter, ok := f.storage.(storagepkg.HistoricalWriter)
	if !ok {
		// Storage doesn't support balance tracking - skip silently
		return nil
	}

	// Also check for HistoricalReader (needed to check if address is initialized)
	histReader, ok := f.storage.(storagepkg.HistoricalReader)
	if !ok {
		// Storage doesn't support historical reading - skip silently
		return nil
	}

	blockNumber := block.NumberU64()
	transactions := block.Transactions()

	// Build receipt map for O(1) lookup (avoids O(n²) matching)
	receiptMap := buildReceiptMap(receipts)

	// Track balance changes for each transaction
	for _, tx := range transactions {
		// O(1) receipt lookup
		receipt := receiptMap[tx.Hash()]
		if receipt == nil {
			continue
		}

		// Get sender address
		from := getTransactionSender(tx)
		if from == (common.Address{}) {
			// Cannot determine sender, skip
			continue
		}

		// Calculate gas cost (gas used * effective gas price)
		gasUsed := new(big.Int).SetUint64(receipt.GasUsed)
		gasPrice := tx.GasPrice()
		if gasPrice == nil {
			gasPrice = big.NewInt(0)
		}
		gasCost := new(big.Int).Mul(gasUsed, gasPrice)

		// Calculate total deduction from sender: value + gas cost
		value := tx.Value()
		if value == nil {
			value = big.NewInt(0)
		}
		totalDeduction := new(big.Int).Add(value, gasCost)

		// Ensure sender address balance is initialized from RPC if first time seeing it
		if err := f.ensureAddressBalanceInitialized(ctx, histReader, histWriter, from, blockNumber); err != nil {
			f.logger.Warn("Failed to initialize sender balance",
				zap.String("address", from.Hex()),
				zap.Uint64("block", blockNumber),
				zap.Error(err),
			)
			// Continue - balance tracking is best-effort
		}

		// Update sender balance (deduct value + gas)
		senderDelta := new(big.Int).Neg(totalDeduction)
		if err := histWriter.UpdateBalance(ctx, from, blockNumber, senderDelta, tx.Hash()); err != nil {
			f.logger.Warn("Failed to update sender balance",
				zap.Uint64("block", blockNumber),
				zap.String("tx", tx.Hash().Hex()),
				zap.String("from", from.Hex()),
				zap.String("delta", senderDelta.String()),
				zap.Error(err),
			)
			// Continue processing - balance tracking failure shouldn't block indexing
		}

		// Update receiver balance (add value only, not gas)
		// Note: For contract creation, tx.To() is nil, so receiver is the contract address
		to := tx.To()
		if to == nil && receipt.ContractAddress != (common.Address{}) {
			// Contract creation - credit the contract address
			to = &receipt.ContractAddress
		}

		if to != nil && value.Sign() > 0 {
			// Ensure receiver address balance is initialized from RPC if first time seeing it
			if err := f.ensureAddressBalanceInitialized(ctx, histReader, histWriter, *to, blockNumber); err != nil {
				f.logger.Warn("Failed to initialize receiver balance",
					zap.String("address", to.Hex()),
					zap.Uint64("block", blockNumber),
					zap.Error(err),
				)
				// Continue - balance tracking is best-effort
			}

			// Only update if there's actual value transfer
			if err := histWriter.UpdateBalance(ctx, *to, blockNumber, value, tx.Hash()); err != nil {
				f.logger.Warn("Failed to update receiver balance",
					zap.Uint64("block", blockNumber),
					zap.String("tx", tx.Hash().Hex()),
					zap.String("to", to.Hex()),
					zap.String("delta", value.String()),
					zap.Error(err),
				)
				// Continue processing
			}
		}
	}

	f.logger.Debug("Processed balance tracking",
		zap.Uint64("height", blockNumber),
		zap.Int("transactions", len(transactions)),
	)

	return nil
}
