package api

import (
	"fmt"
	"math/big"
	"strconv"

	"github.com/ethereum/go-ethereum/common"
	gql "github.com/graphql-go/graphql"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/constants"
	sc "github.com/0xmhha/indexer-go/pkg/chains/stablenet/systemcontracts"
)

// ========== System Contract Resolvers ==========

// resolveTotalSupply resolves the current total supply
func (s *Schema) resolveTotalSupply(p gql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	// Cast storage to SystemContractReader
	reader := s.storage
	if reader == nil {
		return nil, fmt.Errorf("storage does not implement SystemContractReader")
	}

	supply, err := reader.GetTotalSupply(ctx)
	if err != nil {
		s.logger.Error("failed to get total supply", zap.Error(err))
		return nil, err
	}

	return supply.String(), nil
}

// resolveActiveMinters resolves the list of active minters
func (s *Schema) resolveActiveMinters(p gql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	reader := s.storage
	if reader == nil {
		return nil, fmt.Errorf("storage does not implement SystemContractReader")
	}

	minters, err := reader.GetActiveMinters(ctx)
	if err != nil {
		s.logger.Error("failed to get active minters", zap.Error(err))
		return nil, err
	}

	var result []map[string]interface{}
	for _, minter := range minters {
		allowance, err := reader.GetMinterAllowance(ctx, minter)
		if err != nil {
			s.logger.Warn("failed to get minter allowance", zap.String("minter", minter.Hex()), zap.Error(err))
			continue
		}

		result = append(result, map[string]interface{}{
			"address":   minter.Hex(),
			"allowance": allowance.String(),
			"isActive":  true,
		})
	}

	return result, nil
}

// resolveActiveMinterAddresses resolves the list of active minter addresses only
func (s *Schema) resolveActiveMinterAddresses(p gql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	reader := s.storage
	if reader == nil {
		return nil, fmt.Errorf("storage does not implement SystemContractReader")
	}

	minters, err := reader.GetActiveMinters(ctx)
	if err != nil {
		s.logger.Error("failed to get active minter addresses", zap.Error(err))
		return nil, err
	}

	// Convert to hex string addresses
	var result []string
	for _, minter := range minters {
		result = append(result, minter.Hex())
	}

	return result, nil
}

// resolveMinterAllowance resolves the allowance for a specific minter
func (s *Schema) resolveMinterAllowance(p gql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	minterStr, ok := p.Args["minter"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid minter address")
	}

	reader := s.storage
	if reader == nil {
		return nil, fmt.Errorf("storage does not implement SystemContractReader")
	}

	minter := common.HexToAddress(minterStr)
	allowance, err := reader.GetMinterAllowance(ctx, minter)
	if err != nil {
		s.logger.Error("failed to get minter allowance",
			zap.String("minter", minterStr),
			zap.Error(err))
		return nil, err
	}

	return allowance.String(), nil
}

// resolveActiveValidators resolves the list of active validators
func (s *Schema) resolveActiveValidators(p gql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	reader := s.storage
	if reader == nil {
		return nil, fmt.Errorf("storage does not implement SystemContractReader")
	}

	validators, err := reader.GetActiveValidators(ctx)
	if err != nil {
		s.logger.Error("failed to get active validators", zap.Error(err))
		return nil, err
	}

	var result []map[string]interface{}
	for _, validator := range validators {
		result = append(result, map[string]interface{}{
			"address":  validator.Hex(),
			"isActive": true,
		})
	}

	return result, nil
}

// resolveActiveValidatorAddresses resolves the list of active validator addresses only
func (s *Schema) resolveActiveValidatorAddresses(p gql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	reader := s.storage
	if reader == nil {
		return nil, fmt.Errorf("storage does not implement SystemContractReader")
	}

	validators, err := reader.GetActiveValidators(ctx)
	if err != nil {
		s.logger.Error("failed to get active validator addresses", zap.Error(err))
		return nil, err
	}

	// Convert to hex string addresses
	var result []string
	for _, validator := range validators {
		result = append(result, validator.Hex())
	}

	return result, nil
}

