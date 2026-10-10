// Package records is the records feature (refactoring plan R6-1): it
// stores the logs of the tables declared in features.records (see
// pkg/declared) as records, indexed under each declared key.
package records

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/declared"
	"github.com/0xmhha/indexer-go/pkg/feature"
)

// Name is the feature name.
const Name = "records"

// Spec is the feature's section: the declaration and the tables that may
// be rebuilt.
type Spec struct {
	declared.Spec `yaml:",inline"`
	// Rebuild names tables whose records may be removed and indexed again
	// from the start when their definition changed in a way that does not
	// extend them (another event or keys, fewer contracts). Without it
	// such a change stops startup. Queries of a table see only part of it
	// until its rebuild completes.
	Rebuild []string `yaml:"rebuild"`
}

// Settings decodes the feature's settings and compiles them.
func Settings(decode func(feature string, into any) error) (*declared.Plan, error) {
	plan, _, err := settings(decode)
	return plan, err
}

// settings is Settings with the tables that may be rebuilt.
func settings(decode func(feature string, into any) error) (*declared.Plan, map[string]bool, error) {
	var spec Spec
	if err := decode(Name, &spec); err != nil {
		return nil, nil, err
	}
	plan, err := declared.Compile(spec.Spec)
	if err != nil {
		return nil, nil, fmt.Errorf("features.%s: %w", Name, err)
	}
	rebuild := map[string]bool{}
	for _, name := range spec.Rebuild {
		if _, ok := plan.Table(name); !ok {
			return nil, nil, fmt.Errorf("features.%s.rebuild: table %q is not declared", Name, name)
		}
		rebuild[name] = true
	}
	return plan, rebuild, nil
}

type recordsFeature struct{}

func (recordsFeature) Name() string       { return Name }
func (recordsFeature) Requires() []string { return nil }

// records is order-independent: a record is identified by its log.
func (recordsFeature) OrderIndependent() bool { return true }

// EvolvePart implements feature.PartEvolver: a table that gained contracts
// (same event and keys) is backfilled again from the start, which stores
// the added contracts' logs and rewrites the existing records unchanged; any
// other change needs a new table name or a reindex.
func (recordsFeature) EvolvePart(part, stored, current string) feature.PartChange {
	var cur declared.TableDefinition
	if err := json.Unmarshal([]byte(current), &cur); err != nil {
		return feature.PartIncompatible
	}
	switch declared.CompareDefinitions(stored, cur) {
	case declared.DefinitionUnchanged:
		return feature.PartUnchanged
	case declared.DefinitionExtended:
		return feature.PartExtended
	}
	return feature.PartIncompatible
}

// LogsOnly marks the feature as reading only the logs of the declared
// contracts, so it runs in the declared ingest mode.
func (recordsFeature) LogsOnly() bool { return true }

func (recordsFeature) Register(r feature.Registrar) error {
	deps := r.Deps()
	store, ok := deps.Storage.(port.RecordWriter)
	if !ok {
		return errors.New("storage does not support records")
	}
	plan, rebuild, err := settings(deps.DecodeSettings)
	if err != nil {
		return err
	}
	logger := deps.Logger
	if logger == nil {
		logger = zap.NewNop()
	}
	// Each table is a part, so a table added to an indexed database is
	// backfilled alone and a table whose definition changed is refused
	// unless it may be rebuilt.
	if pr, ok := r.(feature.PartRegistrar); ok {
		for _, t := range plan.Tables {
			pr.OnPart(t.Name, t.Definition(), &handler{store: store, plan: plan, table: t, rebuild: rebuild[t.Name], logger: logger})
		}
		return nil
	}
	r.OnBlock(&handler{store: store, plan: plan, logger: logger})
	return nil
}

func init() { feature.Register(recordsFeature{}) }

type handler struct {
	store port.RecordWriter
	plan  *declared.Plan
	// table, when set, is the one table the handler stores; every table
	// otherwise.
	table *declared.TablePlan
	// rebuild: the table is listed in features.records.rebuild.
	rebuild bool
	logger  *zap.Logger
}

// RebuildAllowed implements feature.PartRebuilder.
func (h *handler) RebuildAllowed() bool { return h.table != nil && h.rebuild }

// ResetPart implements feature.PartRebuilder: it removes the table's
// records.
func (h *handler) ResetPart(ctx context.Context) error {
	if h.table == nil {
		return errors.New("records: only a table can be reset")
	}
	return h.store.DeleteRecords(ctx, h.table.Name)
}

// HandleBlock implements feature.BlockHandler: every log of a declared
// table becomes a record. A log of a declared contract and event that does
// not decode (a contract emitting the event with other indexed arguments)
// is skipped with a warning rather than stopping indexing.
func (h *handler) HandleBlock(ctx context.Context, b *feature.Block) error {
	for _, receipt := range b.Receipts {
		for _, l := range receipt.Logs {
			for _, t := range h.plan.Match(l) {
				if h.table != nil && t != h.table {
					continue
				}
				if err := h.save(ctx, b.Model, t, l); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (h *handler) save(ctx context.Context, b *model.Block, t *declared.TablePlan, l *model.Log) error {
	fields, err := t.Decode(l)
	if err != nil {
		h.logger.Warn("Skipping a log that does not decode as its table's event",
			zap.String("table", t.Name), zap.Uint64("block", l.BlockNumber), zap.Uint("log_index", l.Index), zap.Error(err))
		return nil
	}
	r := &port.Record{Table: t.Name, BlockNumber: l.BlockNumber, BlockTime: b.Time, TxHash: l.TxHash,
		LogIndex: l.Index, Address: l.Address, Fields: fields}
	keys := make([]port.RecordKey, len(t.Keys))
	for i, k := range t.Keys {
		keys[i] = port.RecordKey{ID: declared.KeyID(k), Values: make([]string, len(k))}
		for j, f := range k {
			keys[i].Values[j] = fields[f]
		}
	}
	if err := h.store.SaveRecord(ctx, r, keys); err != nil {
		return fmt.Errorf("save record of table %s: %w", t.Name, err)
	}
	return nil
}

// Plans are found by the storage they are served from, so the API of a
// chain finds its tables.
var attached sync.Map // storage -> *declared.Plan

// Attach makes plan the tables served from store.
func Attach(store any, plan *declared.Plan) { attached.Store(store, plan) }

// Lookup returns the tables served from store, nil when there are none.
func Lookup(store any) *declared.Plan {
	if v, ok := attached.Load(store); ok {
		return v.(*declared.Plan)
	}
	return nil
}

// Detach removes the tables of store.
func Detach(store any) { attached.Delete(store) }
