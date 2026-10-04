package fetch

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/chains/stablenet"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	storagepkg "github.com/0xmhha/indexer-go/pkg/storage"
)

// ============================================================================
// Address Indexing and Balance Tracking Methods
// ============================================================================

// processAddressIndexing parses and stores address indexing data from block and receipts
func (f *Fetcher) processAddressIndexing(ctx context.Context, fb *fetchedBlock) error {
	// Check if storage implements AddressIndexWriter
	addressWriter, ok := f.storage.(storagepkg.AddressIndexWriter)
	if !ok {
		// Storage doesn't support address indexing - skip silently
		return nil
	}

	// Check if storage implements Writer for transaction address indexing
	storageWriter, hasWriter := f.storage.(storagepkg.Writer)

	block := fb.geth
	blockNumber := fb.height()
	blockTime := fb.block.Time
	pairs := fb.transactions()

	// Process each transaction and its receipt. Hashes and addresses come
	// from the model so they match the chain (fee delegation keeps its own
	// hash and fee payer).
	for _, p := range pairs {
		tx, receipt := p.tx, p.receipt
		txHash := tx.Hash

		// 0. Index transaction addresses (from, to, feePayer) for transactionsByAddress query
		if hasWriter {
			// Index 'from' address
			from := tx.From
			if from != (common.Address{}) {
				if err := storageWriter.AddTransactionToAddressIndex(ctx, from, txHash); err != nil {
					f.logger.Warn("Failed to index transaction for from address",
						zap.Uint64("block", blockNumber),
						zap.String("tx", txHash.Hex()),
						zap.String("from", from.Hex()),
						zap.Error(err),
					)
					if f.strictStorageErrors {
						return fmt.Errorf("failed to index transaction for from address: %w", err)
					}
				}
			}

			// Index 'to' address (if not contract creation)
			if tx.To != nil {
				to := *tx.To
				if to != from { // Avoid duplicate indexing for self-transfers
					if err := storageWriter.AddTransactionToAddressIndex(ctx, to, txHash); err != nil {
						f.logger.Warn("Failed to index transaction for to address",
							zap.Uint64("block", blockNumber),
							zap.String("tx", txHash.Hex()),
							zap.String("to", to.Hex()),
							zap.Error(err),
						)
						if f.strictStorageErrors {
							return fmt.Errorf("failed to index transaction for to address: %w", err)
						}
					}
				}
			}

			// Index the fee payer of fee delegation transactions (StableNet type 0x16)
			if feePayer, ok := f.delegatedFeePayer(ctx, tx); ok {
				// Avoid duplicate indexing if feePayer is same as from or to
				if feePayer != from && (tx.To == nil || feePayer != *tx.To) {
					if err := storageWriter.AddTransactionToAddressIndex(ctx, feePayer, txHash); err != nil {
						f.logger.Warn("Failed to index transaction for feePayer address",
							zap.Uint64("block", blockNumber),
							zap.String("tx", txHash.Hex()),
							zap.String("feePayer", feePayer.Hex()),
							zap.Error(err),
						)
						if f.strictStorageErrors {
							return fmt.Errorf("failed to index transaction for feePayer address: %w", err)
						}
					}
				}
			}
		}

		// 1. Contract Creation Detection
		// Contract creation is indicated by tx.To() == nil
		if tx.To == nil && receipt.ContractAddress != nil {
			contractAddress := *receipt.ContractAddress
			creation := &storagepkg.ContractCreation{
				ContractAddress: contractAddress,
				Creator:         tx.From,
				TransactionHash: txHash,
				BlockNumber:     blockNumber,
				Timestamp:       blockTime,
				BytecodeSize:    len(contractAddress.Bytes()), // This is simplified
			}

			if err := addressWriter.SaveContractCreation(ctx, creation); err != nil {
				f.logger.Warn("Failed to save contract creation",
					zap.Uint64("block", blockNumber),
					zap.String("tx", txHash.Hex()),
					zap.String("contract", contractAddress.Hex()),
					zap.Error(err),
				)
				if f.strictStorageErrors {
					return fmt.Errorf("failed to save contract creation: %w", err)
				}
			}

			// Index token metadata if this is a token contract
			if f.tokenIndexer != nil {
				if err := f.tokenIndexer.IndexToken(ctx, contractAddress, blockNumber); err != nil {
					f.logger.Debug("Failed to index token metadata (may not be a token contract)",
						zap.String("contract", contractAddress.Hex()),
						zap.Error(err),
					)
				}
			}
		}

		// 2. Parse ERC20/ERC721 Transfer Events from Logs
		for _, log := range p.gethReceipt.Logs {
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
						zap.String("tx", txHash.Hex()),
						zap.String("token", log.Address.Hex()),
						zap.Error(err),
					)
					if f.strictStorageErrors {
						return fmt.Errorf("failed to save ERC20 transfer: %w", err)
					}
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
						zap.String("tx", txHash.Hex()),
						zap.String("token", log.Address.Hex()),
						zap.String("tokenId", tokenId.String()),
						zap.Error(err),
					)
					if f.strictStorageErrors {
						return fmt.Errorf("failed to save ERC721 transfer: %w", err)
					}
				}
			}
		}

		// 3. Process EIP-7702 SetCode Transactions
		if f.setCodeProcessor != nil && tx.Type == types.SetCodeTxType {
			if err := f.setCodeProcessor.ProcessSetCodeTransactionAt(ctx, p.gethTx, p.gethReceipt, blockNumber, fb.block.Hash, blockTime, uint64(p.index)); err != nil {
				f.logger.Warn("Failed to process SetCode transaction",
					zap.Uint64("block", blockNumber),
					zap.String("tx", txHash.Hex()),
					zap.Error(err),
				)
			}
		}
	}

	// 4. Process ERC-4337 UserOperations from block
	if f.userOpProcessor != nil {
		bundles := make([]UserOpBundle, 0, len(pairs))
		for _, p := range pairs {
			bundles = append(bundles, UserOpBundle{Sender: p.tx.From, Receipt: p.gethReceipt})
		}
		if err := f.userOpProcessor.ProcessUserOps(ctx, blockNumber, fb.block.Hash, blockTime, bundles); err != nil {
			f.logger.Warn("Failed to process ERC-4337 UserOperations",
				zap.Uint64("block", blockNumber),
				zap.Error(err),
			)
		}
	}

	// 5. Process ERC-7579 module install/uninstall events
	if f.moduleProcessor != nil {
		if err := f.moduleProcessor.ProcessModuleEventsFromBlock(ctx, block, fb.gethReceipts); err != nil {
			f.logger.Warn("Failed to process ERC-7579 module events",
				zap.Uint64("block", blockNumber),
				zap.Error(err),
			)
		}
	}

	f.logger.Debug("Processed address indexing",
		zap.Uint64("height", blockNumber),
		zap.Int("transactions", len(fb.block.Transactions)),
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

	rpcBalance, err := f.balanceAt(ctx, addr, rpcBlockNumber)
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
	rpcBalance, err := f.balanceAt(ctx, miner, big.NewInt(0))
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

// processBalanceTracking tracks native balance changes from transfers and
// gas payments. The sender pays the value; the gas (gas used times the
// effective gas price from the receipt) is paid by the fee payer of a fee
// delegation transaction, otherwise by the sender.
func (f *Fetcher) processBalanceTracking(ctx context.Context, fb *fetchedBlock) error {
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

	blockNumber := fb.height()

	// debit and credit apply one balance change, initializing the account
	// from RPC the first time it is seen. Balance tracking is best-effort:
	// failures are logged and indexing continues.
	apply := func(addr common.Address, delta *big.Int, txHash common.Hash, what string) {
		if err := f.ensureAddressBalanceInitialized(ctx, histReader, histWriter, addr, blockNumber); err != nil {
			f.logger.Warn("Failed to initialize "+what+" balance",
				zap.String("address", addr.Hex()),
				zap.Uint64("block", blockNumber),
				zap.Error(err),
			)
		}
		if err := histWriter.UpdateBalance(ctx, addr, blockNumber, delta, txHash); err != nil {
			f.logger.Warn("Failed to update "+what+" balance",
				zap.Uint64("block", blockNumber),
				zap.String("tx", txHash.Hex()),
				zap.String("address", addr.Hex()),
				zap.String("delta", delta.String()),
				zap.Error(err),
			)
		}
	}

	pairs := fb.transactions()
	for _, p := range pairs {
		tx, receipt := p.tx, p.receipt
		from := tx.From
		if from == (common.Address{}) {
			// Cannot determine sender, skip
			continue
		}

		gasPrice := receipt.EffectiveGasPrice
		if gasPrice == nil {
			gasPrice = tx.GasPrice
		}
		gasCost := new(big.Int)
		if gasPrice != nil {
			gasCost.Mul(new(big.Int).SetUint64(receipt.GasUsed), gasPrice)
		}
		value := tx.Value
		if value == nil {
			value = new(big.Int)
		}

		payer := from
		if feePayer, ok := f.delegatedFeePayer(ctx, tx); ok {
			payer = feePayer
		}
		if payer == from {
			apply(from, new(big.Int).Neg(new(big.Int).Add(value, gasCost)), tx.Hash, "sender")
		} else {
			if value.Sign() > 0 {
				apply(from, new(big.Int).Neg(value), tx.Hash, "sender")
			}
			apply(payer, new(big.Int).Neg(gasCost), tx.Hash, "fee payer")
		}

		// Credit the receiver with the value (not the gas). For contract
		// creation the receiver is the new contract.
		to := tx.To
		if to == nil && receipt.ContractAddress != nil {
			to = receipt.ContractAddress
		}
		if to != nil && value.Sign() > 0 {
			apply(*to, value, tx.Hash, "receiver")
		}
	}

	f.logger.Debug("Processed balance tracking",
		zap.Uint64("height", blockNumber),
		zap.Int("transactions", len(pairs)),
	)

	return nil
}

// delegatedFeePayer returns the account paying gas for tx when it is not the
// sender. Blocks decoded by the StableNet profile carry it on the
// transaction; blocks read through the legacy client fall back to the stored
// fee delegation metadata.
func (f *Fetcher) delegatedFeePayer(ctx context.Context, tx *model.Transaction) (common.Address, bool) {
	if fd, ok := stablenet.FeeDelegationOf(tx); ok {
		return fd.FeePayer, true
	}
	if tx.Type != stablenet.FeeDelegationTxType {
		return common.Address{}, false
	}
	if fdReader, ok := f.storage.(storagepkg.FeeDelegationReader); ok {
		if meta, err := fdReader.GetFeeDelegationTxMeta(ctx, tx.Hash); err == nil && meta != nil {
			return meta.FeePayer, true
		}
	}
	return common.Address{}, false
}
