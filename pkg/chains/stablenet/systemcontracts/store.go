package systemcontracts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/storage"
)

// Store reads and writes system contract data over the indexer's key-value
// storage (bound to the block transaction through ctx while a block is
// indexed).
type Store struct {
	db     storage.KV
	logger *zap.Logger
}

// NewStore returns a system contract store over db.
func NewStore(db storage.KV, logger *zap.Logger) *Store {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Store{db: db, logger: logger}
}

// Open returns a system contract store over s, which must provide
// key-value access (the Pebble storage does).
func Open(s any, logger *zap.Logger) (*Store, error) {
	db, ok := s.(storage.KV)
	if !ok {
		return nil, fmt.Errorf("storage %T does not support system contract data", s)
	}
	return NewStore(db, logger), nil
}

// Ensure Store implements SystemContractReader
var _ SystemContractReader = (*Store)(nil)

// Ensure Store implements SystemContractWriter
var _ SystemContractWriter = (*Store)(nil)

// ============================================================================
// System Contract Writer Methods
// ============================================================================

// StoreMintEvent stores a mint event
func (s *Store) StoreMintEvent(ctx context.Context, event *MintEvent) error {
	key := MintEventKey(event.BlockNumber, uint64(event.TxIndex), uint64(event.LogIndex))
	data, err := EncodeMintEvent(event)
	if err != nil {
		return fmt.Errorf("failed to encode mint event: %w", err)
	}

	if err := s.db.Put(ctx, key, data); err != nil {
		return fmt.Errorf("failed to store mint event: %w", err)
	}
	// Index by minter for minter-filtered queries; the value is the event key.
	idx := MintMinterIndexKey(event.Minter, event.BlockNumber, uint64(event.TxIndex), uint64(event.LogIndex))
	if err := s.db.Put(ctx, idx, key); err != nil {
		return fmt.Errorf("failed to index mint event: %w", err)
	}

	return nil
}

// StoreBurnEvent stores a burn event
func (s *Store) StoreBurnEvent(ctx context.Context, event *BurnEvent) error {
	key := BurnEventKey(event.BlockNumber, uint64(event.TxIndex), uint64(event.LogIndex))
	data, err := EncodeBurnEvent(event)
	if err != nil {
		return fmt.Errorf("failed to encode burn event: %w", err)
	}

	if err := s.db.Put(ctx, key, data); err != nil {
		return fmt.Errorf("failed to store burn event: %w", err)
	}
	// Index by burner for burner-filtered queries; the value is the event key.
	idx := BurnBurnerIndexKey(event.Burner, event.BlockNumber, uint64(event.TxIndex), uint64(event.LogIndex))
	if err := s.db.Put(ctx, idx, key); err != nil {
		return fmt.Errorf("failed to index burn event: %w", err)
	}

	return nil
}

// StoreMinterConfigEvent stores a minter configuration event
func (s *Store) StoreMinterConfigEvent(ctx context.Context, event *MinterConfigEvent) error {
	key := MinterConfigEventKey(event.Minter, event.BlockNumber, uint64(event.LogIndex))
	data, err := EncodeMinterConfigEvent(event)
	if err != nil {
		return fmt.Errorf("failed to encode minter config event: %w", err)
	}

	if err := s.db.Put(ctx, key, data); err != nil {
		return fmt.Errorf("failed to store minter config event: %w", err)
	}

	return nil
}

// StoreProposal stores a governance proposal
func (s *Store) StoreProposal(ctx context.Context, proposal *Proposal) error {
	key := ProposalKey(proposal.Contract, proposal.ProposalID.String())
	data, err := EncodeProposal(proposal)
	if err != nil {
		return fmt.Errorf("failed to encode proposal: %w", err)
	}

	if err := s.db.Put(ctx, key, data); err != nil {
		return fmt.Errorf("failed to store proposal: %w", err)
	}

	// Store in status index
	statusKey := ProposalStatusIndexKey(proposal.Contract, uint8(proposal.Status), proposal.ProposalID.String())
	if err := s.db.Put(ctx, statusKey, []byte{1}); err != nil {
		return fmt.Errorf("failed to store proposal status index: %w", err)
	}

	return nil
}

