package etherscan

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/0xmhha/indexer-go/pkg/compiler"
	"github.com/0xmhha/indexer-go/pkg/storage"
	"github.com/0xmhha/indexer-go/pkg/verifier"
	"github.com/ethereum/go-ethereum/common"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Handler handles Etherscan-compatible API requests
type Handler struct {
	storage  storage.Storage
	verifier verifier.Verifier
	logger   *zap.Logger

	// Verification job tracking
	jobs   map[string]*VerificationJob
	jobsMu sync.RWMutex
}

// VerificationJob tracks the status of a verification request
type VerificationJob struct {
	GUID      string
	Address   string
	Status    string // "Pending", "Pass", "Fail"
	Message   string
	CreatedAt time.Time
}

// Response represents the Etherscan API response format
type Response struct {
	Status  string      `json:"status"`  // "1" for success, "0" for error
	Message string      `json:"message"` // "OK" or error message
	Result  interface{} `json:"result"`
}

// NewHandler creates a new Etherscan API handler
func NewHandler(store storage.Storage, v verifier.Verifier, logger *zap.Logger) *Handler {
	return &Handler{
		storage:  store,
		verifier: v,
		logger:   logger,
		jobs:     make(map[string]*VerificationJob),
	}
}

// ServeHTTP handles incoming requests
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Parse query parameters
	module := r.URL.Query().Get("module")
	action := r.URL.Query().Get("action")

	// Also check form values for POST requests
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err == nil {
			if module == "" {
				module = r.FormValue("module")
			}
			if action == "" {
				action = r.FormValue("action")
			}
		}
	}

	h.logger.Debug("etherscan API request",
		zap.String("method", r.Method),
		zap.String("module", module),
		zap.String("action", action))

	// Route to appropriate handler
	switch module {
	case "contract":
		h.handleContract(w, r, action)
	default:
		h.sendError(w, "Invalid module")
	}
}

// handleContract handles contract-related actions
func (h *Handler) handleContract(w http.ResponseWriter, r *http.Request, action string) {
	switch action {
	case "verifysourcecode":
		h.handleVerifySourceCode(w, r)
	case "checkverifystatus":
		h.handleCheckVerifyStatus(w, r)
	case "getabi":
		h.handleGetABI(w, r)
	case "getsourcecode":
		h.handleGetSourceCode(w, r)
	default:
		h.sendError(w, "Invalid action")
	}
}

// handleVerifySourceCode handles source code verification requests
// POST /api?module=contract&action=verifysourcecode
func (h *Handler) handleVerifySourceCode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.sendError(w, "POST method required")
		return
	}

	// Parse form data
	if err := r.ParseForm(); err != nil {
		h.sendError(w, "Failed to parse form data")
		return
	}

	// Extract parameters (Etherscan API compatible)
	addressStr := r.FormValue("contractaddress")
	sourceCode := r.FormValue("sourceCode")
	contractName := r.FormValue("contractname")
	compilerVersion := compiler.NormalizeVersion(r.FormValue("compilerversion"))
	optimizationUsed := r.FormValue("optimizationUsed") == "1"
	constructorArgs := r.FormValue("constructorArguments")
	licenseType := r.FormValue("licenseType")

	// Parse optimization runs (default 200)
	optimizationRuns := 200
	if runs := r.FormValue("runs"); runs != "" {
		if _, err := fmt.Sscanf(runs, "%d", &optimizationRuns); err != nil {
			h.logger.Warn("failed to parse optimization runs, using default",
				zap.String("runs", runs),
				zap.Error(err))
		}
	}

	// Validate required fields
	if addressStr == "" {
		h.sendError(w, "Missing contract address")
		return
	}
	if sourceCode == "" {
		h.sendError(w, "Missing source code")
		return
	}
	if compilerVersion == "" {
		h.sendError(w, "Missing compiler version")
		return
	}

	// Validate address format
	if !common.IsHexAddress(addressStr) {
		h.sendError(w, "Invalid contract address format")
		return
	}

	address := common.HexToAddress(addressStr)

	// Generate GUID for tracking
	guid := uuid.New().String()

	// Create job
	job := &VerificationJob{
		GUID:      guid,
		Address:   address.Hex(),
		Status:    "Pending",
		Message:   "Verification in progress",
		CreatedAt: time.Now(),
	}

	h.jobsMu.Lock()
	h.jobs[guid] = job
	h.jobsMu.Unlock()

	// Start verification in background
	go h.processVerification(job, address, sourceCode, contractName, compilerVersion, optimizationUsed, optimizationRuns, constructorArgs, licenseType)

	// Return GUID immediately (Etherscan behavior)
	h.sendSuccess(w, guid)
}

