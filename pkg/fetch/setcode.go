package fetch

import (
	"context"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"

	storagepkg "github.com/0xmhha/indexer-go/pkg/storage"
)

// SetCodeIndexer defines the interface for indexing EIP-7702 SetCode transactions
type SetCodeIndexer interface {
	storagepkg.SetCodeIndexWriter
}

// SetCodeProcessor handles processing of EIP-7702 SetCode transactions
type SetCodeProcessor struct {
	logger  *zap.Logger
	storage SetCodeIndexer
}

// NewSetCodeProcessor creates a new SetCode processor
func NewSetCodeProcessor(logger *zap.Logger, storage SetCodeIndexer) *SetCodeProcessor {
	return &SetCodeProcessor{
		logger:  logger.Named("setcode"),
		storage: storage,
	}
}

// ProcessSetCodeTransaction processes a SetCode transaction and indexes its authorizations
func (p *SetCodeProcessor) ProcessSetCodeTransaction(
	ctx context.Context,
	tx *types.Transaction,
	receipt *types.Receipt,
	block *types.Block,
	txIndex uint64,
) error {
	// Only process SetCode transactions (type 0x04)
	if tx.Type() != types.SetCodeTxType {
		return nil
	}

	authList := tx.SetCodeAuthorizations()
	if len(authList) == 0 {
		p.logger.Debug("SetCode transaction has no authorizations",
			zap.String("txHash", tx.Hash().Hex()))
		return nil
	}

	blockNumber := block.NumberU64()
	blockHash := block.Hash()
	blockTime := time.Unix(int64(block.Time()), 0)
	txHash := tx.Hash()

	// Determine if the transaction was successful
	txSuccess := receipt != nil && receipt.Status == types.ReceiptStatusSuccessful

	p.logger.Debug("Processing SetCode transaction",
		zap.String("txHash", txHash.Hex()),
		zap.Uint64("blockNumber", blockNumber),
		zap.Int("authCount", len(authList)),
		zap.Bool("txSuccess", txSuccess))

	// Extract and process each authorization
	records := make([]*storagepkg.SetCodeAuthorizationRecord, 0, len(authList))

	for i, auth := range authList {
		record := p.extractAuthorizationRecord(
			auth,
			txHash,
			blockNumber,
			blockHash,
			txIndex,
			i,
			blockTime,
			txSuccess,
		)
		records = append(records, record)

		// Update stats for target and authority addresses
		if err := p.storage.IncrementSetCodeStats(ctx, record.TargetAddress, true, false, blockNumber); err != nil {
			p.logger.Warn("Failed to increment target stats",
				zap.String("address", record.TargetAddress.Hex()),
				zap.Error(err))
		}

		if record.AuthorityAddress != (common.Address{}) {
			if err := p.storage.IncrementSetCodeStats(ctx, record.AuthorityAddress, false, true, blockNumber); err != nil {
				p.logger.Warn("Failed to increment authority stats",
					zap.String("address", record.AuthorityAddress.Hex()),
					zap.Error(err))
			}
		}

		// Update delegation state if authorization was applied successfully
		if record.Applied {
			state := &storagepkg.AddressDelegationState{
				Address:           record.AuthorityAddress,
				LastUpdatedBlock:  blockNumber,
				LastUpdatedTxHash: txHash,
			}

			// Check if this is clearing delegation (target is zero address)
			if record.TargetAddress == (common.Address{}) {
				state.HasDelegation = false
				state.DelegationTarget = nil
			} else {
				state.HasDelegation = true
				target := record.TargetAddress
				state.DelegationTarget = &target
			}

			if err := p.storage.UpdateAddressDelegationState(ctx, state); err != nil {
				p.logger.Warn("Failed to update delegation state",
					zap.String("address", record.AuthorityAddress.Hex()),
					zap.Error(err))
			}
		}
	}

	// Save all authorization records in batch
	if err := p.storage.SaveSetCodeAuthorizations(ctx, records); err != nil {
		p.logger.Error("Failed to save SetCode authorizations",
			zap.String("txHash", txHash.Hex()),
			zap.Error(err))
		return err
	}

	p.logger.Info("Indexed SetCode transaction",
		zap.String("txHash", txHash.Hex()),
		zap.Uint64("blockNumber", blockNumber),
		zap.Int("authCount", len(records)))

	return nil
}

