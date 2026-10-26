package execution

import (
	"context"
	stateproof "github.com/0xmhha/indexer-go/pkg/chainfeed/evmstate/proof"
	"github.com/0xmhha/indexer-go/pkg/execution/proofdata"
)

// ReadStateProofSelection reads an existing private bounded selection without
// starting services or opening the source spool/native database.
func ReadStateProofSelection(ctx context.Context, path string) (stateproof.Selection, error) {
	raw, _, e := readPrivateBlobFile(ctx, path, 16<<10)
	if e != nil {
		return stateproof.Selection{}, e
	}
	return proofdata.ParseSelection(raw)
}
