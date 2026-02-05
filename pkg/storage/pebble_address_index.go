package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/cockroachdb/pebble"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/constants"
)

// Compile-time check to ensure PebbleStorage implements AddressIndexReader and AddressIndexWriter
var _ AddressIndexReader = (*PebbleStorage)(nil)
var _ AddressIndexWriter = (*PebbleStorage)(nil)

// ========== Contract Creation Implementation ==========

// GetContractCreation retrieves contract creation information by contract address.
// Returns ErrNotFound if the contract was not created or not indexed.
func (s *PebbleStorage) GetContractCreation(ctx context.Context, contractAddress common.Address) (*ContractCreation, error) {
	if s.closed.Load() {
		return nil, ErrClosed
	}

	key := ContractCreationKey(contractAddress)
	value, closer, err := s.db.Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get contract creation: %w", err)
	}
	defer closer.Close()

	var creation ContractCreation
	if err := json.Unmarshal(value, &creation); err != nil {
		return nil, fmt.Errorf("failed to unmarshal contract creation: %w", err)
	}

	return &creation, nil
}

// GetContractsByCreator retrieves contracts created by a specific address with pagination.
// Returns empty slice if no contracts found.
func (s *PebbleStorage) GetContractsByCreator(ctx context.Context, creator common.Address, limit, offset int) ([]common.Address, error) {
	if s.closed.Load() {
		return nil, ErrClosed
	}

	// Validate pagination parameters
	if limit <= 0 {
		limit = constants.DefaultPaginationLimit
	}
	if limit > constants.DefaultMaxPaginationLimit {
		limit = constants.DefaultMaxPaginationLimit
	}
	if offset < 0 {
		offset = 0
	}

	prefix := ContractCreatorIndexKeyPrefix(creator)

	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	contracts := make([]common.Address, 0, limit)
	count := 0
	skipped := 0

	for iter.First(); iter.Valid(); iter.Next() {
		// Skip offset items
		if skipped < offset {
			skipped++
			continue
		}

		// Check limit
		if count >= limit {
			break
		}

		// Extract contract address from value
		value := iter.Value()
		if len(value) > 0 {
			addr := common.BytesToAddress(value)
			contracts = append(contracts, addr)
			count++
		}
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return contracts, nil
}

// SaveContractCreation saves contract creation information.
// Returns error if storage operation fails.
func (s *PebbleStorage) SaveContractCreation(ctx context.Context, creation *ContractCreation) error {
	if s.closed.Load() {
		return ErrClosed
	}

	if creation == nil {
		return fmt.Errorf("contract creation cannot be nil")
	}

	// Validate required fields
	if creation.ContractAddress == (common.Address{}) {
		return fmt.Errorf("contract address cannot be zero")
	}
	if creation.Creator == (common.Address{}) {
		return fmt.Errorf("creator address cannot be zero")
	}
	if creation.TransactionHash == (common.Hash{}) {
		return fmt.Errorf("transaction hash cannot be zero")
	}

	batch := s.db.NewBatch()
	defer batch.Close()

	// Encode contract creation data
	data, err := json.Marshal(creation)
	if err != nil {
		return fmt.Errorf("failed to marshal contract creation: %w", err)
	}

	// Save main data
	key := ContractCreationKey(creation.ContractAddress)
	if err := batch.Set(key, data, pebble.Sync); err != nil {
		return fmt.Errorf("failed to save contract creation data: %w", err)
	}

	// Save creator index
	creatorIndexKey := ContractCreatorIndexKey(creation.Creator, creation.BlockNumber, creation.TransactionHash)
	if err := batch.Set(creatorIndexKey, creation.ContractAddress.Bytes(), pebble.Sync); err != nil {
		return fmt.Errorf("failed to save creator index: %w", err)
	}

	// Save block index
	blockIndexKey := ContractBlockIndexKey(creation.BlockNumber, creation.ContractAddress)
	if err := batch.Set(blockIndexKey, creation.ContractAddress.Bytes(), pebble.Sync); err != nil {
		return fmt.Errorf("failed to save block index: %w", err)
	}

	// Commit batch
	if err := batch.Commit(pebble.Sync); err != nil {
		return fmt.Errorf("failed to commit contract creation batch: %w", err)
	}

	return nil
}

// ListContracts retrieves all deployed contracts with pagination.
// Returns contracts sorted by deployment block number (descending - newest first).
func (s *PebbleStorage) ListContracts(ctx context.Context, limit, offset int) ([]*ContractCreation, error) {
	if s.closed.Load() {
		return nil, ErrClosed
	}

	// Validate pagination parameters
	if limit <= 0 {
		limit = constants.DefaultPaginationLimit
	}
	if limit > constants.DefaultMaxPaginationLimit {
		limit = constants.DefaultMaxPaginationLimit
	}
	if offset < 0 {
		offset = 0
	}

	// Use block index for reverse chronological order
	// /index/contract/block/{blockNumber}/{contractAddress}
	prefix := []byte(prefixIdxContractBlock)

	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	// Collect all contract addresses first (to sort by block descending)
	var contractAddrs []common.Address
	for iter.Last(); iter.Valid(); iter.Prev() {
		value := iter.Value()
		if len(value) > 0 {
			addr := common.BytesToAddress(value)
			contractAddrs = append(contractAddrs, addr)
		}
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	// Apply pagination
	start := offset
	if start >= len(contractAddrs) {
		return []*ContractCreation{}, nil
	}
	end := start + limit
	if end > len(contractAddrs) {
		end = len(contractAddrs)
	}

	paginatedAddrs := contractAddrs[start:end]

	// Fetch full contract creation info for each address
	contracts := make([]*ContractCreation, 0, len(paginatedAddrs))
	for _, addr := range paginatedAddrs {
		creation, err := s.GetContractCreation(ctx, addr)
		if err != nil {
			s.logger.Warn("failed to get contract creation details",
				zap.String("address", addr.Hex()),
				zap.Error(err))
			continue
		}
		contracts = append(contracts, creation)
	}

	return contracts, nil
}

// GetContractsCount returns the total number of deployed contracts.
func (s *PebbleStorage) GetContractsCount(ctx context.Context) (int, error) {
	if s.closed.Load() {
		return 0, ErrClosed
	}

	prefix := []byte(prefixContractCreation)

	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
	})
	if err != nil {
		return 0, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	count := 0
	for iter.First(); iter.Valid(); iter.Next() {
		count++
	}

	if err := iter.Error(); err != nil {
		return 0, fmt.Errorf("iterator error: %w", err)
	}

	return count, nil
}

// ========== ERC20 Transfer Implementation ==========

// GetERC20Transfer retrieves a specific ERC20 transfer by transaction hash and log index.
// Returns ErrNotFound if the transfer does not exist.
func (s *PebbleStorage) GetERC20Transfer(ctx context.Context, txHash common.Hash, logIndex uint) (*ERC20Transfer, error) {
	if s.closed.Load() {
		return nil, ErrClosed
	}

	key := ERC20TransferKey(txHash, logIndex)
	value, closer, err := s.db.Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get ERC20 transfer: %w", err)
	}
	defer closer.Close()

	var transfer ERC20Transfer
	if err := json.Unmarshal(value, &transfer); err != nil {
		return nil, fmt.Errorf("failed to unmarshal ERC20 transfer: %w", err)
	}

	return &transfer, nil
}

