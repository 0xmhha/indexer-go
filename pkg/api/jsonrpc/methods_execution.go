package jsonrpc

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/0xmhha/indexer-go/internal/api/executionstate"
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

func (h *Handler) getExecutionState(ctx context.Context, kind string, params json.RawMessage) (interface{}, *Error) {
	var p struct {
		Address string `json:"address"`
		Slot    string `json:"slot"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, NewError(InvalidParams, "invalid execution state params", nil)
	}
	value, err := executionstate.View(ctx, h.storage, kind, p.Address, p.Slot)
	if errors.Is(err, port.ErrInvalidData) {
		return nil, NewError(InvalidParams, "address must be 20-byte hex; storage slot must be 32-byte hex", nil)
	}
	if err != nil {
		return nil, NewError(InternalError, "execution state unavailable", err.Error())
	}
	return value, nil
}