// resolveBlacklistedAddresses resolves the list of blacklisted addresses
func (s *Schema) resolveBlacklistedAddresses(p gql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	reader := s.storage
	if reader == nil {
		return nil, fmt.Errorf("storage does not implement SystemContractReader")
	}

	addresses, err := reader.GetBlacklistedAddresses(ctx)
	if err != nil {
		s.logger.Error("failed to get blacklisted addresses", zap.Error(err))
		return nil, err
	}

	var result []string
	for _, addr := range addresses {
		result = append(result, addr.Hex())
	}

	return result, nil
}

// resolveProposals resolves governance proposals with filtering
func (s *Schema) resolveProposals(p gql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	// Parse filter (optional - nil means no filter)
	filter, _ := p.Args["filter"].(map[string]interface{})
	if filter == nil {
		filter = map[string]interface{}{}
	}

	// Parse contract (optional - if not provided, queries all contracts)
	contract := common.Address{} // Zero address queries all contracts
	contractStr := ""
	if contractVal, ok := filter["contract"].(string); ok {
		contractStr = contractVal
		contract = common.HexToAddress(contractStr)
	}

	// Parse status (optional)
	status := sc.ProposalStatusNone
	if statusStr, ok := filter["status"].(string); ok {
		status = parseProposalStatusEnum(statusStr)
	}

	// Parse proposer (optional) - will be filtered client-side
	var proposer common.Address
	var hasProposerFilter bool
	if proposerStr, ok := filter["proposer"].(string); ok && proposerStr != "" {
		proposer = common.HexToAddress(proposerStr)
		hasProposerFilter = true
	}

	// Get pagination parameters
	limit := constants.DefaultPaginationLimit
	offset := 0
	if pagination, ok := p.Args["pagination"].(map[string]interface{}); ok {
		if l, ok := pagination["limit"].(int); ok && l > 0 {
			if l > constants.DefaultMaxPaginationLimit {
				limit = constants.DefaultMaxPaginationLimit
			} else {
				limit = l
			}
		}
		if o, ok := pagination["offset"].(int); ok && o >= 0 {
			offset = o
		}
	}

	reader := s.storage
	if reader == nil {
		return nil, fmt.Errorf("storage does not implement SystemContractReader")
	}

	proposals, err := reader.GetProposals(ctx, contract, status, limit, offset)
	if err != nil {
		logContract := contractStr
		if logContract == "" {
			logContract = "all"
		}
		s.logger.Error("failed to get proposals",
			zap.String("contract", logContract),
			zap.Error(err))
		return nil, err
	}

	var nodes []map[string]interface{}
	for _, proposal := range proposals {
		// Apply proposer filter if specified
		if hasProposerFilter && proposal.Proposer != proposer {
			continue
		}
		nodes = append(nodes, s.proposalToMap(proposal))
	}

	return map[string]interface{}{
		"nodes":      nodes,
		"totalCount": len(nodes),
		"pageInfo": map[string]interface{}{
			"hasNextPage":     len(nodes) >= limit,
			"hasPreviousPage": offset > 0,
			"startCursor":     nil,
			"endCursor":       nil,
		},
	}, nil
}

// resolveProposal resolves a specific proposal by ID
func (s *Schema) resolveProposal(p gql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	contractStr, ok := p.Args["contract"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid contract address")
	}

	proposalIdStr, ok := p.Args["proposalId"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid proposal ID")
	}

	contract := common.HexToAddress(contractStr)
	proposalId, success := new(big.Int).SetString(proposalIdStr, 10)
	if !success {
		return nil, fmt.Errorf("invalid proposal ID format")
	}

	reader := s.storage
	if reader == nil {
		return nil, fmt.Errorf("storage does not implement SystemContractReader")
	}

	proposal, err := reader.GetProposalById(ctx, contract, proposalId)
	if err != nil {
		s.logger.Error("failed to get proposal",
			zap.String("contract", contractStr),
			zap.String("proposalId", proposalIdStr),
			zap.Error(err))
		return nil, err
	}

	if proposal == nil {
		return nil, nil
	}

	return s.proposalToMap(proposal), nil
}

