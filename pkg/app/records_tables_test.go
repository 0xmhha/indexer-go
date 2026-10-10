package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/declared"
	"github.com/0xmhha/indexer-go/pkg/features/records"
	"github.com/0xmhha/indexer-go/pkg/testchain"
)

// withTables replaces the records tables of the receipts spec.
func withTables(sc *testchain.ReceiptsScenario, tables ...declared.Table) func(*config.Config) {
	return func(c *config.Config) {
		spec := receiptsSpec(sc)
		spec.Tables = tables
		if err := c.SetFeatureSettings(records.Name, spec); err != nil {
			panic(err)
		}
	}
}

var (
	receiptsTable = declared.Table{Name: "receipts", Source: "settlement", Event: "PaymentSettled", Keys: [][]string{{"merchant", "orderId"}}}
	byDeviceTable = declared.Table{Name: "by_device", Source: "settlement", Event: "PaymentSettled", Keys: [][]string{{"device"}}}
)

// TestRecordsTableAddedIsBackfilled: a table added to the records spec of an
// indexed database is filled with the logs indexed before it was added, the
// other tables are not processed again, and in the declared modes the logs
// are read a range at a time. A table whose definition changed stops
// startup; a removed table is recorded as stopped.
func TestRecordsTableAddedIsBackfilled(t *testing.T) {
	const head = 1_500
	for name, mode := range map[string]func(*config.Config){
		"full": func(*config.Config) {}, "declared per block": declaredMode, "declared ranges": sparseMode,
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			sc := longReceipts(head)
			srv := testchain.NewServer(sc.Chain)
			defer srv.Close()
			dir := filepath.Join(t.TempDir(), "db")

			app := startRecordsApp(t, srv, dir, sc, mode)
			runLiveUntil(t, app, head)
			app.Shutdown()

			before := srv.Calls()["eth_getLogs"]
			app = startRecordsApp(t, srv, dir, sc, mode, withTables(sc, receiptsTable, byDeviceTable))
			app.fetcher.WaitBackground()
			got, _, err := app.storage.(port.RecordReader).ListRecords(ctx, "by_device", port.FirstPage(100))
			require.NoError(t, err)
			assert.Len(t, got, len(sc.Payments), "the added table holds every earlier payment")
			requireReceipts(t, ctx, app.storage.(port.RecordReader), sc)
			if name != "full" {
				assert.LessOrEqual(t, srv.Calls()["eth_getLogs"]-before, 6, "the backfill reads a range per log request")
			}
			states, err := app.storage.(port.FeatureStateStore).FeatureStates(ctx)
			require.NoError(t, err)
			for _, table := range []string{"receipts", "by_device"} {
				st := states["records/"+table]
				assert.True(t, st.Active, table)
				assert.Nil(t, st.Gap, table)
				assert.NotEmpty(t, st.Definition, table)
			}
			app.Shutdown()

			changed := receiptsTable
			changed.Keys = [][]string{{"merchant"}}
			_, err = newRecordsApp(t, srv, dir, sc, mode, withTables(sc, changed, byDeviceTable))
			require.Error(t, err, "a table whose definition changed stops startup")
			assert.Contains(t, err.Error(), "records/receipts changed since it was indexed")

			app = startRecordsApp(t, srv, dir, sc, mode, withTables(sc, receiptsTable))
			defer app.Shutdown()
			states, err = app.storage.(port.FeatureStateStore).FeatureStates(ctx)
			require.NoError(t, err)
			assert.False(t, states["records/by_device"].Active, "a removed table is recorded as stopped")
			assert.True(t, states["records/receipts"].Active)
		})
	}
}

// TestRecordsTableGainsContract: a table whose source gains a contract
// (same event and keys) is filled again from the start instead of stopping
// startup, so it holds the added contract's earlier logs too and equals a
// database indexed with the new definition from the start.
func TestRecordsTableGainsContract(t *testing.T) {
	ctx := context.Background()
	for name, mode := range map[string]func(*config.Config){
		"full": func(*config.Config) {}, "declared ranges": sparseMode,
	} {
		t.Run(name, func(t *testing.T) {
			sc := longReceipts(300)
			srv := testchain.NewServer(sc.Chain)
			defer srv.Close()
			both := func(c *config.Config) {
				spec := receiptsSpec(sc)
				spec.Sources[0].Address = ""
				spec.Sources[0].Addresses = []string{sc.Settlement.Hex(), sc.Other.Hex()}
				if err := c.SetFeatureSettings(records.Name, spec); err != nil {
					panic(err)
				}
			}
			all := func(app *App) []*port.Record {
				got, _, err := app.storage.(port.RecordReader).ListRecords(ctx, "receipts", port.FirstPage(100))
				require.NoError(t, err)
				return got
			}

			dir := filepath.Join(t.TempDir(), "db")
			app := startRecordsApp(t, srv, dir, sc, mode)
			runLiveUntil(t, app, 300)
			before := len(all(app))
			app.Shutdown()

			app = startRecordsApp(t, srv, dir, sc, mode, both)
			defer app.Shutdown()
			app.fetcher.WaitBackground()
			got := all(app)
			assert.Greater(t, len(got), before, "the added contract's earlier logs are records now")

			fresh := startRecordsApp(t, srv, filepath.Join(t.TempDir(), "fresh"), sc, mode, both)
			defer fresh.Shutdown()
			runLiveUntil(t, fresh, 300)
			assert.Equal(t, all(fresh), got, "as if indexed with the new definition from the start")
		})
	}
}

