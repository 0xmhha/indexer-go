package stable

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/0xmhha/indexer-go/pkg/chains/evm"
	"github.com/0xmhha/indexer-go/pkg/core/gethconv"
	"github.com/0xmhha/indexer-go/pkg/core/model"
)

var (
	systemContract = common.HexToAddress("0x9999")
	// The live testnet fixture uses the legacy reserved sender. Stable's
	// v1.8.0 documentation names address(1) for the same system operation.
	legacySystemSender = common.HexToAddress("0x8888888888888888888888888888888888888888")
	systemSender       = common.HexToAddress("0x01")
	notifySystemLogs   = []byte{0xe8, 0xe9, 0x83, 0xb7}
)

func decodeDynamicFeeTx(raw json.RawMessage) (*model.Transaction, error) {
	var envelope struct {
		Hash        common.Hash     `json:"hash"`
		From        *common.Address `json:"from"`
		BlockNumber *hexutil.Uint64 `json:"blockNumber"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, err
	}
	var tx types.Transaction
	if err := tx.UnmarshalJSON(raw); err != nil {
		return nil, err
	}
	if tx.Hash() != envelope.Hash {
		return nil, fmt.Errorf("%w: stable transaction %s reported %s", evm.ErrHashMismatch, tx.Hash(), envelope.Hash)
	}
	v, r, s := tx.RawSignatureValues()
	if v.Sign() == 0 && r.Sign() == 0 && s.Sign() == 0 {
		id := tx.ChainId()
		if envelope.From == nil || (*envelope.From != legacySystemSender && *envelope.From != systemSender) ||
			tx.To() == nil || *tx.To() != systemContract || !bytes.Equal(tx.Data(), notifySystemLogs) ||
			!id.IsUint64() || (id.Uint64() != 988 && id.Uint64() != 2201) ||
			tx.Value().Sign() != 0 || tx.GasFeeCap().Sign() != 0 || tx.GasTipCap().Sign() != 0 ||
			envelope.BlockNumber == nil || tx.Nonce() != uint64(*envelope.BlockNumber) {
			return nil, fmt.Errorf("stable: unsigned transaction is not a recognized system call")
		}
		m, err := gethconv.ConvertTx(&tx)
		if err != nil {
			return nil, err
		}
		m.From, m.Opaque = *envelope.From, true // no Ethereum sender signature; trust the RPC's system identity
		return m, nil
	}
	m, err := gethconv.RecoverTx(&tx)
	if err != nil {
		return nil, err
	}
	if envelope.From != nil && m.From != *envelope.From {
		return nil, fmt.Errorf("%w: stable transaction %s", evm.ErrSenderMismatch, m.Hash)
	}
	return m, nil
}
