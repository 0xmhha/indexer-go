package api

import (
	"errors"
	"strconv"

	gql "github.com/graphql-go/graphql"

	"github.com/0xmhha/indexer-go/pkg/api/graphql"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/features/dex/orderbook"
)

// The order book (refactoring plan R5-2).
var (
	levelType = gql.NewObject(gql.ObjectConfig{
		Name:        "DexBookLevel",
		Description: "A price level: the base the market gives (asks) or takes (bids) between the previous level and this price, and the quote for it",
		Fields: gql.Fields{
			"price":       {Type: gql.NewNonNull(graphql.BigIntType), Description: "Quote per base in raw units, times 1e18"},
			"baseAmount":  {Type: gql.NewNonNull(graphql.BigIntType), Description: "Raw units"},
			"quoteAmount": {Type: gql.NewNonNull(graphql.BigIntType), Description: "Raw units"},
		},
	})
	reconciliationType = gql.NewObject(gql.ObjectConfig{
		Name:        "DexBookReconciliation",
		Description: "The latest comparison of the book with the contract's state on chain",
		Fields: gql.Fields{
			"blockNumber": {Type: gql.NewNonNull(graphql.BigIntType), Description: "The block compared"},
			"timestamp":   {Type: gql.NewNonNull(graphql.BigIntType), Description: "When it was compared, Unix seconds"},
			"inSync":      {Type: gql.NewNonNull(gql.Boolean), Description: "Compared and equal"},
			"mismatches":  {Type: gql.NewNonNull(gql.NewList(gql.NewNonNull(gql.String)))},
			"error":       {Type: gql.String, Description: "Why the comparison could not be made"},
		},
	})
	bookType = gql.NewObject(gql.ObjectConfig{
		Name: "DexOrderBook",
		Description: "A market's price levels: resting orders of a perpetual market by price; for pools and pairs, the liquidity " +
			"between prices stepBps apart (amounts the pool gives or takes, before fees)",
		Fields: gql.Fields{
			"market":         {Type: gql.NewNonNull(graphql.AddressType)},
			"marketId":       {Type: gql.NewNonNull(graphql.BigIntType)},
			"venue":          {Type: gql.NewNonNull(gql.String)},
			"blockNumber":    {Type: graphql.BigIntType, Description: "The indexed block the book reflects; null while it is being read again"},
			"midPrice":       {Type: graphql.BigIntType, Description: "The pool's or pair's price, or the middle of the best bid and ask; times 1e18"},
			"stepBps":        {Type: gql.Int, Description: "Price step of pool and pair levels; null for order books"},
			"bids":           {Type: gql.NewNonNull(gql.NewList(gql.NewNonNull(levelType))), Description: "Highest price first"},
			"asks":           {Type: gql.NewNonNull(gql.NewList(gql.NewNonNull(levelType))), Description: "Lowest price first"},
			"reconciliation": {Type: reconciliationType, Description: "Null before the first comparison"},
		},
	})
)

var errNoBooks = errors.New("order books are not enabled (features.dex.orderbook)")

func registerOrderBook(e *graphql.Extension) {
	store := e.Storage()
	e.AddQuery("dexOrderBook", &gql.Field{
		Type:        bookType,
		Description: "A market's order book, null when no market is registered under the address and id",
		Args: gql.FieldConfigArgument{
			"market":   {Type: gql.NewNonNull(gql.String), Description: "Pool, pair or order manager address"},
			"marketId": {Type: gql.String, Description: "Perpetual market id (default 0)"},
			"levels":   {Type: gql.Int, Description: "Levels per side (default features.dex.orderbook.levels, at most 200)"},
			"stepBps":  {Type: gql.Int, Description: "Price step of pool and pair levels in basis points (default features.dex.orderbook.step_bps)"},
		},
		Resolve: func(p gql.ResolveParams) (interface{}, error) {
			svc := orderbook.Lookup(store)
			if svc == nil {
				return nil, errNoBooks
			}
			key, err := marketKey(p.Args["market"], p.Args["marketId"])
			if err != nil {
				return nil, err
			}
			book, recon := svc.Book(key)
			if book == nil {
				return nil, nil
			}
			levels, _ := p.Args["levels"].(int)
			step, _ := p.Args["stepBps"].(int)
			return bookMap(book, recon, levels, step), nil
		},
	})
}

func levelsOf(levels []orderbook.Level) []map[string]interface{} {
	out := make([]map[string]interface{}, len(levels))
	for i, l := range levels {
		out[i] = map[string]interface{}{"price": l.Price.String(), "baseAmount": l.Base.String(), "quoteAmount": l.Quote.String()}
	}
	return out
}

func bookMap(b *orderbook.Book, r *orderbook.Reconciliation, levels, step int) map[string]interface{} {
	d := b.Depth(step, levels)
	m := b.Market
	out := map[string]interface{}{
		"market": m.Key.Address.Hex(), "marketId": u64(m.Key.ID), "venue": string(m.Venue),
		"midPrice": optional(d.Mid, d.Mid == nil), "bids": levelsOf(d.Bids), "asks": levelsOf(d.Asks),
	}
	if b.Height != 0 {
		out["blockNumber"] = u64(b.Height)
	}
	if m.Venue != port.DexPerpOrderBook {
		out["stepBps"] = b.StepBps(step)
	}
	if r != nil {
		mismatches := r.Mismatches
		if mismatches == nil {
			mismatches = []string{}
		}
		var errText interface{}
		if r.Err != "" {
			errText = r.Err
		}
		out["reconciliation"] = map[string]interface{}{
			"blockNumber": u64(r.Block), "timestamp": strconv.FormatInt(r.At.Unix(), 10), "inSync": r.InSync(),
			"mismatches": mismatches, "error": errText,
		}
	}
	return out
}