// extractAuthorizationRecord extracts an authorization record from a SetCodeAuthorization
func (p *SetCodeProcessor) extractAuthorizationRecord(
	auth types.SetCodeAuthorization,
	txHash common.Hash,
	blockNumber uint64,
	blockHash common.Hash,
	txIndex uint64,
	authIndex int,
	blockTime time.Time,
	txSuccess bool,
) *storagepkg.SetCodeAuthorizationRecord {
	record := &storagepkg.SetCodeAuthorizationRecord{
		TxHash:        txHash,
		BlockNumber:   blockNumber,
		BlockHash:     blockHash,
		TxIndex:       txIndex,
		AuthIndex:     authIndex,
		TargetAddress: auth.Address,
		ChainID:       auth.ChainID.ToBig(),
		Nonce:         auth.Nonce,
		YParity:       auth.V,
		R:             auth.R.ToBig(),
		S:             auth.S.ToBig(),
		Timestamp:     blockTime,
	}

	// Recover authority address from signature
	authority, err := auth.Authority()
	if err != nil {
		p.logger.Warn("Failed to recover authority from SetCode authorization",
			zap.String("txHash", txHash.Hex()),
			zap.Int("authIndex", authIndex),
			zap.Error(err))
		record.AuthorityAddress = common.Address{}
		record.Applied = false
		record.Error = storagepkg.SetCodeErrRecoveryFailed
	} else {
		record.AuthorityAddress = authority

		// Validate the authorization
		validationErr := p.validateAuthorization(auth, authority, txSuccess)
		if validationErr != "" {
			record.Applied = false
			record.Error = validationErr
		} else {
			// If tx was successful and validation passed, assume authorization was applied
			record.Applied = txSuccess
		}
	}

	return record
}

// validateAuthorization performs basic validation on an authorization
// Note: Full validation (nonce check, account state) requires state access
// which is not available in the indexer. We perform basic sanity checks only.
func (p *SetCodeProcessor) validateAuthorization(
	auth types.SetCodeAuthorization,
	authority common.Address,
	txSuccess bool,
) string {
	// Validate ChainID (0 = any chain, otherwise must match)
	// Note: We can't validate chain ID match without knowing the current chain ID
	// This is handled by the node during execution

	// Check for nonce overflow (2^64-1 is max)
	if auth.Nonce == ^uint64(0) {
		return storagepkg.SetCodeErrNonceOverflow
	}

	// Check signature components are valid (non-zero R and S)
	if auth.R.IsZero() || auth.S.IsZero() {
		return storagepkg.SetCodeErrInvalidSignature
	}

	// If the transaction failed, we can't determine the exact error
	// The node may have rejected it for various reasons
	if !txSuccess {
		// Don't set error - the tx failure could be for other reasons
		return ""
	}

	return storagepkg.SetCodeErrNone
}

// ProcessSetCodeTransactionBatch processes multiple SetCode transactions in a batch
func (p *SetCodeProcessor) ProcessSetCodeTransactionBatch(
	ctx context.Context,
	txs []*types.Transaction,
	receipts []*types.Receipt,
	block *types.Block,
) error {
	for i, tx := range txs {
		if tx.Type() == types.SetCodeTxType {
			var receipt *types.Receipt
			if i < len(receipts) {
				receipt = receipts[i]
			}
			if err := p.ProcessSetCodeTransaction(ctx, tx, receipt, block, uint64(i)); err != nil {
				// Log error but continue processing other transactions
				p.logger.Warn("Failed to process SetCode transaction",
					zap.String("txHash", tx.Hash().Hex()),
					zap.Error(err))
			}
		}
	}
	return nil
}

// ExtractSetCodeAuthorizationsFromTx extracts authorization records from a transaction
// without saving to storage. Useful for API responses.
func ExtractSetCodeAuthorizationsFromTx(
	tx *types.Transaction,
	blockNumber uint64,
	blockHash common.Hash,
	txIndex uint64,
	blockTime time.Time,
) []*storagepkg.SetCodeAuthorizationRecord {
	if tx.Type() != types.SetCodeTxType {
		return nil
	}

	authList := tx.SetCodeAuthorizations()
	if len(authList) == 0 {
		return nil
	}

	txHash := tx.Hash()
	records := make([]*storagepkg.SetCodeAuthorizationRecord, 0, len(authList))

	for i, auth := range authList {
		record := &storagepkg.SetCodeAuthorizationRecord{
			TxHash:        txHash,
			BlockNumber:   blockNumber,
			BlockHash:     blockHash,
			TxIndex:       txIndex,
			AuthIndex:     i,
			TargetAddress: auth.Address,
			ChainID:       auth.ChainID.ToBig(),
			Nonce:         auth.Nonce,
			YParity:       auth.V,
			R:             auth.R.ToBig(),
			S:             auth.S.ToBig(),
			Timestamp:     blockTime,
		}

		// Recover authority address
		if authority, err := auth.Authority(); err == nil {
			record.AuthorityAddress = authority
		}

		records = append(records, record)
	}

	return records
}

// IsSetCodeTransaction checks if a transaction is a SetCode transaction
func IsSetCodeTransaction(tx *types.Transaction) bool {
	return tx.Type() == types.SetCodeTxType
}

// GetSetCodeAuthorizationCount returns the number of authorizations in a SetCode transaction
func GetSetCodeAuthorizationCount(tx *types.Transaction) int {
	if tx.Type() != types.SetCodeTxType {
		return 0
	}
	return len(tx.SetCodeAuthorizations())
}

