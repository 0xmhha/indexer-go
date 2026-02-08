package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/cockroachdb/pebble"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// Ensure PebbleStorage implements SystemContractReader
var _ SystemContractReader = (*PebbleStorage)(nil)

// Ensure PebbleStorage implements SystemContractWriter
var _ SystemContractWriter = (*PebbleStorage)(nil)

// ============================================================================
// System Contract Writer Methods
// ============================================================================

// StoreMintEvent stores a mint event
func (s *PebbleStorage) StoreMintEvent(ctx context.Context, event *MintEvent) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	// Find the transaction and log index
	// For now, use a simple counter approach
	// In production, this should be derived from the actual log index
	txIndex := uint64(0)
	logIndex := uint64(0)

	key := MintEventKey(event.BlockNumber, txIndex, logIndex)
	data, err := EncodeMintEvent(event)
	if err != nil {
		return fmt.Errorf("failed to encode mint event: %w", err)
	}

	if err := s.db.Set(key, data, pebble.Sync); err != nil {
		return fmt.Errorf("failed to store mint event: %w", err)
	}

	return nil
}

// StoreBurnEvent stores a burn event
func (s *PebbleStorage) StoreBurnEvent(ctx context.Context, event *BurnEvent) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	txIndex := uint64(0)
	logIndex := uint64(0)

	key := BurnEventKey(event.BlockNumber, txIndex, logIndex)
	data, err := EncodeBurnEvent(event)
	if err != nil {
		return fmt.Errorf("failed to encode burn event: %w", err)
	}

	if err := s.db.Set(key, data, pebble.Sync); err != nil {
		return fmt.Errorf("failed to store burn event: %w", err)
	}

	return nil
}

// StoreMinterConfigEvent stores a minter configuration event
func (s *PebbleStorage) StoreMinterConfigEvent(ctx context.Context, event *MinterConfigEvent) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	key := MinterConfigEventKey(event.Minter, event.BlockNumber)
	data, err := EncodeMinterConfigEvent(event)
	if err != nil {
		return fmt.Errorf("failed to encode minter config event: %w", err)
	}

	if err := s.db.Set(key, data, pebble.Sync); err != nil {
		return fmt.Errorf("failed to store minter config event: %w", err)
	}

	return nil
}

// StoreProposal stores a governance proposal
func (s *PebbleStorage) StoreProposal(ctx context.Context, proposal *Proposal) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	key := ProposalKey(proposal.Contract, proposal.ProposalID.String())
	data, err := EncodeProposal(proposal)
	if err != nil {
		return fmt.Errorf("failed to encode proposal: %w", err)
	}

	if err := s.db.Set(key, data, pebble.Sync); err != nil {
		return fmt.Errorf("failed to store proposal: %w", err)
	}

	// Store in status index
	statusKey := ProposalStatusIndexKey(proposal.Contract, uint8(proposal.Status), proposal.ProposalID.String())
	if err := s.db.Set(statusKey, []byte{1}, pebble.Sync); err != nil {
		return fmt.Errorf("failed to store proposal status index: %w", err)
	}

	return nil
}

// UpdateProposalStatus updates the status of a proposal
func (s *PebbleStorage) UpdateProposalStatus(ctx context.Context, contract common.Address, proposalID *big.Int, status ProposalStatus, executedAt uint64) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	// Get existing proposal
	key := ProposalKey(contract, proposalID.String())
	data, closer, err := s.db.Get(key)
	if err != nil {
		return fmt.Errorf("failed to get proposal: %w", err)
	}
	defer closer.Close()

	proposal, err := DecodeProposal(data)
	if err != nil {
		return fmt.Errorf("failed to decode proposal: %w", err)
	}

	// Remove old status index
	oldStatusKey := ProposalStatusIndexKey(contract, uint8(proposal.Status), proposalID.String())
	if err := s.db.Delete(oldStatusKey, pebble.Sync); err != nil {
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

	if err := s.db.Set(key, updatedData, pebble.Sync); err != nil {
		return fmt.Errorf("failed to store updated proposal: %w", err)
	}

	// Add new status index
	newStatusKey := ProposalStatusIndexKey(contract, uint8(status), proposalID.String())
	if err := s.db.Set(newStatusKey, []byte{1}, pebble.Sync); err != nil {
		return fmt.Errorf("failed to store new status index: %w", err)
	}

	return nil
}

