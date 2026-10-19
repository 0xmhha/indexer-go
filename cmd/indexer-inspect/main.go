// indexer-inspect is a one-shot PostgreSQL/window metadata reader. It has no
// dependency on app startup, feed registration or deletion recovery.
package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/opsinspect"
	"github.com/0xmhha/indexer-go/pkg/progress"
	"github.com/0xmhha/indexer-go/pkg/storage/postgres"
	"github.com/0xmhha/indexer-go/pkg/window"
	"github.com/ethereum/go-ethereum/common"
)

type openDatabase func(context.Context, postgres.Options) (opsinspect.Database, func() error, error)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := run(ctx, os.Args[1:], os.Stdout, func(ctx context.Context, opts postgres.Options) (opsinspect.Database, func() error, error) {
		db, err := postgres.Open(ctx, opts)
		if err != nil {
			return nil, nil, err
		}
		return db, db.Close, nil
	})
	stop()
	os.Exit(code)
}

func run(ctx context.Context, args []string, output io.Writer, open openDatabase) int {
	fail := func(code string) int {
		_ = json.NewEncoder(output).Encode(map[string]any{"version": 1, "error": code, "gcDecision": "not_authorized"})
		return 1
	}
	fs := flag.NewFlagSet("indexer-inspect", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	layoutPath := fs.String("layout", "", "standalone window layout YAML (existing files only)")
	windowName := fs.String("window", "", "window name from layout")
	chainID := fs.String("chain-id", "", "expected chain ID; no RPC detection")
	genesis := fs.String("genesis", "", "expected 0x-prefixed 32-byte genesis hash")
	schema := fs.String("schema", "", "exact existing per-chain PostgreSQL schema")
	dsnFile := fs.String("dsn-file", "", "private DSN file; never pass a DSN in arguments")
	after := fs.String("after", "", "feature pagination cursor")
	limit := fs.Int("limit", 100, "feature page size, 1..100")
	timeout := fs.Duration("timeout", 5*time.Second, "DB/metadata observation deadline, at most 30s")
	parseErr := fs.Parse(args)
	if errors.Is(parseErr, flag.ErrHelp) {
		fs.SetOutput(output)
		fs.PrintDefaults()
		return 0
	}
	if parseErr != nil || fs.NArg() != 0 || *schema == "" || *layoutPath == "" || *windowName == "" || *timeout <= 0 || *timeout > 30*time.Second || *limit < 1 || *limit > 100 || *after != "" && !progress.ValidName(*after) {
		return fail("invalid_arguments")
	}
	if err := ctx.Err(); err != nil {
		return fail(failureCode(err))
	}
	hash, err := hex.DecodeString(strings.TrimPrefix(*genesis, "0x"))
	if err != nil || !strings.HasPrefix(*genesis, "0x") || len(hash) != 32 {
		return fail("invalid_chain")
	}
	chain := model.FeedChain{ID: *chainID, GenesisHash: common.BytesToHash(hash)}
	if chain.Validate() != nil {
		return fail("invalid_chain")
	}
	file, err := regularFile(*layoutPath)
	if err != nil {
		return fail("layout_unavailable")
	}
	layout, err := window.ReadLayout(file)
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		return fail("invalid_layout")
	}
	opts, err := layout.OptionsFor(*windowName, chain)
	if err != nil {
		return fail("invalid_layout")
	}
	dsn, err := privateDSN(*dsnFile)
	if err != nil {
		return fail("private_dsn_unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	// One short-lived management connection, not another app pool. ReadOnly
	// bypasses schema creation/migrations and has no writable-prefix exception.
	db, closeDB, err := open(ctx, postgres.Options{DSN: dsn, Schema: *schema, MaxConns: 1, ReadOnly: true})
	if err != nil {
		if ctx.Err() != nil {
			return fail(failureCode(ctx.Err()))
		}
		return fail("database_unavailable")
	}
	report, readErr := opsinspect.Read(ctx, db, chain, opts, *after, *limit)
	closeErr = closeDB()
	if readErr != nil {
		return fail(failureCode(readErr))
	}
	if closeErr != nil {
		return fail("database_close_failed")
	}
	if json.NewEncoder(output).Encode(report) != nil {
		return 1
	}
	if len(report.Findings) > 0 {
		return 2
	}
	return 0
}

func failureCode(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline_exceeded"
	case errors.Is(err, opsinspect.ErrChanged):
		return "snapshot_changed"
	case errors.Is(err, capture.ErrConflict), errors.Is(err, window.ErrConflict):
		return "metadata_conflict"
	case errors.Is(err, window.ErrCorrupt):
		return "metadata_corrupt"
	case errors.Is(err, port.ErrNotFound), errors.Is(err, os.ErrNotExist):
		return "record_missing"
	default:
		return "inspection_failed"
	}
}

func privateDSN(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !privateOwner(info) || info.Size() < 1 || info.Size() > 16<<10 {
		return "", os.ErrPermission
	}
	f, err := regularFile(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	current, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !os.SameFile(info, current) || !privateOwner(current) {
		return "", os.ErrPermission
	}
	b, err := io.ReadAll(io.LimitReader(f, (16<<10)+1))
	if err != nil {
		return "", err
	}
	if len(b) > 16<<10 {
		return "", os.ErrPermission
	}
	dsn := strings.TrimRight(string(b), "\r\n")
	if dsn == "" || strings.ContainsAny(dsn, "\x00\r\n") {
		return "", os.ErrPermission
	}
	return dsn, nil
}
