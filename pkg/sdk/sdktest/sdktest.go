// Package sdktest checks that a project's handlers follow the determinism
// rules of docs/SDK.md: the same chain gives the same stored data however
// it is indexed (from genesis, again, with a restart, or by backfill after
// the feature is enabled late), and the handler's source reads no clock,
// randomness, environment or network, and no "latest" node state.
package sdktest

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/pkg/app"
	"github.com/0xmhha/indexer-go/pkg/testchain"
)

// Check is what RequireDeterministic indexes and compares.
type Check struct {
	// Chain is indexed up to its head.
	Chain *testchain.Chain
	// Features are the features checked; Requires are further features
	// enabled in every run (for example records), whose data is checked
	// too when Prefixes cover it. With no Features the run that enables
	// them late is skipped: in the declared mode records cannot be off, so
	// it is checked as a requirement.
	Features, Requires []string
	// Settings are features.<name> sections, as the YAML would decode.
	Settings map[string]any
	// Prefixes are the key prefixes compared: the features' keyspace.
	Prefixes []string
	// Declared selects indexer.mode declared; Finality indexer.finality
	// ("" keeps the default).
	Declared bool
	Finality string
}

// RequireDeterministic indexes c.Chain four ways and requires the keys under
// c.Prefixes to be the same each time:
//
//   - from genesis (the reference);
//   - from genesis again, into a new database: a handler that reads the
//     clock, randomness or state outside the chain differs;
//   - stopped half-way and restarted: a handler that keeps state in memory
//     differs;
//   - with Features enabled only after the head is indexed, so they are
//     backfilled (online when order-independent): a handler that depends
//     on the order blocks are processed in, or on live-only inputs, differs.
//
// It does not check reorganizations: rollback undoes what a handler wrote
// through the block transaction, whatever it wrote.
func RequireDeterministic(t testing.TB, c Check) {
	t.Helper()
	head := c.Chain.Head()
	defer c.Chain.SetHead(head)
	srv := testchain.NewServer(c.Chain)
	defer srv.Close()

	run := func(dir string, features bool) map[string][]byte {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		cfg, err := c.config(srv.URL(), dir, features)
		if err != nil {
			t.Fatalf("sdktest: configuration: %v", err)
		}
		keys, err := app.IndexForCheck(ctx, cfg, c.Chain.Head(), c.Prefixes)
		if err != nil {
			t.Fatalf("sdktest: indexing to block %d: %v", c.Chain.Head(), err)
		}
		return keys
	}
	dir := func(name string) string { return filepath.Join(t.TempDir(), name) }

	ref := run(dir("reference"), true)
	if len(ref) == 0 {
		t.Fatalf("sdktest: the features stored nothing under %v; the check would prove nothing", c.Prefixes)
	}
	requireSame(t, "indexed again", ref, run(dir("again"), true))

	restarted := dir("restarted")
	c.Chain.SetHead(head / 2)
	run(restarted, true)
	c.Chain.SetHead(head)
	requireSame(t, "stopped half-way and restarted", ref, run(restarted, true))

	if len(c.Features) == 0 {
		return
	}
	late := dir("enabled-late")
	run(late, false)
	requireSame(t, "enabled after indexing (backfill)", ref, run(late, true))
}

func (c Check) config(endpoint, dir string, features bool) (*config.Config, error) {
	cfg := config.NewConfig()
	cfg.RPC.Endpoint = endpoint
	cfg.RPC.Timeout = 5 * time.Second
	cfg.Database.Path = dir
	cfg.Indexer.ChunkSize = 1 // retry delay 200ms
	if c.Declared {
		cfg.Indexer.Mode = config.ModeDeclared
	}
	if c.Finality != "" {
		cfg.Indexer.Finality = c.Finality
	}
	cfg.Features = map[string]config.FeatureConfig{}
	for name, s := range c.Settings {
		if err := cfg.SetFeatureSettings(name, s); err != nil {
			return nil, err
		}
	}
	enable := func(name string, on bool) {
		fc := cfg.Features[name]
		fc.Enabled = &on
		cfg.Features[name] = fc
	}
	for _, n := range c.Requires {
		enable(n, true)
	}
	for _, n := range c.Features {
		enable(n, features)
	}
	return cfg, nil
}

// requireSame fails with the first differences between two key sets.
func requireSame(t testing.TB, how string, want, got map[string][]byte) {
	t.Helper()
	var diffs []string
	for k, v := range want {
		g, ok := got[k]
		switch {
		case !ok:
			diffs = append(diffs, fmt.Sprintf("missing %s", printable(k)))
		case !bytes.Equal(v, g):
			diffs = append(diffs, fmt.Sprintf("differs %s:\n    reference %s\n    %s %s", printable(k), short(v), how, short(g)))
		}
	}
	for k := range got {
		if _, ok := want[k]; !ok {
			diffs = append(diffs, fmt.Sprintf("extra %s", printable(k)))
		}
	}
	if len(diffs) == 0 {
		return
	}
	sort.Strings(diffs)
	if len(diffs) > 10 {
		diffs = append(diffs[:10], fmt.Sprintf("... %d more", len(diffs)-10))
	}
	t.Fatalf("sdktest: not deterministic: %s stores other data than indexing from genesis:\n  %s", how, strings.Join(diffs, "\n  "))
}

func printable(k string) string { return fmt.Sprintf("%q", k) }

func short(v []byte) string {
	if len(v) > 96 {
		return fmt.Sprintf("%q... (%d bytes)", v[:96], len(v))
	}
	return fmt.Sprintf("%q", v)
}
