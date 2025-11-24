package storage

import (
	"context"
	"fmt"

	"github.com/cockroachdb/pebble"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// ========== Log Reader Methods ==========

// GetLogs returns logs matching the given filter
func (s *PebbleStorage) GetLogs(ctx context.Context, filter *LogFilter) ([]*types.Log, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	if filter == nil {
		return nil, fmt.Errorf("filter cannot be nil")
	}

	// Validate block range
	if filter.ToBlock > 0 && filter.FromBlock > filter.ToBlock {
		return nil, fmt.Errorf("fromBlock (%d) cannot be greater than toBlock (%d)", filter.FromBlock, filter.ToBlock)
	}

	// If toBlock is 0, use latest height
	toBlock := filter.ToBlock
	if toBlock == 0 {
		latestHeight, err := s.GetLatestHeight(ctx)
		if err != nil && err != ErrNotFound {
			return nil, fmt.Errorf("failed to get latest height: %w", err)
		}
		toBlock = latestHeight
	}

	var logs []*types.Log

	// Strategy 1: If specific addresses are provided, use address index
	if len(filter.Addresses) > 0 {
		for _, addr := range filter.Addresses {
			addrLogs, err := s.getLogsByAddressRange(ctx, addr, filter.FromBlock, toBlock)
			if err != nil {
				return nil, err
			}
			logs = append(logs, addrLogs...)
		}
	} else if len(filter.Topics) > 0 && len(filter.Topics[0]) > 0 {
		// Strategy 2: If topic0 is specified, use topic0 index
		for _, topic := range filter.Topics[0] {
			topicLogs, err := s.getLogsByTopicRange(ctx, topic, 0, filter.FromBlock, toBlock)
			if err != nil {
				return nil, err
			}
			logs = append(logs, topicLogs...)
		}
	} else {
		// Strategy 3: Scan all logs in block range
		for blockNum := filter.FromBlock; blockNum <= toBlock; blockNum++ {
			blockLogs, err := s.GetLogsByBlock(ctx, blockNum)
			if err != nil && err != ErrNotFound {
				return nil, err
			}
			logs = append(logs, blockLogs...)
		}
	}

	// Apply topic filters
	logs = s.filterLogsByTopics(logs, filter.Topics)

	// Apply address filter if we scanned by topics or block range
	if len(filter.Addresses) == 0 && (len(filter.Topics) > 0 || len(logs) > 0) {
		logs = s.filterLogsByAddresses(logs, filter.Addresses)
	}

	return logs, nil
}

// GetLogsByBlock returns all logs in a specific block
func (s *PebbleStorage) GetLogsByBlock(ctx context.Context, blockNumber uint64) ([]*types.Log, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	prefix := LogBlockIndexKeyPrefix(blockNumber)
	upperBound := make([]byte, len(prefix), len(prefix)+1)
	copy(upperBound, prefix)
	upperBound = append(upperBound, 0xff)

	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: upperBound,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var logs []*types.Log

	for iter.First(); iter.Valid(); iter.Next() {
		// Value is empty, we need to get the actual log data
		// Parse key to extract txIndex and logIndex
		// Key format: /index/logs/block/{blockNumber}/{txIndex}/{logIndex}
		key := string(iter.Key())
		var txIndex, logIndex uint
		if _, err := fmt.Sscanf(key[len(string(prefix)):], "%06d/%06d", &txIndex, &logIndex); err != nil {
			continue // Skip invalid keys
		}

		// Get log data
		logKey := LogKey(blockNumber, txIndex, logIndex)
		logData, closer, err := s.db.Get(logKey)
		if err != nil {
			continue // Skip missing logs
		}

		log, err := DecodeLog(logData)
		closer.Close()
		if err != nil {
			continue // Skip invalid logs
		}

		logs = append(logs, log)
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return logs, nil
}

// GetLogsByAddress returns logs emitted by a specific contract
func (s *PebbleStorage) GetLogsByAddress(ctx context.Context, address common.Address, fromBlock, toBlock uint64) ([]*types.Log, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	return s.getLogsByAddressRange(ctx, address, fromBlock, toBlock)
}

// GetLogsByTopic returns logs with a specific topic at a specific position
func (s *PebbleStorage) GetLogsByTopic(ctx context.Context, topic common.Hash, topicIndex int, fromBlock, toBlock uint64) ([]*types.Log, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	if topicIndex < 0 || topicIndex > 3 {
		return nil, fmt.Errorf("invalid topic index: %d (must be 0-3)", topicIndex)
	}

	return s.getLogsByTopicRange(ctx, topic, topicIndex, fromBlock, toBlock)
}

// ========== Log Writer Methods ==========

// IndexLogs indexes logs from receipts
func (s *PebbleStorage) IndexLogs(ctx context.Context, logs []*types.Log) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	batch := s.NewBatch()
	defer batch.Close()

	for _, log := range logs {
		if err := s.indexLogToBatch(batch.(*pebbleBatch), log); err != nil {
			return fmt.Errorf("failed to index log: %w", err)
		}
	}

	return batch.Commit()
}

// IndexLog indexes a single log
func (s *PebbleStorage) IndexLog(ctx context.Context, log *types.Log) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	batch := s.NewBatch()
	defer batch.Close()

	if err := s.indexLogToBatch(batch.(*pebbleBatch), log); err != nil {
		return err
	}

	return batch.Commit()
}

