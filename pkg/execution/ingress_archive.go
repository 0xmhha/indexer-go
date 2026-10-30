package execution

import (
	"bytes"

	"github.com/0xmhha/indexer-go/pkg/chainfeed/evmdata"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/rlp"
)

// This checks lossless layout and byte equality, not EVM/consensus validity.
// Prepare and the chain's origin resolver own those independent checks.
func matchIngressArchive(a port.ExecutionIngressArchive, in port.ExecutionIngressInput) error {
	if a.Validate() != nil || a.Ref != in.Ref || a.Parent != in.Parent || a.Digest != in.Digest {
		return port.ErrExecutionConflict
	}
	if _, rest, err := rlp.SplitList(a.Datasets[0].Payload); err != nil || len(rest) != 0 {
		return port.ErrExecutionConflict
	}
	parts, err := evmdata.Items(a.Datasets[1].Payload, 3)
	if err != nil || len(parts) < 2 {
		return port.ErrExecutionConflict
	}
	encoded, err := rlp.EncodeToBytes(append([]rlp.RawValue{a.Datasets[0].Payload}, parts...))
	if err != nil || !bytes.Equal(encoded, in.BlockRLP) || !bytes.Equal(a.Datasets[2].Payload, in.ReceiptsRLP) {
		return port.ErrExecutionConflict
	}
	return nil
}
