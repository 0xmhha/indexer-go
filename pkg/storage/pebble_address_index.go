package storage

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/cockroachdb/pebble"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/constants"
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// Compile-time check to ensure PebbleStorage implements AddressIndexReader and AddressIndexWriter
var _ port.AddressIndexReader = (*PebbleStorage)(nil)
var _ port.AddressIndexWriter = (*PebbleStorage)(nil)

// addressIndexPageLimit returns the page size of an address index list:
// page.Limit, constants.DefaultPaginationLimit when it is not positive, and
// at most constants.DefaultMaxPaginationLimit.
func addressIndexPageLimit(page port.Page) int {
	return min(pageLimit(page, constants.DefaultPaginationLimit), constants.DefaultMaxPaginationLimit)
}

// addressIndexHasValue keeps the index entries that hold a value.
func addressIndexHasValue(_, value []byte) bool { return len(value) > 0 }

// ========== Contract Creation Implementation ==========

// GetContractCreation retrieves contract creation information by contract address.
// Returns ErrNotFound if the contract was not created or not indexed.
func (s *PebbleStorage) GetContractCreation(ctx context.Context, contractAddress common.Address) (*port.ContractCreation, error) {
	if s.closed.Load() {
		return nil, port.ErrClosed
	}

	key := ContractCreationKey(contractAddress)
	value, closer, err := s.kv(ctx).Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			return nil, port.ErrNotFound
		}
		return nil, fmt.Errorf("failed to get contract creation: %w", err)
	}
	defer closer.Close()

	var creation port.ContractCreation
	if err := json.Unmarshal(value, &creation); err != nil {
		return nil, fmt.Errorf("failed to unmarshal contract creation: %w", err)
	}

	return &creation, nil
}

// GetContractsByCreator returns one page of the contracts created by a
// specific address, in deployment block order.
// Returns empty slice if no contracts found.
func (s *PebbleStorage) GetContractsByCreator(ctx context.Context, creator common.Address, page port.Page) ([]common.Address, string, error) {
	if s.closed.Load() {
		return nil, "", port.ErrClosed
	}

	prefix := ContractCreatorIndexKeyPrefix(creator)
	entries, next, err := s.scanPage(ctx, prefix, prefixUpperBound(prefix), false, page, addressIndexPageLimit(page), addressIndexHasValue)
	if err != nil {
		return nil, "", err
	}

	contracts := make([]common.Address, len(entries))
	for i, e := range entries {
		contracts[i] = common.BytesToAddress(e.Value)
	}
	return contracts, next, nil
}

