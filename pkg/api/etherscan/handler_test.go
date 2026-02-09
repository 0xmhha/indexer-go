package etherscan

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/pkg/storage"
	"github.com/0xmhha/indexer-go/pkg/verifier"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// --- Mock Storage ---
// mockStorage embeds storage.Storage to satisfy the full interface,
// but only implements ContractVerificationReader/Writer methods used by Handler.

type mockStorage struct {
	storage.Storage // embedded nil - satisfies interface; only verification methods overridden
	verifications   map[common.Address]*storage.ContractVerification
	setErr          error
	getErr          error
}

func newMockStorage() *mockStorage {
	return &mockStorage{
		verifications: make(map[common.Address]*storage.ContractVerification),
	}
}

func (m *mockStorage) GetContractVerification(_ context.Context, address common.Address) (*storage.ContractVerification, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	v, ok := m.verifications[address]
	if !ok {
		return nil, storage.ErrNotFound
	}
	return v, nil
}

func (m *mockStorage) IsContractVerified(_ context.Context, address common.Address) (bool, error) {
	v, ok := m.verifications[address]
	if !ok {
		return false, nil
	}
	return v.IsVerified, nil
}

func (m *mockStorage) ListVerifiedContracts(_ context.Context, _, _ int) ([]common.Address, error) {
	return nil, nil
}

func (m *mockStorage) CountVerifiedContracts(_ context.Context) (int, error) {
	return len(m.verifications), nil
}

func (m *mockStorage) SetContractVerification(_ context.Context, v *storage.ContractVerification) error {
	if m.setErr != nil {
		return m.setErr
	}
	m.verifications[v.Address] = v
	return nil
}

func (m *mockStorage) DeleteContractVerification(_ context.Context, address common.Address) error {
	delete(m.verifications, address)
	return nil
}

// --- Mock Verifier ---

type mockVerifier struct {
	result *verifier.VerificationResult
	err    error
}

func (m *mockVerifier) Verify(_ context.Context, _ *verifier.VerificationRequest) (*verifier.VerificationResult, error) {
	return m.result, m.err
}

func (m *mockVerifier) GetDeployedBytecode(_ context.Context, _ common.Address) (string, error) {
	return "", nil
}

func (m *mockVerifier) CompareBytecode(_, _, _ string) (bool, error) {
	return false, nil
}

func (m *mockVerifier) Close() error {
	return nil
}

// --- Helper functions ---

func newTestHandler(stor *mockStorage, v verifier.Verifier) *Handler {
	return NewHandler(stor, v, zap.NewNop())
}

func parseResponse(t *testing.T, rec *httptest.ResponseRecorder) Response {
	t.Helper()
	var resp Response
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	return resp
}

// --- NewHandler tests ---

func TestNewHandler(t *testing.T) {
	h := NewHandler(newMockStorage(), nil, zap.NewNop())
	assert.NotNil(t, h)
	assert.NotNil(t, h.jobs)
}

// --- ServeHTTP routing tests ---

