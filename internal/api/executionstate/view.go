// Package executionstate formats public execution state without losing integer
// precision. Query adapters retain the store's snapshot and coverage errors.
package executionstate

import (
	"context"
	"fmt"
	"math/big"
	"strconv"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

func View(ctx context.Context, store any, kind, address, slot string) (map[string]interface{}, error) {
	if len(address) != 42 || address[:2] != "0x" || !common.IsHexAddress(address) {
		return nil, port.ErrInvalidData
	}
	addr := execwire.Address(common.HexToAddress(address))
	var key execwire.Word
	if kind == "storage" {
		raw, err := hexutil.Decode(slot)
		if err != nil || len(raw) != 32 {
			return nil, port.ErrInvalidData
		}
		copy(key[:], raw)
	}
	reader, ok := store.(port.ExecutionStateReader)
	if !ok {
		return nil, fmt.Errorf("execution state unavailable for this store")
	}
	var point port.ExecutionStatePoint
	var err error
	out := map[string]interface{}{"address": common.Address(addr).Hex()}
	switch kind {
	case "account":
		var v port.ExecutionAccountView
		v, err = reader.VisibleExecutionAccount(ctx, addr)
		point = v.ExecutionStatePoint
		out["exists"], out["nonce"], out["balance"], out["extra"] = v.Account.Exists, strconv.FormatUint(v.Account.Nonce, 10), new(big.Int).SetBytes(v.Account.Balance[:]).String(), strconv.FormatUint(v.Account.Extra, 10)
		out["codeHash"], out["storageRoot"] = common.Hash(v.Account.CodeHash).Hex(), common.Hash(v.Account.StorageRoot).Hex()
	case "storage":
		var v port.ExecutionStorageView
		v, err = reader.VisibleExecutionStorage(ctx, addr, key)
		point = v.ExecutionStatePoint
		out["slot"], out["value"] = common.Hash(key).Hex(), common.Hash(v.Value).Hex()
	case "code":
		var v port.ExecutionCodeView
		v, err = reader.VisibleExecutionCode(ctx, addr)
		point = v.ExecutionStatePoint
		out["code"] = hexutil.Encode(v.Code)
	default:
		return nil, port.ErrInvalidData
	}
	if err != nil {
		return nil, err
	}
	out["height"], out["generation"] = strconv.FormatUint(point.Ref.Height, 10), strconv.FormatUint(point.Ref.Generation, 10)
	out["blockHash"], out["stateRoot"] = common.Hash(point.Ref.Hash).Hex(), common.Hash(point.Root).Hex()
	return out, nil
}
