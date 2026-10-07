package graphql

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/ethereum/go-ethereum/common"
	"github.com/graphql-go/graphql"

	"github.com/0xmhha/indexer-go/pkg/core/gethconv"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/events"
)

// WithReorgQueries adds queries for chain reorganizations and the blocks
// they removed. After a reorganization the node forgets the old branch;
// these queries keep it available so applications can correct what they
// showed.
func (b *SchemaBuilder) WithReorgQueries() *SchemaBuilder {
	s := b.schema
	b.queries["reorgs"] = &graphql.Field{
		Type:        graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(reorgType))),
		Description: "Chain reorganizations, newest first",
		Args: graphql.FieldConfigArgument{
			"limit":  &graphql.ArgumentConfig{Type: graphql.Int, DefaultValue: 20},
			"offset": &graphql.ArgumentConfig{Type: graphql.Int, DefaultValue: 0},
		},
		Resolve: s.resolveReorgs,
	}
	b.queries["reorg"] = &graphql.Field{
		Type:        reorgType,
		Description: "A chain reorganization by id",
		Args: graphql.FieldConfigArgument{
			"id": &graphql.ArgumentConfig{Type: graphql.NewNonNull(bigIntType)},
		},
		Resolve: s.resolveReorg,
	}
	b.queries["orphanedBlocks"] = &graphql.Field{
		Type:        graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(orphanedBlockType))),
		Description: "Blocks removed by reorganizations at a height",
		Args: graphql.FieldConfigArgument{
			"number": &graphql.ArgumentConfig{Type: graphql.NewNonNull(bigIntType)},
		},
		Resolve: s.resolveOrphanedBlocks,
	}
	b.queries["orphanedBlock"] = &graphql.Field{
		Type:        orphanedBlockType,
		Description: "A block removed by a reorganization, by hash",
		Args: graphql.FieldConfigArgument{
			"hash": &graphql.ArgumentConfig{Type: graphql.NewNonNull(hashType)},
		},
		Resolve: s.resolveOrphanedBlock,
	}
	b.queries["orphanedTransaction"] = &graphql.Field{
		Type:        graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(orphanedTransactionType))),
		Description: "A transaction as it was in removed blocks (more than one if it was orphaned again)",
		Args: graphql.FieldConfigArgument{
			"hash": &graphql.ArgumentConfig{Type: graphql.NewNonNull(hashType)},
		},
		Resolve: s.resolveOrphanedTransaction,
	}
	return b
}

func (s *Schema) orphans() (port.OrphanReader, error) {
	r, ok := s.storage.(port.OrphanReader)
	if !ok {
		return nil, fmt.Errorf("storage does not keep orphaned blocks")
	}
	return r, nil
}

func (s *Schema) resolveReorgs(p graphql.ResolveParams) (interface{}, error) {
	r, err := s.orphans()
	if err != nil {
		return nil, err
	}
	limit, _ := p.Args["limit"].(int)
	offset, _ := p.Args["offset"].(int)
	if limit <= 0 || limit > 1000 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	reorgs, _, err := r.GetReorgs(p.Context, port.Page{Limit: limit, Offset: offset})
	if err != nil {
		return nil, err
	}
	out := make([]interface{}, len(reorgs))
	for i, rg := range reorgs {
		out[i] = reorgToMap(rg)
	}
	return out, nil
}

func (s *Schema) resolveReorg(p graphql.ResolveParams) (interface{}, error) {
	r, err := s.orphans()
	if err != nil {
		return nil, err
	}
	idStr, _ := p.Args["id"].(string)
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid reorg id: %w", err)
	}
	rg, err := r.GetReorg(p.Context, id)
	if errors.Is(err, port.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return reorgToMap(rg), nil
}

func (s *Schema) resolveOrphanedBlocks(p graphql.ResolveParams) (interface{}, error) {
	r, err := s.orphans()
	if err != nil {
		return nil, err
	}
	numStr, _ := p.Args["number"].(string)
	n, err := strconv.ParseUint(numStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid block number: %w", err)
	}
	obs, err := r.GetOrphanedBlocksAt(p.Context, n)
	if err != nil {
		return nil, err
	}
	out := make([]interface{}, len(obs))
	for i, ob := range obs {
		out[i] = s.orphanedBlockToMap(p, r, ob)
	}
	return out, nil
}