// ========== Internal Helper Methods ==========

// indexLogToBatch adds log indexing operations to a batch
func (s *PebbleStorage) indexLogToBatch(batch *pebbleBatch, log *types.Log) error {
	if log == nil {
		return fmt.Errorf("log cannot be nil")
	}

	// Encode log data
	encoded, err := EncodeLog(log)
	if err != nil {
		return fmt.Errorf("failed to encode log: %w", err)
	}

	// Store log data
	logKey := LogKey(log.BlockNumber, log.TxIndex, log.Index)
	if err := batch.batch.Set(logKey, encoded, nil); err != nil {
		return fmt.Errorf("failed to store log data: %w", err)
	}

	// Index by contract address
	addrKey := LogAddressIndexKey(log.Address, log.BlockNumber, log.TxIndex, log.Index)
	if err := batch.batch.Set(addrKey, []byte{1}, nil); err != nil {
		return fmt.Errorf("failed to store address index: %w", err)
	}

	// Index by topics
	if len(log.Topics) > 0 {
		topic0Key := LogTopic0IndexKey(log.Topics[0], log.BlockNumber, log.TxIndex, log.Index)
		if err := batch.batch.Set(topic0Key, []byte{1}, nil); err != nil {
			return fmt.Errorf("failed to store topic0 index: %w", err)
		}
	}
	if len(log.Topics) > 1 {
		topic1Key := LogTopic1IndexKey(log.Topics[1], log.BlockNumber, log.TxIndex, log.Index)
		if err := batch.batch.Set(topic1Key, []byte{1}, nil); err != nil {
			return fmt.Errorf("failed to store topic1 index: %w", err)
		}
	}
	if len(log.Topics) > 2 {
		topic2Key := LogTopic2IndexKey(log.Topics[2], log.BlockNumber, log.TxIndex, log.Index)
		if err := batch.batch.Set(topic2Key, []byte{1}, nil); err != nil {
			return fmt.Errorf("failed to store topic2 index: %w", err)
		}
	}
	if len(log.Topics) > 3 {
		topic3Key := LogTopic3IndexKey(log.Topics[3], log.BlockNumber, log.TxIndex, log.Index)
		if err := batch.batch.Set(topic3Key, []byte{1}, nil); err != nil {
			return fmt.Errorf("failed to store topic3 index: %w", err)
		}
	}

	// Index by block
	blockKey := LogBlockIndexKey(log.BlockNumber, log.TxIndex, log.Index)
	if err := batch.batch.Set(blockKey, []byte{1}, nil); err != nil {
		return fmt.Errorf("failed to store block index: %w", err)
	}

	batch.count += 5 + len(log.Topics) // 1 data + 1 addr + topics + 1 block
	return nil
}