// UpdateProposalStatus updates the status of a proposal
func (s *Store) UpdateProposalStatus(ctx context.Context, contract common.Address, proposalID *big.Int, status ProposalStatus, executedAt uint64) error {
	// Get existing proposal
	key := ProposalKey(contract, proposalID.String())
	data, err := s.db.Get(ctx, key)
	if errors.Is(err, storage.ErrNotFound) {
		// Created before the index started, or its creation was not indexed.
		return fmt.Errorf("proposal %s of %s: %w", proposalID, contract.Hex(), storage.ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("failed to get proposal: %w", err)
	}

	proposal, err := DecodeProposal(data)
	if err != nil {
		return fmt.Errorf("failed to decode proposal: %w", err)
	}

	// Remove old status index
	oldStatusKey := ProposalStatusIndexKey(contract, uint8(proposal.Status), proposalID.String())
	if err := s.db.Delete(ctx, oldStatusKey); err != nil {
		return fmt.Errorf("failed to delete old status index: %w", err)
	}

	// Update proposal
	proposal.Status = status
	if executedAt > 0 {
		proposal.ExecutedAt = &executedAt
	}

	// Store updated proposal
	updatedData, err := EncodeProposal(proposal)
	if err != nil {
		return fmt.Errorf("failed to encode updated proposal: %w", err)
	}

	if err := s.db.Put(ctx, key, updatedData); err != nil {
		return fmt.Errorf("failed to store updated proposal: %w", err)
	}

	// Add new status index
	newStatusKey := ProposalStatusIndexKey(contract, uint8(status), proposalID.String())
	if err := s.db.Put(ctx, newStatusKey, []byte{1}); err != nil {
		return fmt.Errorf("failed to store new status index: %w", err)
	}

	return nil
}

// StoreProposalVote stores a vote on a proposal
func (s *Store) StoreProposalVote(ctx context.Context, vote *ProposalVote) error {
	key := ProposalVoteKey(vote.Contract, vote.ProposalID.String(), vote.Voter)
	data, err := EncodeProposalVote(vote)
	if err != nil {
		return fmt.Errorf("failed to encode vote: %w", err)
	}

	if err := s.db.Put(ctx, key, data); err != nil {
		return fmt.Errorf("failed to store vote: %w", err)
	}

	return nil
}

// StoreGasTipUpdateEvent stores a gas tip update event
func (s *Store) StoreGasTipUpdateEvent(ctx context.Context, event *GasTipUpdateEvent) error {
	key := GasTipUpdateEventKey(event.BlockNumber, uint64(event.LogIndex))
	data, err := EncodeGasTipUpdateEvent(event)
	if err != nil {
		return fmt.Errorf("failed to encode gas tip update event: %w", err)
	}

	if err := s.db.Put(ctx, key, data); err != nil {
		return fmt.Errorf("failed to store gas tip update event: %w", err)
	}

	return nil
}

// StoreBlacklistEvent stores a blacklist event
func (s *Store) StoreBlacklistEvent(ctx context.Context, event *BlacklistEvent) error {
	key := BlacklistEventKey(event.Account, event.BlockNumber, uint64(event.LogIndex))
	data, err := EncodeBlacklistEvent(event)
	if err != nil {
		return fmt.Errorf("failed to encode blacklist event: %w", err)
	}

	if err := s.db.Put(ctx, key, data); err != nil {
		return fmt.Errorf("failed to store blacklist event: %w", err)
	}

	return nil
}

// StoreValidatorChangeEvent stores a validator change event
func (s *Store) StoreValidatorChangeEvent(ctx context.Context, event *ValidatorChangeEvent) error {
	key := ValidatorChangeEventKey(event.Validator, event.BlockNumber, uint64(event.LogIndex))
	data, err := EncodeValidatorChangeEvent(event)
	if err != nil {
		return fmt.Errorf("failed to encode validator change event: %w", err)
	}

	if err := s.db.Put(ctx, key, data); err != nil {
		return fmt.Errorf("failed to store validator change event: %w", err)
	}

	return nil
}

// StoreMemberChangeEvent stores a member change event
func (s *Store) StoreMemberChangeEvent(ctx context.Context, event *MemberChangeEvent) error {
	key := MemberChangeEventKey(event.Contract, event.BlockNumber, uint64(event.LogIndex))
	data, err := EncodeMemberChangeEvent(event)
	if err != nil {
		return fmt.Errorf("failed to encode member change event: %w", err)
	}

	if err := s.db.Put(ctx, key, data); err != nil {
		return fmt.Errorf("failed to store member change event: %w", err)
	}

	return nil
}

// StoreEmergencyPauseEvent stores an emergency pause event
func (s *Store) StoreEmergencyPauseEvent(ctx context.Context, event *EmergencyPauseEvent) error {
	key := EmergencyPauseEventKey(event.Contract, event.BlockNumber, uint64(event.LogIndex))
	data, err := EncodeEmergencyPauseEvent(event)
	if err != nil {
		return fmt.Errorf("failed to encode emergency pause event: %w", err)
	}

	if err := s.db.Put(ctx, key, data); err != nil {
		return fmt.Errorf("failed to store emergency pause event: %w", err)
	}

	return nil
}

// StoreDepositMintProposal stores a deposit mint proposal
func (s *Store) StoreDepositMintProposal(ctx context.Context, proposal *DepositMintProposal) error {
	key := DepositMintProposalKey(proposal.ProposalID.String())
	data, err := EncodeDepositMintProposal(proposal)
	if err != nil {
		return fmt.Errorf("failed to encode deposit mint proposal: %w", err)
	}

	if err := s.db.Put(ctx, key, data); err != nil {
		return fmt.Errorf("failed to store deposit mint proposal: %w", err)
	}

	return nil
}

// UpdateTotalSupply updates the total supply
func (s *Store) UpdateTotalSupply(ctx context.Context, delta *big.Int) error {
	// Get current total supply
	key := TotalSupplyKey()
	data, err := s.db.Get(ctx, key)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			// Initialize to 0
			data = storage.EncodeBigInt(big.NewInt(0))
		} else {
			return fmt.Errorf("failed to get total supply: %w", err)
		}
	}

	currentSupply := storage.DecodeBigInt(data)
	newSupply := new(big.Int).Add(currentSupply, delta)

	// Store new total supply
	newData := storage.EncodeBigInt(newSupply)
	if err := s.db.Put(ctx, key, newData); err != nil {
		return fmt.Errorf("failed to update total supply: %w", err)
	}

	return nil
}

