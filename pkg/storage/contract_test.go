package storage

import (
	"context"
	"os"
	"os/exec"
	"sort"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/core/port/porttest"
)

// TestPortContracts checks the Pebble storage against the contracts of the
// storage ports (refactoring plan R1-4).
func TestPortContracts(t *testing.T) {
	porttest.Run(t, func(t *testing.T) any { return newTestPebble(t) })
}

// brokenStores are Pebble storages with one deliberately wrong method. The
// contracts must fail on every one of them; otherwise they do not pin the
// behaviour they claim to.
var brokenStores = map[string]func(*PebbleStorage) any{
	"GetBlocksExcludesEnd":         func(s *PebbleStorage) any { return &blocksExcludeEnd{s} },
	"AddressListIgnoresCursor":     func(s *PebbleStorage) any { return &addressListIgnoresCursor{s} },
	"CommitDiscards":               func(s *PebbleStorage) any { return &commitDiscards{s} },
	"ReceiptsByBlockWrongOrder":    func(s *PebbleStorage) any { return &receiptsReversed{s} },
	"MissingBlockIsNotErrNotFound": func(s *PebbleStorage) any { return &missingBlockNil{s} },
}

// TestPortContractsCatchBrokenStores runs the contracts against each broken
// store in a child process and requires them to fail.
func TestPortContractsCatchBrokenStores(t *testing.T) {
	if name := os.Getenv("PORTTEST_BROKEN"); name != "" {
		porttest.Run(t, func(t *testing.T) any { return brokenStores[name](newTestPebble(t)) })
		return
	}
	if testing.Short() {
		t.Skip("runs the contracts once per broken store")
	}
	names := make([]string, 0, len(brokenStores))
	for name := range brokenStores {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		cmd := exec.Command(os.Args[0], "-test.run=^TestPortContractsCatchBrokenStores$", "-test.count=1")
		cmd.Env = append(os.Environ(), "PORTTEST_BROKEN="+name)
		out, err := cmd.CombinedOutput()
		if err == nil || !strings.Contains(string(out), "--- FAIL: TestPortContractsCatchBrokenStores/") {
			t.Errorf("the contracts pass on the broken store %s", name)
		}
	}
}

// blocksExcludeEnd treats the end of a block range as exclusive.
type blocksExcludeEnd struct{ *PebbleStorage }

func (s *blocksExcludeEnd) GetBlocks(ctx context.Context, start, end uint64) ([]*model.Block, error) {
	if end == start {
		return nil, nil
	}
	return s.PebbleStorage.GetBlocks(ctx, start, end-1)
}

// addressListIgnoresCursor always lists an address's transactions from the
// start, ignoring the page cursor.
type addressListIgnoresCursor struct{ *PebbleStorage }

func (s *addressListIgnoresCursor) GetTransactionsByAddress(ctx context.Context, addr common.Address, page port.Page) ([]common.Hash, string, error) {
	page.After = ""
	return s.PebbleStorage.GetTransactionsByAddress(ctx, addr, page)
}

// commitDiscards rolls a block transaction back when asked to commit it.
type commitDiscards struct{ *PebbleStorage }

func (s *commitDiscards) BeginBlock(ctx context.Context) (context.Context, port.BlockTx, error) {
	txCtx, tx, err := s.PebbleStorage.BeginBlock(ctx)
	if err != nil {
		return txCtx, nil, err
	}
	return txCtx, discardingTx{tx}, nil
}

type discardingTx struct{ port.BlockTx }

func (tx discardingTx) Commit() error { tx.Rollback(); return nil }

// receiptsReversed returns a block's receipts in reverse transaction order.
type receiptsReversed struct{ *PebbleStorage }

func (s *receiptsReversed) GetReceiptsByBlockNumber(ctx context.Context, n uint64) ([]*model.Receipt, error) {
	rs, err := s.PebbleStorage.GetReceiptsByBlockNumber(ctx, n)
	for i, j := 0, len(rs)-1; i < j; i, j = i+1, j-1 {
		rs[i], rs[j] = rs[j], rs[i]
	}
	return rs, err
}

// missingBlockNil reports a missing block as nil without an error.
type missingBlockNil struct{ *PebbleStorage }

func (s *missingBlockNil) GetBlock(ctx context.Context, n uint64) (*model.Block, error) {
	b, err := s.PebbleStorage.GetBlock(ctx, n)
	if err != nil {
		return nil, nil
	}
	return b, nil
}
