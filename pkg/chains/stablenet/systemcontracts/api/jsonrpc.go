package api

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/0xmhha/indexer-go/pkg/api/jsonrpc"
	sc "github.com/0xmhha/indexer-go/pkg/chains/stablenet/systemcontracts"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

// getTotalSupply returns the current total supply of native coins
func (h *rpcHandler) getTotalSupply(ctx context.Context, params json.RawMessage) (interface{}, *jsonrpc.Error) {
	reader := h.storage
	if reader == nil {
		return nil, jsonrpc.NewError(jsonrpc.InternalError, "system contract reader not available", nil)
	}

	supply, err := reader.GetTotalSupply(ctx)
	if err != nil {
		h.logger.Error("failed to get total supply", zap.Error(err))
		return nil, jsonrpc.NewError(jsonrpc.InternalError, "failed to get total supply", err.Error())
	}

	return map[string]interface{}{
		"totalSupply": supply.String(),
	}, nil
}

// getActiveMinters returns all active minters and their allowances
func (h *rpcHandler) getActiveMinters(ctx context.Context, params json.RawMessage) (interface{}, *jsonrpc.Error) {
	reader := h.storage
	if reader == nil {
		return nil, jsonrpc.NewError(jsonrpc.InternalError, "system contract reader not available", nil)
	}

	minters, err := reader.GetActiveMinters(ctx)
	if err != nil {
		h.logger.Error("failed to get active minters", zap.Error(err))
		return nil, jsonrpc.NewError(jsonrpc.InternalError, "failed to get active minters", err.Error())
	}

	result := make([]map[string]interface{}, 0, len(minters))
	for _, minter := range minters {
		allowance, err := reader.GetMinterAllowance(ctx, minter)
		if err != nil {
			h.logger.Warn("failed to get minter allowance", zap.String("minter", minter.Hex()), zap.Error(err))
			continue
		}

		result = append(result, map[string]interface{}{
			"address":   minter.Hex(),
			"allowance": allowance.String(),
			"isActive":  true,
		})
	}

	return map[string]interface{}{
		"minters": result,
	}, nil
}

// getMinterAllowance returns the allowance for a specific minter
func (h *rpcHandler) getMinterAllowance(ctx context.Context, params json.RawMessage) (interface{}, *jsonrpc.Error) {
	var p struct {
		Minter string `json:"minter"`
	}

	if err := json.Unmarshal(params, &p); err != nil {
		return nil, jsonrpc.NewError(jsonrpc.InvalidParams, "invalid params", err.Error())
	}

	if p.Minter == "" {
		return nil, jsonrpc.NewError(jsonrpc.InvalidParams, "missing required parameter: minter", nil)
	}

	reader := h.storage
	if reader == nil {
		return nil, jsonrpc.NewError(jsonrpc.InternalError, "system contract reader not available", nil)
	}

	minter := common.HexToAddress(p.Minter)
	allowance, err := reader.GetMinterAllowance(ctx, minter)
	if err != nil {
		h.logger.Error("failed to get minter allowance", zap.String("minter", p.Minter), zap.Error(err))
		return nil, jsonrpc.NewError(jsonrpc.InternalError, "failed to get minter allowance", err.Error())
	}

	return map[string]interface{}{
		"minter":    p.Minter,
		"allowance": allowance.String(),
	}, nil
}

// getActiveValidators returns all active validators
func (h *rpcHandler) getActiveValidators(ctx context.Context, params json.RawMessage) (interface{}, *jsonrpc.Error) {
	reader := h.storage
	if reader == nil {
		return nil, jsonrpc.NewError(jsonrpc.InternalError, "system contract reader not available", nil)
	}

	validators, err := reader.GetActiveValidators(ctx)
	if err != nil {
		h.logger.Error("failed to get active validators", zap.Error(err))
		return nil, jsonrpc.NewError(jsonrpc.InternalError, "failed to get active validators", err.Error())
	}

	result := make([]map[string]interface{}, 0, len(validators))
	for _, validator := range validators {
		result = append(result, map[string]interface{}{
			"address":  validator.Hex(),
			"isActive": true,
		})
	}

	return map[string]interface{}{
		"validators": result,
	}, nil
}

// getBlacklistedAddresses returns all blacklisted addresses
func (h *rpcHandler) getBlacklistedAddresses(ctx context.Context, params json.RawMessage) (interface{}, *jsonrpc.Error) {
	reader := h.storage
	if reader == nil {
		return nil, jsonrpc.NewError(jsonrpc.InternalError, "system contract reader not available", nil)
	}

	addresses, err := reader.GetBlacklistedAddresses(ctx)
	if err != nil {
		h.logger.Error("failed to get blacklisted addresses", zap.Error(err))
		return nil, jsonrpc.NewError(jsonrpc.InternalError, "failed to get blacklisted addresses", err.Error())
	}

	result := make([]string, 0, len(addresses))
	for _, addr := range addresses {
		result = append(result, addr.Hex())
	}

	return map[string]interface{}{
		"addresses": result,
	}, nil
}

