package watchlist

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// Well-known event signatures
var (
	// ERC20 Transfer(address indexed from, address indexed to, uint256 value)
	ERC20TransferTopic = common.HexToHash("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef")

	// ERC721 Transfer(address indexed from, address indexed to, uint256 indexed tokenId)
	// Same signature as ERC20 Transfer, differentiated by indexed tokenId
	ERC721TransferTopic = common.HexToHash("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef")
)

// EventMatcher handles matching blockchain events against watched addresses
type EventMatcher struct {
	bloomFilters map[string]*BloomFilter     // Chain-specific bloom filters
	addresses    map[string]map[common.Address]*WatchedAddress // chainID -> address -> WatchedAddress
}

// NewEventMatcher creates a new event matcher
func NewEventMatcher() *EventMatcher {
	return &EventMatcher{
		bloomFilters: make(map[string]*BloomFilter),
		addresses:    make(map[string]map[common.Address]*WatchedAddress),
	}
}

// AddAddress adds an address to the matcher
func (m *EventMatcher) AddAddress(watched *WatchedAddress) {
	// Ensure chain map exists
	if m.addresses[watched.ChainID] == nil {
		m.addresses[watched.ChainID] = make(map[common.Address]*WatchedAddress)
	}

	// Add to address map
	m.addresses[watched.ChainID][watched.Address] = watched

	// Add to bloom filter
	if m.bloomFilters[watched.ChainID] == nil {
		m.bloomFilters[watched.ChainID] = NewBloomFilter(nil)
	}
	m.bloomFilters[watched.ChainID].Add(watched.Address)
}

// RemoveAddress removes an address from the matcher
// Note: Bloom filters don't support removal, so we may have false positives
// For exact checks, we still consult the addresses map
func (m *EventMatcher) RemoveAddress(chainID string, address common.Address) {
	if chainMap := m.addresses[chainID]; chainMap != nil {
		delete(chainMap, address)
	}
}

// SetBloomFilter sets the bloom filter for a chain (from storage)
func (m *EventMatcher) SetBloomFilter(chainID string, bf *BloomFilter) {
	m.bloomFilters[chainID] = bf
}

// GetBloomFilter returns the bloom filter for a chain
func (m *EventMatcher) GetBloomFilter(chainID string) *BloomFilter {
	return m.bloomFilters[chainID]
}

// MatchTransaction checks if a transaction matches any watched addresses
// Returns matched events for the transaction
func (m *EventMatcher) MatchTransaction(
	chainID string,
	tx *types.Transaction,
	receipt *types.Receipt,
	blockNumber uint64,
	blockHash common.Hash,
	blockTime uint64,
) []*WatchEvent {
	var events []*WatchEvent

	// Quick bloom filter check
	bf := m.bloomFilters[chainID]
	chainAddrs := m.addresses[chainID]
	if bf == nil || chainAddrs == nil {
		return events
	}

	// Get transaction sender
	signer := types.LatestSignerForChainID(tx.ChainId())
	from, err := types.Sender(signer, tx)
	if err != nil {
		return events
	}

	to := tx.To()

	// Check if sender is watched (tx_from)
	if bf.MightContain(from) {
		if watched, ok := chainAddrs[from]; ok && watched.Filter.TxFrom {
			if m.matchesValueFilter(tx.Value(), watched.Filter.MinValue) {
				events = append(events, m.createTxEvent(
					watched,
					WatchEventTypeTxFrom,
					tx, receipt, from, to,
					blockNumber, blockHash, blockTime,
				))
			}
		}
	}

	// Check if recipient is watched (tx_to)
	if to != nil && bf.MightContain(*to) {
		if watched, ok := chainAddrs[*to]; ok && watched.Filter.TxTo {
			if m.matchesValueFilter(tx.Value(), watched.Filter.MinValue) {
				events = append(events, m.createTxEvent(
					watched,
					WatchEventTypeTxTo,
					tx, receipt, from, to,
					blockNumber, blockHash, blockTime,
				))
			}
		}
	}

	return events
}

// MatchLogs checks logs for ERC20/ERC721 transfers and contract events
func (m *EventMatcher) MatchLogs(
	chainID string,
	logs []*types.Log,
	blockNumber uint64,
	blockHash common.Hash,
	blockTime uint64,
) []*WatchEvent {
	var events []*WatchEvent

	bf := m.bloomFilters[chainID]
	chainAddrs := m.addresses[chainID]
	if bf == nil || chainAddrs == nil {
		return events
	}

	for _, log := range logs {
		// Check if log emitter is watched (for contract logs)
		if bf.MightContain(log.Address) {
			if watched, ok := chainAddrs[log.Address]; ok && watched.Filter.Logs {
				events = append(events, m.createLogEvent(
					watched,
					WatchEventTypeLog,
					log,
					blockNumber, blockHash, blockTime,
				))
			}
		}

		// Check for ERC20/ERC721 Transfer events
		if len(log.Topics) >= 3 && log.Topics[0] == ERC20TransferTopic {
			from := common.BytesToAddress(log.Topics[1].Bytes())
			to := common.BytesToAddress(log.Topics[2].Bytes())

			// Determine if ERC20 or ERC721 based on topic count and data length
			isERC721 := len(log.Topics) == 4 // ERC721 has indexed tokenId

			if isERC721 {
				// ERC721 Transfer
				// Check if from address is watched
				if bf.MightContain(from) {
					if watched, ok := chainAddrs[from]; ok && watched.Filter.ERC721 {
						events = append(events, m.createERC721Event(
							watched,
							log,
							from, to,
							blockNumber, blockHash, blockTime,
						))
					}
				}

				// Check if to address is watched
				if bf.MightContain(to) {
					if watched, ok := chainAddrs[to]; ok && watched.Filter.ERC721 {
						events = append(events, m.createERC721Event(
							watched,
							log,
							from, to,
							blockNumber, blockHash, blockTime,
						))
					}
				}
			} else {
				// ERC20 Transfer
				// Check if from address is watched
				if bf.MightContain(from) {
					if watched, ok := chainAddrs[from]; ok && watched.Filter.ERC20 {
						events = append(events, m.createERC20Event(
							watched,
							log,
							from, to,
							blockNumber, blockHash, blockTime,
						))
					}
				}

				// Check if to address is watched
				if bf.MightContain(to) {
					if watched, ok := chainAddrs[to]; ok && watched.Filter.ERC20 {
						events = append(events, m.createERC20Event(
							watched,
							log,
							from, to,
							blockNumber, blockHash, blockTime,
						))
					}
				}
			}
		}
	}

	return events
}

