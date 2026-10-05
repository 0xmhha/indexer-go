package api

import (
	"encoding/json"
	"fmt"

	"github.com/ethereum/go-ethereum/common"

	"github.com/0xmhha/indexer-go/pkg/api/graphql"
	sc "github.com/0xmhha/indexer-go/pkg/chains/stablenet/systemcontracts"
	"github.com/0xmhha/indexer-go/pkg/events"
)

// registerSubscriptions describes how the systemContractEvents subscription
// is served.
func registerSubscriptions() {
	graphql.RegisterSubscription("systemContractEvents", graphql.SubscriptionSpec{
		EventType: sc.EventTypeSystemContract,
		Filter: func(variables map[string]interface{}) (*events.Filter, error) {
			return buildSystemContractFilter(variables["filter"])
		},
		Payload: systemContractEventsPayload,
	})
}

func systemContractEventsPayload(event events.Event) (interface{}, bool) {
	scEvent, ok := event.(*sc.SystemContractEvent)
	if !ok {
		return nil, false
	}
	// Serialize data to JSON string
	dataJSON, _ := json.Marshal(scEvent.Data)
	eventData := map[string]interface{}{
		"contract":        scEvent.Contract.Hex(),
		"eventName":       string(scEvent.EventName),
		"blockNumber":     fmt.Sprintf("%d", scEvent.BlockNumber),
		"transactionHash": scEvent.TxHash.Hex(),
		"logIndex":        scEvent.LogIndex,
		"data":            string(dataJSON),
		"timestamp":       fmt.Sprintf("%d", scEvent.CreatedAt.Unix()),
	}
	return eventData, true
}

func buildSystemContractFilter(raw interface{}) (*events.Filter, error) {
	if raw == nil {
		return nil, nil
	}
	filterMap, ok := raw.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid system contract filter format")
	}

	filter := events.NewFilter()

	// Parse contract address filter
	if contractVal, ok := filterMap["contract"]; ok {
		contractStr, ok := contractVal.(string)
		if !ok {
			return nil, fmt.Errorf("contract must be a string")
		}
		address, err := parseAddress(contractStr)
		if err != nil {
			return nil, err
		}
		filter.Addresses = append(filter.Addresses, address)
	}

	// Parse event types filter (stored in custom data)
	if eventTypesVal, ok := filterMap["eventTypes"]; ok {
		eventTypesSlice, ok := eventTypesVal.([]interface{})
		if ok && len(eventTypesSlice) > 0 {
			eventTypes := make([]string, 0, len(eventTypesSlice))
			for _, et := range eventTypesSlice {
				if etStr, ok := et.(string); ok {
					eventTypes = append(eventTypes, etStr)
				}
			}
			if len(eventTypes) > 0 {
				filter.CustomData = map[string]interface{}{
					"eventTypes": eventTypes,
				}
			}
		}
	}

	if filter.IsEmpty() {
		return nil, nil
	}

	return filter, nil
}

func parseAddress(value string) (common.Address, error) {
	if !common.IsHexAddress(value) {
		return common.Address{}, fmt.Errorf("invalid address: %s", value)
	}
	return common.HexToAddress(value), nil
}