// SaveContractCreation saves contract creation information.
// Returns error if storage operation fails.
func (s *PebbleStorage) SaveContractCreation(ctx context.Context, creation *port.ContractCreation) error {
	if s.closed.Load() {
		return port.ErrClosed
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

	batch := s.newBatch(ctx)
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
	if err := s.commitBatch(ctx, batch, pebble.Sync); err != nil {
		return fmt.Errorf("failed to commit contract creation batch: %w", err)
	}

	return nil
}

// ListContracts returns one page of all deployed contracts, newest
// deployment block first.
func (s *PebbleStorage) ListContracts(ctx context.Context, page port.Page) ([]*port.ContractCreation, string, error) {
	if s.closed.Load() {
		return nil, "", port.ErrClosed
	}

	// The block index /index/contract/block/{blockNumber}/{contractAddress}
	// read in reverse gives the newest deployment first.
	prefix := []byte(prefixIdxContractBlock)
	entries, next, err := s.scanPage(ctx, prefix, prefixUpperBound(prefix), true, page, addressIndexPageLimit(page), addressIndexHasValue)
	if err != nil {
		return nil, "", err
	}

	// Fetch full contract creation info for each address
	contracts := make([]*port.ContractCreation, 0, len(entries))
	for _, e := range entries {
		addr := common.BytesToAddress(e.Value)
		creation, err := s.GetContractCreation(ctx, addr)
		if err != nil {
			s.logger.Warn("failed to get contract creation details",
				zap.String("address", addr.Hex()),
				zap.Error(err))
			continue
		}
		contracts = append(contracts, creation)
	}

	return contracts, next, nil
}

// GetContractsCount returns the total number of deployed contracts.
func (s *PebbleStorage) GetContractsCount(ctx context.Context) (int, error) {
	if s.closed.Load() {
		return 0, port.ErrClosed
	}

	prefix := []byte(prefixContractCreation)

	iter, err := s.kv(ctx).NewIter(&pebble.IterOptions{
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
func (s *PebbleStorage) GetERC20Transfer(ctx context.Context, txHash common.Hash, logIndex uint) (*port.ERC20Transfer, error) {
	if s.closed.Load() {
		return nil, port.ErrClosed
	}

	key := ERC20TransferKey(txHash, logIndex)
	value, closer, err := s.kv(ctx).Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			return nil, port.ErrNotFound
		}
		return nil, fmt.Errorf("failed to get ERC20 transfer: %w", err)
	}
	defer closer.Close()

	var transfer port.ERC20Transfer
	if err := json.Unmarshal(value, &transfer); err != nil {
		return nil, fmt.Errorf("failed to unmarshal ERC20 transfer: %w", err)
	}

	return &transfer, nil
}

// GetERC20TransfersByToken returns one page of the ERC20 transfers of a
// specific token contract, in block and log index order.
func (s *PebbleStorage) GetERC20TransfersByToken(ctx context.Context, tokenAddress common.Address, page port.Page) ([]*port.ERC20Transfer, string, error) {
	if s.closed.Load() {
		return nil, "", port.ErrClosed
	}
	return s.erc20TransferPage(ctx, ERC20TokenIndexKeyPrefix(tokenAddress), page)
}

// GetERC20TransfersByAddress returns one page of the ERC20 transfers
// involving a specific address, in block and log index order.
// If isFrom is true, returns transfers where address is the sender.
// If isFrom is false, returns transfers where address is the recipient.
func (s *PebbleStorage) GetERC20TransfersByAddress(ctx context.Context, address common.Address, isFrom bool, page port.Page) ([]*port.ERC20Transfer, string, error) {
	if s.closed.Load() {
		return nil, "", port.ErrClosed
	}

	var prefix []byte
	if isFrom {
		prefix = ERC20FromIndexKeyPrefix(address)
	} else {
		prefix = ERC20ToIndexKeyPrefix(address)
	}
	return s.erc20TransferPage(ctx, prefix, page)
}

// erc20TransferPage reads one page of an ERC20 transfer index and loads the
// transfers it points to.
func (s *PebbleStorage) erc20TransferPage(ctx context.Context, prefix []byte, page port.Page) ([]*port.ERC20Transfer, string, error) {
	refs, next, err := s.transferIndexPage(ctx, prefix, page)
	if err != nil {
		return nil, "", err
	}
	transfers := make([]*port.ERC20Transfer, 0, len(refs))
	for _, ref := range refs {
		transfer, err := s.GetERC20Transfer(ctx, ref.txHash, ref.logIndex)
		if err != nil {
			// Skip if not found, but log error
			s.logger.Warn("Failed to get ERC20 transfer", zap.String("txHash", ref.txHash.Hex()), zap.Uint("logIndex", ref.logIndex), zap.Error(err))
			continue
		}
		transfers = append(transfers, transfer)
	}
	return transfers, next, nil
}

// transferRef locates a transfer: the transaction hash an index entry holds
// and the log index at the end of its key.
type transferRef struct {
	txHash   common.Hash
	logIndex uint
}

// transferIndexPage reads one page of a transfer index (ERC20 or ERC721,
// by token, sender or recipient). Keys end in /{logIndex:06d} and values
// hold the transaction hash; entries without a value or a log index are
// not items of the list.
func (s *PebbleStorage) transferIndexPage(ctx context.Context, prefix []byte, page port.Page) ([]transferRef, string, error) {
	entries, next, err := s.scanPage(ctx, prefix, prefixUpperBound(prefix), false, page, addressIndexPageLimit(page), func(key, value []byte) bool {
		_, ok := transferLogIndex(key)
		return len(value) > 0 && ok
	})
	if err != nil {
		return nil, "", err
	}
	refs := make([]transferRef, len(entries))
	for i, e := range entries {
		logIndex, _ := transferLogIndex(e.Key)
		refs[i] = transferRef{txHash: common.BytesToHash(e.Value), logIndex: logIndex}
	}
	return refs, next, nil
}

// transferLogIndex parses the log index from the last six digits of a
// transfer index key: /index/{erc20|erc721}/{token|from|to}/{address}/{blockNumber}/{logIndex}.
func transferLogIndex(key []byte) (uint, bool) {
	if len(key) < 6 {
		return 0, false
	}
	var logIndex uint
	for _, c := range key[len(key)-6:] {
		if c < '0' || c > '9' {
			return 0, false
		}
		logIndex = logIndex*10 + uint(c-'0')
	}
	return logIndex, true
}

// SaveERC20Transfer saves an ERC20 token transfer.
// Returns error if storage operation fails.
func (s *PebbleStorage) SaveERC20Transfer(ctx context.Context, transfer *port.ERC20Transfer) error {
	if s.closed.Load() {
		return port.ErrClosed
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

	batch := s.newBatch(ctx)
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
	if err := s.commitBatch(ctx, batch, pebble.Sync); err != nil {
		return fmt.Errorf("failed to commit ERC20 transfer batch: %w", err)
	}

	return nil
}

// ========== ERC721 Transfer Implementation ==========

// GetERC721Transfer retrieves a specific ERC721 transfer by transaction hash and log index.
// Returns ErrNotFound if the transfer does not exist.
func (s *PebbleStorage) GetERC721Transfer(ctx context.Context, txHash common.Hash, logIndex uint) (*port.ERC721Transfer, error) {
	if s.closed.Load() {
		return nil, port.ErrClosed
	}

	key := ERC721TransferKey(txHash, logIndex)
	value, closer, err := s.kv(ctx).Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			return nil, port.ErrNotFound
		}
		return nil, fmt.Errorf("failed to get ERC721 transfer: %w", err)
	}
	defer closer.Close()

	var transfer port.ERC721Transfer
	if err := json.Unmarshal(value, &transfer); err != nil {
		return nil, fmt.Errorf("failed to unmarshal ERC721 transfer: %w", err)
	}

	return &transfer, nil
}

// GetERC721TransfersByToken returns one page of the ERC721 transfers of a
// specific token contract, in block and log index order.
func (s *PebbleStorage) GetERC721TransfersByToken(ctx context.Context, tokenAddress common.Address, page port.Page) ([]*port.ERC721Transfer, string, error) {
	if s.closed.Load() {
		return nil, "", port.ErrClosed
	}
	return s.erc721TransferPage(ctx, ERC721TokenIndexKeyPrefix(tokenAddress), page)
}

// GetERC721TransfersByAddress returns one page of the ERC721 transfers
// involving a specific address, in block and log index order.
// If isFrom is true, returns transfers where address is the sender.
// If isFrom is false, returns transfers where address is the recipient.
func (s *PebbleStorage) GetERC721TransfersByAddress(ctx context.Context, address common.Address, isFrom bool, page port.Page) ([]*port.ERC721Transfer, string, error) {
	if s.closed.Load() {
		return nil, "", port.ErrClosed
	}

	var prefix []byte
	if isFrom {
		prefix = ERC721FromIndexKeyPrefix(address)
	} else {
		prefix = ERC721ToIndexKeyPrefix(address)
	}
	return s.erc721TransferPage(ctx, prefix, page)
}

// erc721TransferPage reads one page of an ERC721 transfer index and loads
// the transfers it points to.
func (s *PebbleStorage) erc721TransferPage(ctx context.Context, prefix []byte, page port.Page) ([]*port.ERC721Transfer, string, error) {
	refs, next, err := s.transferIndexPage(ctx, prefix, page)
	if err != nil {
		return nil, "", err
	}
	transfers := make([]*port.ERC721Transfer, 0, len(refs))
	for _, ref := range refs {
		transfer, err := s.GetERC721Transfer(ctx, ref.txHash, ref.logIndex)
		if err != nil {
			s.logger.Warn("Failed to get ERC721 transfer", zap.String("txHash", ref.txHash.Hex()), zap.Uint("logIndex", ref.logIndex), zap.Error(err))
			continue
		}
		transfers = append(transfers, transfer)
	}
	return transfers, next, nil
}

// GetERC721Owner retrieves the current owner of a specific NFT token.
// Returns ErrNotFound if the token has not been transferred or does not exist.
func (s *PebbleStorage) GetERC721Owner(ctx context.Context, tokenAddress common.Address, tokenId *big.Int) (common.Address, error) {
	if s.closed.Load() {
		return common.Address{}, port.ErrClosed
	}

	if tokenId == nil {
		return common.Address{}, fmt.Errorf("tokenId cannot be nil")
	}

	key := ERC721TokenOwnerKey(tokenAddress, tokenId.String())
	value, closer, err := s.kv(ctx).Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			return common.Address{}, port.ErrNotFound
		}
		return common.Address{}, fmt.Errorf("failed to get ERC721 owner: %w", err)
	}
	defer closer.Close()

	owner := common.BytesToAddress(value)
	return owner, nil
}