// UpdateActiveMinter updates the active minter status
func (s *Store) UpdateActiveMinter(ctx context.Context, minter common.Address, allowance *big.Int, active bool) error {
	key := MinterActiveIndexKey(minter)

	if active {
		// Store minter allowance
		data := storage.EncodeBigInt(allowance)
		if err := s.db.Put(ctx, key, data); err != nil {
			return fmt.Errorf("failed to set active minter: %w", err)
		}
	} else {
		// Remove minter
		if err := s.db.Delete(ctx, key); err != nil {
			return fmt.Errorf("failed to remove active minter: %w", err)
		}
	}

	return nil
}

// UpdateActiveValidator updates the active validator status
func (s *Store) UpdateActiveValidator(ctx context.Context, validator common.Address, active bool) error {
	key := ValidatorActiveIndexKey(validator)

	if active {
		// Mark validator as active
		if err := s.db.Put(ctx, key, []byte{1}); err != nil {
			return fmt.Errorf("failed to set active validator: %w", err)
		}
	} else {
		// Remove validator
		if err := s.db.Delete(ctx, key); err != nil {
			return fmt.Errorf("failed to remove active validator: %w", err)
		}
	}

	return nil
}

// UpdateBlacklistStatus updates the blacklist status of an address
func (s *Store) UpdateBlacklistStatus(ctx context.Context, address common.Address, blacklisted bool) error {
	key := BlacklistActiveIndexKey(address)

	if blacklisted {
		// Mark address as blacklisted
		if err := s.db.Put(ctx, key, []byte{1}); err != nil {
			return fmt.Errorf("failed to set blacklist status: %w", err)
		}
	} else {
		// Remove from blacklist
		if err := s.db.Delete(ctx, key); err != nil {
			return fmt.Errorf("failed to remove blacklist status: %w", err)
		}
	}

	return nil
}

