package app

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rpc"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/feature"
)

// checkNodeState refuses to start when an enabled feature reads the node's
// state before each block it processes (feature.StateReader) and the node
// no longer keeps the state before the first such block: a node that is not
// an archive node keeps only recent states, and the feature would start
// accounts it cannot read from 0 (balance.native) or read contracts at the
// latest state (token.metadata). A feature whose settings set
// allow_pruned_state is not checked.
func (a *App) checkNodeState(ctx context.Context, units []feature.Unit, features []string) error {
	var readers []string
	for _, name := range features {
		if !feature.ReadsState(name) {
			continue
		}
		var st struct {
			AllowPrunedState bool `yaml:"allow_pruned_state"`
		}
		if err := a.config.FeatureSettings(name, &st); err != nil {
			return err
		}
		if !st.AllowPrunedState {
			readers = append(readers, name)
		}
	}
	if len(readers) == 0 {
		return nil
	}

	if a.nodeFeed != nil {
		var native, external []string
		for _, name := range readers {
			if name == "balance.native" {
				native = append(native, name)
			} else {
				external = append(external, name)
			}
		}
		if len(native) > 0 {
			if err := a.checkStateReaders(ctx, units, native, a.nodeFeed.BalanceAt); err != nil {
				return err
			}
		}
		if len(external) == 0 {
			return nil
		}
		return a.checkStateReaders(ctx, units, external, a.fetcher.BalanceAt)
	}
	return a.checkStateReaders(ctx, units, readers, a.fetcher.BalanceAt)
}

func (a *App) checkStateReaders(ctx context.Context, units []feature.Unit, readers []string, balanceAt func(context.Context, common.Address, *big.Int) (*big.Int, error)) error {
	first, err := a.firstStateHeight(ctx, units, readers)
	if err != nil {
		return err
	}
	head, err := a.profileSrc.Head(ctx)
	if err != nil {
		return fmt.Errorf("check the node's state: %w", err)
	}
	at := first
	if at > 0 {
		at-- // the state before the first block
	}
	if at > head {
		return nil // the node has not reached it; its state will be recent
	}
	_, err = balanceAt(ctx, common.Address{}, new(big.Int).SetUint64(at))
	var answered rpc.Error
	switch {
	case err == nil:
		return nil
	case !errors.As(err, &answered):
		return fmt.Errorf("check the node's state at block %d: %w", at, err)
	}
	a.logger.Error("The node keeps no state of the block before the first block to index",
		zap.Strings("features", readers), zap.Uint64("block", at), zap.Error(err))
	return fmt.Errorf("%v read the node's state before each block, but the node keeps no state of block %d (%v): "+
		"use an archive node, start from a height whose state the node keeps (indexer.start_height), disable the feature, "+
		"or set features.<name>.allow_pruned_state: true to go on without it (balance.native starts accounts the node cannot give from 0, "+
		"token.metadata reads contracts at the latest state)", readers, at, err)
}

// firstStateHeight is the lowest block one of the features will process:
// the next block to index, or the start of a backfill of a feature enabled
// on an indexed database.
func (a *App) firstStateHeight(ctx context.Context, units []feature.Unit, readers []string) (uint64, error) {
	start := a.config.Indexer.StartHeight
	latest, err := a.storage.GetLatestHeight(ctx)
	hasData := err == nil
	if err != nil && !errors.Is(err, port.ErrNotFound) {
		return 0, err
	}
	first := start
	if hasData {
		first = max(latest+1, start)
	}
	fs, ok := a.storage.(port.FeatureStateStore)
	if !ok || !hasData {
		return first, nil
	}
	states, err := fs.FeatureStates(ctx)
	if err != nil {
		return 0, err
	}
	_, jobs, err := feature.ReconcileUnits(units, states, latest, hasData)
	if err != nil {
		return 0, err
	}
	reads := map[string]bool{}
	for _, name := range readers {
		reads[name] = true
	}
	for _, job := range jobs {
		if reads[feature.FeatureOf(job.Feature)] {
			// The backfill starts no lower than the start height (fetch/backfill.go).
			first = min(first, max(job.From, min(start, job.To)))
		}
	}
	return first, nil
}