// resolveProposalVotes resolves votes for a specific proposal
func (s *Schema) resolveProposalVotes(p gql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	contractStr, ok := p.Args["contract"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid contract address")
	}

	proposalIdStr, ok := p.Args["proposalId"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid proposal ID")
	}

	contract := common.HexToAddress(contractStr)
	proposalId, success := new(big.Int).SetString(proposalIdStr, 10)
	if !success {
		return nil, fmt.Errorf("invalid proposal ID format")
	}

	reader := s.storage
	if reader == nil {
		return nil, fmt.Errorf("storage does not implement SystemContractReader")
	}

	votes, err := reader.GetProposalVotes(ctx, contract, proposalId)
	if err != nil {
		s.logger.Error("failed to get proposal votes",
			zap.String("contract", contractStr),
			zap.String("proposalId", proposalIdStr),
			zap.Error(err))
		return nil, err
	}

	var result []map[string]interface{}
	for _, vote := range votes {
		result = append(result, s.proposalVoteToMap(vote))
	}

	return result, nil
}

// Helper function to convert Proposal to map
func (s *Schema) proposalToMap(proposal *sc.Proposal) map[string]interface{} {
	m := map[string]interface{}{
		"contract":          proposal.Contract.Hex(),
		"proposalId":        proposal.ProposalID.String(),
		"proposer":          proposal.Proposer.Hex(),
		"actionType":        common.Bytes2Hex(proposal.ActionType[:]),
		"callData":          common.Bytes2Hex(proposal.CallData),
		"memberVersion":     proposal.MemberVersion.String(),
		"requiredApprovals": int(proposal.RequiredApprovals),
		"approved":          int(proposal.Approved),
		"rejected":          int(proposal.Rejected),
		"status":            proposalStatusEnum(proposal.Status),
		"createdAt":         fmt.Sprintf("%d", proposal.CreatedAt),
		"blockNumber":       fmt.Sprintf("%d", proposal.BlockNumber),
		"transactionHash":   proposal.TxHash.Hex(),
	}

	if proposal.ExecutedAt != nil {
		m["executedAt"] = fmt.Sprintf("%d", *proposal.ExecutedAt)
	} else {
		m["executedAt"] = nil
	}

	return m
}

// Helper function to convert ProposalVote to map
func (s *Schema) proposalVoteToMap(vote *sc.ProposalVote) map[string]interface{} {
	return map[string]interface{}{
		"contract":        vote.Contract.Hex(),
		"proposalId":      vote.ProposalID.String(),
		"voter":           vote.Voter.Hex(),
		"approval":        vote.Approval,
		"blockNumber":     fmt.Sprintf("%d", vote.BlockNumber),
		"transactionHash": vote.TxHash.Hex(),
		"timestamp":       fmt.Sprintf("%d", vote.Timestamp),
	}
}

// Helper function to parse ProposalStatus from string
func parseProposalStatusEnum(statusStr string) sc.ProposalStatus {
	switch statusStr {
	case "NONE":
		return sc.ProposalStatusNone
	case "VOTING":
		return sc.ProposalStatusVoting
	case "APPROVED":
		return sc.ProposalStatusApproved
	case "EXECUTED":
		return sc.ProposalStatusExecuted
	case "CANCELLED":
		return sc.ProposalStatusCancelled
	case "EXPIRED":
		return sc.ProposalStatusExpired
	case "FAILED":
		return sc.ProposalStatusFailed
	case "REJECTED":
		return sc.ProposalStatusRejected
	default:
		return sc.ProposalStatusNone
	}
}

// Helper function to convert ProposalStatus to string
func proposalStatusEnum(status sc.ProposalStatus) string {
	switch status {
	case sc.ProposalStatusNone:
		return "NONE"
	case sc.ProposalStatusVoting:
		return "VOTING"
	case sc.ProposalStatusApproved:
		return "APPROVED"
	case sc.ProposalStatusExecuted:
		return "EXECUTED"
	case sc.ProposalStatusCancelled:
		return "CANCELLED"
	case sc.ProposalStatusExpired:
		return "EXPIRED"
	case sc.ProposalStatusFailed:
		return "FAILED"
	case sc.ProposalStatusRejected:
		return "REJECTED"
	default:
		return "NONE"
	}
}

