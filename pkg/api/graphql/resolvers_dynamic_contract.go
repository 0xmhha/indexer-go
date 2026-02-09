package graphql

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/ethereum/go-ethereum/common"
	"github.com/graphql-go/graphql"
	"go.uber.org/zap"
)

// resolveRegisteredContract returns a single registered contract by address
func (s *Schema) resolveRegisteredContract(p graphql.ResolveParams) (interface{}, error) {
	if s.contractRegistrationService == nil {
		return nil, fmt.Errorf("contract registration service not available")
	}

	addressStr, ok := p.Args["address"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid address")
	}

	address := common.HexToAddress(addressStr)
	reg, err := s.contractRegistrationService.GetContract(p.Context, address)
	if err != nil {
		return nil, nil // Not found
	}

	return contractRegistrationToMap(reg), nil
}

// resolveRegisteredContracts returns all registered contracts
func (s *Schema) resolveRegisteredContracts(p graphql.ResolveParams) (interface{}, error) {
	if s.contractRegistrationService == nil {
		return []interface{}{}, nil
	}

	registrations, err := s.contractRegistrationService.ListContracts(p.Context)
	if err != nil {
		s.logger.Error("failed to list registered contracts", zap.Error(err))
		return nil, err
	}

	result := make([]interface{}, len(registrations))
	for i, reg := range registrations {
		result[i] = contractRegistrationToMap(reg)
	}

	return result, nil
}

// resolveDynamicContractEvents returns events from registered contracts
func (s *Schema) resolveDynamicContractEvents(p graphql.ResolveParams) (interface{}, error) {
	if s.contractRegistrationService == nil {
		return []interface{}{}, nil
	}

	ctx := p.Context

	// Parse filter
	var contractAddr *common.Address
	var eventNames []string
	var fromBlock, toBlock uint64

	if filter, ok := p.Args["filter"].(map[string]interface{}); ok {
		if addr, ok := filter["contract"].(string); ok && addr != "" {
			a := common.HexToAddress(addr)
			contractAddr = &a
		}
		if names, ok := filter["eventNames"].([]interface{}); ok {
			for _, n := range names {
				if name, ok := n.(string); ok {
					eventNames = append(eventNames, name)
				}
			}
		}
		if fb, ok := filter["fromBlock"].(string); ok {
			parsed, err := strconv.ParseUint(fb, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid fromBlock value %q: %w", fb, err)
			}
			fromBlock = parsed
		}
		if tb, ok := filter["toBlock"].(string); ok {
			parsed, err := strconv.ParseUint(tb, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid toBlock value %q: %w", tb, err)
			}
			toBlock = parsed
		}
	}

	// Parse pagination
	limit := 100
	offset := 0
	if pagination, ok := p.Args["pagination"].(map[string]interface{}); ok {
		if l, ok := pagination["limit"].(int); ok && l > 0 {
			limit = l
			if limit > 1000 {
				limit = 1000
			}
		}
		if o, ok := pagination["offset"].(int); ok && o >= 0 {
			offset = o
		}
	}

	// Default block range
	if toBlock == 0 {
		latestHeight, err := s.storage.GetLatestHeight(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get latest height: %w", err)
		}
		toBlock = latestHeight
	}
	if fromBlock == 0 && toBlock > 1000 {
		fromBlock = toBlock - 1000
	}

	// Determine which contracts to query
	var contracts []common.Address
	if contractAddr != nil {
		contracts = []common.Address{*contractAddr}
	} else {
		regs, err := s.contractRegistrationService.ListContracts(ctx)
		if err != nil {
			return nil, err
		}
		for _, reg := range regs {
			contracts = append(contracts, reg.Address)
		}
	}

	// Collect events from all relevant contracts
	var allEvents []interface{}
	eventNameSet := make(map[string]bool)
	for _, name := range eventNames {
		eventNameSet[name] = true
	}

	for _, addr := range contracts {
		reg, err := s.contractRegistrationService.GetContract(ctx, addr)
		if err != nil {
			continue
		}

		logs, err := s.storage.GetLogsByAddress(ctx, addr, fromBlock, toBlock)
		if err != nil {
			s.logger.Warn("failed to get logs for contract",
				zap.String("address", addr.Hex()),
				zap.Error(err))
			continue
		}

		for _, log := range logs {
			if len(log.Topics) == 0 {
				continue
			}

			// Try to decode event name from ABI
			eventName := log.Topics[0].Hex() // fallback to topic hash
			// Try to find event name from registration events list
			for _, name := range reg.Events {
				// Use the parser to check if this log matches
				if s.contractRegistrationService.IsRegistered(addr) {
					eventName = name
					break
				}
			}

			// Filter by event names if specified
			if len(eventNameSet) > 0 && !eventNameSet[eventName] {
				continue
			}

			// Build event data
			dataMap := make(map[string]interface{})
			dataMap["topics"] = func() []string {
				topics := make([]string, len(log.Topics))
				for i, t := range log.Topics {
					topics[i] = t.Hex()
				}
				return topics
			}()
			dataMap["data"] = fmt.Sprintf("0x%x", log.Data)

			dataJSON, _ := json.Marshal(dataMap)

			// Get block timestamp
			timestamp := "0"
			block, err := s.storage.GetBlock(ctx, log.BlockNumber)
			if err == nil && block != nil {
				timestamp = fmt.Sprintf("%d", block.Header().Time)
			}

			allEvents = append(allEvents, map[string]interface{}{
				"contract":     addr.Hex(),
				"contractName": reg.Name,
				"eventName":    eventName,
				"blockNumber":  fmt.Sprintf("%d", log.BlockNumber),
				"txHash":       log.TxHash.Hex(),
				"logIndex":     int(log.Index),
				"data":         string(dataJSON),
				"timestamp":    timestamp,
			})
		}
	}

	// Apply pagination
	total := len(allEvents)
	if offset >= total {
		return []interface{}{}, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}

	return allEvents[offset:end], nil
}

