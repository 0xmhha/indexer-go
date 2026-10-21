package featuretest_test

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/declared"
	"github.com/0xmhha/indexer-go/pkg/feature"
	"github.com/0xmhha/indexer-go/pkg/feature/featuretest"
	"github.com/0xmhha/indexer-go/pkg/storage"
	"github.com/0xmhha/indexer-go/pkg/testchain"
)

// probe is a test feature that stores one value per block under
// /x/featuretest/<name>/<block>, computed by value.
type probe struct {
	name  string
	value valueFunc
	// handler, when set, makes the value function of each handler.
	handler func() valueFunc
}

type valueFunc func(ctx context.Context, deps feature.Deps, b *feature.Block) uint64

func (p probe) Name() string         { return "featuretest." + p.name }
func (probe) Requires() []string     { return nil }
func (probe) OrderIndependent() bool { return true }
func (p probe) prefix() string       { return "/x/featuretest/" + p.name + "/" }
func (p probe) Register(r feature.Registrar) error {
	kv, ok := r.Deps().Storage.(port.KV)
	if !ok {
		return errors.New("the storage has no key-value store")
	}
	deps, value := r.Deps(), p.value
	if p.handler != nil {
		value = p.handler()
	}
	r.OnBlock(feature.BlockHandlerFunc(func(ctx context.Context, b *feature.Block) error {
		key := binary.BigEndian.AppendUint64([]byte(p.prefix()), b.Model.Number)
		return kv.Put(ctx, key, binary.BigEndian.AppendUint64(nil, value(ctx, deps, b)))
	}))
	return nil
}

var (
	// hash is deterministic: a value of the block.
	hashProbe = probe{name: "hash", value: func(_ context.Context, _ feature.Deps, b *feature.Block) uint64 {
		return binary.BigEndian.Uint64(b.Model.Hash[:8])
	}}
	// clock reads the clock.
	clockProbe = probe{name: "clock", value: func(context.Context, feature.Deps, *feature.Block) uint64 {
		return uint64(time.Now().UnixNano())
	}}
	// memory counts the blocks its handler handled, in memory.
	memoryProbe = probe{name: "memory", handler: func() valueFunc {
		var n uint64
		return func(context.Context, feature.Deps, *feature.Block) uint64 { n++; return n }
	}}
	// live reads the latest indexed height: the previous block when
	// indexing live, the head when backfilled.
	liveProbe = probe{name: "live", value: func(ctx context.Context, deps feature.Deps, _ *feature.Block) uint64 {
		h, _ := deps.Storage.GetLatestHeight(ctx)
		return h
	}}
)

func init() {
	for _, p := range []probe{hashProbe, clockProbe, memoryProbe, liveProbe} {
		feature.Register(p)
	}
	storage.RegisterKeyspace("featuretest", storage.ChainData, "/x/featuretest/")
}

func check(p probe) featuretest.Check {
	ch := testchain.NewChain(testchain.DefaultChainID, nil)
	for range 12 {
		ch.AddBlock()
	}
	return featuretest.Check{Chain: ch, Features: []string{p.Name()}, Prefixes: []string{p.prefix()}}
}

// fatalTB records the failure of a check expected to fail.
type fatalTB struct {
	testing.TB
	msg string
}

func (f *fatalTB) Helper() {}
func (f *fatalTB) Fatalf(format string, args ...any) {
	f.msg = fmt.Sprintf(format, args...)
	panic(f)
}

func failure(t *testing.T, fn func(testing.TB)) (msg string) {
	tb := &fatalTB{TB: t}
	defer func() {
		if r := recover(); r != nil {
			if r != tb {
				panic(r)
			}
			msg = tb.msg
		}
	}()
	fn(tb)
	return ""
}

func TestRequireDeterministic(t *testing.T) {
	t.Run("a block's own values pass", func(t *testing.T) {
		featuretest.RequireDeterministic(t, check(hashProbe))
	})
	for _, c := range []struct {
		probe probe
		run   string
	}{
		{clockProbe, "indexed again"},
		{memoryProbe, "stopped half-way and restarted"},
		{liveProbe, "enabled after indexing (backfill)"},
	} {
		t.Run(c.probe.name+" is caught by "+c.run, func(t *testing.T) {
			msg := failure(t, func(tb testing.TB) { featuretest.RequireDeterministic(tb, check(c.probe)) })
			require.NotEmpty(t, msg, "the check passed")
			assert.Contains(t, msg, "not deterministic: "+c.run)
		})
	}
	t.Run("a check that stores nothing fails", func(t *testing.T) {
		c := check(hashProbe)
		c.Prefixes = []string{"/x/featuretest/none/"}
		assert.Contains(t, failure(t, func(tb testing.TB) { featuretest.RequireDeterministic(tb, c) }), "stored nothing")
	})
}

// TestRecordsAreDeterministic checks the records feature: in the full mode
// (enabled late too), and in the declared mode per block and with
// finalized ranges, where it is always on.
func TestRecordsAreDeterministic(t *testing.T) {
	sc := testchain.BuildReceipts()
	spec := declared.Spec{
		Sources: []declared.Source{{Name: "settlement", Address: sc.Settlement.Hex(), Events: []string{testchain.PaymentSettledSignature}}},
		Tables:  []declared.Table{{Name: "receipts", Source: "settlement", Event: "PaymentSettled", Keys: [][]string{{"merchant", "orderId"}}}},
	}
	prefixes := []string{"/rec/", "/reckey/"}
	settings := map[string]any{"records": spec}
	t.Run("full", func(t *testing.T) {
		featuretest.RequireDeterministic(t, featuretest.Check{Chain: sc.Chain, Features: []string{"records"}, Settings: settings, Prefixes: prefixes})
	})
	for _, finality := range []string{"head", "finalized"} {
		t.Run("declared, finality "+finality, func(t *testing.T) {
			featuretest.RequireDeterministic(t, featuretest.Check{
				Chain: sc.Chain, Requires: []string{"records"}, Settings: settings,
				Prefixes: prefixes, Declared: true, Finality: finality,
			})
		})
	}
}

func TestScanSource(t *testing.T) {
	found, err := featuretest.ScanSource("testdata/scan")
	require.NoError(t, err)
	var got []string
	for _, f := range found {
		got = append(got, fmt.Sprintf("%d %s", f.Pos.Line, f.Use))
	}
	assert.Equal(t, []string{
		"18 time.Now", "19 math/rand.Intn", "20 os.Getenv", "21 net/http.Get", "22 CallContract(..., nil)",
	}, got, strings.Join(got, "\n"))
}

// TestBuiltInFeaturesFollowTheRules scans the indexer's own feature
// packages.
func TestBuiltInFeaturesFollowTheRules(t *testing.T) {
	featuretest.RequireNoForbiddenUses(t, featureDirs(t)...)
}
