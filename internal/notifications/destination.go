package notifications

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"
)

// Webhook and Slack destinations are chosen by whoever registers a
// setting, so delivery must not reach addresses inside the indexer's own
// network (server-side request forgery): loopback, private and link-local
// ranges, cloud metadata (169.254.169.254) and the like. The check runs on
// the address actually dialed, after name resolution, so a name that
// resolves to such an address later (DNS rebinding) is refused too, and
// redirects are not followed, so a public endpoint cannot forward the
// request inward. AllowPrivateDestinations turns the check off for
// development and tests.

// errBlockedDestination is returned when a destination is in a blocked
// range.
var errBlockedDestination = errors.New("destination address is not allowed: loopback, private, link-local and other internal ranges are blocked")

// blockedNets are the ranges net.IP's predicates do not cover. IPv4-mapped
// IPv6 addresses are checked as IPv4 (blockedIP normalizes them first).
var blockedNets = func() []*net.IPNet {
	var out []*net.IPNet
	for _, cidr := range []string{
		"0.0.0.0/8",       // "this" network
		"100.64.0.0/10",   // carrier-grade NAT
		"192.0.0.0/24",    // IETF protocol assignments
		"192.0.2.0/24",    // documentation
		"198.18.0.0/15",   // benchmarking
		"198.51.100.0/24", // documentation
		"203.0.113.0/24",  // documentation
		"240.0.0.0/4",     // reserved, broadcast
		"64:ff9b::/96",    // NAT64 (reaches IPv4 inside)
		"64:ff9b:1::/48",  // local-use NAT64
		"2001:db8::/32",   // documentation
	} {
		_, n, err := net.ParseCIDR(cidr)
		if err != nil {
			panic(err)
		}
		out = append(out, n)
	}
	return out
}()

// blockedIP reports whether ip may not be a delivery destination.
func blockedIP(ip net.IP) bool {
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return true
	}
	for _, n := range blockedNets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// newDeliveryClient returns the HTTP client of webhook and Slack
// deliveries: it dials only allowed addresses (unless allowPrivate), uses
// no proxy from the environment and does not follow redirects.
func newDeliveryClient(timeout time.Duration, allowPrivate bool) *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	if !allowPrivate {
		dialer.Control = func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip := net.ParseIP(host)
			if ip == nil || blockedIP(ip) {
				return fmt.Errorf("%w (%s)", errBlockedDestination, host)
			}
			return nil
		}
	}
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext:         dialer.DialContext,
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// checkDestination validates a destination URL when a setting is
// registered: http or https, and, unless allowPrivate, no host that is an
// address in a blocked range or a local name. Names are checked again
// when delivery dials them.
func checkDestination(raw string, allowPrivate bool) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("URL must use http or https scheme")
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("URL has no host")
	}
	if u.User != nil {
		return fmt.Errorf("URL must not carry credentials")
	}
	if allowPrivate {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil {
		if blockedIP(ip) {
			return errBlockedDestination
		}
		return nil
	}
	name := strings.ToLower(strings.TrimSuffix(host, "."))
	if name == "localhost" || strings.HasSuffix(name, ".localhost") || strings.HasSuffix(name, ".local") || strings.HasSuffix(name, ".internal") || !strings.Contains(name, ".") {
		return errBlockedDestination
	}
	return nil
}