// resolveRegisterContract handles the registerContract mutation
func (s *Schema) resolveRegisterContract(p graphql.ResolveParams) (interface{}, error) {
	if s.contractRegistrationService == nil {
		return nil, fmt.Errorf("contract registration service not available")
	}

	input, ok := p.Args["input"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid input")
	}

	addressStr, _ := input["address"].(string)
	name, _ := input["name"].(string)
	abiJSON, _ := input["abi"].(string)

	var blockNumber uint64
	if bn, ok := input["blockNumber"].(string); ok {
		parsed, err := strconv.ParseUint(bn, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid blockNumber value %q: %w", bn, err)
		}
		blockNumber = parsed
	}

	reg, err := s.contractRegistrationService.RegisterContract(p.Context, events.RegisterContractInput{
		Address:     addressStr,
		Name:        name,
		ABI:         abiJSON,
		BlockNumber: blockNumber,
	})
	if err != nil {
		return nil, err
	}

	return contractRegistrationToMap(reg), nil
}

// resolveUnregisterContract handles the unregisterContract mutation
func (s *Schema) resolveUnregisterContract(p graphql.ResolveParams) (interface{}, error) {
	if s.contractRegistrationService == nil {
		return nil, fmt.Errorf("contract registration service not available")
	}

	addressStr, ok := p.Args["address"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid address")
	}

	address := common.HexToAddress(addressStr)
	err := s.contractRegistrationService.UnregisterContract(p.Context, address)
	if err != nil {
		return false, err
	}

	return true, nil
}

// contractRegistrationToMap converts a ContractRegistration to a GraphQL map
func contractRegistrationToMap(reg *events.ContractRegistration) map[string]interface{} {
	evts := make([]interface{}, len(reg.Events))
	for i, e := range reg.Events {
		evts[i] = e
	}

	return map[string]interface{}{
		"address":      reg.Address.Hex(),
		"name":         reg.Name,
		"abi":          reg.ABI,
		"registeredAt": fmt.Sprintf("%d", reg.RegisteredAt.Unix()),
		"blockNumber":  fmt.Sprintf("%d", reg.BlockNumber),
		"isVerified":   reg.IsVerified,
		"events":       evts,
	}
}
