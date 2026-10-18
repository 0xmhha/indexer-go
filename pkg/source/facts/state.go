package facts

import (
	"context"
	"errors"
	"fmt"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/evmstate"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/evmtrace"
	"math/big"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/feature"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
)

func missingState(ctx context.Context, what string) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	return fmt.Errorf("%w: %s is not captured by the execution-only plan", ErrCoverage, what)
}

// These explicit errors cannot be mistaken for a provider's pruned-state RPC
// answer and do not permit legacy allow_pruned_state zero/latest fallback.
func (s *Source) BalanceAt(ctx context.Context, _ common.Address, _ *big.Int) (*big.Int, error) {
	return nil, missingState(ctx, "account balance")
}
func (s *Source) BalanceAtHash(ctx context.Context, _ common.Address, _ common.Hash) (*big.Int, error) {
	return nil, missingState(ctx, "account balance by hash")
}
func (s *Source) BalanceBatchAtHash(ctx context.Context, _ []common.Address, _ common.Hash) ([]*big.Int, []error, error) {
	return nil, nil, missingState(ctx, "account balance batch")
}
func (s *Source) CallContract(ctx context.Context, _ ethereum.CallMsg, _ interface{}) ([]byte, error) {
	return nil, missingState(ctx, "contract call")
}
func (s *Source) CodeAt(ctx context.Context, _ common.Address, _ interface{}) ([]byte, error) {
	return nil, missingState(ctx, "contract code")
}
func (s *Source) CallContractAtHash(ctx context.Context, _ ethereum.CallMsg, _ common.Hash) ([]byte, error) {
	return nil, missingState(ctx, "contract call by hash")
}
func (s *Source) CodeAtHash(ctx context.Context, _ common.Address, _ common.Hash) ([]byte, error) {
	return nil, missingState(ctx, "contract code by hash")
}
func (s *Source) CallContractBatchAtHash(ctx context.Context, _ common.Address, _ [][]byte, _ common.Hash) ([][]byte, []error, error) {
	return nil, nil, missingState(ctx, "contract call batch")
}

var _ feature.ContractReader = (*Source)(nil)
var _ feature.HashContractReader = (*Source)(nil)
var _ feature.HashContractBatchReader = (*Source)(nil)

// BindFeatures replaces every known feature read dependency with DB-only
// facts/explicit coverage failures. Existing durably initialized balances can
// advance from verified deltas. New balances/metadata require a future state
// supplier or an explicit, separately verified projection checkpoint import.
// Custom handlers must also use these dependencies rather than private RPCs.
func (s *Source) BindFeatures(d feature.Deps) feature.Deps {
	d.Profile = s.cfg.Profile
	d.BlockAt = s.Block
	d.BalanceAt = s.BalanceAt
	d.BalanceAtHash = s.BalanceAtHash
	d.BalanceBatchAtHash = s.BalanceBatchAtHash
	d.Contracts = s
	if resolver, ok := d.Storage.(interface{ SetGenesisBalanceResolver(port.BalanceSource) }); ok {
		resolver.SetGenesisBalanceResolver(s)
	}
	return d
}

// Logs returns complete committed log models for a block without offering a
// declared-range shortcut. Full writer ingest still uses BlockWithReceipts.
func (s *Source) Logs(ctx context.Context, n uint64) ([]*model.Log, error) {
	_, rs, e := s.BlockWithReceipts(ctx, n)
	if e != nil {
		return nil, e
	}
	logs := []*model.Log{}
	for _, r := range rs {
		logs = append(logs, r.Logs...)
	}
	return logs, nil
}

// StateSnapshot reads optional proof-verified, explicitly tracked state. It
// does not expand this Source's capture plan or enable arbitrary EVM calls,
// bootstrap balances or state-reading features. Missing selection is coverage
// failure, even if a different subset was captured for the same block.
func (s *Source) StateSnapshot(ctx context.Context, n uint64, selection evmstate.Selection) (evmstate.Result, error) {
	id, e := s.identity(ctx, n)
	if e != nil {
		return evmstate.Result{}, e
	}
	out, e := evmstate.ReadSnapshot(ctx, s.cfg.Store, s.cfg.Profile, s.epoch, id, selection)
	if errors.Is(e, port.ErrNotFound) {
		return evmstate.Result{}, fmt.Errorf("%w: tracked state selection: %w", ErrCoverage, e)
	}
	if e != nil {
		return evmstate.Result{}, e
	}
	return out, nil
}

// StateTransition is a DB-only before/after read of a pinned tracked stream.
// Missing/pruned inputs never fall back to zero/latest or native accounting.
func (s *Source) StateTransition(ctx context.Context, n uint64, d model.SupplementStreamDescriptor) (evmstate.TransitionState, evmstate.TransitionState, error) {
	return evmstate.ReadTransition(ctx, s.cfg.Store, s.cfg.Profile, s.epoch, d, n)
}

// CallTraces is an exact-definition DB-only page of provider-reported calls.
// It does not activate balance accounting or the legacy internal-call tables.
func (s *Source) CallTraces(ctx context.Context, n uint64, d model.SupplementStreamDescriptor, after uint32, limit int) (evmtrace.Page, error) {
	id, e := s.identity(ctx, n)
	if e != nil {
		return evmtrace.Page{}, e
	}
	out, e := evmtrace.ReadPage(ctx, s.cfg.Store, s.cfg.Profile, s.epoch, id, d, after, limit)
	if e == port.ErrNotFound {
		return evmtrace.Page{}, fmt.Errorf("%w: call trace input: %w", ErrCoverage, e)
	}
	return out, e
}
