package graphql

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/0xmhha/indexer-go/pkg/watchlist"
	"github.com/ethereum/go-ethereum/common"
	"github.com/graphql-go/graphql"
)

// SetWatchlistService sets the watchlist service for resolvers
func (s *Schema) SetWatchlistService(service watchlist.Service) {
	s.watchlistService = service
}

// resolveWatchedAddresses returns all watched addresses with optional filtering
func (s *Schema) resolveWatchedAddresses(p graphql.ResolveParams) (interface{}, error) {
	if s.watchlistService == nil {
		return []interface{}{}, nil
	}

	ctx := p.Context
	if ctx == nil {
		ctx = context.Background()
	}

	filter := &watchlist.ListFilter{}

	if chainID, ok := p.Args["chainId"].(string); ok {
		filter.ChainID = chainID
	}

	if limit, ok := p.Args["limit"].(int); ok {
		filter.Limit = limit
	} else {
		filter.Limit = 100
	}

	if offset, ok := p.Args["offset"].(int); ok {
		filter.Offset = offset
	}

	addresses, err := s.watchlistService.ListWatchedAddresses(ctx, filter)
	if err != nil {
		return nil, err
	}

	result := make([]map[string]interface{}, 0, len(addresses))
	for _, addr := range addresses {
		result = append(result, watchedAddressToMap(addr))
	}

	return result, nil
}

// resolveWatchedAddress returns a single watched address by ID
func (s *Schema) resolveWatchedAddress(p graphql.ResolveParams) (interface{}, error) {
	if s.watchlistService == nil {
		return nil, fmt.Errorf("watchlist service not enabled")
	}

	id, ok := p.Args["id"].(string)
	if !ok {
		return nil, fmt.Errorf("id is required")
	}

	ctx := p.Context
	if ctx == nil {
		ctx = context.Background()
	}

	addr, err := s.watchlistService.GetWatchedAddress(ctx, id)
	if err != nil {
		return nil, err
	}

	return watchedAddressToMap(addr), nil
}

// resolveWatchEvents returns watch events with filtering
func (s *Schema) resolveWatchEvents(p graphql.ResolveParams) (interface{}, error) {
	if s.watchlistService == nil {
		return []interface{}{}, nil
	}

	ctx := p.Context
	if ctx == nil {
		ctx = context.Background()
	}

	addressID, _ := p.Args["addressId"].(string)
	limit := 100
	if l, ok := p.Args["limit"].(int); ok {
		limit = l
	}

	if addressID == "" {
		// Return empty list if no address specified
		return []interface{}{}, nil
	}

	events, err := s.watchlistService.GetRecentEvents(ctx, addressID, limit)
	if err != nil {
		return nil, err
	}

	result := make([]map[string]interface{}, 0, len(events))
	for _, event := range events {
		result = append(result, watchEventToMap(event))
	}

	return result, nil
}

// resolveWatchAddress adds a new address to the watchlist
func (s *Schema) resolveWatchAddress(p graphql.ResolveParams) (interface{}, error) {
	if s.watchlistService == nil {
		return nil, fmt.Errorf("watchlist service not enabled")
	}

	input, ok := p.Args["input"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("input is required")
	}

	addressStr := getString(input, "address")
	if !common.IsHexAddress(addressStr) {
		return nil, fmt.Errorf("invalid address: %s", addressStr)
	}

	req := &watchlist.WatchRequest{
		Address: common.HexToAddress(addressStr),
		ChainID: getString(input, "chainId"),
		Label:   getString(input, "label"),
		Filter:  parseWatchFilter(input["filter"]),
	}

	ctx := p.Context
	if ctx == nil {
		ctx = context.Background()
	}

	addr, err := s.watchlistService.WatchAddress(ctx, req)
	if err != nil {
		return nil, err
	}

	return watchedAddressToMap(addr), nil
}

// resolveUnwatchAddress removes an address from the watchlist
func (s *Schema) resolveUnwatchAddress(p graphql.ResolveParams) (interface{}, error) {
	if s.watchlistService == nil {
		return nil, fmt.Errorf("watchlist service not enabled")
	}

	id, ok := p.Args["id"].(string)
	if !ok {
		return nil, fmt.Errorf("id is required")
	}

	ctx := p.Context
	if ctx == nil {
		ctx = context.Background()
	}

	if err := s.watchlistService.UnwatchAddress(ctx, id); err != nil {
		return nil, err
	}

	return true, nil
}

// resolveUpdateWatchFilter updates the filter for a watched address
func (s *Schema) resolveUpdateWatchFilter(p graphql.ResolveParams) (interface{}, error) {
	if s.watchlistService == nil {
		return nil, fmt.Errorf("watchlist service not enabled")
	}

	id, ok := p.Args["id"].(string)
	if !ok {
		return nil, fmt.Errorf("id is required")
	}

	filterInput, ok := p.Args["filter"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("filter is required")
	}

	ctx := p.Context
	if ctx == nil {
		ctx = context.Background()
	}

	// Get existing address
	addr, err := s.watchlistService.GetWatchedAddress(ctx, id)
	if err != nil {
		return nil, err
	}

	// Update filter
	addr.Filter = parseWatchFilterMap(filterInput)

	// Re-watch with updated filter
	req := &watchlist.WatchRequest{
		Address: addr.Address,
		ChainID: addr.ChainID,
		Label:   addr.Label,
		Filter:  addr.Filter,
	}

	// Unwatch and re-watch
	if err := s.watchlistService.UnwatchAddress(ctx, id); err != nil {
		return nil, err
	}

	newAddr, err := s.watchlistService.WatchAddress(ctx, req)
	if err != nil {
		return nil, err
	}

	return watchedAddressToMap(newAddr), nil
}

