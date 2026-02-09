package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"
)

func newTestAuthConfig() AuthConfig {
	return AuthConfig{
		APIKeys: map[string]string{
			"test-key-123": "test-app",
			"admin-key-456": "admin",
		},
		AllowedPaths: map[string]bool{
			"/health":  true,
			"/metrics": true,
			"/version": true,
		},
	}
}

func newTestHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})
}

func TestAPIKeyAuth_ValidKey_Header(t *testing.T) {
	logger := zap.NewNop()
	cfg := newTestAuthConfig()
	handler := APIKeyAuth(cfg, logger)(newTestHandler())

	req := httptest.NewRequest("GET", "/graphql", nil)
	req.Header.Set(APIKeyHeader, "test-key-123")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if rec.Body.String() != "OK" {
		t.Errorf("expected OK, got %s", rec.Body.String())
	}
}

func TestAPIKeyAuth_ValidKey_QueryParam(t *testing.T) {
	logger := zap.NewNop()
	cfg := newTestAuthConfig()
	handler := APIKeyAuth(cfg, logger)(newTestHandler())

	req := httptest.NewRequest("GET", "/graphql?api_key=test-key-123", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestAPIKeyAuth_ValidKey_BearerToken(t *testing.T) {
	logger := zap.NewNop()
	cfg := newTestAuthConfig()
	handler := APIKeyAuth(cfg, logger)(newTestHandler())

	req := httptest.NewRequest("GET", "/rpc", nil)
	req.Header.Set("Authorization", "Bearer admin-key-456")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestAPIKeyAuth_MissingKey(t *testing.T) {
	logger := zap.NewNop()
	cfg := newTestAuthConfig()
	handler := APIKeyAuth(cfg, logger)(newTestHandler())

	req := httptest.NewRequest("GET", "/graphql", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}

	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if resp["error"] != "unauthorized" {
		t.Errorf("expected error 'unauthorized', got %q", resp["error"])
	}
	if resp["message"] != "missing API key" {
		t.Errorf("expected message 'missing API key', got %q", resp["message"])
	}
}

func TestAPIKeyAuth_InvalidKey(t *testing.T) {
	logger := zap.NewNop()
	cfg := newTestAuthConfig()
	handler := APIKeyAuth(cfg, logger)(newTestHandler())

	req := httptest.NewRequest("GET", "/graphql", nil)
	req.Header.Set(APIKeyHeader, "wrong-key")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}

	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if resp["message"] != "invalid API key" {
		t.Errorf("expected 'invalid API key', got %q", resp["message"])
	}
}

func TestAPIKeyAuth_AllowedPaths(t *testing.T) {
	logger := zap.NewNop()
	cfg := newTestAuthConfig()
	handler := APIKeyAuth(cfg, logger)(newTestHandler())

	for _, path := range []string{"/health", "/metrics", "/version"} {
		req := httptest.NewRequest("GET", path, nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("path %s: expected 200, got %d", path, rec.Code)
		}
	}
}

func TestAPIKeyAuth_ProtectedPath_NoKey(t *testing.T) {
	logger := zap.NewNop()
	cfg := newTestAuthConfig()
	handler := APIKeyAuth(cfg, logger)(newTestHandler())

	// Non-allowed paths require a key
	for _, path := range []string{"/graphql", "/rpc", "/ws", "/api"} {
		req := httptest.NewRequest("GET", path, nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("path %s: expected 401, got %d", path, rec.Code)
		}
	}
}

func TestAPIKeyAuth_ContextContainsLabel(t *testing.T) {
	logger := zap.NewNop()
	cfg := newTestAuthConfig()

	var gotLabel string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		label, ok := APIKeyFromContext(r.Context())
		if ok {
			gotLabel = label
		}
		w.WriteHeader(http.StatusOK)
	})

	handler := APIKeyAuth(cfg, logger)(inner)

	req := httptest.NewRequest("GET", "/graphql", nil)
	req.Header.Set(APIKeyHeader, "admin-key-456")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if gotLabel != "admin" {
		t.Errorf("expected label 'admin', got %q", gotLabel)
	}
}

func TestAPIKeyAuth_HeaderPriority(t *testing.T) {
	logger := zap.NewNop()
	cfg := newTestAuthConfig()
	handler := APIKeyAuth(cfg, logger)(newTestHandler())

	// Header takes priority over query param
	req := httptest.NewRequest("GET", "/graphql?api_key=wrong-key", nil)
	req.Header.Set(APIKeyHeader, "test-key-123")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 (header key valid), got %d", rec.Code)
	}
}

func TestAPIKeyAuth_EmptyConfig(t *testing.T) {
	logger := zap.NewNop()
	cfg := AuthConfig{
		APIKeys:      map[string]string{},
		AllowedPaths: map[string]bool{},
	}
	handler := APIKeyAuth(cfg, logger)(newTestHandler())

	// Any key should be rejected when no keys are configured
	req := httptest.NewRequest("GET", "/graphql", nil)
	req.Header.Set(APIKeyHeader, "any-key")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestAPIKeyAuth_ResponseContentType(t *testing.T) {
	logger := zap.NewNop()
	cfg := newTestAuthConfig()
	handler := APIKeyAuth(cfg, logger)(newTestHandler())

	req := httptest.NewRequest("GET", "/graphql", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected Content-Type 'application/json', got %q", ct)
	}
}

func TestValidateAPIKey_ConstantTime(t *testing.T) {
	keys := map[string]string{
		"key-abc-123": "app1",
		"key-def-456": "app2",
	}

	// Valid key
	label, ok := validateAPIKey(keys, "key-abc-123")
	if !ok {
		t.Error("expected valid key")
	}
	if label != "app1" {
		t.Errorf("expected label 'app1', got %q", label)
	}

	// Invalid key
	_, ok = validateAPIKey(keys, "key-xxx-999")
	if ok {
		t.Error("expected invalid key")
	}
}

func TestAPIKeyFromContext_NotSet(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	_, ok := APIKeyFromContext(req.Context())
	if ok {
		t.Error("expected no API key in context")
	}
}
