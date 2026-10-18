package arcfeed

import (
	"context"
	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/chainfeed"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/evmstate"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/evmtrace"
	"github.com/0xmhha/indexer-go/pkg/chains/arc"
	"github.com/0xmhha/indexer-go/pkg/source"
	"github.com/0xmhha/indexer-go/pkg/source/facts"
)

// NewFactsSource pins the same Arc profile/accounting used by this module's
// capture verifier. Only the audited testnet is enabled. It borrows the store
// and the existing live shared-pool canonical reader; it starts no node/client.
func NewFactsSource(ctx context.Context, store *capture.Store, fresh source.CanonicalReader, consumer string) (*facts.Source, error) {
	if store == nil {
		return nil, chainfeed.ErrUnsupported
	}
	st, e := store.State(ctx)
	if e != nil {
		return nil, e
	}
	if st.Anchor.Chain.ID != "5042002" {
		return nil, chainfeed.ErrUnsupported
	}
	return facts.New(ctx, facts.Config{Store: store, Profile: arc.New(), Canonical: fresh, Consumer: consumer})
}

// NewStateSupplier supplies tracked EIP-1186 proofs/code after a block is
// captured. Call promptly while the provider retains that state, then let
// downstream workers use Read without RPC. The caller lends the existing
// chain-wide 5 HTTP/s batch client; this module never dials another endpoint.
// No public-provider proof support or full-state coverage is assumed.
func NewStateSupplier(ctx context.Context, store *capture.Store, client evmstate.BatchCaller) (*evmstate.Supplier, error) {
	if store == nil {
		return nil, chainfeed.ErrUnsupported
	}
	st, e := store.State(ctx)
	if e != nil {
		return nil, e
	}
	if st.Anchor.Chain.ID != "5042002" {
		return nil, chainfeed.ErrUnsupported
	}
	return evmstate.New(store, arc.New(), client)
}

// NewCallTraceSupplier lends the same chain profile and shared RPC client to
// the optional provider-reported call-tree supplier. Actual endpoint debug
// support is required; this does not enable execution or full trace coverage.
func NewCallTraceSupplier(ctx context.Context, store *capture.Store, client evmtrace.BatchCaller) (*evmtrace.Supplier, error) {
	if store == nil {
		return nil, chainfeed.ErrUnsupported
	}
	st, e := store.State(ctx)
	if e != nil {
		return nil, e
	}
	if st.Anchor.Chain.ID != "5042002" {
		return nil, chainfeed.ErrUnsupported
	}
	return evmtrace.New(store, arc.New(), client)
}