// resolveMintEvents resolves mint events with filtering and pagination
func (s *Schema) resolveMintEvents(p gql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	filter, ok := p.Args["filter"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid filter")
	}

	// Parse block range
	var fromBlock, toBlock uint64
	if fb, ok := filter["fromBlock"].(string); ok {
		parsed, _ := strconv.ParseUint(fb, 10, 64)
		fromBlock = parsed
	}
	if tb, ok := filter["toBlock"].(string); ok {
		parsed, _ := strconv.ParseUint(tb, 10, 64)
		toBlock = parsed
	}

	// Parse optional minter address (support both 'minter' and 'address' fields)
	var minter common.Address
	if minterStr, ok := filter["minter"].(string); ok && minterStr != "" {
		minter = common.HexToAddress(minterStr)
	} else if addressStr, ok := filter["address"].(string); ok && addressStr != "" {
		// Support 'address' as alias for 'minter'
		minter = common.HexToAddress(addressStr)
	}

	// Pagination
	limit := constants.DefaultPaginationLimit
	offset := 0
	if pagination, ok := p.Args["pagination"].(map[string]interface{}); ok {
		if l, ok := pagination["limit"].(int); ok && l > 0 {
			if l > 100 {
				limit = 100
			} else {
				limit = l
			}
		}
		if o, ok := pagination["offset"].(int); ok && o >= 0 {
			offset = o
		}
	}

	reader := s.storage
	if reader == nil {
		return nil, fmt.Errorf("storage does not implement SystemContractReader")
	}

	events, err := reader.GetMintEvents(ctx, fromBlock, toBlock, minter, limit, offset)
	if err != nil {
		s.logger.Error("failed to get mint events", zap.Error(err))
		return nil, err
	}

	var nodes []map[string]interface{}
	for _, event := range events {
		nodes = append(nodes, s.mintEventToMap(event))
	}

	return map[string]interface{}{
		"nodes":      nodes,
		"totalCount": len(nodes),
		"pageInfo": map[string]interface{}{
			"hasNextPage":     len(nodes) >= limit,
			"hasPreviousPage": offset > 0,
			"startCursor":     nil,
			"endCursor":       nil,
		},
	}, nil
}

// resolveBurnEvents resolves burn events with filtering and pagination
func (s *Schema) resolveBurnEvents(p gql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	filter, ok := p.Args["filter"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid filter")
	}

	// Parse block range
	var fromBlock, toBlock uint64
	if fb, ok := filter["fromBlock"].(string); ok {
		parsed, _ := strconv.ParseUint(fb, 10, 64)
		fromBlock = parsed
	}
	if tb, ok := filter["toBlock"].(string); ok {
		parsed, _ := strconv.ParseUint(tb, 10, 64)
		toBlock = parsed
	}

	// Parse optional burner address (support both 'burner' and 'address' fields)
	var burner common.Address
	if burnerStr, ok := filter["burner"].(string); ok && burnerStr != "" {
		burner = common.HexToAddress(burnerStr)
	} else if addressStr, ok := filter["address"].(string); ok && addressStr != "" {
		// Support 'address' as alias for 'burner'
		burner = common.HexToAddress(addressStr)
	}

	// Pagination
	limit := constants.DefaultPaginationLimit
	offset := 0
	if pagination, ok := p.Args["pagination"].(map[string]interface{}); ok {
		if l, ok := pagination["limit"].(int); ok && l > 0 {
			if l > 100 {
				limit = 100
			} else {
				limit = l
			}
		}
		if o, ok := pagination["offset"].(int); ok && o >= 0 {
			offset = o
		}
	}

	reader := s.storage
	if reader == nil {
		return nil, fmt.Errorf("storage does not implement SystemContractReader")
	}

	events, err := reader.GetBurnEvents(ctx, fromBlock, toBlock, burner, limit, offset)
	if err != nil {
		s.logger.Error("failed to get burn events", zap.Error(err))
		return nil, err
	}

	var nodes []map[string]interface{}
	for _, event := range events {
		nodes = append(nodes, s.burnEventToMap(event))
	}

	return map[string]interface{}{
		"nodes":      nodes,
		"totalCount": len(nodes),
		"pageInfo": map[string]interface{}{
			"hasNextPage":     len(nodes) >= limit,
			"hasPreviousPage": offset > 0,
			"startCursor":     nil,
			"endCursor":       nil,
		},
	}, nil
}

