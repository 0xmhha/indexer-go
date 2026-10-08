package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/0xmhha/indexer-go/pkg/multichain"
)

// oneChain serves one chain's store for the multi-chain routes.
type oneChain struct {
	store port.QueryStore
	bus   *events.EventBus
}

func (c oneChain) ChainStore(id string) (port.QueryStore, *events.EventBus, bool) {
	return c.store, c.bus, id == "a"
}

func (c oneChain) ListChains() []*multichain.ChainInfo { return []*multichain.ChainInfo{{ID: "a"}} }

// TestServerStopReleasesGoroutines (refactoring plan R0-7, C1): a server
// with every part that runs in the background (rate limiter cleanup,
// JSON-RPC filter managers at the root and per chain, the /ws hub, GraphQL
// subscriptions) leaves no goroutine behind once it is stopped and its
// event bus has stopped, as the app shuts down.
func TestServerStopReleasesGoroutines(t *testing.T) {
	baseline := goleak.IgnoreCurrent()
	for _, multi := range []bool{false, true} {
		bus := events.NewEventBus(16, 16)
		go bus.Run()
		cfg := DefaultConfig()
		cfg.EnableRateLimit, cfg.EnableJSONRPC, cfg.EnableWebSocket, cfg.EnableGraphQL = true, true, true, true
		var (
			s   *Server
			err error
		)
		if multi {
			s, err = NewServerWithOptions(cfg, zap.NewNop(), nil, &ServerOptions{Chains: oneChain{store: &mockStorage{}, bus: bus}})
		} else {
			s, err = NewServer(cfg, zap.NewNop(), &mockStorage{})
		}
		require.NoError(t, err)
		s.SetEventBus(bus)

		// Serve a request on each part so the lazily built ones exist.
		paths := []string{"/rpc", "/graphql"}
		if multi {
			paths = []string{"/chains/a/rpc", "/chains/a/graphql"}
		}
		for _, p := range paths {
			rec := httptest.NewRecorder()
			s.Router().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, p, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"eth_chainId","query":"{ __typename }"}`)))
		}

		require.NoError(t, s.Stop(context.Background()))
		bus.Stop()
	}
	goleak.VerifyNone(t, baseline)
}
