package jsonrpc

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/0xmhha/indexer-go/internal/api/executioncalls"
)

func (h *Handler) getExecutionCalls(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	p, err := executioncalls.DecodeParams(params)
	if err != nil {
		return nil, NewError(InvalidParams, "invalid_execution_call_query", nil)
	}
	result, err := executioncalls.View(ctx, h.storage, p)
	if err != nil {
		var e *executioncalls.ContractError
		if !errors.As(err, &e) {
			e = executioncalls.Error(err)
		}
		code := -32050
		switch e.Code {
		case "invalid_execution_call_query":
			code = InvalidParams
		case "execution_calls_not_collected":
			code = -32051
		case "execution_call_cursor_stale":
			code = -32052
		}
		return nil, NewError(code, e.Code, e.Extensions())
	}
	return result, nil
}
