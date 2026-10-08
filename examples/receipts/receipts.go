// Package receipts is an example of a project's own handlers built on the
// indexer SDK (pkg/sdk) without changing the indexer: the receipt lookup of
// a payment settlement contract (the P07 receipt indexer).
//
// The indexer stores the contract's PaymentSettled logs as the records of
// a declared table (features.records, indexer.mode declared). This package
// adds:
//
//   - GET /receipts/{merchant}/{orderId}: the earliest PaymentSettled log
//     of the order, marked duplicate when the order settled more than once,
//     or 404 NOT_INDEXED;
//   - the receipts.totals feature, which keeps each merchant's receipt
//     count and amount in the indexer's key-value store, block by block,
//     and GET /merchants/{merchant}/totals.
package receipts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"regexp"

	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/sdk"
)

// Table is the declared table of PaymentSettled logs; it must declare the
// key [merchant, orderId].
const Table = "receipts"

// TotalsName is the feature keeping each merchant's totals.
const TotalsName = "receipts.totals"

const totalsPrefix = "/x/receipts/total/"

func init() {
	sdk.RegisterFeature(totals{})
	sdk.RegisterKeyspace(TotalsName, "/x/receipts/")
	sdk.RegisterRoute(http.MethodGet, "/receipts/{merchant}/{orderId}", receiptRoute)
	sdk.RegisterRoute(http.MethodGet, "/merchants/{merchant}/totals", totalsRoute)
}

// Receipt is the response of GET /receipts/{merchant}/{orderId}.
type Receipt struct {
	Merchant    string `json:"merchant"`
	OrderID     string `json:"orderId"`
	Device      string `json:"device"`
	Amount      string `json:"amount"`
	BlockNumber uint64 `json:"blockNumber"`
	BlockTime   uint64 `json:"blockTime"`
	TxHashShort string `json:"txHashShort"`
	Duplicate   bool   `json:"duplicate"`
}

// Short writes a hash as 0x, its next 6 digits, "…" and its last 4.
func Short(h string) string {
	if len(h) < 12 {
		return h
	}
	return h[:8] + "…" + h[len(h)-4:]
}

var (
	addressPattern = regexp.MustCompile(`^0x[0-9a-fA-F]{40}$`)
	orderPattern   = regexp.MustCompile(`^0x[0-9a-fA-F]{64}$`)
)

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func receiptRoute(store sdk.Store, logger *zap.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		merchant, order := sdk.URLParam(r, "merchant"), sdk.URLParam(r, "orderId")
		if !addressPattern.MatchString(merchant) || !orderPattern.MatchString(order) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "BAD_REQUEST"})
			return
		}
		// The earliest two logs of the order: one is the receipt, two mean
		// the order settled twice.
		recs, _, err := sdk.LookupRecords(r.Context(), store, Table, map[string]string{"merchant": merchant, "orderId": order}, sdk.Page{Limit: 2})
		if err != nil {
			logger.Error("Receipt lookup failed", zap.Error(err))
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "INTERNAL"})
			return
		}
		if len(recs) == 0 {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "NOT_INDEXED"})
			return
		}
		first := recs[0]
		writeJSON(w, http.StatusOK, Receipt{
			Merchant: first.Fields["merchant"], OrderID: first.Fields["orderId"], Device: first.Fields["device"],
			Amount: first.Fields["amount"], BlockNumber: first.BlockNumber, BlockTime: first.BlockTime,
			TxHashShort: Short(first.TxHash.Hex()), Duplicate: len(recs) > 1,
		})
	})
}

// Totals are a merchant's receipts so far.
type Totals struct {
	Receipts uint64 `json:"receipts"`
	Amount   string `json:"amount"`
}

func totalsKey(merchant string) []byte { return []byte(totalsPrefix + merchant) }

func readTotals(ctx context.Context, kv sdk.KV, merchant string) (Totals, error) {
	data, err := kv.Get(ctx, totalsKey(merchant))
	if errors.Is(err, sdk.ErrNotFound) {
		return Totals{Amount: "0"}, nil
	}
	if err != nil {
		return Totals{}, err
	}
	var t Totals
	return t, json.Unmarshal(data, &t)
}

// totals is the receipts.totals feature.
type totals struct{}

func (totals) Name() string       { return TotalsName }
func (totals) Requires() []string { return []string{"records"} }

// Sums do not depend on the order blocks are processed in.
func (totals) OrderIndependent() bool { return true }

// It reads only the declared logs, so it runs with indexer.mode declared.
func (totals) LogsOnly() bool { return true }

func (totals) Register(r sdk.Registrar) error {
	deps := r.Deps()
	tables, err := sdk.DeclaredTables(deps)
	if err != nil {
		return err
	}
	table, ok := tables.Table(Table)
	if !ok {
		return fmt.Errorf("%s needs the declared table %q", TotalsName, Table)
	}
	kv, err := sdk.KVOf(deps.Storage)
	if err != nil {
		return err
	}
	r.OnBlock(sdk.BlockHandlerFunc(func(ctx context.Context, b *sdk.Block) error {
		for _, receipt := range b.Receipts {
			for _, l := range receipt.Logs {
				if !matches(tables, table, l) {
					continue
				}
				fields, err := table.Decode(l)
				if err != nil {
					continue // the records feature warns about it
				}
				if err := add(ctx, kv, fields["merchant"], fields["amount"]); err != nil {
					return err
				}
			}
		}
		return nil
	}))
	return nil
}

func matches(tables *sdk.Tables, table *sdk.Table, l *sdk.Log) bool {
	for _, t := range tables.Match(l) {
		if t == table {
			return true
		}
	}
	return false
}

// add adds a receipt to a merchant's totals; inside the block transaction
// a later read sees the write.
func add(ctx context.Context, kv sdk.KV, merchant, amount string) error {
	t, err := readTotals(ctx, kv, merchant)
	if err != nil {
		return err
	}
	sum, ok1 := new(big.Int).SetString(t.Amount, 10)
	a, ok2 := new(big.Int).SetString(amount, 10)
	if !ok1 || !ok2 {
		return fmt.Errorf("amounts %q and %q", t.Amount, amount)
	}
	t.Receipts++
	t.Amount = sum.Add(sum, a).String()
	data, err := json.Marshal(t)
	if err != nil {
		return err
	}
	return kv.Put(ctx, totalsKey(merchant), data)
}

func totalsRoute(store sdk.Store, logger *zap.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		merchant := sdk.URLParam(r, "merchant")
		if !addressPattern.MatchString(merchant) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "BAD_REQUEST"})
			return
		}
		kv, err := sdk.KVOf(store)
		if err == nil {
			var t Totals
			if t, err = readTotals(r.Context(), kv, normalize(merchant)); err == nil {
				writeJSON(w, http.StatusOK, t)
				return
			}
		}
		logger.Error("Totals lookup failed", zap.Error(err))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "INTERNAL"})
	})
}

// normalize writes an address as records store it (lower-case).
func normalize(address string) string {
	b := []byte(address)
	for i, c := range b {
		if c >= 'A' && c <= 'F' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}
