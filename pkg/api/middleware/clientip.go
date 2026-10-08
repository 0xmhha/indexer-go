package middleware

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"

	"go.uber.org/zap"
)

// ParseTrustedProxies parses api.trusted_proxies: IP addresses and CIDR
// ranges of the reverse proxies whose forwarding headers are believed.
func ParseTrustedProxies(entries []string) ([]netip.Prefix, error) {
	out := make([]netip.Prefix, 0, len(entries))
	for _, e := range entries {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		if p, err := netip.ParsePrefix(e); err == nil {
			out = append(out, p.Masked())
			continue
		}
		a, err := netip.ParseAddr(e)
		if err != nil {
			return nil, fmt.Errorf("trusted proxy %q is neither an IP address nor a CIDR range", e)
		}
		a = a.Unmap()
		out = append(out, netip.PrefixFrom(a, a.BitLen()))
	}
	return out, nil
}

// ClientIP sets r.RemoteAddr to the client's address, so that logs and the
// rate limit see the client rather than the proxy in front of it.
//
// The forwarding headers (X-Forwarded-For, X-Real-IP) are believed only
// when the connection comes from a trusted proxy: anyone else can write
// them. X-Forwarded-For is read from the right, skipping trusted proxies;
// the first address that is not one is the client (the addresses left of
// it were written by the client and are not believed). Without trusted
// proxies the headers are ignored, and the first request that carries one
// is logged so that a deployment behind a proxy notices.
func ClientIP(trusted []netip.Prefix, logger *zap.Logger) func(http.Handler) http.Handler {
	var warnOnce sync.Once
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			peer, ok := remoteAddr(r)
			if !ok {
				next.ServeHTTP(w, r)
				return
			}
			if !isTrusted(trusted, peer) {
				if r.Header.Get("X-Forwarded-For") != "" || r.Header.Get("X-Real-IP") != "" {
					warnOnce.Do(func() {
						logger.Warn("Ignoring forwarding headers from an untrusted peer; list the reverse proxy in api.trusted_proxies",
							zap.String("peer", peer.String()))
					})
				}
				r.RemoteAddr = peer.String()
				next.ServeHTTP(w, r)
				return
			}
			r.RemoteAddr = forwardedClient(r, trusted, peer).String()
			next.ServeHTTP(w, r)
		})
	}
}

// forwardedClient returns the client a trusted proxy at peer forwarded r
// for.
func forwardedClient(r *http.Request, trusted []netip.Prefix, peer netip.Addr) netip.Addr {
	var hops []string
	for _, h := range r.Header.Values("X-Forwarded-For") {
		hops = append(hops, strings.Split(h, ",")...)
	}
	client := peer
	for i := len(hops) - 1; i >= 0; i-- {
		a, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			// Unreadable: the hops left of it cannot be attributed.
			return client
		}
		client = a.Unmap()
		if !isTrusted(trusted, client) {
			return client
		}
	}
	if len(hops) == 0 {
		if a, err := netip.ParseAddr(strings.TrimSpace(r.Header.Get("X-Real-IP"))); err == nil {
			return a.Unmap()
		}
	}
	return client
}

// remoteAddr returns the address of the connection's peer.
func remoteAddr(r *http.Request) (netip.Addr, bool) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	a, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, false
	}
	return a.Unmap(), true
}

func isTrusted(trusted []netip.Prefix, a netip.Addr) bool {
	for _, p := range trusted {
		if p.Contains(a) {
			return true
		}
	}
	return false
}
