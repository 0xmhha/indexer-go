package api

import (
	"github.com/0xmhha/indexer-go/pkg/api/graphql"
	"github.com/0xmhha/indexer-go/pkg/chains/stablenet/consensus"
	"github.com/0xmhha/indexer-go/pkg/events"
)

// registerSubscriptions describes how the consensus subscriptions are served.
func registerSubscriptions() {
	graphql.RegisterSubscription("consensusBlock", graphql.SubscriptionSpec{
		EventType: consensus.EventTypeConsensusBlock,
		Payload:   consensusBlockPayload,
	})
	graphql.RegisterSubscription("consensusFork", graphql.SubscriptionSpec{
		EventType: consensus.EventTypeConsensusFork,
		Payload:   consensusForkPayload,
	})
	graphql.RegisterSubscription("consensusValidatorChange", graphql.SubscriptionSpec{
		EventType: consensus.EventTypeConsensusValidatorChange,
		Payload:   consensusValidatorChangePayload,
	})
	graphql.RegisterSubscription("consensusError", graphql.SubscriptionSpec{
		EventType: consensus.EventTypeConsensusError,
		Payload:   consensusErrorPayload,
	})
}

func consensusBlockPayload(event events.Event) (interface{}, bool) {
	consensusEvent, ok := event.(*consensus.ConsensusBlockEvent)
	if !ok {
		return nil, false
	}
	consensusData := map[string]interface{}{
		"blockNumber":         consensusEvent.BlockNumber,
		"blockHash":           consensusEvent.BlockHash.Hex(),
		"timestamp":           consensusEvent.BlockTimestamp,
		"round":               consensusEvent.Round,
		"prevRound":           consensusEvent.PrevRound,
		"roundChanged":        consensusEvent.RoundChanged,
		"proposer":            consensusEvent.Proposer.Hex(),
		"validatorCount":      consensusEvent.ValidatorCount,
		"prepareCount":        consensusEvent.PrepareCount,
		"commitCount":         consensusEvent.CommitCount,
		"participationRate":   consensusEvent.ParticipationRate,
		"missedValidatorRate": consensusEvent.MissedValidatorRate,
		"isEpochBoundary":     consensusEvent.IsEpochBoundary,
	}
	if consensusEvent.EpochNumber != nil {
		consensusData["epochNumber"] = *consensusEvent.EpochNumber
	}
	if consensusEvent.EpochValidators != nil {
		validators := make([]string, len(consensusEvent.EpochValidators))
		for i, v := range consensusEvent.EpochValidators {
			validators[i] = v.Hex()
		}
		consensusData["epochValidators"] = validators
	}
	return consensusData, true
}

func consensusForkPayload(event events.Event) (interface{}, bool) {
	forkEvent, ok := event.(*consensus.ConsensusForkEvent)
	if !ok {
		return nil, false
	}
	forkData := map[string]interface{}{
		"forkBlockNumber": forkEvent.ForkBlockNumber,
		"forkBlockHash":   forkEvent.ForkBlockHash.Hex(),
		"chain1Hash":      forkEvent.Chain1Hash.Hex(),
		"chain1Height":    forkEvent.Chain1Height,
		"chain1Weight":    forkEvent.Chain1Weight,
		"chain2Hash":      forkEvent.Chain2Hash.Hex(),
		"chain2Height":    forkEvent.Chain2Height,
		"chain2Weight":    forkEvent.Chain2Weight,
		"resolved":        forkEvent.Resolved,
		"winningChain":    forkEvent.WinningChain,
		"detectedAt":      forkEvent.DetectedAt.Unix(),
		"detectionLag":    forkEvent.DetectionLag,
	}
	return forkData, true
}

func consensusValidatorChangePayload(event events.Event) (interface{}, bool) {
	changeEvent, ok := event.(*consensus.ConsensusValidatorChangeEvent)
	if !ok {
		return nil, false
	}
	changeData := map[string]interface{}{
		"blockNumber":            changeEvent.BlockNumber,
		"blockHash":              changeEvent.BlockHash.Hex(),
		"timestamp":              changeEvent.BlockTimestamp,
		"epochNumber":            changeEvent.EpochNumber,
		"isEpochBoundary":        changeEvent.IsEpochBoundary,
		"changeType":             changeEvent.ChangeType,
		"previousValidatorCount": changeEvent.PreviousValidatorCount,
		"newValidatorCount":      changeEvent.NewValidatorCount,
	}
	if len(changeEvent.AddedValidators) > 0 {
		added := make([]string, len(changeEvent.AddedValidators))
		for i, v := range changeEvent.AddedValidators {
			added[i] = v.Hex()
		}
		changeData["addedValidators"] = added
	}
	if len(changeEvent.RemovedValidators) > 0 {
		removed := make([]string, len(changeEvent.RemovedValidators))
		for i, v := range changeEvent.RemovedValidators {
			removed[i] = v.Hex()
		}
		changeData["removedValidators"] = removed
	}
	if len(changeEvent.ValidatorSet) > 0 {
		validators := make([]string, len(changeEvent.ValidatorSet))
		for i, v := range changeEvent.ValidatorSet {
			validators[i] = v.Hex()
		}
		changeData["validatorSet"] = validators
	}
	if changeEvent.AdditionalInfo != "" {
		changeData["additionalInfo"] = changeEvent.AdditionalInfo
	}
	return changeData, true
}

func consensusErrorPayload(event events.Event) (interface{}, bool) {
	errorEvent, ok := event.(*consensus.ConsensusErrorEvent)
	if !ok {
		return nil, false
	}
	errorData := map[string]interface{}{
		"blockNumber":        errorEvent.BlockNumber,
		"blockHash":          errorEvent.BlockHash.Hex(),
		"timestamp":          errorEvent.BlockTimestamp,
		"errorType":          errorEvent.ErrorType,
		"severity":           errorEvent.Severity,
		"errorMessage":       errorEvent.ErrorMessage,
		"round":              errorEvent.Round,
		"expectedValidators": errorEvent.ExpectedValidators,
		"actualSigners":      errorEvent.ActualSigners,
		"participationRate":  errorEvent.ParticipationRate,
		"consensusImpacted":  errorEvent.ConsensusImpacted,
		"recoveryTime":       errorEvent.RecoveryTime,
	}
	if len(errorEvent.MissedValidators) > 0 {
		missed := make([]string, len(errorEvent.MissedValidators))
		for i, v := range errorEvent.MissedValidators {
			missed[i] = v.Hex()
		}
		errorData["missedValidators"] = missed
	}
	if errorEvent.ErrorDetails != "" {
		errorData["errorDetails"] = errorEvent.ErrorDetails
	}
	return errorData, true
}