// StoreProposalVote stores a vote on a proposal
func (s *PebbleStorage) StoreProposalVote(ctx context.Context, vote *ProposalVote) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	key := ProposalVoteKey(vote.Contract, vote.ProposalID.String(), vote.Voter)
	data, err := EncodeProposalVote(vote)
	if err != nil {
		return fmt.Errorf("failed to encode vote: %w", err)
	}

	if err := s.db.Set(key, data, pebble.Sync); err != nil {
		return fmt.Errorf("failed to store vote: %w", err)
	}

	return nil
}

// StoreGasTipUpdateEvent stores a gas tip update event
func (s *PebbleStorage) StoreGasTipUpdateEvent(ctx context.Context, event *GasTipUpdateEvent) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	txIndex := uint64(0)

	key := GasTipUpdateEventKey(event.BlockNumber, txIndex)
	data, err := EncodeGasTipUpdateEvent(event)
	if err != nil {
		return fmt.Errorf("failed to encode gas tip update event: %w", err)
	}

	if err := s.db.Set(key, data, pebble.Sync); err != nil {
		return fmt.Errorf("failed to store gas tip update event: %w", err)
	}

	return nil
}

// StoreBlacklistEvent stores a blacklist event
func (s *PebbleStorage) StoreBlacklistEvent(ctx context.Context, event *BlacklistEvent) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	key := BlacklistEventKey(event.Account, event.BlockNumber)
	data, err := EncodeBlacklistEvent(event)
	if err != nil {
		return fmt.Errorf("failed to encode blacklist event: %w", err)
	}

	if err := s.db.Set(key, data, pebble.Sync); err != nil {
		return fmt.Errorf("failed to store blacklist event: %w", err)
	}

	return nil
}

// StoreValidatorChangeEvent stores a validator change event
func (s *PebbleStorage) StoreValidatorChangeEvent(ctx context.Context, event *ValidatorChangeEvent) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	key := ValidatorChangeEventKey(event.Validator, event.BlockNumber)
	data, err := EncodeValidatorChangeEvent(event)
	if err != nil {
		return fmt.Errorf("failed to encode validator change event: %w", err)
	}

	if err := s.db.Set(key, data, pebble.Sync); err != nil {
		return fmt.Errorf("failed to store validator change event: %w", err)
	}

	return nil
}

// StoreMemberChangeEvent stores a member change event
func (s *PebbleStorage) StoreMemberChangeEvent(ctx context.Context, event *MemberChangeEvent) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	txIndex := uint64(0)

	key := MemberChangeEventKey(event.Contract, event.BlockNumber, txIndex)
	data, err := EncodeMemberChangeEvent(event)
	if err != nil {
		return fmt.Errorf("failed to encode member change event: %w", err)
	}

	if err := s.db.Set(key, data, pebble.Sync); err != nil {
		return fmt.Errorf("failed to store member change event: %w", err)
	}

	return nil
}

// StoreEmergencyPauseEvent stores an emergency pause event
func (s *PebbleStorage) StoreEmergencyPauseEvent(ctx context.Context, event *EmergencyPauseEvent) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	txIndex := uint64(0)

	key := EmergencyPauseEventKey(event.Contract, event.BlockNumber, txIndex)
	data, err := EncodeEmergencyPauseEvent(event)
	if err != nil {
		return fmt.Errorf("failed to encode emergency pause event: %w", err)
	}

	if err := s.db.Set(key, data, pebble.Sync); err != nil {
		return fmt.Errorf("failed to store emergency pause event: %w", err)
	}

	return nil
}

// StoreDepositMintProposal stores a deposit mint proposal
func (s *PebbleStorage) StoreDepositMintProposal(ctx context.Context, proposal *DepositMintProposal) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	key := DepositMintProposalKey(proposal.ProposalID.String())
	data, err := EncodeDepositMintProposal(proposal)
	if err != nil {
		return fmt.Errorf("failed to encode deposit mint proposal: %w", err)
	}

	if err := s.db.Set(key, data, pebble.Sync); err != nil {
		return fmt.Errorf("failed to store deposit mint proposal: %w", err)
	}

	return nil
}

