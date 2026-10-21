package middleware

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"

	"go.uber.org/zap"
)

// contextKey is a private type for context keys in this package.
type contextKey string

const (
	// APIKeyHeader is the header used to pass the API key.
	APIKeyHeader = "X-API-Key"

	// apiKeyContextKey stores the authenticated API key in request context.
	apiKeyContextKey contextKey = "api_key"
)

// AuthConfig holds configuration for the authentication middleware.
type AuthConfig struct {
	// APIKeys is the set of valid API keys. Keys map to labels for logging.
	APIKeys map[string]string

	// AllowedPaths are paths that bypass authentication (e.g., /health, /metrics).
	AllowedPaths map[string]bool
}

// WithAPIKeyLabel returns ctx carrying an accepted API key's label, as the
// middleware does (for callers that authenticate by other means, and
// tests).
func WithAPIKeyLabel(ctx context.Context, label string) context.Context {
	return context.WithValue(ctx, apiKeyContextKey, label)
}

// APIKeyFromContext returns the API key from the request context, if present.
func APIKeyFromContext(ctx context.Context) (string, bool) {
	key, ok := ctx.Value(apiKeyContextKey).(string)
	return key, ok
}

// APIKeyAuth returns a middleware that validates API keys from the X-API-Key header
// or the "api_key" query parameter. Requests without a valid key receive 401 Unauthorized.
// Paths in allowedPaths bypass authentication entirely.
func APIKeyAuth(cfg AuthConfig, logger *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip auth for allowed paths
			if cfg.AllowedPaths[r.URL.Path] {
				next.ServeHTTP(w, r)
				return
			}

			key := requestAPIKey(r)

			if key == "" {
				logger.Debug("request missing API key",
					zap.String("path", r.URL.Path),
					zap.String("ip", extractClientIP(r)),
				)
				writeUnauthorized(w, "missing API key")
				return
			}

			// Validate key using constant-time comparison
			label, valid := validateAPIKey(cfg.APIKeys, key)
			if !valid {
				logger.Warn("invalid API key",
					zap.String("path", r.URL.Path),
					zap.String("ip", extractClientIP(r)),
				)
				writeUnauthorized(w, "invalid API key")
				return
			}

			logger.Debug("authenticated request",
				zap.String("key_label", label),
				zap.String("path", r.URL.Path),
			)

			// Store key label in context for downstream handlers
			ctx := context.WithValue(r.Context(), apiKeyContextKey, label)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// requestAPIKey returns the API key of a request: the X-API-Key header,
// the "api_key" query parameter or Authorization: Bearer, "" for none.
func requestAPIKey(r *http.Request) string {
	if key := r.Header.Get(APIKeyHeader); key != "" {
		return key
	}
	if key := r.URL.Query().Get("api_key"); key != "" {
		return key
	}
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return ""
}

// APIKeyIdentify returns a middleware for APIs that need a key only for
// some operations: a request with a valid key gets its label in the
// context (APIKeyFromContext), a request without a key goes on without
// one, and a request with an unknown key is refused (401), so a wrong key
// is never taken for none. The operations check APIKeyFromContext.
func APIKeyIdentify(keys map[string]string, logger *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := requestAPIKey(r)
			if key == "" {
				next.ServeHTTP(w, r)
				return
			}
			label, valid := validateAPIKey(keys, key)
			if !valid {
				logger.Warn("invalid API key", zap.String("path", r.URL.Path), zap.String("ip", extractClientIP(r)))
				writeUnauthorized(w, "invalid API key")
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), apiKeyContextKey, label)))
		})
	}
}

// validateAPIKey checks if the provided key matches any configured API key
// using constant-time comparison to prevent timing attacks.
func validateAPIKey(keys map[string]string, provided string) (string, bool) {
	for key, label := range keys {
		if subtle.ConstantTimeCompare([]byte(key), []byte(provided)) == 1 {
			return label, true
		}
	}
	return "", false
}

// writeUnauthorized writes a 401 Unauthorized JSON response.
func writeUnauthorized(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":"unauthorized","message":"` + message + `"}`))
}