// matchesValueFilter checks if a value matches the minimum value filter
func (m *EventMatcher) matchesValueFilter(value *big.Int, minValue string) bool {
	if minValue == "" {
		return true
	}

	min, ok := new(big.Int).SetString(minValue, 10)
	if !ok {
		return true // Invalid filter, allow through
	}

	return value.Cmp(min) >= 0
}

// createTxEvent creates a transaction event
func (m *EventMatcher) createTxEvent(
	watched *WatchedAddress,
	eventType WatchEventType,
	tx *types.Transaction,
	receipt *types.Receipt,
	from common.Address,
	to *common.Address,
	blockNumber uint64,
	blockHash common.Hash,
	blockTime uint64,
) *WatchEvent {
	event := &WatchEvent{
		AddressID:   watched.ID,
		Address:     watched.Address,
		ChainID:     watched.ChainID,
		EventType:   eventType,
		BlockNumber: blockNumber,
		BlockHash:   blockHash,
		TxHash:      tx.Hash(),
		TxIndex:     uint(receipt.TransactionIndex),
		Value:       tx.Value().String(),
		From:        from,
	}

	if to != nil {
		event.To = *to
	}

	event.Data = &TxEventData{
		From:     from,
		To:       event.To,
		Value:    tx.Value().String(),
		GasUsed:  receipt.GasUsed,
		GasPrice: tx.GasPrice().String(),
		Nonce:    tx.Nonce(),
	}

	return event
}

// createLogEvent creates a log event
func (m *EventMatcher) createLogEvent(
	watched *WatchedAddress,
	eventType WatchEventType,
	log *types.Log,
	blockNumber uint64,
	blockHash common.Hash,
	blockTime uint64,
) *WatchEvent {
	logIndex := uint(log.Index)
	return &WatchEvent{
		AddressID:   watched.ID,
		Address:     watched.Address,
		ChainID:     watched.ChainID,
		EventType:   eventType,
		BlockNumber: blockNumber,
		BlockHash:   blockHash,
		TxHash:      log.TxHash,
		TxIndex:     uint(log.TxIndex),
		LogIndex:    &logIndex,
		Data: &LogEventData{
			Address: log.Address,
			Topics:  log.Topics,
			Data:    common.Bytes2Hex(log.Data),
		},
	}
}

// createERC20Event creates an ERC20 transfer event
func (m *EventMatcher) createERC20Event(
	watched *WatchedAddress,
	log *types.Log,
	from, to common.Address,
	blockNumber uint64,
	blockHash common.Hash,
	blockTime uint64,
) *WatchEvent {
	logIndex := uint(log.Index)

	// Parse amount from log data
	amount := new(big.Int).SetBytes(log.Data)

	return &WatchEvent{
		AddressID:   watched.ID,
		Address:     watched.Address,
		ChainID:     watched.ChainID,
		EventType:   WatchEventTypeERC20Transfer,
		BlockNumber: blockNumber,
		BlockHash:   blockHash,
		TxHash:      log.TxHash,
		TxIndex:     uint(log.TxIndex),
		LogIndex:    &logIndex,
		From:        from,
		To:          to,
		TokenAmount: amount.String(),
		Data: &ERC20EventData{
			Token:  log.Address,
			From:   from,
			To:     to,
			Amount: amount.String(),
		},
	}
}

// createERC721Event creates an ERC721 transfer event
func (m *EventMatcher) createERC721Event(
	watched *WatchedAddress,
	log *types.Log,
	from, to common.Address,
	blockNumber uint64,
	blockHash common.Hash,
	blockTime uint64,
) *WatchEvent {
	logIndex := uint(log.Index)

	// Parse tokenId from topic[3]
	tokenID := new(big.Int).SetBytes(log.Topics[3].Bytes())

	return &WatchEvent{
		AddressID:   watched.ID,
		Address:     watched.Address,
		ChainID:     watched.ChainID,
		EventType:   WatchEventTypeERC721Transfer,
		BlockNumber: blockNumber,
		BlockHash:   blockHash,
		TxHash:      log.TxHash,
		TxIndex:     uint(log.TxIndex),
		LogIndex:    &logIndex,
		From:        from,
		To:          to,
		TokenID:     tokenID.String(),
		Data: &ERC721EventData{
			Token:   log.Address,
			From:    from,
			To:      to,
			TokenID: tokenID.String(),
		},
	}
}

// HasWatchedAddresses checks if there are any watched addresses for a chain
func (m *EventMatcher) HasWatchedAddresses(chainID string) bool {
	return len(m.addresses[chainID]) > 0
}

// GetWatchedAddressCount returns the number of watched addresses for a chain
func (m *EventMatcher) GetWatchedAddressCount(chainID string) int {
	return len(m.addresses[chainID])
}