// UpdateTotalSupply updates the total supply
func (s *PebbleStorage) UpdateTotalSupply(ctx context.Context, delta *big.Int) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	// Get current total supply
	key := TotalSupplyKey()
	data, closer, err := s.db.Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			// Initialize to 0
			data = EncodeBigInt(big.NewInt(0))
		} else {
			return fmt.Errorf("failed to get total supply: %w", err)
		}
	} else {
		defer closer.Close()
	}

	currentSupply := DecodeBigInt(data)
	newSupply := new(big.Int).Add(currentSupply, delta)

	// Store new total supply
	newData := EncodeBigInt(newSupply)
	if err := s.db.Set(key, newData, pebble.Sync); err != nil {
		return fmt.Errorf("failed to update total supply: %w", err)
	}

	return nil
}

// UpdateActiveMinter updates the active minter status
func (s *PebbleStorage) UpdateActiveMinter(ctx context.Context, minter common.Address, allowance *big.Int, active bool) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	key := MinterActiveIndexKey(minter)

	if active {
		// Store minter allowance
		data := EncodeBigInt(allowance)
		if err := s.db.Set(key, data, pebble.Sync); err != nil {
			return fmt.Errorf("failed to set active minter: %w", err)
		}
	} else {
		// Remove minter
		if err := s.db.Delete(key, pebble.Sync); err != nil {
			return fmt.Errorf("failed to remove active minter: %w", err)
		}
	}

	return nil
}

// UpdateActiveValidator updates the active validator status
func (s *PebbleStorage) UpdateActiveValidator(ctx context.Context, validator common.Address, active bool) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	key := ValidatorActiveIndexKey(validator)

	if active {
		// Mark validator as active
		if err := s.db.Set(key, []byte{1}, pebble.Sync); err != nil {
			return fmt.Errorf("failed to set active validator: %w", err)
		}
	} else {
		// Remove validator
		if err := s.db.Delete(key, pebble.Sync); err != nil {
			return fmt.Errorf("failed to remove active validator: %w", err)
		}
	}

	return nil
}

// UpdateBlacklistStatus updates the blacklist status of an address
func (s *PebbleStorage) UpdateBlacklistStatus(ctx context.Context, address common.Address, blacklisted bool) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	key := BlacklistActiveIndexKey(address)

	if blacklisted {
		// Mark address as blacklisted
		if err := s.db.Set(key, []byte{1}, pebble.Sync); err != nil {
			return fmt.Errorf("failed to set blacklist status: %w", err)
		}
	} else {
		// Remove from blacklist
		if err := s.db.Delete(key, pebble.Sync); err != nil {
			return fmt.Errorf("failed to remove blacklist status: %w", err)
		}
	}

	return nil
}

// IndexSystemContractEvent indexes a single system contract event from a log
// This is a placeholder implementation - actual parsing logic should be handled by events package
func (s *PebbleStorage) IndexSystemContractEvent(ctx context.Context, log *types.Log) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	// This method should be called by the events package's SystemContractEventParser
	// which will parse the log and call the appropriate Store* methods
	return fmt.Errorf("IndexSystemContractEvent should be called from events package")
}

// IndexSystemContractEvents indexes multiple system contract events from logs (batch operation)
func (s *PebbleStorage) IndexSystemContractEvents(ctx context.Context, logs []*types.Log) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	// Batch index all events
	for _, log := range logs {
		if err := s.IndexSystemContractEvent(ctx, log); err != nil {
			return fmt.Errorf("failed to index event at block %d: %w", log.BlockNumber, err)
		}
	}

	return nil
}

// ============================================================================
// System Contract Reader Methods
// ============================================================================

// GetTotalSupply returns the current total supply
func (s *PebbleStorage) GetTotalSupply(ctx context.Context) (*big.Int, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	key := TotalSupplyKey()
	data, closer, err := s.db.Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			return big.NewInt(0), nil
		}
		return nil, fmt.Errorf("failed to get total supply: %w", err)
	}
	defer closer.Close()

	return DecodeBigInt(data), nil
}

