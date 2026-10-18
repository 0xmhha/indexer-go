package rpc

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"

	"github.com/0xmhha/indexer-go/pkg/source"
)

// FinalizedCheckpoint uses this Source's existing RPC client/pool. It never
// substitutes latest, safe or a confirmation depth for the finalized tag.
func (s *Source) FinalizedCheckpoint(ctx context.Context) (uint64, common.Hash, error) {
	var raw json.RawMessage
	if err := s.rpc.CallContext(ctx, &raw, "eth_getBlockByNumber", "finalized", false); err != nil {
		return 0, common.Hash{}, fmt.Errorf("source: finalized: %w", err)
	}
	if isNull(raw) {
		return 0, common.Hash{}, source.ErrFinalityUnavailable
	}
	var head struct {
		Number *hexutil.Uint64 `json:"number"`
		Hash   common.Hash     `json:"hash"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return 0, common.Hash{}, err
	}
	if head.Number == nil || head.Hash == (common.Hash{}) {
		return 0, common.Hash{}, fmt.Errorf("source: malformed finalized checkpoint")
	}
	return uint64(*head.Number), head.Hash, nil
}
