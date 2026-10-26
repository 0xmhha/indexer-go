// Package evmstate supplies tracked state proofs through the capture contract.
package evmstate

import (
	"context"
	core "github.com/0xmhha/indexer-go/pkg/chainfeed/evmstate/proof"
	"github.com/ethereum/go-ethereum/common"
)

const (
	Codec          = core.Codec
	MaxAccounts    = core.MaxAccounts
	MaxSlots       = core.MaxSlots
	MaxNodes       = core.MaxNodes
	MaxNodeBytes   = core.MaxNodeBytes
	MaxProofBytes  = core.MaxProofBytes
	MaxCodeBytes   = core.MaxCodeBytes
	MaxBundleBytes = core.MaxBundleBytes
)

var ErrProof = core.ErrProof

type Target = core.Target
type Selection = core.Selection
type Nodes = core.Nodes
type StorageProof = core.StorageProof
type Proof = core.Proof
type Evidence = core.Evidence
type Bundle = core.Bundle
type Slot = core.Slot
type Account = core.Account

func Verify(root common.Hash, selection Selection, bundle Bundle) ([]Account, error) {
	return core.Verify(root, selection, bundle)
}
func VerifyContext(ctx context.Context, root common.Hash, selection Selection, bundle Bundle) ([]Account, error) {
	return core.VerifyContext(ctx, root, selection, bundle)
}