// GetMintEvents returns mint events within a block range
func (s *PebbleStorage) GetMintEvents(ctx context.Context, fromBlock, toBlock uint64, minter common.Address, limit, offset int) ([]*MintEvent, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	// Use minter-specific index if minter is specified, otherwise scan all mint events
	var keyPrefix []byte
	var lowerBound, upperBound []byte

	if minter != (common.Address{}) {
		// Use minter index for efficient filtering
		lowerBound = MintMinterIndexKey(minter, fromBlock)
		upperBound = MintMinterIndexKey(minter, toBlock+1)
	} else {
		// Scan all mint events in block range
		keyPrefix = MintEventKeyPrefix()
		lowerBound = []byte(fmt.Sprintf("%s%020d/", string(keyPrefix), fromBlock))
		upperBound = []byte(fmt.Sprintf("%s%020d/", string(keyPrefix), toBlock+1))
	}

	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: lowerBound,
		UpperBound: upperBound,
	})
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
			data, closer, err := s.db.Get(eventKey)
			if err != nil {
				if err == pebble.ErrNotFound {
					continue
				}
				return nil, fmt.Errorf("failed to get mint event: %w", err)
			}
			eventData = data
			closer.Close()
		} else {
			eventData = iter.Value()
		}

		// Decode event
		event := &MintEvent{}
		if err := json.Unmarshal(eventData, event); err != nil {
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
func (s *PebbleStorage) GetBurnEvents(ctx context.Context, fromBlock, toBlock uint64, burner common.Address, limit, offset int) ([]*BurnEvent, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	// Use burner-specific index if burner is specified, otherwise scan all burn events
	var lowerBound, upperBound []byte

	if burner != (common.Address{}) {
		// Use burner index for efficient filtering
		lowerBound = BurnBurnerIndexKey(burner, fromBlock)
		upperBound = BurnBurnerIndexKey(burner, toBlock+1)
	} else {
		// Scan all burn events in block range
		keyPrefix := BurnEventKeyPrefix()
		lowerBound = []byte(fmt.Sprintf("%s%020d/", string(keyPrefix), fromBlock))
		upperBound = []byte(fmt.Sprintf("%s%020d/", string(keyPrefix), toBlock+1))
	}

	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: lowerBound,
		UpperBound: upperBound,
	})
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
			data, closer, err := s.db.Get(eventKey)
			if err != nil {
				if err == pebble.ErrNotFound {
					continue
				}
				return nil, fmt.Errorf("failed to get burn event: %w", err)
			}
			eventData = data
			closer.Close()
		} else {
			eventData = iter.Value()
		}

		// Decode event
		event := &BurnEvent{}
		if err := json.Unmarshal(eventData, event); err != nil {
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
func (s *PebbleStorage) GetActiveMinters(ctx context.Context) ([]common.Address, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	keyPrefix := MinterActiveIndexKeyPrefix()
	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: keyPrefix,
		UpperBound: append(keyPrefix, 0xff),
	})
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
func (s *PebbleStorage) GetMinterAllowance(ctx context.Context, minter common.Address) (*big.Int, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	key := MinterActiveIndexKey(minter)
	data, closer, err := s.db.Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			return big.NewInt(0), nil
		}
		return nil, fmt.Errorf("failed to get minter allowance: %w", err)
	}
	defer closer.Close()

	return DecodeBigInt(data), nil
}

