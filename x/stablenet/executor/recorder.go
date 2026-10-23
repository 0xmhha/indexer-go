// Package executor replays already imported StableNet blocks for indexing.
// It never publishes database state or decides the indexer's visible height.
package executor

import (
	"errors"

	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
)

var (
	ErrJournalMismatch    = errors.New("executor: journal does not reproduce state")
	ErrCommitForbidden    = errors.New("executor: second-pass commit forbidden")
	ErrRootMismatch       = errors.New("executor: original block root mismatch")
	ErrUnsupportedPrelude = errors.New("executor: unsupported pre-transaction protocol changes")
)

type Chain interface {
	consensus.ChainHeaderReader
	core.ChainContext
}
type ReplayInput struct {
	Chain      Chain
	Database   state.Database
	Parent     *types.Header
	Block      *types.Block
	Receipts   types.Receipts
	Generation uint64
}
