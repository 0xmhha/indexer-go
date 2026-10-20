package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/opsinspect"
	"github.com/0xmhha/indexer-go/pkg/storage/postgres"
	"github.com/0xmhha/indexer-go/pkg/window"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

type fakeDB struct {
	data   map[string][]byte
	page   port.IndexingPage
	fail   error
	onView func()
}
type fakeValues map[string][]byte

func (v fakeValues) Get(k []byte) ([]byte, error) {
	b, ok := v[string(k)]
	if !ok {
		return nil, port.ErrNotFound
	}
	return bytes.Clone(b), nil
}
func (d *fakeDB) CaptureView(ctx context.Context, _ model.FeedChain, fn func(port.CaptureReader) error) error {
	if d.onView != nil {
		d.onView()
	}
	if e := ctx.Err(); e != nil {
		return e
	}
	if d.fail != nil {
		return d.fail
	}
	return fn(fakeValues(d.data))
}
func (*fakeDB) CaptureUpdate(context.Context, model.FeedChain, func(port.CaptureWriter) error) error {
	panic("unexpected database write")
}
func (d *fakeDB) ReadIndexingPage(ctx context.Context, _ string, _ int) (port.IndexingPage, error) {
	return d.page, ctx.Err()
}

func commandFixture(t *testing.T) ([]string, *fakeDB, string) {
	t.Helper()
	root := t.TempDir()
	volume := filepath.Join(root, "volume")
	require.NoError(t, os.Mkdir(volume, 0700))
	chain := model.FeedChain{ID: "5042002", GenesisHash: common.Hash{1}}
	path := filepath.Join(volume, "raw")
	layout := fmt.Sprintf("volumes:\n  fixture:\n    root: %q\n    id: fixture\n    allow_directory: true\n    max_bytes: 8388608\nwindows:\n  arc:\n    volume: fixture\n    path: %q\n    max_bytes: 2097152\n    segment_bytes: 32768\n    max_payload_bytes: 2048\n    max_segment_records: 8\n", volume, path)
	l, e := window.ReadLayout(bytes.NewBufferString(layout))
	require.NoError(t, e)
	opts, e := l.OptionsFor("arc", chain)
	require.NoError(t, e)
	require.NoError(t, window.InitializeVolume(opts.Volume))
	raw, e := window.Create(opts)
	require.NoError(t, e)
	require.NoError(t, raw.Close())
	layoutPath := filepath.Join(root, "layout.yaml")
	require.NoError(t, os.WriteFile(layoutPath, []byte(layout), 0600))
	dsnPath := filepath.Join(root, "dsn")
	require.NoError(t, os.WriteFile(dsnPath, []byte("postgresql://reader:SECRET-FIXTURE@localhost/fixture\n"), 0600))
	plan := model.CapturePlan{Version: 1, Required: []model.DatasetRequirement{{Kind: model.DatasetBodies, Coverage: model.DatasetCoverage{Scope: model.CoverageComplete}}}}
	anchor := model.FeedBlock{Chain: chain, Hash: chain.GenesisHash}
	d := &fakeDB{data: map[string][]byte{}, page: port.IndexingPage{Descriptor: &model.ProjectionDescriptor{Version: 1, Anchor: anchor, Plan: plan}, Units: []port.IndexingUnit{}}}
	d.data["state"], e = json.Marshal(capture.State{Generation: 1, Plan: plan, Anchor: anchor, Through: anchor})
	require.NoError(t, e)
	d.data["app/feed-window/v1"], e = json.Marshal(capture.WindowBinding{Volume: opts.Volume.ID, Root: volume, Path: path, Reservation: opts.Reservation})
	require.NoError(t, e)
	return []string{"--layout", layoutPath, "--window", "arc", "--chain-id", chain.ID, "--genesis", chain.GenesisHash.Hex(), "--schema", "fixture_arc", "--dsn-file", dsnPath}, d, dsnPath
}