// GetMinterHistory returns configuration history for a minter
func (s *PebbleStorage) GetMinterHistory(ctx context.Context, minter common.Address) ([]*MinterConfigEvent, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	keyPrefix := MinterConfigEventKeyPrefix(minter)
	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: keyPrefix,
		UpperBound: append(keyPrefix, 0xff),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var events []*MinterConfigEvent
	for iter.First(); iter.Valid(); iter.Next() {
		event := &MinterConfigEvent{}
		if err := json.Unmarshal(iter.Value(), event); err != nil {
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
func (s *PebbleStorage) GetActiveValidators(ctx context.Context) ([]common.Address, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	keyPrefix := ValidatorActiveIndexKeyPrefix()
	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: keyPrefix,
		UpperBound: append(keyPrefix, 0xff),
	})
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
func (s *PebbleStorage) GetGasTipHistory(ctx context.Context, fromBlock, toBlock uint64) ([]*GasTipUpdateEvent, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	keyPrefix := GasTipUpdateEventKeyPrefix()
	lowerBound := []byte(fmt.Sprintf("%s%020d/", string(keyPrefix), fromBlock))
	upperBound := []byte(fmt.Sprintf("%s%020d/", string(keyPrefix), toBlock+1))

	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: lowerBound,
		UpperBound: upperBound,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var events []*GasTipUpdateEvent
	for iter.First(); iter.Valid(); iter.Next() {
		event := &GasTipUpdateEvent{}
		if err := json.Unmarshal(iter.Value(), event); err != nil {
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
func (s *PebbleStorage) GetValidatorHistory(ctx context.Context, validator common.Address) ([]*ValidatorChangeEvent, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	keyPrefix := ValidatorChangeEventKeyPrefix(validator)
	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: keyPrefix,
		UpperBound: append(keyPrefix, 0xff),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var events []*ValidatorChangeEvent
	for iter.First(); iter.Valid(); iter.Next() {
		event := &ValidatorChangeEvent{}
		if err := json.Unmarshal(iter.Value(), event); err != nil {
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
func (s *PebbleStorage) GetMinterConfigHistory(ctx context.Context, fromBlock, toBlock uint64) ([]*MinterConfigEvent, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	// Scan all minters' config events in the block range
	// This requires iterating through all minter config events since keys are organized by minter
	keyPrefix := []byte(prefixSysMinterConfig)
	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: keyPrefix,
		UpperBound: append(keyPrefix, 0xff),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var events []*MinterConfigEvent
	for iter.First(); iter.Valid(); iter.Next() {
		event := &MinterConfigEvent{}
		if err := json.Unmarshal(iter.Value(), event); err != nil {
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
func (s *PebbleStorage) GetEmergencyPauseHistory(ctx context.Context, contract common.Address) ([]*EmergencyPauseEvent, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	keyPrefix := EmergencyPauseEventKeyPrefix(contract)
	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: keyPrefix,
		UpperBound: append(keyPrefix, 0xff),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var events []*EmergencyPauseEvent
	for iter.First(); iter.Valid(); iter.Next() {
		event := &EmergencyPauseEvent{}
		if err := json.Unmarshal(iter.Value(), event); err != nil {
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
func (s *PebbleStorage) GetDepositMintProposals(ctx context.Context, fromBlock, toBlock uint64, status ProposalStatus) ([]*DepositMintProposal, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	// Scan all deposit mint proposals
	keyPrefix := []byte(prefixSysDepositMint)
	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: keyPrefix,
		UpperBound: append(keyPrefix, 0xff),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var proposals []*DepositMintProposal
	for iter.First(); iter.Valid(); iter.Next() {
		proposal := &DepositMintProposal{}
		if err := json.Unmarshal(iter.Value(), proposal); err != nil {
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
func (s *PebbleStorage) GetBurnHistory(ctx context.Context, fromBlock, toBlock uint64, user common.Address) ([]*BurnEvent, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	// Use GetBurnEvents which already implements this functionality
	return s.GetBurnEvents(ctx, fromBlock, toBlock, user, 0, 0)
}

// GetBlacklistedAddresses returns list of blacklisted addresses
func (s *PebbleStorage) GetBlacklistedAddresses(ctx context.Context) ([]common.Address, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	keyPrefix := BlacklistActiveIndexKeyPrefix()
	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: keyPrefix,
		UpperBound: append(keyPrefix, 0xff),
	})
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
func (s *PebbleStorage) GetBlacklistHistory(ctx context.Context, address common.Address) ([]*BlacklistEvent, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	keyPrefix := BlacklistEventKeyPrefix(address)
	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: keyPrefix,
		UpperBound: append(keyPrefix, 0xff),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var events []*BlacklistEvent
	for iter.First(); iter.Valid(); iter.Next() {
		event := &BlacklistEvent{}
		if err := json.Unmarshal(iter.Value(), event); err != nil {
			return nil, fmt.Errorf("failed to decode blacklist event: %w", err)
		}
		events = append(events, event)
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return events, nil
}

// GetAuthorizedAccounts returns list of authorized accounts
func (s *PebbleStorage) GetAuthorizedAccounts(ctx context.Context) ([]common.Address, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	// NOTE: Authorized accounts tracking is not yet implemented in the storage layer
	// The event parsers log these events but don't store them yet
	// This would require:
	// 1. Adding AuthorizedAccountEvent type to storage/types.go
	// 2. Adding schema keys for authorized account index
	// 3. Implementing storage methods in parseAuthorizedAccountAdded/RemovedEvent
	// For now, return empty list instead of error for API compatibility
	return []common.Address{}, nil
}

// GetProposals returns proposals with optional status filter
func (s *PebbleStorage) GetProposals(ctx context.Context, contract common.Address, status ProposalStatus, limit, offset int) ([]*Proposal, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	keyPrefix := ProposalStatusIndexKeyPrefix(contract, uint8(status))
	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: keyPrefix,
		UpperBound: append(keyPrefix, 0xff),
	})
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
		data, closer, err := s.db.Get(proposalKey)
		if err != nil {
			continue // Skip if proposal not found
		}

		proposal, err := DecodeProposal(data)
		closer.Close()
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
func (s *PebbleStorage) GetProposalById(ctx context.Context, contract common.Address, proposalId *big.Int) (*Proposal, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	key := ProposalKey(contract, proposalId.String())
	data, closer, err := s.db.Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get proposal: %w", err)
	}
	defer closer.Close()

	proposal, err := DecodeProposal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to decode proposal: %w", err)
	}

	return proposal, nil
}

// GetProposalVotes returns votes for a specific proposal
func (s *PebbleStorage) GetProposalVotes(ctx context.Context, contract common.Address, proposalId *big.Int) ([]*ProposalVote, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	keyPrefix := ProposalVoteKeyPrefix(contract, proposalId.String())
	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: keyPrefix,
		UpperBound: append(keyPrefix, 0xff),
	})
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
func (s *PebbleStorage) GetMemberHistory(ctx context.Context, contract common.Address) ([]*MemberChangeEvent, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	keyPrefix := MemberChangeEventKeyPrefix(contract)
	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: keyPrefix,
		UpperBound: append(keyPrefix, 0xff),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var events []*MemberChangeEvent
	for iter.First(); iter.Valid(); iter.Next() {
		event := &MemberChangeEvent{}
		if err := json.Unmarshal(iter.Value(), event); err != nil {
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
func (s *PebbleStorage) StoreMaxProposalsUpdateEvent(ctx context.Context, event *MaxProposalsUpdateEvent) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	txIndex := uint64(0)
	key := MaxProposalsUpdateEventKey(event.Contract, event.BlockNumber, txIndex)
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to encode max proposals update event: %w", err)
	}

	if err := s.db.Set(key, data, pebble.Sync); err != nil {
		return fmt.Errorf("failed to store max proposals update event: %w", err)
	}

	return nil
}

// StoreProposalExecutionSkippedEvent stores a proposal execution skipped event
func (s *PebbleStorage) StoreProposalExecutionSkippedEvent(ctx context.Context, event *ProposalExecutionSkippedEvent) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	txIndex := uint64(0)
	key := ProposalExecutionSkippedEventKey(event.Contract, event.BlockNumber, txIndex)
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to encode proposal execution skipped event: %w", err)
	}

	if err := s.db.Set(key, data, pebble.Sync); err != nil {
		return fmt.Errorf("failed to store proposal execution skipped event: %w", err)
	}

	return nil
}

// GetMaxProposalsUpdateHistory returns max proposals per member update history for a contract
func (s *PebbleStorage) GetMaxProposalsUpdateHistory(ctx context.Context, contract common.Address) ([]*MaxProposalsUpdateEvent, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	keyPrefix := MaxProposalsUpdateEventKeyPrefix(contract)
	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: keyPrefix,
		UpperBound: append(keyPrefix, 0xff),
	})
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
func (s *PebbleStorage) GetProposalExecutionSkippedEvents(ctx context.Context, contract common.Address, proposalID *big.Int) ([]*ProposalExecutionSkippedEvent, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	keyPrefix := ProposalExecutionSkippedEventKeyPrefix(contract)
	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: keyPrefix,
		UpperBound: append(keyPrefix, 0xff),
	})
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