// processVerification performs the actual verification
func (h *Handler) processVerification(
	job *VerificationJob,
	address common.Address,
	sourceCode, contractName, compilerVersion string,
	optimizationUsed bool,
	optimizationRuns int,
	constructorArgs, licenseType string,
) {
	ctx := context.Background()

	// Check if verifier is available
	if h.verifier == nil {
		h.updateJobStatus(job, "Fail", "Verifier not configured")
		return
	}

	// Build verification request
	req := &verifier.VerificationRequest{
		Address:              address,
		SourceCode:           sourceCode,
		CompilerVersion:      compilerVersion,
		ContractName:         contractName,
		OptimizationEnabled:  optimizationUsed,
		OptimizationRuns:     optimizationRuns,
		ConstructorArguments: constructorArgs,
		LicenseType:          licenseType,
	}

	// Execute verification
	result, err := h.verifier.Verify(ctx, req)
	if err != nil {
		h.logger.Error("verification failed",
			zap.String("address", address.Hex()),
			zap.Error(err))
		h.updateJobStatus(job, "Fail", fmt.Sprintf("Verification failed: %v", err))
		return
	}

	if !result.Success {
		errMsg := "Bytecode mismatch"
		if result.Error != nil {
			errMsg = result.Error.Error()
		}
		h.updateJobStatus(job, "Fail", errMsg)
		return
	}

	// Store verification result
	verificationWriter, ok := h.storage.(storage.ContractVerificationWriter)
	if !ok {
		h.updateJobStatus(job, "Fail", "Storage does not support verification writes")
		return
	}

	// Extract contract name from "path/to/file.sol:ContractName" format
	storedName := contractName
	if idx := strings.LastIndex(contractName, ":"); idx != -1 {
		storedName = contractName[idx+1:]
	}

	verification := &storage.ContractVerification{
		Address:              address,
		IsVerified:           true,
		Name:                 storedName,
		CompilerVersion:      compilerVersion,
		OptimizationEnabled:  optimizationUsed,
		OptimizationRuns:     optimizationRuns,
		SourceCode:           sourceCode,
		ABI:                  result.ABI,
		ConstructorArguments: constructorArgs,
		VerifiedAt:           time.Now(),
		LicenseType:          licenseType,
	}

	if err := verificationWriter.SetContractVerification(ctx, verification); err != nil {
		h.logger.Error("failed to store verification",
			zap.String("address", address.Hex()),
			zap.Error(err))
		h.updateJobStatus(job, "Fail", "Failed to store verification result")
		return
	}

	h.logger.Info("contract verified successfully via Etherscan API",
		zap.String("address", address.Hex()),
		zap.String("compiler", compilerVersion))

	h.updateJobStatus(job, "Pass", "Contract source code verified")
}

// handleCheckVerifyStatus handles verification status check requests
// GET /api?module=contract&action=checkverifystatus&guid=xxx
func (h *Handler) handleCheckVerifyStatus(w http.ResponseWriter, r *http.Request) {
	guid := r.URL.Query().Get("guid")
	if guid == "" {
		guid = r.FormValue("guid")
	}

	if guid == "" {
		h.sendError(w, "Missing guid parameter")
		return
	}

	h.jobsMu.RLock()
	job, exists := h.jobs[guid]
	h.jobsMu.RUnlock()

	if !exists {
		h.sendError(w, "Unknown guid")
		return
	}

	// Return status in Etherscan format
	if job.Status == "Pending" {
		h.sendResponse(w, "0", "Pending in queue", job.Message)
	} else if job.Status == "Pass" {
		h.sendResponse(w, "1", "Pass - Verified", job.Message)
	} else {
		h.sendResponse(w, "0", "Fail - Unable to verify", job.Message)
	}
}