// ============================================================================
// System Contract Reader Methods
// ============================================================================

// GetTotalSupply returns the current total supply
func (s *Store) GetTotalSupply(ctx context.Context) (*big.Int, error) {
	key := TotalSupplyKey()
	data, err := s.db.Get(ctx, key)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return big.NewInt(0), nil
		}
		return nil, fmt.Errorf("failed to get total supply: %w", err)
	}

	return storage.DecodeBigInt(data), nil
}

// GetMintEvents returns mint events within a block range
func (s *Store) GetMintEvents(ctx context.Context, fromBlock, toBlock uint64, minter common.Address, limit, offset int) ([]*MintEvent, error) {
	// Use minter-specific index if minter is specified, otherwise scan all mint events
	var keyPrefix []byte
	var lowerBound, upperBound []byte

	if minter != (common.Address{}) {
		// Use minter index for efficient filtering
		lowerBound = MintMinterIndexBound(minter, fromBlock)
		upperBound = MintMinterIndexBound(minter, toBlock+1)
	} else {
		// Scan all mint events in block range
		keyPrefix = MintEventKeyPrefix()
		lowerBound = []byte(fmt.Sprintf("%s%020d/", string(keyPrefix), fromBlock))
		upperBound = []byte(fmt.Sprintf("%s%020d/", string(keyPrefix), toBlock+1))
	}

	iter, err := s.db.NewCursor(ctx, lowerBound, upperBound)
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var events []*MintEvent
	count := 0
	skipped := 0

	for iter.First(); iter.Valid(); iter.Next() {
		// Skip offset items
		if skipped < offset {
			skipped++
			continue
		}

		// Check limit
		if limit > 0 && count >= limit {
			break
		}

		// If using index, get actual event data
		var eventData []byte
		if minter != (common.Address{}) {
			// Index value contains the actual event key
			eventKey := iter.Value()
			data, err := s.db.Get(ctx, eventKey)
			if err != nil {
				if errors.Is(err, storage.ErrNotFound) {
					continue
				}
				return nil, fmt.Errorf("failed to get mint event: %w", err)
			}
			eventData = data
		} else {
			eventData = iter.Value()
		}

		// Decode event
		event, err := DecodeMintEvent(eventData)
		if err != nil {
			return nil, fmt.Errorf("failed to decode mint event: %w", err)
		}

		events = append(events, event)
		count++
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return events, nil
}

// GetBurnEvents returns burn events within a block range
func (s *Store) GetBurnEvents(ctx context.Context, fromBlock, toBlock uint64, burner common.Address, limit, offset int) ([]*BurnEvent, error) {
	// Use burner-specific index if burner is specified, otherwise scan all burn events
	var lowerBound, upperBound []byte

	if burner != (common.Address{}) {
		// Use burner index for efficient filtering
		lowerBound = BurnBurnerIndexBound(burner, fromBlock)
		upperBound = BurnBurnerIndexBound(burner, toBlock+1)
	} else {
		// Scan all burn events in block range
		keyPrefix := BurnEventKeyPrefix()
		lowerBound = []byte(fmt.Sprintf("%s%020d/", string(keyPrefix), fromBlock))
		upperBound = []byte(fmt.Sprintf("%s%020d/", string(keyPrefix), toBlock+1))
	}

	iter, err := s.db.NewCursor(ctx, lowerBound, upperBound)
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var events []*BurnEvent
	count := 0
	skipped := 0

	for iter.First(); iter.Valid(); iter.Next() {
		// Skip offset items
		if skipped < offset {
			skipped++
			continue
		}

		// Check limit
		if limit > 0 && count >= limit {
			break
		}

		// If using index, get actual event data
		var eventData []byte
		if burner != (common.Address{}) {
			// Index value contains the actual event key
			eventKey := iter.Value()
			data, err := s.db.Get(ctx, eventKey)
			if err != nil {
				if errors.Is(err, storage.ErrNotFound) {
					continue
				}
				return nil, fmt.Errorf("failed to get burn event: %w", err)
			}
			eventData = data
		} else {
			eventData = iter.Value()
		}

		// Decode event
		event, err := DecodeBurnEvent(eventData)
		if err != nil {
			return nil, fmt.Errorf("failed to decode burn event: %w", err)
		}

		events = append(events, event)
		count++
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return events, nil
}

// GetActiveMinters returns list of active minters
func (s *Store) GetActiveMinters(ctx context.Context) ([]common.Address, error) {
	keyPrefix := MinterActiveIndexKeyPrefix()
	iter, err := s.db.NewCursor(ctx, keyPrefix, append(keyPrefix, 0xff))
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var minters []common.Address
	for iter.First(); iter.Valid(); iter.Next() {
		// Extract address from key
		key := string(iter.Key())
		addrHex := key[len(string(keyPrefix)):]
		addr := common.HexToAddress(addrHex)
		minters = append(minters, addr)
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return minters, nil
}

// GetMinterAllowance returns the allowance for a specific minter
func (s *Store) GetMinterAllowance(ctx context.Context, minter common.Address) (*big.Int, error) {
	key := MinterActiveIndexKey(minter)
	data, err := s.db.Get(ctx, key)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return big.NewInt(0), nil
		}
		return nil, fmt.Errorf("failed to get minter allowance: %w", err)
	}

	return storage.DecodeBigInt(data), nil
}

// GetMinterHistory returns configuration history for a minter
func (s *Store) GetMinterHistory(ctx context.Context, minter common.Address) ([]*MinterConfigEvent, error) {
	keyPrefix := MinterConfigEventKeyPrefix(minter)
	iter, err := s.db.NewCursor(ctx, keyPrefix, append(keyPrefix, 0xff))
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var events []*MinterConfigEvent
	for iter.First(); iter.Valid(); iter.Next() {
		event, err := DecodeMinterConfigEvent(iter.Value())
		if err != nil {
			return nil, fmt.Errorf("failed to decode minter config event: %w", err)
		}
		events = append(events, event)
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return events, nil
}

// GetActiveValidators returns list of active validators
func (s *Store) GetActiveValidators(ctx context.Context) ([]common.Address, error) {
	keyPrefix := ValidatorActiveIndexKeyPrefix()
	iter, err := s.db.NewCursor(ctx, keyPrefix, append(keyPrefix, 0xff))
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var validators []common.Address
	for iter.First(); iter.Valid(); iter.Next() {
		// Extract address from key
		key := string(iter.Key())
		addrHex := key[len(string(keyPrefix)):]
		addr := common.HexToAddress(addrHex)
		validators = append(validators, addr)
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return validators, nil
}

// GetGasTipHistory returns gas tip update history
func (s *Store) GetGasTipHistory(ctx context.Context, fromBlock, toBlock uint64) ([]*GasTipUpdateEvent, error) {
	keyPrefix := GasTipUpdateEventKeyPrefix()
	lowerBound := []byte(fmt.Sprintf("%s%020d/", string(keyPrefix), fromBlock))
	upperBound := []byte(fmt.Sprintf("%s%020d/", string(keyPrefix), toBlock+1))

	iter, err := s.db.NewCursor(ctx, lowerBound, upperBound)
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var events []*GasTipUpdateEvent
	for iter.First(); iter.Valid(); iter.Next() {
		event, err := DecodeGasTipUpdateEvent(iter.Value())
		if err != nil {
			return nil, fmt.Errorf("failed to decode gas tip event: %w", err)
		}
		events = append(events, event)
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return events, nil
}

// GetValidatorHistory returns validator change history
func (s *Store) GetValidatorHistory(ctx context.Context, validator common.Address) ([]*ValidatorChangeEvent, error) {
	keyPrefix := ValidatorChangeEventKeyPrefix(validator)
	iter, err := s.db.NewCursor(ctx, keyPrefix, append(keyPrefix, 0xff))
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var events []*ValidatorChangeEvent
	for iter.First(); iter.Valid(); iter.Next() {
		event, err := DecodeValidatorChangeEvent(iter.Value())
		if err != nil {
			return nil, fmt.Errorf("failed to decode validator change event: %w", err)
		}
		events = append(events, event)
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return events, nil
}

// GetMinterConfigHistory returns minter configuration history
func (s *Store) GetMinterConfigHistory(ctx context.Context, fromBlock, toBlock uint64) ([]*MinterConfigEvent, error) {
	// Scan all minters' config events in the block range
	// This requires iterating through all minter config events since keys are organized by minter
	keyPrefix := []byte(prefixSysMinterConfig)
	iter, err := s.db.NewCursor(ctx, keyPrefix, append(keyPrefix, 0xff))
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var events []*MinterConfigEvent
	for iter.First(); iter.Valid(); iter.Next() {
		event, err := DecodeMinterConfigEvent(iter.Value())
		if err != nil {
			return nil, fmt.Errorf("failed to decode minter config event: %w", err)
		}

		// Filter by block range
		if event.BlockNumber >= fromBlock && event.BlockNumber <= toBlock {
			events = append(events, event)
		}
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return events, nil
}

// GetEmergencyPauseHistory returns emergency pause event history
func (s *Store) GetEmergencyPauseHistory(ctx context.Context, contract common.Address) ([]*EmergencyPauseEvent, error) {
	keyPrefix := EmergencyPauseEventKeyPrefix(contract)
	iter, err := s.db.NewCursor(ctx, keyPrefix, append(keyPrefix, 0xff))
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var events []*EmergencyPauseEvent
	for iter.First(); iter.Valid(); iter.Next() {
		event, err := DecodeEmergencyPauseEvent(iter.Value())
		if err != nil {
			return nil, fmt.Errorf("failed to decode emergency pause event: %w", err)
		}
		events = append(events, event)
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return events, nil
}

// GetDepositMintProposals returns deposit mint proposals
func (s *Store) GetDepositMintProposals(ctx context.Context, fromBlock, toBlock uint64, status ProposalStatus) ([]*DepositMintProposal, error) {
	// Scan all deposit mint proposals
	keyPrefix := []byte(prefixSysDepositMint)
	iter, err := s.db.NewCursor(ctx, keyPrefix, append(keyPrefix, 0xff))
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var proposals []*DepositMintProposal
	for iter.First(); iter.Valid(); iter.Next() {
		proposal, err := DecodeDepositMintProposal(iter.Value())
		if err != nil {
			return nil, fmt.Errorf("failed to decode deposit mint proposal: %w", err)
		}

		// Filter by block range and status
		if proposal.BlockNumber >= fromBlock && proposal.BlockNumber <= toBlock {
			if status == ProposalStatusAll || proposal.Status == status {
				proposals = append(proposals, proposal)
			}
		}
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return proposals, nil
}

// GetBurnHistory returns burn event history
func (s *Store) GetBurnHistory(ctx context.Context, fromBlock, toBlock uint64, user common.Address) ([]*BurnEvent, error) {
	// Use GetBurnEvents which already implements this functionality
	return s.GetBurnEvents(ctx, fromBlock, toBlock, user, 0, 0)
}

// GetBlacklistedAddresses returns list of blacklisted addresses
func (s *Store) GetBlacklistedAddresses(ctx context.Context) ([]common.Address, error) {
	keyPrefix := BlacklistActiveIndexKeyPrefix()
	iter, err := s.db.NewCursor(ctx, keyPrefix, append(keyPrefix, 0xff))
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var addresses []common.Address
	for iter.First(); iter.Valid(); iter.Next() {
		// Extract address from key
		key := string(iter.Key())
		addrHex := key[len(string(keyPrefix)):]
		addr := common.HexToAddress(addrHex)
		addresses = append(addresses, addr)
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return addresses, nil
}

// GetBlacklistHistory returns blacklist event history for an address
func (s *Store) GetBlacklistHistory(ctx context.Context, address common.Address) ([]*BlacklistEvent, error) {
	keyPrefix := BlacklistEventKeyPrefix(address)
	iter, err := s.db.NewCursor(ctx, keyPrefix, append(keyPrefix, 0xff))
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var events []*BlacklistEvent
	for iter.First(); iter.Valid(); iter.Next() {
		event, err := DecodeBlacklistEvent(iter.Value())
		if err != nil {
			return nil, fmt.Errorf("failed to decode blacklist event: %w", err)
		}
		events = append(events, event)
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return events, nil
}

// StoreAuthorizedAccountEvent stores an authorized account added/removed event
func (s *Store) StoreAuthorizedAccountEvent(ctx context.Context, event *AuthorizedAccountEvent) error {
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal authorized account event: %w", err)
	}

	key := AuthorizedAccountEventKey(event.Contract, event.BlockNumber, uint64(event.LogIndex))
	if err := s.db.Put(ctx, key, data); err != nil {
		return fmt.Errorf("failed to store authorized account event: %w", err)
	}

	return nil
}

// GetAuthorizedAccounts returns list of authorized accounts by replaying add/remove events
func (s *Store) GetAuthorizedAccounts(ctx context.Context) ([]common.Address, error) {
	// Scan all authorized account events for GovCouncil contract and replay to derive current state
	keyPrefix := AuthorizedAccountEventKeyPrefix(GovCouncilAddress)
	iter, err := s.db.NewCursor(ctx, keyPrefix, append(keyPrefix, 0xff))
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	// Replay events in order to derive current authorized accounts set
	accountSet := make(map[common.Address]bool)
	for iter.First(); iter.Valid(); iter.Next() {
		event := &AuthorizedAccountEvent{}
		if err := json.Unmarshal(iter.Value(), event); err != nil {
			return nil, fmt.Errorf("failed to decode authorized account event: %w", err)
		}
		if event.Action == "added" {
			accountSet[event.Account] = true
		} else if event.Action == "removed" {
			delete(accountSet, event.Account)
		}
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	accounts := make([]common.Address, 0, len(accountSet))
	for addr := range accountSet {
		accounts = append(accounts, addr)
	}

	return accounts, nil
}

// GetProposals returns proposals with optional status filter
func (s *Store) GetProposals(ctx context.Context, contract common.Address, status ProposalStatus, limit, offset int) ([]*Proposal, error) {
	keyPrefix := ProposalStatusIndexKeyPrefix(contract, uint8(status))
	iter, err := s.db.NewCursor(ctx, keyPrefix, append(keyPrefix, 0xff))
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var proposals []*Proposal
	count := 0
	skipped := 0

	for iter.First(); iter.Valid(); iter.Next() {
		// Skip offset items
		if skipped < offset {
			skipped++
			continue
		}

		// Check limit
		if limit > 0 && count >= limit {
			break
		}

		// Extract proposal ID from index key and get proposal
		key := string(iter.Key())
		proposalID := key[len(string(keyPrefix)):]

		proposalKey := ProposalKey(contract, proposalID)
		data, err := s.db.Get(ctx, proposalKey)
		if err != nil {
			continue // Skip if proposal not found
		}

		proposal, err := DecodeProposal(data)
		if err != nil {
			continue // Skip if decode fails
		}

		proposals = append(proposals, proposal)
		count++
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return proposals, nil
}

// GetProposalById returns a specific proposal by ID
func (s *Store) GetProposalById(ctx context.Context, contract common.Address, proposalId *big.Int) (*Proposal, error) {
	key := ProposalKey(contract, proposalId.String())
	data, err := s.db.Get(ctx, key)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get proposal: %w", err)
	}

	proposal, err := DecodeProposal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to decode proposal: %w", err)
	}

	return proposal, nil
}

// GetProposalVotes returns votes for a specific proposal
func (s *Store) GetProposalVotes(ctx context.Context, contract common.Address, proposalId *big.Int) ([]*ProposalVote, error) {
	keyPrefix := ProposalVoteKeyPrefix(contract, proposalId.String())
	iter, err := s.db.NewCursor(ctx, keyPrefix, append(keyPrefix, 0xff))
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var votes []*ProposalVote
	for iter.First(); iter.Valid(); iter.Next() {
		vote, err := DecodeProposalVote(iter.Value())
		if err != nil {
			continue // Skip invalid votes
		}
		votes = append(votes, vote)
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return votes, nil
}

// GetMemberHistory returns member change history for a contract
func (s *Store) GetMemberHistory(ctx context.Context, contract common.Address) ([]*MemberChangeEvent, error) {
	keyPrefix := MemberChangeEventKeyPrefix(contract)
	iter, err := s.db.NewCursor(ctx, keyPrefix, append(keyPrefix, 0xff))
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var events []*MemberChangeEvent
	for iter.First(); iter.Valid(); iter.Next() {
		event, err := DecodeMemberChangeEvent(iter.Value())
		if err != nil {
			return nil, fmt.Errorf("failed to decode member change event: %w", err)
		}
		events = append(events, event)
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return events, nil
}

// StoreMaxProposalsUpdateEvent stores a max proposals per member update event
func (s *Store) StoreMaxProposalsUpdateEvent(ctx context.Context, event *MaxProposalsUpdateEvent) error {
	key := MaxProposalsUpdateEventKey(event.Contract, event.BlockNumber, uint64(event.LogIndex))
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to encode max proposals update event: %w", err)
	}

	if err := s.db.Put(ctx, key, data); err != nil {
		return fmt.Errorf("failed to store max proposals update event: %w", err)
	}

	return nil
}

// StoreProposalExecutionSkippedEvent stores a proposal execution skipped event
func (s *Store) StoreProposalExecutionSkippedEvent(ctx context.Context, event *ProposalExecutionSkippedEvent) error {
	key := ProposalExecutionSkippedEventKey(event.Contract, event.BlockNumber, uint64(event.LogIndex))
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to encode proposal execution skipped event: %w", err)
	}

	if err := s.db.Put(ctx, key, data); err != nil {
		return fmt.Errorf("failed to store proposal execution skipped event: %w", err)
	}

	return nil
}

// GetMaxProposalsUpdateHistory returns max proposals per member update history for a contract
func (s *Store) GetMaxProposalsUpdateHistory(ctx context.Context, contract common.Address) ([]*MaxProposalsUpdateEvent, error) {
	keyPrefix := MaxProposalsUpdateEventKeyPrefix(contract)
	iter, err := s.db.NewCursor(ctx, keyPrefix, append(keyPrefix, 0xff))
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var results []*MaxProposalsUpdateEvent
	for iter.First(); iter.Valid(); iter.Next() {
		event := &MaxProposalsUpdateEvent{}
		if err := json.Unmarshal(iter.Value(), event); err != nil {
			return nil, fmt.Errorf("failed to decode max proposals update event: %w", err)
		}
		results = append(results, event)
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return results, nil
}

// GetProposalExecutionSkippedEvents returns proposal execution skipped events for a contract
func (s *Store) GetProposalExecutionSkippedEvents(ctx context.Context, contract common.Address, proposalID *big.Int) ([]*ProposalExecutionSkippedEvent, error) {
	keyPrefix := ProposalExecutionSkippedEventKeyPrefix(contract)
	iter, err := s.db.NewCursor(ctx, keyPrefix, append(keyPrefix, 0xff))
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var results []*ProposalExecutionSkippedEvent
	for iter.First(); iter.Valid(); iter.Next() {
		event := &ProposalExecutionSkippedEvent{}
		if err := json.Unmarshal(iter.Value(), event); err != nil {
			return nil, fmt.Errorf("failed to decode proposal execution skipped event: %w", err)
		}
		// Filter by proposalID if specified
		if proposalID != nil && event.ProposalID != nil && event.ProposalID.Cmp(proposalID) != 0 {
			continue
		}
		results = append(results, event)
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return results, nil
}
