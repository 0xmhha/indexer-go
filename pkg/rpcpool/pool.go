// Package rpcpool spreads the indexer's JSON-RPC calls over several node
// endpoints (refactoring plan R2-1). Pool is an http.RoundTripper: the
// go-ethereum RPC client sends every request through it, so ethclient, the
// block source and the recording proxy fail over together without knowing
// about it.
//
// Endpoints are used in priority order. A request goes to the first
// endpoint that is not cooling down; when the connection fails, times out
// or the endpoint answers 429 or a 5xx status, the endpoint cools down and
// the request is sent to the next one. After the cooldown the endpoint is
// used again, so the indexer returns to its primary node once it recovers.
// JSON-RPC errors (a node answering with an error object) are not failures
// of the endpoint and are returned as they are.
package rpcpool

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"go.uber.org/zap"
	"golang.org/x/time/rate"
)

// DefaultCooldown is how long a failed endpoint is skipped.
const DefaultCooldown = 10 * time.Second

// Config configures a Pool.
type Config struct {
	// Endpoints are the nodes' HTTP(S) JSON-RPC URLs in priority order.
	Endpoints []string
	// Timeout bounds one attempt at one endpoint (0: no limit besides the
	// request's context).
	Timeout time.Duration
	// RateLimit caps requests per second over all endpoints (0: no limit).
	RateLimit float64
	// Burst is how many requests may exceed RateLimit at once (default 1).
	Burst int
	// Cooldown is how long a failed endpoint is skipped (default
	// DefaultCooldown).
	Cooldown time.Duration
	// Transport sends the requests (default http.DefaultTransport).
	Transport http.RoundTripper
	Logger    *zap.Logger
}

// Pool is an http.RoundTripper over several endpoints.
type Pool struct {
	endpoints []*endpoint
	timeout   time.Duration
	cooldown  time.Duration
	limiter   *rate.Limiter
	transport http.RoundTripper
	logger    *zap.Logger
	now       func() time.Time
}

type endpoint struct {
	url *url.URL
	mu  sync.Mutex
	// downUntil is when the endpoint may be used again after a failure.
	downUntil time.Time
	failures  uint64
	requests  uint64
}

// New returns a pool over cfg.Endpoints.
func New(cfg Config) (*Pool, error) {
	if len(cfg.Endpoints) == 0 {
		return nil, errors.New("rpcpool: no endpoint")
	}
	p := &Pool{
		timeout:   cfg.Timeout,
		cooldown:  cfg.Cooldown,
		transport: cfg.Transport,
		logger:    cfg.Logger,
		now:       time.Now,
	}
	if p.cooldown <= 0 {
		p.cooldown = DefaultCooldown
	}
	if p.transport == nil {
		p.transport = http.DefaultTransport
	}
	if p.logger == nil {
		p.logger = zap.NewNop()
	}
	if cfg.RateLimit > 0 {
		burst := cfg.Burst
		if burst <= 0 {
			burst = 1
		}
		p.limiter = rate.NewLimiter(rate.Limit(cfg.RateLimit), burst)
	}
	for _, raw := range cfg.Endpoints {
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, fmt.Errorf("rpcpool: endpoint %q is not an HTTP(S) URL", raw)
		}
		p.endpoints = append(p.endpoints, &endpoint{url: u})
	}
	return p, nil
}

// Primary returns the first endpoint's URL; RPC clients dial it, and the
// pool sends each request to the endpoint it chooses.
func (p *Pool) Primary() string { return p.endpoints[0].url.String() }

// errStatus is a failed attempt the endpoint answered with an HTTP status.
type errStatus struct {
	endpoint string
	status   int
}

func (e *errStatus) Error() string {
	return fmt.Sprintf("rpcpool: %s answered HTTP %d", e.endpoint, e.status)
}

// RoundTrip sends req to the first available endpoint and fails over to the
// next ones. The response body is read before returning, so the attempt's
// timeout covers it.
func (p *Pool) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	var body []byte
	if req.Body != nil {
		var err error
		if body, err = io.ReadAll(req.Body); err != nil {
			return nil, err
		}
		_ = req.Body.Close()
	}
	if p.limiter != nil {
		if err := p.limiter.Wait(ctx); err != nil {
			return nil, err
		}
	}

	var lastErr error
	for _, ep := range p.order() {
		resp, err := p.attempt(ctx, req, body, ep)
		if err == nil {
			ep.markUp()
			return resp, nil
		}
		if ctx.Err() != nil {
			// The caller gave up; that says nothing about the endpoint.
			return nil, ctx.Err()
		}
		lastErr = err
		if now := p.now(); ep.markDown(now, now.Add(p.cooldown)) {
			p.logger.Warn("RPC endpoint failed, using the next one",
				zap.String("endpoint", ep.url.Redacted()), zap.Error(err))
		}
	}
	return nil, fmt.Errorf("rpcpool: every endpoint failed: %w", lastErr)
}

// order returns the endpoints to try: the available ones in priority order,
// then those cooling down (better than failing when all of them are).
func (p *Pool) order() []*endpoint {
	now := p.now()
	var up, down []*endpoint
	for _, ep := range p.endpoints {
		if ep.available(now) {
			up = append(up, ep)
		} else {
			down = append(down, ep)
		}
	}
	return append(up, down...)
}

// attempt sends one request to ep and reads the whole response.
func (p *Pool) attempt(ctx context.Context, req *http.Request, body []byte, ep *endpoint) (*http.Response, error) {
	ep.count()
	actx := ctx
	if p.timeout > 0 {
		var cancel context.CancelFunc
		actx, cancel = context.WithTimeout(ctx, p.timeout)
		defer cancel()
	}
	out := req.Clone(actx)
	out.URL = ep.url
	out.Host = ep.url.Host
	out.Body = io.NopCloser(bytes.NewReader(body))
	out.ContentLength = int64(len(body))
	out.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }

	resp, err := p.transport.RoundTrip(out)
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, &errStatus{endpoint: ep.url.Redacted(), status: resp.StatusCode}
	}
	resp.Body = io.NopCloser(bytes.NewReader(data))
	resp.ContentLength = int64(len(data))
	return resp, nil
}

func (ep *endpoint) available(now time.Time) bool {
	ep.mu.Lock()
	defer ep.mu.Unlock()
	return !now.Before(ep.downUntil)
}

// markDown starts a cooldown and reports whether the endpoint was up.
func (ep *endpoint) markDown(now, until time.Time) bool {
	ep.mu.Lock()
	defer ep.mu.Unlock()
	wasUp := !now.Before(ep.downUntil)
	ep.downUntil = until
	ep.failures++
	return wasUp
}

func (ep *endpoint) markUp() {
	ep.mu.Lock()
	defer ep.mu.Unlock()
	ep.downUntil = time.Time{}
}

func (ep *endpoint) count() {
	ep.mu.Lock()
	defer ep.mu.Unlock()
	ep.requests++
}

// EndpointStatus describes one endpoint.
type EndpointStatus struct {
	URL       string
	Available bool
	Requests  uint64
	Failures  uint64
}

// Status returns the endpoints' state, in priority order.
func (p *Pool) Status() []EndpointStatus {
	now := p.now()
	out := make([]EndpointStatus, len(p.endpoints))
	for i, ep := range p.endpoints {
		ep.mu.Lock()
		out[i] = EndpointStatus{URL: ep.url.Redacted(), Available: !now.Before(ep.downUntil), Requests: ep.requests, Failures: ep.failures}
		ep.mu.Unlock()
	}
	return out
}