func (s *Schema) resolveOrphanedBlock(p graphql.ResolveParams) (interface{}, error) {
	r, err := s.orphans()
	if err != nil {
		return nil, err
	}
	hashStr, _ := p.Args["hash"].(string)
	ob, err := r.GetOrphanedBlock(p.Context, common.HexToHash(hashStr))
	if errors.Is(err, port.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s.orphanedBlockToMap(p, r, ob), nil
}

func (s *Schema) resolveOrphanedTransaction(p graphql.ResolveParams) (interface{}, error) {
	r, err := s.orphans()
	if err != nil {
		return nil, err
	}
	hashStr, _ := p.Args["hash"].(string)
	txHash := common.HexToHash(hashStr)
	obs, err := r.GetOrphanedTransaction(p.Context, txHash)
	if err != nil {
		return nil, err
	}

	// Where the transaction is canonical now, if anywhere.
	var reincluded interface{}
	if _, loc, err := s.storage.GetTransaction(p.Context, txHash); err == nil {
		reincluded = blockRefToMap(loc.BlockHeight, loc.BlockHash)
	} else if !errors.Is(err, port.ErrNotFound) {
		return nil, err
	}

	out := make([]interface{}, 0, len(obs))
	for _, ob := range obs {
		for i, tx := range ob.Block.Transactions {
			if tx.Hash != txHash {
				continue
			}
			txMap := s.transactionToMap(tx, &port.TxLocation{BlockHeight: ob.Block.Number, BlockHash: ob.Block.Hash, TxIndex: uint64(i)})
			var receipt interface{}
			if i < len(ob.Receipts) {
				receipt = s.receiptToMap(gethconv.ReceiptToGeth(ob.Receipts[i]))
			}
			out = append(out, map[string]interface{}{
				"transaction":  txMap,
				"receipt":      receipt,
				"block":        blockRefToMap(ob.Block.Number, ob.Block.Hash),
				"reorgId":      fmt.Sprintf("%d", ob.ReorgSeq),
				"reincludedIn": reincluded,
			})
		}
	}
	return out, nil
}

func (s *Schema) orphanedBlockToMap(p graphql.ResolveParams, r port.OrphanReader, ob *port.OrphanedBlock) map[string]interface{} {
	receipts := make([]interface{}, len(ob.Receipts))
	for i, rc := range ob.Receipts {
		receipts[i] = s.receiptToMap(gethconv.ReceiptToGeth(rc))
	}
	var reorg interface{}
	if rg, err := r.GetReorg(p.Context, ob.ReorgSeq); err == nil {
		reorg = reorgToMap(rg)
	}
	return map[string]interface{}{
		"block":    s.blockToMap(ob.Block),
		"receipts": receipts,
		"reorg":    reorg,
	}
}

func blockRefToMap(number uint64, hash common.Hash) map[string]interface{} {
	return map[string]interface{}{"number": fmt.Sprintf("%d", number), "hash": hash.Hex()}
}

func reorgToMap(r *port.Reorg) map[string]interface{} {
	removed := make([]interface{}, len(r.Removed))
	for i, b := range r.Removed {
		removed[i] = blockRefToMap(b.Number, b.Hash)
	}
	return map[string]interface{}{
		"id":            fmt.Sprintf("%d", r.Seq),
		"forkNumber":    fmt.Sprintf("%d", r.ForkNumber),
		"forkHash":      r.ForkHash.Hex(),
		"oldHead":       fmt.Sprintf("%d", r.OldHead),
		"depth":         len(r.Removed),
		"removedBlocks": removed,
		"detectedAt":    fmt.Sprintf("%d", r.DetectedAt),
	}
}

// reorgEventToMap is the payload of the reorg subscription.
func reorgEventToMap(e *events.ReorgEvent) map[string]interface{} {
	removed := make([]interface{}, len(e.Removed))
	for i, b := range e.Removed {
		removed[i] = blockRefToMap(b.Number, b.Hash)
	}
	return map[string]interface{}{
		"id":            fmt.Sprintf("%d", e.Seq),
		"forkNumber":    fmt.Sprintf("%d", e.ForkNumber),
		"forkHash":      e.ForkHash.Hex(),
		"oldHead":       fmt.Sprintf("%d", e.OldHead),
		"depth":         len(e.Removed),
		"removedBlocks": removed,
		"detectedAt":    fmt.Sprintf("%d", e.CreatedAt.Unix()),
	}
}