// resolveMinterHistory resolves minter configuration history
func (s *Schema) resolveMinterHistory(p gql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	minterStr, ok := p.Args["minter"].(string)
	if !ok {
		return nil, fmt.Errorf("minter address is required")
	}
	minter := common.HexToAddress(minterStr)

	reader := s.storage
	if reader == nil {
		return nil, fmt.Errorf("storage does not implement SystemContractReader")
	}

	events, err := reader.GetMinterHistory(ctx, minter)
	if err != nil {
		s.logger.Error("failed to get minter history", zap.String("minter", minterStr), zap.Error(err))
		return nil, err
	}

	var result []map[string]interface{}
	for _, event := range events {
		result = append(result, s.minterConfigEventToMap(event))
	}

	return result, nil
}

// resolveValidatorHistory resolves validator change history
func (s *Schema) resolveValidatorHistory(p gql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	validatorStr, ok := p.Args["validator"].(string)
	if !ok {
		return nil, fmt.Errorf("validator address is required")
	}
	validator := common.HexToAddress(validatorStr)

	reader := s.storage
	if reader == nil {
		return nil, fmt.Errorf("storage does not implement SystemContractReader")
	}

	events, err := reader.GetValidatorHistory(ctx, validator)
	if err != nil {
		s.logger.Error("failed to get validator history", zap.String("validator", validatorStr), zap.Error(err))
		return nil, err
	}

	var result []map[string]interface{}
	for _, event := range events {
		result = append(result, s.validatorChangeEventToMap(event))
	}

	return result, nil
}

// resolveGasTipHistory resolves gas tip update history
func (s *Schema) resolveGasTipHistory(p gql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	filter, ok := p.Args["filter"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid filter")
	}

	// Parse block range
	var fromBlock, toBlock uint64
	if fb, ok := filter["fromBlock"].(string); ok {
		parsed, _ := strconv.ParseUint(fb, 10, 64)
		fromBlock = parsed
	}
	if tb, ok := filter["toBlock"].(string); ok {
		parsed, _ := strconv.ParseUint(tb, 10, 64)
		toBlock = parsed
	}

	reader := s.storage
	if reader == nil {
		return nil, fmt.Errorf("storage does not implement SystemContractReader")
	}

	events, err := reader.GetGasTipHistory(ctx, fromBlock, toBlock)
	if err != nil {
		s.logger.Error("failed to get gas tip history", zap.Error(err))
		return nil, err
	}

	var result []map[string]interface{}
	for _, event := range events {
		result = append(result, s.gasTipUpdateEventToMap(event))
	}

	return result, nil
}

// resolveBlacklistHistory resolves blacklist change history
func (s *Schema) resolveBlacklistHistory(p gql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	addressStr, ok := p.Args["address"].(string)
	if !ok {
		return nil, fmt.Errorf("address is required")
	}
	address := common.HexToAddress(addressStr)

	reader := s.storage
	if reader == nil {
		return nil, fmt.Errorf("storage does not implement SystemContractReader")
	}

	events, err := reader.GetBlacklistHistory(ctx, address)
	if err != nil {
		s.logger.Error("failed to get blacklist history", zap.String("address", addressStr), zap.Error(err))
		return nil, err
	}

	var result []map[string]interface{}
	for _, event := range events {
		result = append(result, s.blacklistEventToMap(event))
	}

	return result, nil
}

// resolveMemberHistory resolves member change history
func (s *Schema) resolveMemberHistory(p gql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	contractStr, ok := p.Args["contract"].(string)
	if !ok {
		return nil, fmt.Errorf("contract address is required")
	}
	contract := common.HexToAddress(contractStr)

	reader := s.storage
	if reader == nil {
		return nil, fmt.Errorf("storage does not implement SystemContractReader")
	}

	events, err := reader.GetMemberHistory(ctx, contract)
	if err != nil {
		s.logger.Error("failed to get member history", zap.String("contract", contractStr), zap.Error(err))
		return nil, err
	}

	var result []map[string]interface{}
	for _, event := range events {
		result = append(result, s.memberChangeEventToMap(event))
	}

	return result, nil
}