// handleGetABI handles ABI retrieval requests
// GET /api?module=contract&action=getabi&address=xxx
func (h *Handler) handleGetABI(w http.ResponseWriter, r *http.Request) {
	addressStr := r.URL.Query().Get("address")
	if addressStr == "" {
		h.sendError(w, "Missing address parameter")
		return
	}

	if !common.IsHexAddress(addressStr) {
		h.sendError(w, "Invalid address format")
		return
	}

	address := common.HexToAddress(addressStr)

	// Get verification data
	reader, ok := h.storage.(storage.ContractVerificationReader)
	if !ok {
		h.sendError(w, "Storage does not support verification queries")
		return
	}

	verification, err := reader.GetContractVerification(context.Background(), address)
	if err != nil {
		if err == storage.ErrNotFound {
			h.sendError(w, "Contract source code not verified")
			return
		}
		h.sendError(w, "Failed to get contract data")
		return
	}

	if !verification.IsVerified {
		h.sendError(w, "Contract source code not verified")
		return
	}

	h.sendSuccess(w, verification.ABI)
}

// handleGetSourceCode handles source code retrieval requests
// GET /api?module=contract&action=getsourcecode&address=xxx
func (h *Handler) handleGetSourceCode(w http.ResponseWriter, r *http.Request) {
	addressStr := r.URL.Query().Get("address")
	if addressStr == "" {
		h.sendError(w, "Missing address parameter")
		return
	}

	if !common.IsHexAddress(addressStr) {
		h.sendError(w, "Invalid address format")
		return
	}

	address := common.HexToAddress(addressStr)

	// Get verification data
	reader, ok := h.storage.(storage.ContractVerificationReader)
	if !ok {
		h.sendError(w, "Storage does not support verification queries")
		return
	}

	verification, err := reader.GetContractVerification(context.Background(), address)
	if err != nil {
		if err == storage.ErrNotFound {
			h.sendError(w, "Contract source code not verified")
			return
		}
		h.sendError(w, "Failed to get contract data")
		return
	}

	if !verification.IsVerified {
		h.sendError(w, "Contract source code not verified")
		return
	}

	// Return in Etherscan format (array with single result)
	result := []map[string]interface{}{
		{
			"SourceCode":           verification.SourceCode,
			"ABI":                  verification.ABI,
			"ContractName":         verification.Name,
			"CompilerVersion":      verification.CompilerVersion,
			"OptimizationUsed":     boolToString(verification.OptimizationEnabled),
			"Runs":                 fmt.Sprintf("%d", verification.OptimizationRuns),
			"ConstructorArguments": verification.ConstructorArguments,
			"LicenseType":          verification.LicenseType,
		},
	}

	h.sendSuccess(w, result)
}

// updateJobStatus updates the status of a verification job
func (h *Handler) updateJobStatus(job *VerificationJob, status, message string) {
	h.jobsMu.Lock()
	defer h.jobsMu.Unlock()
	job.Status = status
	job.Message = message
}

// sendSuccess sends a successful response
func (h *Handler) sendSuccess(w http.ResponseWriter, result interface{}) {
	h.sendResponse(w, "1", "OK", result)
}

// sendError sends an error response
func (h *Handler) sendError(w http.ResponseWriter, message string) {
	h.sendResponse(w, "0", "NOTOK", message)
}

// sendResponse sends a response in Etherscan format
func (h *Handler) sendResponse(w http.ResponseWriter, status, message string, result interface{}) {
	response := Response{
		Status:  status,
		Message: message,
		Result:  result,
	}
	_ = json.NewEncoder(w).Encode(response)
}

// boolToString converts bool to "1" or "0"
func boolToString(b bool) string {
	if b {
		return "1"
	}
	return "0"
}