// getLogsByAddressRange retrieves logs by address within a block range
func (s *PebbleStorage) getLogsByAddressRange(ctx context.Context, address common.Address, fromBlock, toBlock uint64) ([]*types.Log, error) {
	prefix := LogAddressIndexKeyPrefix(address)

	// Create iterator with block range bounds
	startKey := LogAddressIndexKey(address, fromBlock, 0, 0)
	endKey := LogAddressIndexKey(address, toBlock+1, 0, 0)

	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: startKey,
		UpperBound: endKey,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var logs []*types.Log

	for iter.First(); iter.Valid(); iter.Next() {
		// Parse key to extract block, tx, log indexes
		key := string(iter.Key())
		var blockNum uint64
		var txIndex, logIndex uint
		if _, err := fmt.Sscanf(key[len(string(prefix)):], "/%020d/%06d/%06d", &blockNum, &txIndex, &logIndex); err != nil {
			continue
		}

		// Get log data
		logKey := LogKey(blockNum, txIndex, logIndex)
		logData, closer, err := s.db.Get(logKey)
		if err != nil {
			continue
		}

		log, err := DecodeLog(logData)
		closer.Close()
		if err != nil {
			continue
		}

		logs = append(logs, log)
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return logs, nil
}

// getLogsByTopicRange retrieves logs by topic within a block range
func (s *PebbleStorage) getLogsByTopicRange(ctx context.Context, topic common.Hash, topicIndex int, fromBlock, toBlock uint64) ([]*types.Log, error) {
	var prefix []byte
	var startKey, endKey []byte

	switch topicIndex {
	case 0:
		prefix = LogTopic0IndexKeyPrefix(topic)
		startKey = LogTopic0IndexKey(topic, fromBlock, 0, 0)
		endKey = LogTopic0IndexKey(topic, toBlock+1, 0, 0)
	case 1:
		prefix = LogTopic1IndexKeyPrefix(topic)
		startKey = LogTopic1IndexKey(topic, fromBlock, 0, 0)
		endKey = LogTopic1IndexKey(topic, toBlock+1, 0, 0)
	case 2:
		prefix = LogTopic2IndexKeyPrefix(topic)
		startKey = LogTopic2IndexKey(topic, fromBlock, 0, 0)
		endKey = LogTopic2IndexKey(topic, toBlock+1, 0, 0)
	case 3:
		prefix = LogTopic3IndexKeyPrefix(topic)
		startKey = LogTopic3IndexKey(topic, fromBlock, 0, 0)
		endKey = LogTopic3IndexKey(topic, toBlock+1, 0, 0)
	default:
		return nil, fmt.Errorf("invalid topic index: %d", topicIndex)
	}

	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: startKey,
		UpperBound: endKey,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var logs []*types.Log

	for iter.First(); iter.Valid(); iter.Next() {
		// Parse key to extract block, tx, log indexes
		key := string(iter.Key())
		var blockNum uint64
		var txIndex, logIndex uint
		if _, err := fmt.Sscanf(key[len(string(prefix)):], "/%020d/%06d/%06d", &blockNum, &txIndex, &logIndex); err != nil {
			continue
		}

		// Get log data
		logKey := LogKey(blockNum, txIndex, logIndex)
		logData, closer, err := s.db.Get(logKey)
		if err != nil {
			continue
		}

		log, err := DecodeLog(logData)
		closer.Close()
		if err != nil {
			continue
		}

		logs = append(logs, log)
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return logs, nil
}

// filterLogsByTopics filters logs by topic criteria
func (s *PebbleStorage) filterLogsByTopics(logs []*types.Log, topics [][]common.Hash) []*types.Log {
	if len(topics) == 0 {
		return logs
	}

	filtered := make([]*types.Log, 0, len(logs))

	for _, log := range logs {
		if s.matchesTopicFilter(log, topics) {
			filtered = append(filtered, log)
		}
	}

	return filtered
}

// filterLogsByAddresses filters logs by contract addresses
func (s *PebbleStorage) filterLogsByAddresses(logs []*types.Log, addresses []common.Address) []*types.Log {
	if len(addresses) == 0 {
		return logs
	}

	filtered := make([]*types.Log, 0, len(logs))
	addressMap := make(map[common.Address]bool)
	for _, addr := range addresses {
		addressMap[addr] = true
	}

	for _, log := range logs {
		if addressMap[log.Address] {
			filtered = append(filtered, log)
		}
	}

	return filtered
}

// matchesTopicFilter checks if a log matches the topic filter
func (s *PebbleStorage) matchesTopicFilter(log *types.Log, topicFilter [][]common.Hash) bool {
	for i, topicOptions := range topicFilter {
		if len(topicOptions) == 0 {
			// nil means "any value" for this position
			continue
		}

		if i >= len(log.Topics) {
			// Log doesn't have enough topics
			return false
		}

		// Check if log's topic at position i matches any of the options
		matched := false
		for _, option := range topicOptions {
			if log.Topics[i] == option {
				matched = true
				break
			}
		}

		if !matched {
			return false
		}
	}

	return true
}