// resolveEmergencyPauseHistory resolves emergency pause history
func (s *Schema) resolveEmergencyPauseHistory(p gql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	contractStr, ok := p.Args["contract"].(string)
	if !ok {
		return nil, fmt.Errorf("contract address is required")
	}
	contract := common.HexToAddress(contractStr)

	reader := s.storage
	if reader == nil {
		return nil, fmt.Errorf("storage does not implement SystemContractReader")
	}

	events, err := reader.GetEmergencyPauseHistory(ctx, contract)
	if err != nil {
		s.logger.Error("failed to get emergency pause history", zap.String("contract", contractStr), zap.Error(err))
		return nil, err
	}

	var result []map[string]interface{}
	for _, event := range events {
		result = append(result, s.emergencyPauseEventToMap(event))
	}

	return result, nil
}

// resolveDepositMintProposals resolves deposit mint proposals
func (s *Schema) resolveDepositMintProposals(p gql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	filter, ok := p.Args["filter"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid filter")
	}

	// Parse block range
	var fromBlock, toBlock uint64
	if fb, ok := filter["fromBlock"].(string); ok {
		parsed, _ := strconv.ParseUint(fb, 10, 64)
		fromBlock = parsed
	}
	if tb, ok := filter["toBlock"].(string); ok {
		parsed, _ := strconv.ParseUint(tb, 10, 64)
		toBlock = parsed
	}

	// Parse optional status filter
	status := sc.ProposalStatusNone
	if statusStr, ok := filter["status"].(string); ok && statusStr != "" {
		status = parseProposalStatusEnum(statusStr)
	}

	reader := s.storage
	if reader == nil {
		return nil, fmt.Errorf("storage does not implement SystemContractReader")
	}

	proposals, err := reader.GetDepositMintProposals(ctx, fromBlock, toBlock, status)
	if err != nil {
		s.logger.Error("failed to get deposit mint proposals", zap.Error(err))
		return nil, err
	}

	var result []map[string]interface{}
	for _, proposal := range proposals {
		result = append(result, s.depositMintProposalToMap(proposal))
	}

	return result, nil
}

// Phase 2.3: Add missing system contract query resolvers

// resolveMinterConfigHistory resolves minter configuration change history across all minters
func (s *Schema) resolveMinterConfigHistory(p gql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	filter, ok := p.Args["filter"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid filter")
	}

	// Parse block range
	var fromBlock, toBlock uint64
	if fb, ok := filter["fromBlock"].(string); ok {
		parsed, _ := strconv.ParseUint(fb, 10, 64)
		fromBlock = parsed
	}
	if tb, ok := filter["toBlock"].(string); ok {
		parsed, _ := strconv.ParseUint(tb, 10, 64)
		toBlock = parsed
	}

	reader := s.storage
	if reader == nil {
		return nil, fmt.Errorf("storage does not implement SystemContractReader")
	}

	events, err := reader.GetMinterConfigHistory(ctx, fromBlock, toBlock)
	if err != nil {
		s.logger.Error("failed to get minter config history",
			zap.Uint64("fromBlock", fromBlock),
			zap.Uint64("toBlock", toBlock),
			zap.Error(err))
		return nil, err
	}

	var result []map[string]interface{}
	for _, event := range events {
		result = append(result, s.minterConfigEventToMap(event))
	}

	return result, nil
}

// resolveAuthorizedAccounts resolves list of authorized accounts from GovCouncil
func (s *Schema) resolveAuthorizedAccounts(p gql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	reader := s.storage
	if reader == nil {
		return nil, fmt.Errorf("storage does not implement SystemContractReader")
	}

	accounts, err := reader.GetAuthorizedAccounts(ctx)
	if err != nil {
		s.logger.Error("failed to get authorized accounts", zap.Error(err))
		return nil, err
	}

	// Convert addresses to hex strings
	var result []string
	for _, account := range accounts {
		result = append(result, account.Hex())
	}

	return result, nil
}