// resolveRecentEventsField resolves the recentEvents field on WatchedAddress
func (s *Schema) resolveRecentEventsField(p graphql.ResolveParams) (interface{}, error) {
	if s.watchlistService == nil {
		return []interface{}{}, nil
	}

	source, ok := p.Source.(map[string]interface{})
	if !ok {
		return []interface{}{}, nil
	}

	addressID, ok := source["id"].(string)
	if !ok {
		return []interface{}{}, nil
	}

	limit := 10
	if l, ok := p.Args["limit"].(int); ok {
		limit = l
	}

	ctx := p.Context
	if ctx == nil {
		ctx = context.Background()
	}

	events, err := s.watchlistService.GetRecentEvents(ctx, addressID, limit)
	if err != nil {
		return []interface{}{}, nil
	}

	result := make([]map[string]interface{}, 0, len(events))
	for _, event := range events {
		result = append(result, watchEventToMap(event))
	}

	return result, nil
}

// Helper functions

func watchedAddressToMap(addr *watchlist.WatchedAddress) map[string]interface{} {
	result := map[string]interface{}{
		"id":        addr.ID,
		"address":   addr.Address.Hex(),
		"chainId":   addr.ChainID,
		"label":     addr.Label,
		"filter":    watchFilterToMap(addr.Filter),
		"createdAt": addr.CreatedAt.Format(time.RFC3339),
	}

	if addr.Stats != nil {
		result["stats"] = watchStatsToMap(addr.Stats)
	}

	return result
}

func watchFilterToMap(filter *watchlist.WatchFilter) map[string]interface{} {
	if filter == nil {
		filter = watchlist.DefaultWatchFilter()
	}

	result := map[string]interface{}{
		"txFrom": filter.TxFrom,
		"txTo":   filter.TxTo,
		"erc20":  filter.ERC20,
		"erc721": filter.ERC721,
		"logs":   filter.Logs,
	}

	if filter.MinValue != "" {
		result["minValue"] = filter.MinValue
	}

	return result
}

func watchStatsToMap(stats *watchlist.WatchStats) map[string]interface{} {
	result := map[string]interface{}{
		"totalEvents": int(stats.TotalEvents),
		"txFromCount": int(stats.TxFromCount),
		"txToCount":   int(stats.TxToCount),
		"erc20Count":  int(stats.ERC20Count),
		"erc721Count": int(stats.ERC721Count),
		"logCount":    int(stats.LogCount),
	}

	if !stats.LastEventAt.IsZero() {
		result["lastEventAt"] = stats.LastEventAt.Format(time.RFC3339)
	}

	return result
}

func watchEventToMap(event *watchlist.WatchEvent) map[string]interface{} {
	result := map[string]interface{}{
		"id":          event.ID,
		"addressId":   event.AddressID,
		"chainId":     event.ChainID,
		"eventType":   string(event.EventType),
		"blockNumber": strconv.FormatUint(event.BlockNumber, 10),
		"txHash":      event.TxHash.Hex(),
		"timestamp":   event.Timestamp.Format(time.RFC3339),
	}

	if event.LogIndex != nil {
		result["logIndex"] = int(*event.LogIndex)
	}

	// Serialize data to JSON string
	if event.Data != nil {
		dataBytes, err := json.Marshal(event.Data)
		if err == nil {
			result["data"] = string(dataBytes)
		} else {
			result["data"] = "{}"
		}
	} else {
		result["data"] = "{}"
	}

	return result
}

func parseWatchFilter(v interface{}) *watchlist.WatchFilter {
	if v == nil {
		return watchlist.DefaultWatchFilter()
	}

	m, ok := v.(map[string]interface{})
	if !ok {
		return watchlist.DefaultWatchFilter()
	}

	return parseWatchFilterMap(m)
}

func parseWatchFilterMap(m map[string]interface{}) *watchlist.WatchFilter {
	filter := &watchlist.WatchFilter{}

	if v, ok := m["txFrom"].(bool); ok {
		filter.TxFrom = v
	}
	if v, ok := m["txTo"].(bool); ok {
		filter.TxTo = v
	}
	if v, ok := m["erc20"].(bool); ok {
		filter.ERC20 = v
	}
	if v, ok := m["erc721"].(bool); ok {
		filter.ERC721 = v
	}
	if v, ok := m["logs"].(bool); ok {
		filter.Logs = v
	}
	if v, ok := m["minValue"].(string); ok {
		filter.MinValue = v
	}

	return filter
}