// CalculateSetCodeIntrinsicGas calculates the intrinsic gas for a SetCode transaction
// Formula: TxGas + (len(authList) * TxAuthTupleGas) + accessListGas + dataGas
func CalculateSetCodeIntrinsicGas(tx *types.Transaction) uint64 {
	if tx.Type() != types.SetCodeTxType {
		return 0
	}

	const (
		TxGas          = 21000 // Base transaction gas
		TxAuthTupleGas = 12500 // Gas per authorization
	)

	// Base gas
	gas := uint64(TxGas)

	// Authorization gas
	authCount := len(tx.SetCodeAuthorizations())
	gas += uint64(authCount) * TxAuthTupleGas

	// Access list gas (if any)
	accessList := tx.AccessList()
	if accessList != nil {
		const (
			TxAccessListAddressGas    = 2400 // Gas per address in access list
			TxAccessListStorageKeyGas = 1900 // Gas per storage key
		)
		for _, entry := range accessList {
			gas += TxAccessListAddressGas
			gas += uint64(len(entry.StorageKeys)) * TxAccessListStorageKeyGas
		}
	}

	// Data gas
	data := tx.Data()
	if len(data) > 0 {
		const (
			TxDataZeroGas    = 4  // Gas per zero byte
			TxDataNonZeroGas = 16 // Gas per non-zero byte
		)
		for _, b := range data {
			if b == 0 {
				gas += TxDataZeroGas
			} else {
				gas += TxDataNonZeroGas
			}
		}
	}

	return gas
}

// SetCodeTxStats holds statistics about SetCode transactions
type SetCodeTxStats struct {
	TotalTransactions   int
	TotalAuthorizations int
	AppliedCount        int
	FailedCount         int
	UniqueTargets       int
	UniqueAuthorities   int
}

// CalculateSetCodeTxStats calculates statistics from a list of authorization records
func CalculateSetCodeTxStats(records []*storagepkg.SetCodeAuthorizationRecord) SetCodeTxStats {
	stats := SetCodeTxStats{}
	if len(records) == 0 {
		return stats
	}

	txHashes := make(map[common.Hash]bool)
	targets := make(map[common.Address]bool)
	authorities := make(map[common.Address]bool)

	for _, r := range records {
		txHashes[r.TxHash] = true
		targets[r.TargetAddress] = true
		if r.AuthorityAddress != (common.Address{}) {
			authorities[r.AuthorityAddress] = true
		}

		if r.Applied {
			stats.AppliedCount++
		} else {
			stats.FailedCount++
		}
	}

	stats.TotalTransactions = len(txHashes)
	stats.TotalAuthorizations = len(records)
	stats.UniqueTargets = len(targets)
	stats.UniqueAuthorities = len(authorities)

	return stats
}

// SetCodeGasBreakdown provides a detailed gas breakdown for a SetCode transaction
type SetCodeGasBreakdown struct {
	BaseGas          uint64 `json:"baseGas"`
	AuthorizationGas uint64 `json:"authorizationGas"`
	AccessListGas    uint64 `json:"accessListGas"`
	DataGas          uint64 `json:"dataGas"`
	TotalIntrinsic   uint64 `json:"totalIntrinsic"`
	AuthCount        int    `json:"authCount"`
}

// GetSetCodeGasBreakdown returns a detailed gas breakdown for a SetCode transaction
func GetSetCodeGasBreakdown(tx *types.Transaction) *SetCodeGasBreakdown {
	if tx.Type() != types.SetCodeTxType {
		return nil
	}

	const (
		TxGas                     = 21000
		TxAuthTupleGas            = 12500
		TxAccessListAddressGas    = 2400
		TxAccessListStorageKeyGas = 1900
		TxDataZeroGas             = 4
		TxDataNonZeroGas          = 16
	)

	breakdown := &SetCodeGasBreakdown{
		BaseGas:   TxGas,
		AuthCount: len(tx.SetCodeAuthorizations()),
	}

	// Authorization gas
	breakdown.AuthorizationGas = uint64(breakdown.AuthCount) * TxAuthTupleGas

	// Access list gas
	for _, entry := range tx.AccessList() {
		breakdown.AccessListGas += TxAccessListAddressGas
		breakdown.AccessListGas += uint64(len(entry.StorageKeys)) * TxAccessListStorageKeyGas
	}

	// Data gas
	data := tx.Data()
	for _, b := range data {
		if b == 0 {
			breakdown.DataGas += TxDataZeroGas
		} else {
			breakdown.DataGas += TxDataNonZeroGas
		}
	}

	breakdown.TotalIntrinsic = breakdown.BaseGas + breakdown.AuthorizationGas +
		breakdown.AccessListGas + breakdown.DataGas

	return breakdown
}

// Ensure we use big.Int to avoid unused import error
var _ = big.NewInt(0)