// getProposal returns a specific governance proposal
func (h *rpcHandler) getProposal(ctx context.Context, params json.RawMessage) (interface{}, *jsonrpc.Error) {
	var p struct {
		Contract   string `json:"contract"`
		ProposalID string `json:"proposalId"`
	}

	if err := json.Unmarshal(params, &p); err != nil {
		return nil, jsonrpc.NewError(jsonrpc.InvalidParams, "invalid params", err.Error())
	}

	if p.Contract == "" {
		return nil, jsonrpc.NewError(jsonrpc.InvalidParams, "missing required parameter: contract", nil)
	}
	if p.ProposalID == "" {
		return nil, jsonrpc.NewError(jsonrpc.InvalidParams, "missing required parameter: proposalId", nil)
	}

	reader := h.storage
	if reader == nil {
		return nil, jsonrpc.NewError(jsonrpc.InternalError, "system contract reader not available", nil)
	}

	contract := common.HexToAddress(p.Contract)
	proposalID, ok := new(big.Int).SetString(p.ProposalID, 10)
	if !ok {
		return nil, jsonrpc.NewError(jsonrpc.InvalidParams, "invalid proposal ID format", nil)
	}

	proposal, err := reader.GetProposalById(ctx, contract, proposalID)
	if err != nil {
		h.logger.Error("failed to get proposal",
			zap.String("contract", p.Contract),
			zap.String("proposalId", p.ProposalID),
			zap.Error(err))
		return nil, jsonrpc.NewError(jsonrpc.InternalError, "failed to get proposal", err.Error())
	}

	if proposal == nil {
		return nil, jsonrpc.NewError(jsonrpc.InternalError, "proposal not found", nil)
	}

	return h.proposalToJSON(proposal), nil
}

// getProposals returns governance proposals with filtering
func (h *rpcHandler) getProposals(ctx context.Context, params json.RawMessage) (interface{}, *jsonrpc.Error) {
	var p struct {
		Contract string `json:"contract"`
		Status   string `json:"status,omitempty"`
		Limit    int    `json:"limit,omitempty"`
		Offset   int    `json:"offset,omitempty"`
	}

	if err := json.Unmarshal(params, &p); err != nil {
		return nil, jsonrpc.NewError(jsonrpc.InvalidParams, "invalid params", err.Error())
	}

	if p.Contract == "" {
		return nil, jsonrpc.NewError(jsonrpc.InvalidParams, "missing required parameter: contract", nil)
	}

	// Default pagination
	if p.Limit <= 0 {
		p.Limit = 10
	}
	if p.Limit > 100 {
		p.Limit = 100
	}

	reader := h.storage
	if reader == nil {
		return nil, jsonrpc.NewError(jsonrpc.InternalError, "system contract reader not available", nil)
	}

	contract := common.HexToAddress(p.Contract)
	status := parseProposalStatus(p.Status)

	proposals, err := reader.GetProposals(ctx, contract, status, p.Limit, p.Offset)
	if err != nil {
		h.logger.Error("failed to get proposals",
			zap.String("contract", p.Contract),
			zap.String("status", p.Status),
			zap.Error(err))
		return nil, jsonrpc.NewError(jsonrpc.InternalError, "failed to get proposals", err.Error())
	}

	result := make([]map[string]interface{}, 0, len(proposals))
	for _, proposal := range proposals {
		result = append(result, h.proposalToJSON(proposal))
	}

	return map[string]interface{}{
		"proposals":  result,
		"totalCount": len(result),
	}, nil
}