// Helper function to convert MintEvent to map
func (s *Schema) mintEventToMap(event *sc.MintEvent) map[string]interface{} {
	return map[string]interface{}{
		"blockNumber":     fmt.Sprintf("%d", event.BlockNumber),
		"transactionHash": event.TxHash.Hex(),
		"minter":          event.Minter.Hex(),
		"to":              event.To.Hex(),
		"amount":          event.Amount.String(),
		"timestamp":       fmt.Sprintf("%d", event.Timestamp),
	}
}

// Helper function to convert BurnEvent to map
func (s *Schema) burnEventToMap(event *sc.BurnEvent) map[string]interface{} {
	m := map[string]interface{}{
		"blockNumber":     fmt.Sprintf("%d", event.BlockNumber),
		"transactionHash": event.TxHash.Hex(),
		"burner":          event.Burner.Hex(),
		"amount":          event.Amount.String(),
		"timestamp":       fmt.Sprintf("%d", event.Timestamp),
	}
	if event.WithdrawalID != "" {
		m["withdrawalId"] = event.WithdrawalID
	}
	return m
}

// Helper function to convert MinterConfigEvent to map
func (s *Schema) minterConfigEventToMap(event *sc.MinterConfigEvent) map[string]interface{} {
	m := map[string]interface{}{
		"blockNumber":     fmt.Sprintf("%d", event.BlockNumber),
		"transactionHash": event.TxHash.Hex(),
		"minter":          event.Minter.Hex(),
		"contract":        nil,
		"allowance":       event.Allowance.String(),
		"action":          event.Action,
		"timestamp":       fmt.Sprintf("%d", event.Timestamp),
	}
	if event.Contract != (common.Address{}) {
		m["contract"] = event.Contract.Hex()
	}
	return m
}

// Helper function to convert ValidatorChangeEvent to map
func (s *Schema) validatorChangeEventToMap(event *sc.ValidatorChangeEvent) map[string]interface{} {
	m := map[string]interface{}{
		"blockNumber":     fmt.Sprintf("%d", event.BlockNumber),
		"transactionHash": event.TxHash.Hex(),
		"validator":       event.Validator.Hex(),
		"action":          event.Action,
		"timestamp":       fmt.Sprintf("%d", event.Timestamp),
	}
	if event.OldValidator != nil {
		m["oldValidator"] = event.OldValidator.Hex()
	}
	return m
}

// Helper function to convert GasTipUpdateEvent to map
func (s *Schema) gasTipUpdateEventToMap(event *sc.GasTipUpdateEvent) map[string]interface{} {
	return map[string]interface{}{
		"blockNumber":     fmt.Sprintf("%d", event.BlockNumber),
		"transactionHash": event.TxHash.Hex(),
		"oldTip":          event.OldTip.String(),
		"newTip":          event.NewTip.String(),
		"updater":         event.Updater.Hex(),
		"timestamp":       fmt.Sprintf("%d", event.Timestamp),
	}
}

// Helper function to convert BlacklistEvent to map
func (s *Schema) blacklistEventToMap(event *sc.BlacklistEvent) map[string]interface{} {
	return map[string]interface{}{
		"blockNumber":     fmt.Sprintf("%d", event.BlockNumber),
		"transactionHash": event.TxHash.Hex(),
		"account":         event.Account.Hex(),
		"action":          event.Action,
		"proposalId":      event.ProposalID.String(),
		"timestamp":       fmt.Sprintf("%d", event.Timestamp),
	}
}

// Helper function to convert MemberChangeEvent to map
func (s *Schema) memberChangeEventToMap(event *sc.MemberChangeEvent) map[string]interface{} {
	m := map[string]interface{}{
		"contract":        event.Contract.Hex(),
		"blockNumber":     fmt.Sprintf("%d", event.BlockNumber),
		"transactionHash": event.TxHash.Hex(),
		"member":          event.Member.Hex(),
		"action":          event.Action,
		"totalMembers":    fmt.Sprintf("%d", event.TotalMembers),
		"newQuorum":       int(event.NewQuorum),
		"timestamp":       fmt.Sprintf("%d", event.Timestamp),
	}
	if event.OldMember != nil {
		m["oldMember"] = event.OldMember.Hex()
	}
	return m
}