// GetNFTsByOwner returns one page of the NFTs a specific address owns, in
// index key order (contract address, then token id as a decimal string).
// Returns empty slice if no NFTs found.
func (s *PebbleStorage) GetNFTsByOwner(ctx context.Context, owner common.Address, page port.Page) ([]*port.NFTOwnership, string, error) {
	if s.closed.Load() {
		return nil, "", port.ErrClosed
	}

	// Key format: /index/erc721/owner/{ownerAddress}/{contractAddress}/{tokenId}
	prefix := ERC721OwnerIndexKeyPrefix(owner)
	parse := func(key []byte) (common.Address, *big.Int, bool) {
		parts := splitNFTKey(string(key[len(prefix):]))
		if len(parts) < 2 {
			return common.Address{}, nil, false
		}
		tokenId, ok := new(big.Int).SetString(parts[1], 10)
		if !ok {
			return common.Address{}, nil, false
		}
		return common.HexToAddress(parts[0]), tokenId, true
	}
	// The cursor is the key of the last NFT returned, so it stays valid when
	// that NFT leaves the owner: the next page seeks past the deleted key.
	entries, next, err := s.scanPage(ctx, prefix, prefixUpperBound(prefix), false, page, addressIndexPageLimit(page), func(key, _ []byte) bool {
		if _, _, ok := parse(key); !ok {
			s.logger.Warn("Invalid NFT owner index key", zap.String("key", string(key)))
			return false
		}
		return true
	})
	if err != nil {
		return nil, "", err
	}

	nfts := make([]*port.NFTOwnership, len(entries))
	for i, e := range entries {
		contractAddress, tokenId, _ := parse(e.Key)
		nfts[i] = &port.NFTOwnership{
			ContractAddress: contractAddress,
			TokenId:         tokenId,
			Owner:           owner,
		}
	}
	return nfts, next, nil
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
func (s *PebbleStorage) SaveERC721Transfer(ctx context.Context, transfer *port.ERC721Transfer) error {
	if s.closed.Load() {
		return port.ErrClosed
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

	batch := s.newBatch(ctx)
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
	if err := s.commitBatch(ctx, batch, pebble.Sync); err != nil {
		return fmt.Errorf("failed to commit ERC721 transfer batch: %w", err)
	}

	return nil
}

// ========== Internal Transaction Implementation ==========

// GetInternalTransactions retrieves all internal transactions for a given transaction hash.
// Returns empty slice if no internal transactions found or tracing is disabled.
func (s *PebbleStorage) GetInternalTransactions(ctx context.Context, txHash common.Hash) ([]*port.InternalTransaction, error) {
	if s.closed.Load() {
		return nil, port.ErrClosed
	}

	prefix := InternalTransactionKeyPrefix(txHash)

	iter, err := s.kv(ctx).NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	internals := make([]*port.InternalTransaction, 0, 16)

	for iter.First(); iter.Valid(); iter.Next() {
		value := iter.Value()
		if len(value) == 0 {
			continue
		}

		var internal port.InternalTransaction
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

// GetInternalTransactionsByAddress returns one page of the internal calls
// involving a specific address, in block order and, within a transaction,
// in call order.
// If isFrom is true, returns calls where address is the caller.
// If isFrom is false, returns calls where address is the callee.
//
// One index key /index/internal/{from|to}/{address}/{blockNumber}/{txHash}
// covers every call of its transaction that involves the address, so
// Limit and Offset count calls, not keys, and a page may end between two
// calls of one transaction. The cursor therefore addresses a call: the key
// of its transaction followed by its call index (internalTxCursor). The
// next page seeks to that key and skips the calls up to that index, so a
// cursor page costs the same at any depth.
func (s *PebbleStorage) GetInternalTransactionsByAddress(ctx context.Context, address common.Address, isFrom bool, page port.Page) ([]*port.InternalTransaction, string, error) {
	if s.closed.Load() {
		return nil, "", port.ErrClosed
	}

	limit := addressIndexPageLimit(page)

	var prefix []byte
	if isFrom {
		prefix = InternalTxFromIndexKeyPrefix(address)
	} else {
		prefix = InternalTxToIndexKeyPrefix(address)
	}
	upper := prefixUpperBound(prefix)

	var afterKey []byte
	afterCall := -1
	if page.After != "" {
		var err error
		if afterKey, afterCall, err = decodeInternalTxCursor(page.After, prefix, upper); err != nil {
			return nil, "", err
		}
	}

	iter, err := s.kv(ctx).NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: upper,
	})
	if err != nil {
		return nil, "", fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	// call is one item of the list with the index key it was found under.
	type call struct {
		key      []byte
		internal *port.InternalTransaction
	}
	// Read one call more than the page holds to know whether more follow.
	calls := make([]call, 0, limit+1)
	skip := page.Offset
	valid := iter.First()
	if afterKey != nil {
		skip = 0
		valid = iter.SeekGE(afterKey)
	}
	seenTxs := make(map[common.Hash]bool)

	for ; valid && len(calls) <= limit; valid = iter.Next() {
		s.pageSteps.Add(1)
		if err := ctx.Err(); err != nil {
			return nil, "", err
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

		// The cursor's transaction continues after the cursor's call.
		resume := afterKey != nil && bytes.Equal(key, afterKey)
		keyCopy := append([]byte(nil), key...)

		// Filter by address
		for _, internal := range txInternals {
			if (isFrom && internal.From != address) || (!isFrom && internal.To != address) {
				continue
			}
			if resume && internal.Index <= afterCall {
				continue
			}
			// Skip offset items
			if skip > 0 {
				skip--
				continue
			}
			calls = append(calls, call{key: keyCopy, internal: internal})
			if len(calls) > limit {
				break
			}
		}
	}

	if err := iter.Error(); err != nil {
		return nil, "", fmt.Errorf("iterator error: %w", err)
	}

	var next string
	if len(calls) > limit {
		calls = calls[:limit]
		last := calls[limit-1]
		next = encodeInternalTxCursor(last.key, last.internal.Index)
	}
	internals := make([]*port.InternalTransaction, len(calls))
	for i, c := range calls {
		internals[i] = c.internal
	}
	return internals, next, nil
}

// encodeInternalTxCursor returns the cursor that continues after call index
// callIndex of the transaction under index key key: the key followed by the
// call index as four big-endian bytes.
func encodeInternalTxCursor(key []byte, callIndex int) string {
	raw := make([]byte, len(key)+4)
	copy(raw, key)
	binary.BigEndian.PutUint32(raw[len(key):], uint32(callIndex))
	return encodeCursor(raw)
}

// decodeInternalTxCursor returns the index key and call index of an
// internal transaction cursor. A cursor that is malformed or whose key is
// outside [lower, upper) is port.ErrInvalidCursor.
func decodeInternalTxCursor(cursor string, lower, upper []byte) ([]byte, int, error) {
	raw, err := decodeCursor(cursor, lower, upper)
	if err != nil {
		return nil, 0, err
	}
	if len(raw) < 4 {
		return nil, 0, port.ErrInvalidCursor
	}
	key := raw[:len(raw)-4]
	if bytes.Compare(key, lower) < 0 {
		return nil, 0, port.ErrInvalidCursor
	}
	return key, int(binary.BigEndian.Uint32(raw[len(key):])), nil
}

// SaveInternalTransactions saves all internal transactions for a given transaction hash.
// The internals slice must be ordered by execution order (index field).
// Returns error if storage operation fails.
func (s *PebbleStorage) SaveInternalTransactions(ctx context.Context, txHash common.Hash, internals []*port.InternalTransaction) error {
	if s.closed.Load() {
		return port.ErrClosed
	}

	if txHash == (common.Hash{}) {
		return fmt.Errorf("transaction hash cannot be zero")
	}

	if len(internals) == 0 {
		// No internal transactions to save
		return nil
	}

	batch := s.newBatch(ctx)
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
	if err := s.commitBatch(ctx, batch, pebble.Sync); err != nil {
		return fmt.Errorf("failed to commit internal transactions batch: %w", err)
	}

	return nil
}