// getProposalVotes returns votes for a specific proposal
func (h *rpcHandler) getProposalVotes(ctx context.Context, params json.RawMessage) (interface{}, *jsonrpc.Error) {
	var p struct {
		Contract   string `json:"contract"`
		ProposalID string `json:"proposalId"`
	}

	if err := json.Unmarshal(params, &p); err != nil {
		return nil, jsonrpc.NewError(jsonrpc.InvalidParams, "invalid params", err.Error())
	}

	if p.Contract == "" {
		return nil, jsonrpc.NewError(jsonrpc.InvalidParams, "missing required parameter: contract", nil)
	}
	if p.ProposalID == "" {
		return nil, jsonrpc.NewError(jsonrpc.InvalidParams, "missing required parameter: proposalId", nil)
	}

	reader := h.storage
	if reader == nil {
		return nil, jsonrpc.NewError(jsonrpc.InternalError, "system contract reader not available", nil)
	}

	contract := common.HexToAddress(p.Contract)
	proposalID, ok := new(big.Int).SetString(p.ProposalID, 10)
	if !ok {
		return nil, jsonrpc.NewError(jsonrpc.InvalidParams, "invalid proposal ID format", nil)
	}

	votes, err := reader.GetProposalVotes(ctx, contract, proposalID)
	if err != nil {
		h.logger.Error("failed to get proposal votes",
			zap.String("contract", p.Contract),
			zap.String("proposalId", p.ProposalID),
			zap.Error(err))
		return nil, jsonrpc.NewError(jsonrpc.InternalError, "failed to get proposal votes", err.Error())
	}

	result := make([]map[string]interface{}, 0, len(votes))
	for _, vote := range votes {
		result = append(result, map[string]interface{}{
			"contract":        vote.Contract.Hex(),
			"proposalId":      vote.ProposalID.String(),
			"voter":           vote.Voter.Hex(),
			"approval":        vote.Approval,
			"blockNumber":     fmt.Sprintf("0x%x", vote.BlockNumber),
			"transactionHash": vote.TxHash.Hex(),
			"timestamp":       fmt.Sprintf("%d", vote.Timestamp),
		})
	}

	return map[string]interface{}{
		"votes": result,
	}, nil
}

// getMintEvents returns mint events with filtering
func (h *rpcHandler) getMintEvents(ctx context.Context, params json.RawMessage) (interface{}, *jsonrpc.Error) {
	var p struct {
		FromBlock uint64 `json:"fromBlock,omitempty"`
		ToBlock   uint64 `json:"toBlock,omitempty"`
		Minter    string `json:"minter,omitempty"`
		Limit     int    `json:"limit,omitempty"`
		Offset    int    `json:"offset,omitempty"`
	}

	if err := json.Unmarshal(params, &p); err != nil {
		return nil, jsonrpc.NewError(jsonrpc.InvalidParams, "invalid params", err.Error())
	}

	// Default pagination
	if p.Limit <= 0 {
		p.Limit = 10
	}
	if p.Limit > 100 {
		p.Limit = 100
	}

	reader := h.storage
	if reader == nil {
		return nil, jsonrpc.NewError(jsonrpc.InternalError, "system contract reader not available", nil)
	}

	var minter common.Address
	if p.Minter != "" {
		minter = common.HexToAddress(p.Minter)
	}

	events, err := reader.GetMintEvents(ctx, p.FromBlock, p.ToBlock, minter, p.Limit, p.Offset)
	if err != nil {
		h.logger.Error("failed to get mint events", zap.Error(err))
		return nil, jsonrpc.NewError(jsonrpc.InternalError, "failed to get mint events", err.Error())
	}

	result := make([]map[string]interface{}, 0, len(events))
	for _, event := range events {
		result = append(result, map[string]interface{}{
			"blockNumber":     fmt.Sprintf("0x%x", event.BlockNumber),
			"transactionHash": event.TxHash.Hex(),
			"minter":          event.Minter.Hex(),
			"to":              event.To.Hex(),
			"amount":          event.Amount.String(),
			"timestamp":       fmt.Sprintf("%d", event.Timestamp),
		})
	}

	return map[string]interface{}{
		"events":     result,
		"totalCount": len(result),
	}, nil
}

// getBurnEvents returns burn events with filtering
func (h *rpcHandler) getBurnEvents(ctx context.Context, params json.RawMessage) (interface{}, *jsonrpc.Error) {
	var p struct {
		FromBlock uint64 `json:"fromBlock,omitempty"`
		ToBlock   uint64 `json:"toBlock,omitempty"`
		Burner    string `json:"burner,omitempty"`
		Limit     int    `json:"limit,omitempty"`
		Offset    int    `json:"offset,omitempty"`
	}

	if err := json.Unmarshal(params, &p); err != nil {
		return nil, jsonrpc.NewError(jsonrpc.InvalidParams, "invalid params", err.Error())
	}

	// Default pagination
	if p.Limit <= 0 {
		p.Limit = 10
	}
	if p.Limit > 100 {
		p.Limit = 100
	}

	reader := h.storage
	if reader == nil {
		return nil, jsonrpc.NewError(jsonrpc.InternalError, "system contract reader not available", nil)
	}

	var burner common.Address
	if p.Burner != "" {
		burner = common.HexToAddress(p.Burner)
	}

	events, err := reader.GetBurnEvents(ctx, p.FromBlock, p.ToBlock, burner, p.Limit, p.Offset)
	if err != nil {
		h.logger.Error("failed to get burn events", zap.Error(err))
		return nil, jsonrpc.NewError(jsonrpc.InternalError, "failed to get burn events", err.Error())
	}

	result := make([]map[string]interface{}, 0, len(events))
	for _, event := range events {
		e := map[string]interface{}{
			"blockNumber":     fmt.Sprintf("0x%x", event.BlockNumber),
			"transactionHash": event.TxHash.Hex(),
			"burner":          event.Burner.Hex(),
			"amount":          event.Amount.String(),
			"timestamp":       fmt.Sprintf("%d", event.Timestamp),
		}
		if event.WithdrawalID != "" {
			e["withdrawalId"] = event.WithdrawalID
		}
		result = append(result, e)
	}

	return map[string]interface{}{
		"events":     result,
		"totalCount": len(result),
	}, nil
}