// Helper function to convert EmergencyPauseEvent to map
func (s *Schema) emergencyPauseEventToMap(event *sc.EmergencyPauseEvent) map[string]interface{} {
	return map[string]interface{}{
		"contract":        event.Contract.Hex(),
		"blockNumber":     fmt.Sprintf("%d", event.BlockNumber),
		"transactionHash": event.TxHash.Hex(),
		"proposalId":      event.ProposalID.String(),
		"action":          event.Action,
		"timestamp":       fmt.Sprintf("%d", event.Timestamp),
	}
}

// Helper function to convert DepositMintProposal to map
func (s *Schema) depositMintProposalToMap(proposal *sc.DepositMintProposal) map[string]interface{} {
	return map[string]interface{}{
		"proposalId":      proposal.ProposalID.String(),
		"requester":       proposal.Requester.Hex(),
		"beneficiary":     proposal.Beneficiary.Hex(),
		"amount":          proposal.Amount.String(),
		"depositId":       proposal.DepositID,
		"bankReference":   proposal.BankReference,
		"status":          proposalStatusEnum(proposal.Status),
		"blockNumber":     fmt.Sprintf("%d", proposal.BlockNumber),
		"transactionHash": proposal.TxHash.Hex(),
		"timestamp":       fmt.Sprintf("%d", proposal.Timestamp),
	}
}

// resolveMaxProposalsUpdateHistory resolves max proposals per member update history
func (s *Schema) resolveMaxProposalsUpdateHistory(p gql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	contractStr, ok := p.Args["contract"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid contract address")
	}

	contract := common.HexToAddress(contractStr)

	reader := s.storage
	if reader == nil {
		return nil, fmt.Errorf("storage does not implement SystemContractReader")
	}

	events, err := reader.GetMaxProposalsUpdateHistory(ctx, contract)
	if err != nil {
		s.logger.Error("failed to get max proposals update history",
			zap.String("contract", contractStr),
			zap.Error(err))
		return nil, err
	}

	var result []map[string]interface{}
	for _, event := range events {
		result = append(result, map[string]interface{}{
			"contract":        event.Contract.Hex(),
			"blockNumber":     fmt.Sprintf("%d", event.BlockNumber),
			"transactionHash": event.TxHash.Hex(),
			"oldMax":          int(event.OldMax),
			"newMax":          int(event.NewMax),
			"timestamp":       fmt.Sprintf("%d", event.Timestamp),
		})
	}

	if result == nil {
		result = []map[string]interface{}{}
	}

	return result, nil
}

// resolveProposalExecutionSkippedEvents resolves proposal execution skipped events
func (s *Schema) resolveProposalExecutionSkippedEvents(p gql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	contractStr, ok := p.Args["contract"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid contract address")
	}

	contract := common.HexToAddress(contractStr)

	var proposalID *big.Int
	if pidStr, ok := p.Args["proposalId"].(string); ok && pidStr != "" {
		proposalID = new(big.Int)
		proposalID.SetString(pidStr, 10)
	}

	reader := s.storage
	if reader == nil {
		return nil, fmt.Errorf("storage does not implement SystemContractReader")
	}

	events, err := reader.GetProposalExecutionSkippedEvents(ctx, contract, proposalID)
	if err != nil {
		s.logger.Error("failed to get proposal execution skipped events",
			zap.String("contract", contractStr),
			zap.Error(err))
		return nil, err
	}

	var result []map[string]interface{}
	for _, event := range events {
		pidStr := "0"
		if event.ProposalID != nil {
			pidStr = event.ProposalID.String()
		}
		result = append(result, map[string]interface{}{
			"contract":        event.Contract.Hex(),
			"blockNumber":     fmt.Sprintf("%d", event.BlockNumber),
			"transactionHash": event.TxHash.Hex(),
			"account":         event.Account.Hex(),
			"proposalId":      pidStr,
			"reason":          event.Reason,
			"timestamp":       fmt.Sprintf("%d", event.Timestamp),
		})
	}

	if result == nil {
		result = []map[string]interface{}{}
	}

	return result, nil
}
