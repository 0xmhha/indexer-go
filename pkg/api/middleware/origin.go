package middleware

import (
	"net/http"
	"net/url"
	"strings"
)

// Origins is the api.allowed_origins policy, shared by CORS and the
// WebSocket origin check: "*" allows every origin, otherwise the listed
// origins (scheme://host[:port]) are allowed.
type Origins struct {
	any  bool
	list map[string]bool
}

// NewOrigins returns the policy of the allowed origins.
func NewOrigins(allowed []string) Origins {
	o := Origins{list: make(map[string]bool, len(allowed))}
	for _, a := range allowed {
		a = normalizeOrigin(a)
		if a == "*" {
			o.any = true
		} else if a != "" {
			o.list[a] = true
		}
	}
	return o
}

func normalizeOrigin(o string) string {
	return strings.ToLower(strings.TrimRight(strings.TrimSpace(o), "/"))
}

// Allowed reports whether origin is allowed.
func (o Origins) Allowed(origin string) bool {
	return o.any || o.list[normalizeOrigin(origin)]
}

// corsAllowHeaders are the request headers a cross-origin request may send.
const corsAllowHeaders = "Accept, Authorization, Content-Type, X-API-Key, X-CSRF-Token"

// CORS answers cross-origin requests from allowed origins. With "*" it
// answers Access-Control-Allow-Origin: * ; otherwise it names the request's
// origin. It never allows credentials: the API uses no cookies, and
// reflecting any origin together with credentials would let every site
// read responses made with the visitor's credentials. Requests without an
// Origin header are not cross-origin requests and get no CORS headers.
func CORS(o Origins) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if !o.any {
				w.Header().Add("Vary", "Origin")
			}
			if origin != "" && o.Allowed(origin) {
				if o.any {
					w.Header().Set("Access-Control-Allow-Origin", "*")
				} else {
					w.Header().Set("Access-Control-Allow-Origin", origin)
				}
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", corsAllowHeaders)
				w.Header().Set("Access-Control-Max-Age", "300")
			}

			// Handle preflight requests
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusOK)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// CheckWebSocketOrigin is the origin check of WebSocket upgrades: a page of
// another site may open a WebSocket to any server (browsers apply no CORS
// to it), so the server checks the Origin header itself. Allowed are
// clients that send no Origin (not browsers), allowed origins, and pages of
// the server's own host.
func (o Origins) CheckWebSocketOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" || o.Allowed(origin) {
		return true
	}
	u, err := url.Parse(origin)
	return err == nil && strings.EqualFold(u.Host, r.Host)
}