// Helper function to convert Proposal to JSON
func (h *rpcHandler) proposalToJSON(proposal *sc.Proposal) map[string]interface{} {
	result := map[string]interface{}{
		"contract":          proposal.Contract.Hex(),
		"proposalId":        proposal.ProposalID.String(),
		"proposer":          proposal.Proposer.Hex(),
		"actionType":        fmt.Sprintf("0x%x", proposal.ActionType[:]),
		"callData":          fmt.Sprintf("0x%x", proposal.CallData),
		"memberVersion":     proposal.MemberVersion.String(),
		"requiredApprovals": proposal.RequiredApprovals,
		"approved":          proposal.Approved,
		"rejected":          proposal.Rejected,
		"status":            proposalStatusToString(proposal.Status),
		"createdAt":         fmt.Sprintf("%d", proposal.CreatedAt),
		"blockNumber":       fmt.Sprintf("0x%x", proposal.BlockNumber),
		"transactionHash":   proposal.TxHash.Hex(),
	}

	if proposal.ExecutedAt != nil {
		result["executedAt"] = fmt.Sprintf("%d", *proposal.ExecutedAt)
	}

	return result
}

// Helper function to parse ProposalStatus from string
func parseProposalStatus(statusStr string) sc.ProposalStatus {
	switch statusStr {
	case "none", "NONE":
		return sc.ProposalStatusNone
	case "voting", "VOTING":
		return sc.ProposalStatusVoting
	case "approved", "APPROVED":
		return sc.ProposalStatusApproved
	case "executed", "EXECUTED":
		return sc.ProposalStatusExecuted
	case "cancelled", "CANCELLED":
		return sc.ProposalStatusCancelled
	case "expired", "EXPIRED":
		return sc.ProposalStatusExpired
	case "failed", "FAILED":
		return sc.ProposalStatusFailed
	case "rejected", "REJECTED":
		return sc.ProposalStatusRejected
	default:
		return sc.ProposalStatusNone
	}
}

// Helper function to convert ProposalStatus to string
func proposalStatusToString(status sc.ProposalStatus) string {
	switch status {
	case sc.ProposalStatusNone:
		return "none"
	case sc.ProposalStatusVoting:
		return "voting"
	case sc.ProposalStatusApproved:
		return "approved"
	case sc.ProposalStatusExecuted:
		return "executed"
	case sc.ProposalStatusCancelled:
		return "cancelled"
	case sc.ProposalStatusExpired:
		return "expired"
	case sc.ProposalStatusFailed:
		return "failed"
	case sc.ProposalStatusRejected:
		return "rejected"
	default:
		return "none"
	}
}

// rpcHandler serves the system contract JSON-RPC methods. storage is nil
// when the indexer's storage cannot hold system contract data; the methods
// then fail.
type rpcHandler struct {
	storage sc.SystemContractReader
	logger  *zap.Logger
}

func newRPCHandler(d jsonrpc.MethodDeps) *rpcHandler {
	h := &rpcHandler{logger: d.Logger}
	if h.logger == nil {
		h.logger = zap.NewNop()
	}
	h.storage = readerOf(d.Storage, h.logger)
	return h
}

func registerMethods() {
	for name, method := range map[string]func(*rpcHandler, context.Context, json.RawMessage) (interface{}, *jsonrpc.Error){
		"getTotalSupply":          (*rpcHandler).getTotalSupply,
		"getActiveMinters":        (*rpcHandler).getActiveMinters,
		"getMinterAllowance":      (*rpcHandler).getMinterAllowance,
		"getActiveValidators":     (*rpcHandler).getActiveValidators,
		"getBlacklistedAddresses": (*rpcHandler).getBlacklistedAddresses,
		"getProposal":             (*rpcHandler).getProposal,
		"getProposals":            (*rpcHandler).getProposals,
		"getProposalVotes":        (*rpcHandler).getProposalVotes,
		"getMintEvents":           (*rpcHandler).getMintEvents,
		"getBurnEvents":           (*rpcHandler).getBurnEvents,
	} {
		jsonrpc.RegisterMethod(name, func(ctx context.Context, d jsonrpc.MethodDeps, params json.RawMessage) (interface{}, *jsonrpc.Error) {
			return method(newRPCHandler(d), ctx, params)
		})
	}
}
