package api

import (
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/0xmhha/indexer-go/pkg/core/gethconv"
	"github.com/0xmhha/indexer-go/pkg/core/model"
)

// The test doubles here keep go-ethereum values; these helpers convert what
// they return to the model the storage ports use.

func modelLogsOf(ls []*types.Log, err error) ([]*model.Log, error) {
	if err != nil {
		return nil, err
	}
	return gethconv.LogsFromGeth(ls), nil
}