// withRebuild sets the records spec with the tables that may be rebuilt.
func withRebuild(spec declared.Spec, rebuild ...string) func(*config.Config) {
	return func(c *config.Config) {
		if err := c.SetFeatureSettings(records.Name, records.Spec{Spec: spec, Rebuild: rebuild}); err != nil {
			panic(err)
		}
	}
}

// TestRecordsTableRebuilt: a table whose definition changed in a way that
// does not extend it (other keys, a contract removed) stops startup unless
// it is listed in features.records.rebuild; then its records are removed
// and indexed again from the start, and it equals a database indexed with
// the new definition from the start. A table not listed is kept, and a
// later start with the table still listed does not rebuild it again.
func TestRecordsTableRebuilt(t *testing.T) {
	ctx := context.Background()
	for name, mode := range map[string]func(*config.Config){
		"full": func(*config.Config) {}, "declared ranges": sparseMode,
	} {
		t.Run(name, func(t *testing.T) {
			sc := longReceipts(300)
			srv := testchain.NewServer(sc.Chain)
			defer srv.Close()
			spec := func(addresses []string, keys [][]string) declared.Spec {
				s := receiptsSpec(sc)
				s.Sources[0].Address = ""
				s.Sources[0].Addresses = addresses
				changed := receiptsTable
				changed.Keys = keys
				s.Tables = []declared.Table{changed, byDeviceTable}
				return s
			}
			both := []string{sc.Settlement.Hex(), sc.Other.Hex()}
			original := spec(both, [][]string{{"merchant", "orderId"}})
			otherKeys := spec(both, [][]string{{"merchant"}})
			oneContract := spec([]string{sc.Settlement.Hex()}, [][]string{{"merchant"}})
			list := func(app *App, table string) []*port.Record {
				got, _, err := app.storage.(port.RecordReader).ListRecords(ctx, table, port.FirstPage(1000))
				require.NoError(t, err)
				return got
			}
			byKey := func(app *App, key port.RecordKey) []*port.Record {
				got, _, err := app.storage.(port.RecordReader).ListRecordsByKey(ctx, "receipts", key, port.FirstPage(1000))
				require.NoError(t, err)
				return got
			}
			indexedFresh := func(s declared.Spec) map[string][]*port.Record {
				fresh := startRecordsApp(t, srv, filepath.Join(t.TempDir(), "fresh"), sc, mode, withRebuild(s))
				defer fresh.Shutdown()
				runLiveUntil(t, fresh, 300)
				return map[string][]*port.Record{"receipts": list(fresh, "receipts"), "by_device": list(fresh, "by_device")}
			}

			dir := filepath.Join(t.TempDir(), "db")
			app := startRecordsApp(t, srv, dir, sc, mode, withRebuild(original))
			runLiveUntil(t, app, 300)
			devices := list(app, "by_device")
			app.Shutdown()

			_, err := newRecordsApp(t, srv, dir, sc, mode, withRebuild(otherKeys))
			require.ErrorContains(t, err, "records/receipts changed since it was indexed", "not listed: refused")
			_, err = newRecordsApp(t, srv, dir, sc, mode, withRebuild(otherKeys, "missing"))
			require.ErrorContains(t, err, `table "missing" is not declared`)

			// Other keys: the table is rebuilt, the other one kept.
			app = startRecordsApp(t, srv, dir, sc, mode, withRebuild(otherKeys, "receipts"))
			app.fetcher.WaitBackground()
			first := sc.Payments[0]
			merchant := strings.ToLower(first.Merchant.Hex())
			assert.NotEmpty(t, byKey(app, port.RecordKey{ID: "merchant", Values: []string{merchant}}), "the new key finds records")
			assert.Empty(t, byKey(app, port.RecordKey{ID: "merchant,orderId", Values: []string{merchant, first.OrderID.Hex()}}),
				"the earlier key's entries are gone")
			assert.Equal(t, devices, list(app, "by_device"), "the table not listed is kept")
			assert.Equal(t, indexedFresh(otherKeys)["receipts"], list(app, "receipts"), "as if indexed with the new definition from the start")
			states, err := app.storage.(port.FeatureStateStore).FeatureStates(ctx)
			require.NoError(t, err)
			assert.True(t, states["records/receipts"].Active)
			assert.Nil(t, states["records/receipts"].Gap, "the rebuild completed")
			app.Shutdown()

			// A contract removed from the shared source changes both tables.
			app = startRecordsApp(t, srv, dir, sc, mode, withRebuild(oneContract, "receipts", "by_device"))
			app.fetcher.WaitBackground()
			want := indexedFresh(oneContract)
			assert.Less(t, len(list(app, "receipts")), len(devices), "the removed contract's records are gone")
			assert.Equal(t, want["receipts"], list(app, "receipts"))
			assert.Equal(t, want["by_device"], list(app, "by_device"))
			app.Shutdown()

			calls := srv.Calls()["eth_getLogs"]
			app = startRecordsApp(t, srv, dir, sc, mode, withRebuild(oneContract, "receipts", "by_device"))
			defer app.Shutdown()
			app.fetcher.WaitBackground()
			assert.Equal(t, want["receipts"], list(app, "receipts"), "still listed but unchanged: kept")
			if name != "full" {
				assert.Equal(t, calls, srv.Calls()["eth_getLogs"], "and not read again")
			}
		})
	}
}