func TestServeHTTP_InvalidModule(t *testing.T) {
	h := newTestHandler(newMockStorage(), nil)
	req := httptest.NewRequest("GET", "/api?module=invalid&action=test", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	resp := parseResponse(t, rec)
	assert.Equal(t, "0", resp.Status)
	assert.Equal(t, "NOTOK", resp.Message)
}

func TestServeHTTP_InvalidAction(t *testing.T) {
	h := newTestHandler(newMockStorage(), nil)
	req := httptest.NewRequest("GET", "/api?module=contract&action=invalid", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	resp := parseResponse(t, rec)
	assert.Equal(t, "0", resp.Status)
}

func TestServeHTTP_ContentType(t *testing.T) {
	h := newTestHandler(newMockStorage(), nil)
	req := httptest.NewRequest("GET", "/api?module=contract&action=invalid", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
}

// --- handleVerifySourceCode tests ---

func TestHandleVerifySourceCode_NotPost(t *testing.T) {
	h := newTestHandler(newMockStorage(), nil)
	req := httptest.NewRequest("GET", "/api?module=contract&action=verifysourcecode", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	resp := parseResponse(t, rec)
	assert.Equal(t, "0", resp.Status)
	assert.Contains(t, resp.Result, "POST method required")
}

func TestHandleVerifySourceCode_MissingAddress(t *testing.T) {
	h := newTestHandler(newMockStorage(), nil)

	form := url.Values{}
	form.Set("module", "contract")
	form.Set("action", "verifysourcecode")
	form.Set("sourceCode", "contract Foo {}")
	form.Set("compilerversion", "v0.8.0")

	req := httptest.NewRequest("POST", "/api?module=contract&action=verifysourcecode", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	resp := parseResponse(t, rec)
	assert.Equal(t, "0", resp.Status)
	assert.Contains(t, resp.Result, "Missing contract address")
}

func TestHandleVerifySourceCode_MissingSourceCode(t *testing.T) {
	h := newTestHandler(newMockStorage(), nil)

	form := url.Values{}
	form.Set("module", "contract")
	form.Set("action", "verifysourcecode")
	form.Set("contractaddress", "0x1234567890abcdef1234567890abcdef12345678")
	form.Set("compilerversion", "v0.8.0")

	req := httptest.NewRequest("POST", "/api?module=contract&action=verifysourcecode", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	resp := parseResponse(t, rec)
	assert.Equal(t, "0", resp.Status)
	assert.Contains(t, resp.Result, "Missing source code")
}

func TestHandleVerifySourceCode_MissingCompiler(t *testing.T) {
	h := newTestHandler(newMockStorage(), nil)

	form := url.Values{}
	form.Set("module", "contract")
	form.Set("action", "verifysourcecode")
	form.Set("contractaddress", "0x1234567890abcdef1234567890abcdef12345678")
	form.Set("sourceCode", "contract Foo {}")

	req := httptest.NewRequest("POST", "/api?module=contract&action=verifysourcecode", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	resp := parseResponse(t, rec)
	assert.Equal(t, "0", resp.Status)
	assert.Contains(t, resp.Result, "Missing compiler version")
}

func TestHandleVerifySourceCode_InvalidAddress(t *testing.T) {
	h := newTestHandler(newMockStorage(), nil)

	form := url.Values{}
	form.Set("module", "contract")
	form.Set("action", "verifysourcecode")
	form.Set("contractaddress", "not-a-hex-address")
	form.Set("sourceCode", "contract Foo {}")
	form.Set("compilerversion", "v0.8.0")

	req := httptest.NewRequest("POST", "/api?module=contract&action=verifysourcecode", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	resp := parseResponse(t, rec)
	assert.Equal(t, "0", resp.Status)
	assert.Contains(t, resp.Result, "Invalid contract address")
}

func TestHandleVerifySourceCode_ReturnsGUID(t *testing.T) {
	h := newTestHandler(newMockStorage(), &mockVerifier{
		result: &verifier.VerificationResult{Success: true, ABI: "[]"},
	})

	form := url.Values{}
	form.Set("module", "contract")
	form.Set("action", "verifysourcecode")
	form.Set("contractaddress", "0x1234567890abcdef1234567890abcdef12345678")
	form.Set("sourceCode", "contract Foo {}")
	form.Set("compilerversion", "v0.8.0")

	req := httptest.NewRequest("POST", "/api?module=contract&action=verifysourcecode", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	resp := parseResponse(t, rec)
	assert.Equal(t, "1", resp.Status)
	assert.Equal(t, "OK", resp.Message)

	// Result should be a GUID string
	guid, ok := resp.Result.(string)
	assert.True(t, ok)
	assert.NotEmpty(t, guid)

	// Job should exist
	h.jobsMu.RLock()
	job, exists := h.jobs[guid]
	h.jobsMu.RUnlock()
	assert.True(t, exists)
	assert.Equal(t, "Pending", job.Status)
}

// --- handleCheckVerifyStatus tests ---

func TestHandleCheckVerifyStatus_MissingGUID(t *testing.T) {
	h := newTestHandler(newMockStorage(), nil)
	req := httptest.NewRequest("GET", "/api?module=contract&action=checkverifystatus", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	resp := parseResponse(t, rec)
	assert.Equal(t, "0", resp.Status)
	assert.Contains(t, resp.Result, "Missing guid")
}

func TestHandleCheckVerifyStatus_UnknownGUID(t *testing.T) {
	h := newTestHandler(newMockStorage(), nil)
	req := httptest.NewRequest("GET", "/api?module=contract&action=checkverifystatus&guid=nonexistent", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	resp := parseResponse(t, rec)
	assert.Equal(t, "0", resp.Status)
	assert.Contains(t, resp.Result, "Unknown guid")
}

func TestHandleCheckVerifyStatus_Pending(t *testing.T) {
	h := newTestHandler(newMockStorage(), nil)
	h.jobs["test-guid"] = &VerificationJob{
		GUID:    "test-guid",
		Status:  "Pending",
		Message: "Verification in progress",
	}

	req := httptest.NewRequest("GET", "/api?module=contract&action=checkverifystatus&guid=test-guid", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	resp := parseResponse(t, rec)
	assert.Equal(t, "0", resp.Status)
	assert.Equal(t, "Pending in queue", resp.Message)
}

func TestHandleCheckVerifyStatus_Pass(t *testing.T) {
	h := newTestHandler(newMockStorage(), nil)
	h.jobs["test-guid"] = &VerificationJob{
		GUID:    "test-guid",
		Status:  "Pass",
		Message: "Contract source code verified",
	}

	req := httptest.NewRequest("GET", "/api?module=contract&action=checkverifystatus&guid=test-guid", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	resp := parseResponse(t, rec)
	assert.Equal(t, "1", resp.Status)
	assert.Equal(t, "Pass - Verified", resp.Message)
}

func TestHandleCheckVerifyStatus_Fail(t *testing.T) {
	h := newTestHandler(newMockStorage(), nil)
	h.jobs["test-guid"] = &VerificationJob{
		GUID:    "test-guid",
		Status:  "Fail",
		Message: "Bytecode mismatch",
	}

	req := httptest.NewRequest("GET", "/api?module=contract&action=checkverifystatus&guid=test-guid", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	resp := parseResponse(t, rec)
	assert.Equal(t, "0", resp.Status)
	assert.Equal(t, "Fail - Unable to verify", resp.Message)
}

// --- handleGetABI tests ---

func TestHandleGetABI_MissingAddress(t *testing.T) {
	h := newTestHandler(newMockStorage(), nil)
	req := httptest.NewRequest("GET", "/api?module=contract&action=getabi", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	resp := parseResponse(t, rec)
	assert.Equal(t, "0", resp.Status)
	assert.Contains(t, resp.Result, "Missing address")
}

func TestHandleGetABI_InvalidAddress(t *testing.T) {
	h := newTestHandler(newMockStorage(), nil)
	req := httptest.NewRequest("GET", "/api?module=contract&action=getabi&address=invalid", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	resp := parseResponse(t, rec)
	assert.Equal(t, "0", resp.Status)
	assert.Contains(t, resp.Result, "Invalid address")
}

func TestHandleGetABI_NotVerified(t *testing.T) {
	h := newTestHandler(newMockStorage(), nil)
	req := httptest.NewRequest("GET", "/api?module=contract&action=getabi&address=0x1234567890abcdef1234567890abcdef12345678", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	resp := parseResponse(t, rec)
	assert.Equal(t, "0", resp.Status)
	assert.Contains(t, resp.Result, "not verified")
}

func TestHandleGetABI_Verified(t *testing.T) {
	stor := newMockStorage()
	addr := common.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")
	stor.verifications[addr] = &storage.ContractVerification{
		Address:    addr,
		IsVerified: true,
		ABI:        `[{"type":"function","name":"foo"}]`,
	}

	h := newTestHandler(stor, nil)
	req := httptest.NewRequest("GET", "/api?module=contract&action=getabi&address=0x1234567890abcdef1234567890abcdef12345678", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	resp := parseResponse(t, rec)
	assert.Equal(t, "1", resp.Status)
	assert.Equal(t, "OK", resp.Message)
	assert.Contains(t, resp.Result, "foo")
}

func TestHandleGetABI_VerifiedButNotVerifiedFlag(t *testing.T) {
	stor := newMockStorage()
	addr := common.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")
	stor.verifications[addr] = &storage.ContractVerification{
		Address:    addr,
		IsVerified: false, // exists but not verified
		ABI:        `[]`,
	}

	h := newTestHandler(stor, nil)
	req := httptest.NewRequest("GET", "/api?module=contract&action=getabi&address=0x1234567890abcdef1234567890abcdef12345678", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	resp := parseResponse(t, rec)
	assert.Equal(t, "0", resp.Status)
	assert.Contains(t, resp.Result, "not verified")
}

// --- handleGetSourceCode tests ---

func TestHandleGetSourceCode_MissingAddress(t *testing.T) {
	h := newTestHandler(newMockStorage(), nil)
	req := httptest.NewRequest("GET", "/api?module=contract&action=getsourcecode", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	resp := parseResponse(t, rec)
	assert.Equal(t, "0", resp.Status)
}

func TestHandleGetSourceCode_NotVerified(t *testing.T) {
	h := newTestHandler(newMockStorage(), nil)
	req := httptest.NewRequest("GET", "/api?module=contract&action=getsourcecode&address=0x1234567890abcdef1234567890abcdef12345678", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	resp := parseResponse(t, rec)
	assert.Equal(t, "0", resp.Status)
}

func TestHandleGetSourceCode_Verified(t *testing.T) {
	stor := newMockStorage()
	addr := common.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")
	stor.verifications[addr] = &storage.ContractVerification{
		Address:             addr,
		IsVerified:          true,
		Name:                "TestContract",
		CompilerVersion:     "v0.8.19",
		OptimizationEnabled: true,
		OptimizationRuns:    200,
		SourceCode:          "contract Test {}",
		ABI:                 "[]",
		LicenseType:         "MIT",
	}

	h := newTestHandler(stor, nil)
	req := httptest.NewRequest("GET", "/api?module=contract&action=getsourcecode&address=0x1234567890abcdef1234567890abcdef12345678", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	resp := parseResponse(t, rec)
	assert.Equal(t, "1", resp.Status)

	// Result should be an array with one element
	resultArr, ok := resp.Result.([]interface{})
	require.True(t, ok)
	require.Len(t, resultArr, 1)

	entry := resultArr[0].(map[string]interface{})
	assert.Equal(t, "TestContract", entry["ContractName"])
	assert.Equal(t, "v0.8.19", entry["CompilerVersion"])
	assert.Equal(t, "1", entry["OptimizationUsed"])
	assert.Equal(t, "200", entry["Runs"])
	assert.Equal(t, "MIT", entry["LicenseType"])
}

// --- processVerification tests ---

func TestProcessVerification_NilVerifier(t *testing.T) {
	h := newTestHandler(newMockStorage(), nil)
	job := &VerificationJob{GUID: "test", Status: "Pending"}
	h.jobs["test"] = job

	h.processVerification(job, common.HexToAddress("0x01"), "code", "Name", "v0.8.0", false, 200, "", "")

	assert.Equal(t, "Fail", job.Status)
	assert.Contains(t, job.Message, "Verifier not configured")
}

func TestProcessVerification_VerifyError(t *testing.T) {
	v := &mockVerifier{err: assert.AnError}
	h := newTestHandler(newMockStorage(), v)
	job := &VerificationJob{GUID: "test", Status: "Pending"}
	h.jobs["test"] = job

	h.processVerification(job, common.HexToAddress("0x01"), "code", "Name", "v0.8.0", false, 200, "", "")

	assert.Equal(t, "Fail", job.Status)
}

func TestProcessVerification_BytecodeMismatch(t *testing.T) {
	v := &mockVerifier{result: &verifier.VerificationResult{Success: false}}
	h := newTestHandler(newMockStorage(), v)
	job := &VerificationJob{GUID: "test", Status: "Pending"}
	h.jobs["test"] = job

	h.processVerification(job, common.HexToAddress("0x01"), "code", "Name", "v0.8.0", false, 200, "", "")

	assert.Equal(t, "Fail", job.Status)
	assert.Contains(t, job.Message, "Bytecode mismatch")
}

func TestProcessVerification_Success(t *testing.T) {
	stor := newMockStorage()
	v := &mockVerifier{result: &verifier.VerificationResult{Success: true, ABI: `[{"name":"test"}]`}}
	h := newTestHandler(stor, v)
	job := &VerificationJob{GUID: "test", Status: "Pending"}
	h.jobs["test"] = job

	addr := common.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")
	h.processVerification(job, addr, "contract Test {}", "path/to/file.sol:TestContract", "v0.8.19", true, 200, "", "MIT")

	assert.Equal(t, "Pass", job.Status)

	// Verification should be stored
	stored := stor.verifications[addr]
	require.NotNil(t, stored)
	assert.True(t, stored.IsVerified)
	assert.Equal(t, "TestContract", stored.Name) // extracted from path
	assert.Equal(t, "v0.8.19", stored.CompilerVersion)
	assert.True(t, stored.OptimizationEnabled)
}

func TestProcessVerification_ContractNameExtraction(t *testing.T) {
	stor := newMockStorage()
	v := &mockVerifier{result: &verifier.VerificationResult{Success: true, ABI: "[]"}}
	h := newTestHandler(stor, v)
	job := &VerificationJob{GUID: "test", Status: "Pending"}
	h.jobs["test"] = job

	addr := common.HexToAddress("0xabcdef1234567890abcdef1234567890abcdef12")

	// Contract name with path format: "path/to/file.sol:MyContract"
	h.processVerification(job, addr, "code", "contracts/Token.sol:MyToken", "v0.8.0", false, 200, "", "")

	stored := stor.verifications[addr]
	require.NotNil(t, stored)
	assert.Equal(t, "MyToken", stored.Name)
}

// --- updateJobStatus tests ---

func TestUpdateJobStatus(t *testing.T) {
	h := newTestHandler(newMockStorage(), nil)
	job := &VerificationJob{GUID: "test", Status: "Pending", Message: ""}

	h.updateJobStatus(job, "Pass", "Verified")

	assert.Equal(t, "Pass", job.Status)
	assert.Equal(t, "Verified", job.Message)
}

// --- Response helper tests ---

func TestSendSuccess(t *testing.T) {
	h := newTestHandler(newMockStorage(), nil)
	rec := httptest.NewRecorder()

	h.sendSuccess(rec, "hello")

	resp := parseResponse(t, rec)
	assert.Equal(t, "1", resp.Status)
	assert.Equal(t, "OK", resp.Message)
	assert.Equal(t, "hello", resp.Result)
}

func TestSendError(t *testing.T) {
	h := newTestHandler(newMockStorage(), nil)
	rec := httptest.NewRecorder()

	h.sendError(rec, "something broke")

	resp := parseResponse(t, rec)
	assert.Equal(t, "0", resp.Status)
	assert.Equal(t, "NOTOK", resp.Message)
	assert.Equal(t, "something broke", resp.Result)
}

// --- boolToString tests ---

func TestBoolToString(t *testing.T) {
	assert.Equal(t, "1", boolToString(true))
	assert.Equal(t, "0", boolToString(false))
}

// --- Integration-style test ---

func TestFullVerificationWorkflow(t *testing.T) {
	stor := newMockStorage()
	v := &mockVerifier{result: &verifier.VerificationResult{Success: true, ABI: `[{"name":"transfer"}]`}}
	h := newTestHandler(stor, v)

	addr := "0x1234567890abcdef1234567890abcdef12345678"

	// Step 1: Submit verification
	form := url.Values{}
	form.Set("module", "contract")
	form.Set("action", "verifysourcecode")
	form.Set("contractaddress", addr)
	form.Set("sourceCode", "contract Token {}")
	form.Set("compilerversion", "v0.8.19")
	form.Set("optimizationUsed", "1")
	form.Set("runs", "200")

	req := httptest.NewRequest("POST", "/api?module=contract&action=verifysourcecode", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	resp := parseResponse(t, rec)
	require.Equal(t, "1", resp.Status)
	guid := resp.Result.(string)

	// Wait for background verification to complete
	time.Sleep(100 * time.Millisecond)

	// Step 2: Check status
	req = httptest.NewRequest("GET", "/api?module=contract&action=checkverifystatus&guid="+guid, nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	resp = parseResponse(t, rec)
	assert.Equal(t, "1", resp.Status)
	assert.Equal(t, "Pass - Verified", resp.Message)

	// Step 3: Get ABI
	req = httptest.NewRequest("GET", "/api?module=contract&action=getabi&address="+addr, nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	resp = parseResponse(t, rec)
	assert.Equal(t, "1", resp.Status)
	assert.Contains(t, resp.Result, "transfer")

	// Step 4: Get source code
	req = httptest.NewRequest("GET", "/api?module=contract&action=getsourcecode&address="+addr, nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	resp = parseResponse(t, rec)
	assert.Equal(t, "1", resp.Status)
}

// --- POST form parameter tests ---

func TestServeHTTP_PostFormParameters(t *testing.T) {
	h := newTestHandler(newMockStorage(), nil)

	form := url.Values{}
	form.Set("module", "contract")
	form.Set("action", "getabi")

	req := httptest.NewRequest("POST", "/api", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	resp := parseResponse(t, rec)
	// Should route to getabi handler (missing address error)
	assert.Equal(t, "0", resp.Status)
	assert.Contains(t, resp.Result, "Missing address")
}

// --- Implements http.Handler ---

func TestHandler_ImplementsHTTPHandler(t *testing.T) {
	var _ http.Handler = (*Handler)(nil)
}