func TestInspectCommandReadOnlyOptionsAndClose(t *testing.T) {
	for _, kind := range []string{"ok", "partial", "read_error", "close_error", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			args, db, _ := commandFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if kind == "partial" {
				db.page.Next = "more"
			}
			if kind == "read_error" {
				db.fail = errors.New("error containing SECRET-FIXTURE")
			}
			if kind == "cancel" {
				db.onView = cancel
			}
			closed, calls := 0, 0
			var output bytes.Buffer
			code := run(ctx, args, &output, func(ctx context.Context, opts postgres.Options) (opsinspect.Database, func() error, error) {
				calls++
				require.True(t, opts.ReadOnly)
				require.EqualValues(t, 1, opts.MaxConns)
				require.Empty(t, opts.WritablePrefixes)
				require.Equal(t, "fixture_arc", opts.Schema)
				require.Contains(t, opts.DSN, "SECRET-FIXTURE")
				_, deadline := ctx.Deadline()
				require.True(t, deadline)
				return db, func() error {
					closed++
					if kind == "close_error" {
						return errors.New("SECRET-FIXTURE")
					}
					return nil
				}, nil
			})
			require.Equal(t, 1, calls)
			require.Equal(t, 1, closed)
			require.NotContains(t, output.String(), "SECRET-FIXTURE")
			switch kind {
			case "ok":
				require.Equal(t, 0, code)
			case "partial":
				require.Equal(t, 2, code)
				require.Contains(t, output.String(), "feature_page_incomplete")
			default:
				require.Equal(t, 1, code)
			}
			if kind == "cancel" {
				require.Contains(t, output.String(), "cancelled")
			}
			require.Contains(t, output.String(), `"gcDecision":"not_authorized"`)
		})
	}
}

func TestInspectCommandInvalidInputsNeverOpenDatabase(t *testing.T) {
	for _, extra := range [][]string{{"--limit", "0"}, {"--timeout", "31s"}, {"--genesis", "0x1234"}, {"--window", "missing"}, {"--unknown", "SECRET-FIXTURE"}} {
		t.Run(fmt.Sprint(extra[0]), func(t *testing.T) {
			args, _, _ := commandFixture(t)
			var output bytes.Buffer
			code := run(context.Background(), append(args, extra...), &output, func(context.Context, postgres.Options) (opsinspect.Database, func() error, error) {
				t.Fatal("invalid input opened database")
				return nil, nil, nil
			})
			require.Equal(t, 1, code)
			require.NotContains(t, output.String(), "SECRET-FIXTURE")
		})
	}
}

func TestPrivateDSNRejectsPublicLinksOversizeAndControlBytes(t *testing.T) {
	for _, kind := range []string{"public", "symlink", "hardlink", "oversize", "control", "empty"} {
		t.Run(kind, func(t *testing.T) {
			args, _, path := commandFixture(t)
			switch kind {
			case "public":
				require.NoError(t, os.Chmod(path, 0644))
			case "symlink":
				require.NoError(t, os.Rename(path, path+".real"))
				require.NoError(t, os.Symlink(path+".real", path))
			case "hardlink":
				require.NoError(t, os.Link(path, path+".link"))
			case "oversize":
				require.NoError(t, os.WriteFile(path, make([]byte, (16<<10)+1), 0600))
			case "control":
				require.NoError(t, os.WriteFile(path, []byte("secret\x00value"), 0600))
			case "empty":
				require.NoError(t, os.WriteFile(path, nil, 0600))
			}
			var output bytes.Buffer
			code := run(context.Background(), args, &output, func(context.Context, postgres.Options) (opsinspect.Database, func() error, error) {
				t.Fatal("unsafe DSN reached database")
				return nil, nil, nil
			})
			require.Equal(t, 1, code)
			require.Contains(t, output.String(), "private_dsn_unavailable")
			require.NotContains(t, output.String(), "SECRET-FIXTURE")
		})
	}
}

func TestInspectCommandDatabaseErrorsAreRedacted(t *testing.T) {
	args, _, _ := commandFixture(t)
	var output bytes.Buffer
	code := run(context.Background(), args, &output, func(context.Context, postgres.Options) (opsinspect.Database, func() error, error) {
		return nil, nil, errors.New("postgresql://reader:SECRET-FIXTURE@localhost")
	})
	require.Equal(t, 1, code)
	require.Contains(t, output.String(), "database_unavailable")
	require.NotContains(t, output.String(), "SECRET-FIXTURE")
}

func TestInspectCommandHelpDoesNotOpenDatabase(t *testing.T) {
	var output bytes.Buffer
	code := run(context.Background(), []string{"--help"}, &output, func(context.Context, postgres.Options) (opsinspect.Database, func() error, error) {
		t.Fatal("help opened database")
		return nil, nil, nil
	})
	require.Zero(t, code)
	require.Contains(t, output.String(), "-dsn-file")
}