// GetERC20TransfersByToken retrieves ERC20 transfers for a specific token contract with pagination.
func (s *PebbleStorage) GetERC20TransfersByToken(ctx context.Context, tokenAddress common.Address, limit, offset int) ([]*ERC20Transfer, error) {
	if s.closed.Load() {
		return nil, ErrClosed
	}

	// Validate pagination parameters
	if limit <= 0 {
		limit = constants.DefaultPaginationLimit
	}
	if limit > constants.DefaultMaxPaginationLimit {
		limit = constants.DefaultMaxPaginationLimit
	}
	if offset < 0 {
		offset = 0
	}

	prefix := ERC20TokenIndexKeyPrefix(tokenAddress)

	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	transfers := make([]*ERC20Transfer, 0, limit)
	count := 0
	skipped := 0

	for iter.First(); iter.Valid(); iter.Next() {
		// Skip offset items
		if skipped < offset {
			skipped++
			continue
		}

		// Check limit
		if count >= limit {
			break
		}

		// Extract txHash from value
		value := iter.Value()
		if len(value) == 0 {
			continue
		}

		txHash := common.BytesToHash(value)

		// Extract logIndex from key (last 6 digits)
		key := iter.Key()
		// Key format: /index/erc20/token/{address}/{blockNumber}/{logIndex}
		// Parse logIndex from the end of the key
		keyStr := string(key)
		var logIndex uint
		_, _ = fmt.Sscanf(keyStr[len(keyStr)-6:], "%06d", &logIndex)

		// Fetch the actual transfer data
		transfer, err := s.GetERC20Transfer(ctx, txHash, logIndex)
		if err != nil {
			// Skip if not found, but log error
			s.logger.Warn("Failed to get ERC20 transfer", zap.String("txHash", txHash.Hex()), zap.Uint("logIndex", logIndex), zap.Error(err))
			continue
		}

		transfers = append(transfers, transfer)
		count++
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return transfers, nil
}

// GetERC20TransfersByAddress retrieves ERC20 transfers involving a specific address.
// If isFrom is true, returns transfers where address is the sender.
// If isFrom is false, returns transfers where address is the recipient.
func (s *PebbleStorage) GetERC20TransfersByAddress(ctx context.Context, address common.Address, isFrom bool, limit, offset int) ([]*ERC20Transfer, error) {
	if s.closed.Load() {
		return nil, ErrClosed
	}

	// Validate pagination parameters
	if limit <= 0 {
		limit = constants.DefaultPaginationLimit
	}
	if limit > constants.DefaultMaxPaginationLimit {
		limit = constants.DefaultMaxPaginationLimit
	}
	if offset < 0 {
		offset = 0
	}

	var prefix []byte
	if isFrom {
		prefix = ERC20FromIndexKeyPrefix(address)
	} else {
		prefix = ERC20ToIndexKeyPrefix(address)
	}

	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	transfers := make([]*ERC20Transfer, 0, limit)
	count := 0
	skipped := 0

	for iter.First(); iter.Valid(); iter.Next() {
		// Skip offset items
		if skipped < offset {
			skipped++
			continue
		}

		// Check limit
		if count >= limit {
			break
		}

		// Extract txHash from value
		value := iter.Value()
		if len(value) == 0 {
			continue
		}

		txHash := common.BytesToHash(value)

		// Extract logIndex from key
		key := iter.Key()
		keyStr := string(key)
		var logIndex uint
		_, _ = fmt.Sscanf(keyStr[len(keyStr)-6:], "%06d", &logIndex)

		// Fetch the actual transfer data
		transfer, err := s.GetERC20Transfer(ctx, txHash, logIndex)
		if err != nil {
			s.logger.Warn("Failed to get ERC20 transfer", zap.String("txHash", txHash.Hex()), zap.Uint("logIndex", logIndex), zap.Error(err))
			continue
		}

		transfers = append(transfers, transfer)
		count++
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return transfers, nil
}

// SaveERC20Transfer saves an ERC20 token transfer.
// Returns error if storage operation fails.
func (s *PebbleStorage) SaveERC20Transfer(ctx context.Context, transfer *ERC20Transfer) error {
	if s.closed.Load() {
		return ErrClosed
	}

	if transfer == nil {
		return fmt.Errorf("ERC20 transfer cannot be nil")
	}

	// Validate required fields
	if transfer.ContractAddress == (common.Address{}) {
		return fmt.Errorf("contract address cannot be zero")
	}
	if transfer.TransactionHash == (common.Hash{}) {
		return fmt.Errorf("transaction hash cannot be zero")
	}
	if transfer.Value == nil {
		return fmt.Errorf("value cannot be nil")
	}

	batch := s.db.NewBatch()
	defer batch.Close()

	// Encode transfer data
	data, err := json.Marshal(transfer)
	if err != nil {
		return fmt.Errorf("failed to marshal ERC20 transfer: %w", err)
	}

	// Save main data
	key := ERC20TransferKey(transfer.TransactionHash, transfer.LogIndex)
	if err := batch.Set(key, data, pebble.Sync); err != nil {
		return fmt.Errorf("failed to save ERC20 transfer data: %w", err)
	}

	// Save token index
	tokenIndexKey := ERC20TokenIndexKey(transfer.ContractAddress, transfer.BlockNumber, transfer.LogIndex)
	if err := batch.Set(tokenIndexKey, transfer.TransactionHash.Bytes(), pebble.Sync); err != nil {
		return fmt.Errorf("failed to save token index: %w", err)
	}

	// Save from index
	fromIndexKey := ERC20FromIndexKey(transfer.From, transfer.BlockNumber, transfer.LogIndex)
	if err := batch.Set(fromIndexKey, transfer.TransactionHash.Bytes(), pebble.Sync); err != nil {
		return fmt.Errorf("failed to save from index: %w", err)
	}

	// Save to index
	toIndexKey := ERC20ToIndexKey(transfer.To, transfer.BlockNumber, transfer.LogIndex)
	if err := batch.Set(toIndexKey, transfer.TransactionHash.Bytes(), pebble.Sync); err != nil {
		return fmt.Errorf("failed to save to index: %w", err)
	}

	// Commit batch
	if err := batch.Commit(pebble.Sync); err != nil {
		return fmt.Errorf("failed to commit ERC20 transfer batch: %w", err)
	}

	return nil
}

// ========== ERC721 Transfer Implementation ==========

// GetERC721Transfer retrieves a specific ERC721 transfer by transaction hash and log index.
// Returns ErrNotFound if the transfer does not exist.
func (s *PebbleStorage) GetERC721Transfer(ctx context.Context, txHash common.Hash, logIndex uint) (*ERC721Transfer, error) {
	if s.closed.Load() {
		return nil, ErrClosed
	}

	key := ERC721TransferKey(txHash, logIndex)
	value, closer, err := s.db.Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get ERC721 transfer: %w", err)
	}
	defer closer.Close()

	var transfer ERC721Transfer
	if err := json.Unmarshal(value, &transfer); err != nil {
		return nil, fmt.Errorf("failed to unmarshal ERC721 transfer: %w", err)
	}

	return &transfer, nil
}

// GetERC721TransfersByToken retrieves ERC721 transfers for a specific token contract with pagination.
func (s *PebbleStorage) GetERC721TransfersByToken(ctx context.Context, tokenAddress common.Address, limit, offset int) ([]*ERC721Transfer, error) {
	if s.closed.Load() {
		return nil, ErrClosed
	}

	// Validate pagination parameters
	if limit <= 0 {
		limit = constants.DefaultPaginationLimit
	}
	if limit > constants.DefaultMaxPaginationLimit {
		limit = constants.DefaultMaxPaginationLimit
	}
	if offset < 0 {
		offset = 0
	}

	prefix := ERC721TokenIndexKeyPrefix(tokenAddress)

	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	transfers := make([]*ERC721Transfer, 0, limit)
	count := 0
	skipped := 0

	for iter.First(); iter.Valid(); iter.Next() {
		// Skip offset items
		if skipped < offset {
			skipped++
			continue
		}

		// Check limit
		if count >= limit {
			break
		}

		// Extract txHash from value
		value := iter.Value()
		if len(value) == 0 {
			continue
		}

		txHash := common.BytesToHash(value)

		// Extract logIndex from key
		key := iter.Key()
		keyStr := string(key)
		var logIndex uint
		_, _ = fmt.Sscanf(keyStr[len(keyStr)-6:], "%06d", &logIndex)

		// Fetch the actual transfer data
		transfer, err := s.GetERC721Transfer(ctx, txHash, logIndex)
		if err != nil {
			s.logger.Warn("Failed to get ERC721 transfer", zap.String("txHash", txHash.Hex()), zap.Uint("logIndex", logIndex), zap.Error(err))
			continue
		}

		transfers = append(transfers, transfer)
		count++
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return transfers, nil
}

// GetERC721TransfersByAddress retrieves ERC721 transfers involving a specific address.
// If isFrom is true, returns transfers where address is the sender.
// If isFrom is false, returns transfers where address is the recipient.
func (s *PebbleStorage) GetERC721TransfersByAddress(ctx context.Context, address common.Address, isFrom bool, limit, offset int) ([]*ERC721Transfer, error) {
	if s.closed.Load() {
		return nil, ErrClosed
	}

	// Validate pagination parameters
	if limit <= 0 {
		limit = constants.DefaultPaginationLimit
	}
	if limit > constants.DefaultMaxPaginationLimit {
		limit = constants.DefaultMaxPaginationLimit
	}
	if offset < 0 {
		offset = 0
	}

	var prefix []byte
	if isFrom {
		prefix = ERC721FromIndexKeyPrefix(address)
	} else {
		prefix = ERC721ToIndexKeyPrefix(address)
	}

	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	transfers := make([]*ERC721Transfer, 0, limit)
	count := 0
	skipped := 0

	for iter.First(); iter.Valid(); iter.Next() {
		// Skip offset items
		if skipped < offset {
			skipped++
			continue
		}

		// Check limit
		if count >= limit {
			break
		}

		// Extract txHash from value
		value := iter.Value()
		if len(value) == 0 {
			continue
		}

		txHash := common.BytesToHash(value)

		// Extract logIndex from key
		key := iter.Key()
		keyStr := string(key)
		var logIndex uint
		_, _ = fmt.Sscanf(keyStr[len(keyStr)-6:], "%06d", &logIndex)

		// Fetch the actual transfer data
		transfer, err := s.GetERC721Transfer(ctx, txHash, logIndex)
		if err != nil {
			s.logger.Warn("Failed to get ERC721 transfer", zap.String("txHash", txHash.Hex()), zap.Uint("logIndex", logIndex), zap.Error(err))
			continue
		}

		transfers = append(transfers, transfer)
		count++
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return transfers, nil
}

// GetERC721Owner retrieves the current owner of a specific NFT token.
// Returns ErrNotFound if the token has not been transferred or does not exist.
func (s *PebbleStorage) GetERC721Owner(ctx context.Context, tokenAddress common.Address, tokenId *big.Int) (common.Address, error) {
	if s.closed.Load() {
		return common.Address{}, ErrClosed
	}

	if tokenId == nil {
		return common.Address{}, fmt.Errorf("tokenId cannot be nil")
	}

	key := ERC721TokenOwnerKey(tokenAddress, tokenId.String())
	value, closer, err := s.db.Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			return common.Address{}, ErrNotFound
		}
		return common.Address{}, fmt.Errorf("failed to get ERC721 owner: %w", err)
	}
	defer closer.Close()

	owner := common.BytesToAddress(value)
	return owner, nil
}

// GetNFTsByOwner retrieves all NFTs owned by a specific address with pagination.
// Returns empty slice if no NFTs found.
func (s *PebbleStorage) GetNFTsByOwner(ctx context.Context, owner common.Address, limit, offset int) ([]*NFTOwnership, error) {
	if s.closed.Load() {
		return nil, ErrClosed
	}

	// Validate pagination parameters
	if limit <= 0 {
		limit = constants.DefaultPaginationLimit
	}
	if limit > constants.DefaultMaxPaginationLimit {
		limit = constants.DefaultMaxPaginationLimit
	}
	if offset < 0 {
		offset = 0
	}

	prefix := ERC721OwnerIndexKeyPrefix(owner)

	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	nfts := make([]*NFTOwnership, 0, limit)
	count := 0
	skipped := 0

	for iter.First(); iter.Valid(); iter.Next() {
		// Skip offset items
		if skipped < offset {
			skipped++
			continue
		}

		// Check limit
		if count >= limit {
			break
		}

		// Parse the key to extract contractAddress and tokenId
		// Key format: /index/erc721/owner/{ownerAddress}/{contractAddress}/{tokenId}
		key := string(iter.Key())
		prefixStr := string(prefix)
		remaining := key[len(prefixStr):]

		// Find the separator between contractAddress and tokenId
		parts := splitNFTKey(remaining)
		if len(parts) < 2 {
			s.logger.Warn("Invalid NFT owner index key", zap.String("key", key))
			continue
		}

		contractAddress := common.HexToAddress(parts[0])
		tokenId, ok := new(big.Int).SetString(parts[1], 10)
		if !ok {
			s.logger.Warn("Invalid tokenId in NFT owner index key", zap.String("key", key), zap.String("tokenId", parts[1]))
			continue
		}

		nfts = append(nfts, &NFTOwnership{
			ContractAddress: contractAddress,
			TokenId:         tokenId,
			Owner:           owner,
		})
		count++
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return nfts, nil
}

// splitNFTKey splits the remaining key into contractAddress and tokenId
// Input: "0x123.../123"
// Output: ["0x123...", "123"]
func splitNFTKey(remaining string) []string {
	// Find the last "/" to split contractAddress and tokenId
	lastSlash := -1
	for i := len(remaining) - 1; i >= 0; i-- {
		if remaining[i] == '/' {
			lastSlash = i
			break
		}
	}
	if lastSlash <= 0 {
		return nil
	}
	return []string{remaining[:lastSlash], remaining[lastSlash+1:]}
}

// SaveERC721Transfer saves an ERC721 NFT transfer.
// Also updates the current owner index for the token.
// Returns error if storage operation fails.
func (s *PebbleStorage) SaveERC721Transfer(ctx context.Context, transfer *ERC721Transfer) error {
	if s.closed.Load() {
		return ErrClosed
	}

	if transfer == nil {
		return fmt.Errorf("ERC721 transfer cannot be nil")
	}

	// Validate required fields
	if transfer.ContractAddress == (common.Address{}) {
		return fmt.Errorf("contract address cannot be zero")
	}
	if transfer.TransactionHash == (common.Hash{}) {
		return fmt.Errorf("transaction hash cannot be zero")
	}
	if transfer.TokenId == nil {
		return fmt.Errorf("tokenId cannot be nil")
	}

	batch := s.db.NewBatch()
	defer batch.Close()

	// Encode transfer data
	data, err := json.Marshal(transfer)
	if err != nil {
		return fmt.Errorf("failed to marshal ERC721 transfer: %w", err)
	}

	// Save main data
	key := ERC721TransferKey(transfer.TransactionHash, transfer.LogIndex)
	if err := batch.Set(key, data, pebble.Sync); err != nil {
		return fmt.Errorf("failed to save ERC721 transfer data: %w", err)
	}

	// Save token index
	tokenIndexKey := ERC721TokenIndexKey(transfer.ContractAddress, transfer.BlockNumber, transfer.LogIndex)
	if err := batch.Set(tokenIndexKey, transfer.TransactionHash.Bytes(), pebble.Sync); err != nil {
		return fmt.Errorf("failed to save token index: %w", err)
	}

	// Save from index
	fromIndexKey := ERC721FromIndexKey(transfer.From, transfer.BlockNumber, transfer.LogIndex)
	if err := batch.Set(fromIndexKey, transfer.TransactionHash.Bytes(), pebble.Sync); err != nil {
		return fmt.Errorf("failed to save from index: %w", err)
	}

	// Save to index
	toIndexKey := ERC721ToIndexKey(transfer.To, transfer.BlockNumber, transfer.LogIndex)
	if err := batch.Set(toIndexKey, transfer.TransactionHash.Bytes(), pebble.Sync); err != nil {
		return fmt.Errorf("failed to save to index: %w", err)
	}

	// Update current owner (token -> owner mapping)
	ownerKey := ERC721TokenOwnerKey(transfer.ContractAddress, transfer.TokenId.String())
	if err := batch.Set(ownerKey, transfer.To.Bytes(), pebble.Sync); err != nil {
		return fmt.Errorf("failed to save owner index: %w", err)
	}

	// Update owner-to-NFT reverse index
	// Remove old owner's index entry (if not minting from zero address)
	zeroAddress := common.Address{}
	if transfer.From != zeroAddress {
		oldOwnerIndexKey := ERC721OwnerIndexKey(transfer.From, transfer.ContractAddress, transfer.TokenId.String())
		if err := batch.Delete(oldOwnerIndexKey, pebble.Sync); err != nil {
			s.logger.Warn("Failed to delete old owner index",
				zap.String("from", transfer.From.Hex()),
				zap.String("contract", transfer.ContractAddress.Hex()),
				zap.String("tokenId", transfer.TokenId.String()),
				zap.Error(err))
		}
	}

	// Add new owner's index entry (if not burning to zero address)
	if transfer.To != zeroAddress {
		newOwnerIndexKey := ERC721OwnerIndexKey(transfer.To, transfer.ContractAddress, transfer.TokenId.String())
		if err := batch.Set(newOwnerIndexKey, []byte{1}, pebble.Sync); err != nil {
			return fmt.Errorf("failed to save new owner index: %w", err)
		}
	}

	// Commit batch
	if err := batch.Commit(pebble.Sync); err != nil {
		return fmt.Errorf("failed to commit ERC721 transfer batch: %w", err)
	}

	return nil
}

// ========== Internal Transaction Implementation ==========

// GetInternalTransactions retrieves all internal transactions for a given transaction hash.
// Returns empty slice if no internal transactions found or tracing is disabled.
func (s *PebbleStorage) GetInternalTransactions(ctx context.Context, txHash common.Hash) ([]*InternalTransaction, error) {
	if s.closed.Load() {
		return nil, ErrClosed
	}

	prefix := InternalTransactionKeyPrefix(txHash)

	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	internals := make([]*InternalTransaction, 0)

	for iter.First(); iter.Valid(); iter.Next() {
		value := iter.Value()
		if len(value) == 0 {
			continue
		}

		var internal InternalTransaction
		if err := json.Unmarshal(value, &internal); err != nil {
			s.logger.Warn("Failed to unmarshal internal transaction", zap.String("txHash", txHash.Hex()), zap.Error(err))
			continue
		}

		internals = append(internals, &internal)
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return internals, nil
}

// GetInternalTransactionsByAddress retrieves internal transactions involving a specific address.
// If isFrom is true, returns transactions where address is the caller.
// If isFrom is false, returns transactions where address is the callee.
func (s *PebbleStorage) GetInternalTransactionsByAddress(ctx context.Context, address common.Address, isFrom bool, limit, offset int) ([]*InternalTransaction, error) {
	if s.closed.Load() {
		return nil, ErrClosed
	}

	// Validate pagination parameters
	if limit <= 0 {
		limit = constants.DefaultPaginationLimit
	}
	if limit > constants.DefaultMaxPaginationLimit {
		limit = constants.DefaultMaxPaginationLimit
	}
	if offset < 0 {
		offset = 0
	}

	var prefix []byte
	if isFrom {
		prefix = InternalTxFromIndexKeyPrefix(address)
	} else {
		prefix = InternalTxToIndexKeyPrefix(address)
	}

	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	internals := make([]*InternalTransaction, 0, limit)
	count := 0
	skipped := 0
	seenTxs := make(map[common.Hash]bool)

	for iter.First(); iter.Valid(); iter.Next() {
		// Skip offset items
		if skipped < offset {
			skipped++
			continue
		}

		// Check limit
		if count >= limit {
			break
		}

		// Extract txHash from key
		key := iter.Key()
		keyStr := string(key)
		// Key format: /index/internal/from/{address}/{blockNumber}/{txHash}
		// Extract txHash (last 66 characters: 0x + 64 hex digits)
		if len(keyStr) < 66 {
			continue
		}
		txHashStr := keyStr[len(keyStr)-66:]
		txHash := common.HexToHash(txHashStr)

		// Skip if we already processed this transaction
		if seenTxs[txHash] {
			continue
		}
		seenTxs[txHash] = true

		// Fetch all internal transactions for this tx
		txInternals, err := s.GetInternalTransactions(ctx, txHash)
		if err != nil {
			s.logger.Warn("Failed to get internal transactions", zap.String("txHash", txHash.Hex()), zap.Error(err))
			continue
		}

		// Filter by address
		for _, internal := range txInternals {
			if isFrom && internal.From == address {
				internals = append(internals, internal)
				count++
				if count >= limit {
					break
				}
			} else if !isFrom && internal.To == address {
				internals = append(internals, internal)
				count++
				if count >= limit {
					break
				}
			}
		}
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return internals, nil
}

// SaveInternalTransactions saves all internal transactions for a given transaction hash.
// The internals slice must be ordered by execution order (index field).
// Returns error if storage operation fails.
func (s *PebbleStorage) SaveInternalTransactions(ctx context.Context, txHash common.Hash, internals []*InternalTransaction) error {
	if s.closed.Load() {
		return ErrClosed
	}

	if txHash == (common.Hash{}) {
		return fmt.Errorf("transaction hash cannot be zero")
	}

	if len(internals) == 0 {
		// No internal transactions to save
		return nil
	}

	batch := s.db.NewBatch()
	defer batch.Close()

	for _, internal := range internals {
		if internal == nil {
			continue
		}

		// Validate required fields
		if internal.TransactionHash != txHash {
			return fmt.Errorf("internal transaction hash mismatch: expected %s, got %s", txHash.Hex(), internal.TransactionHash.Hex())
		}

		// Encode internal transaction data
		data, err := json.Marshal(internal)
		if err != nil {
			return fmt.Errorf("failed to marshal internal transaction: %w", err)
		}

		// Save main data
		key := InternalTransactionKey(txHash, internal.Index)
		if err := batch.Set(key, data, pebble.Sync); err != nil {
			return fmt.Errorf("failed to save internal transaction data: %w", err)
		}

		// Save from index
		fromIndexKey := InternalTxFromIndexKey(internal.From, internal.BlockNumber, txHash)
		if err := batch.Set(fromIndexKey, []byte{1}, pebble.Sync); err != nil {
			return fmt.Errorf("failed to save from index: %w", err)
		}

		// Save to index
		toIndexKey := InternalTxToIndexKey(internal.To, internal.BlockNumber, txHash)
		if err := batch.Set(toIndexKey, []byte{1}, pebble.Sync); err != nil {
			return fmt.Errorf("failed to save to index: %w", err)
		}

		// Save block index
		blockIndexKey := InternalTxBlockIndexKey(internal.BlockNumber, txHash)
		if err := batch.Set(blockIndexKey, []byte{1}, pebble.Sync); err != nil {
			return fmt.Errorf("failed to save block index: %w", err)
		}
	}

	// Commit batch
	if err := batch.Commit(pebble.Sync); err != nil {
		return fmt.Errorf("failed to commit internal transactions batch: %w", err)
	}

	return nil
}
