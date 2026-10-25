package port

import (
	"context"
	"errors"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/model"
)

// ExecutionWriter names a queue, not a permanently owned DB connection.
type ExecutionWriter string

const (
	ExecutionBlocks       ExecutionWriter = "blocks"
	ExecutionTransactions ExecutionWriter = "transactions"
	ExecutionReceipts     ExecutionWriter = "receipts"
	ExecutionLogs         ExecutionWriter = "logs"
	ExecutionAccounts     ExecutionWriter = "accounts"
	ExecutionStorage      ExecutionWriter = "storage"
	ExecutionContracts    ExecutionWriter = "contracts"
)

func ExecutionWriters() []ExecutionWriter {
	return []ExecutionWriter{ExecutionBlocks, ExecutionTransactions, ExecutionReceipts, ExecutionLogs, ExecutionAccounts, ExecutionStorage, ExecutionContracts}
}

var (
	ErrExecutionConflict      = errors.New("execution: conflicting identity or content")
	ErrExecutionSequence      = errors.New("execution: noncontiguous part")
	ErrExecutionStale         = errors.New("execution: stale generation")
	ErrExecutionMaintenance   = errors.New("execution: offline retention in progress; explicit completion required")
	ErrExecutionRewinding     = errors.New("execution: rewind in progress; retry")
	ErrExecutionCoverage      = errors.New("execution: required initial state is not covered")
	ErrExecutionUninitialized = errors.New("execution: control is not initialized")
	ErrExecutionCanonicalLag  = errors.New("execution: RPC canonical view has not converged with peer")
)

type ExecutionConfig struct {
	Chain           execwire.Chain
	Anchor          execwire.Ref
	AnchorRoot      execwire.Hash
	PartitionBlocks uint64
}

type ExecutionControl struct {
	ExecutionConfig
	Generation    uint64
	Visible       execwire.Ref
	Root          execwire.Hash
	Maintenance   bool
	Rewinding     bool
	BaselineReady bool
}

// ExecutionBaseline is an explicit, upstream-verified snapshot of covered keys
// at the declared anchor. It does not claim full-state coverage or bootstrap.
type ExecutionBaseline struct {
	Ref      execwire.Ref
	Root     execwire.Hash
	Accounts []execwire.AccountChange
	Storage  []execwire.StorageChange
	Code     []execwire.Code
}

type ExecutionStateStore interface {
	SeedExecution(context.Context, ExecutionBaseline) error
	ApplyExecutionState(context.Context, ExecutionWriter, execwire.Result, ...ExecutionCreation) error
	ExecutionAccountAt(context.Context, execwire.Address, uint64) (execwire.Account, error)
	ExecutionStorageAt(context.Context, execwire.Address, execwire.Word, uint64) (execwire.Word, error)
	ExecutionCodeAt(context.Context, execwire.Address, uint64) ([]byte, error)
}

// ExecutionCreation comes from a verified decoded outer creation receipt and
// transaction. A code transition alone cannot prove its creator or CREATE.
type ExecutionCreation struct {
	Address execwire.Address
	Creator execwire.Address
	TxIndex uint32
	TxHash  execwire.Hash
}

type ExecutionCoreWriter interface {
	ApplyExecutionCore(context.Context, ExecutionWriter, *model.Block, []*model.Receipt) error
}

type ExecutionPart struct {
	Writer ExecutionWriter
	Ref    execwire.Ref
	Parent execwire.Hash
	Digest execwire.Hash
}

// ExecutionStore uses the existing four-connection store. A part transaction
// is durable unpublished storage; it never advances public completion or ACKs.
// The callback uses its bound context synchronously and must not issue RPCs.
type ExecutionStore interface {
	InitExecution(context.Context, ExecutionConfig) error
	ExecutionStatus(context.Context) (ExecutionControl, error)
	WriteExecutionPart(context.Context, ExecutionPart, func(context.Context) error) error
	ExecutionCursor(context.Context, ExecutionWriter) (ExecutionPart, error)
	ExecutionParts(context.Context, uint64) ([]ExecutionPart, error)
}

// ExecutionPublication identifies one whole-block result. CommitExecution runs
// apply once after all seven durable parts, flushes undo/outbox before fresh,
// then checks the generation again and commits. Only success permits source ACK.
type ExecutionPublication struct {
	Ref                  execwire.Ref
	Parent, Digest, Root execwire.Hash
	CallTraces           bool
}
type ExecutionPublisher interface {
	CommitExecution(context.Context, ExecutionPublication, func(context.Context) error, func(context.Context) error) (bool, error)
}

// ExecutionConfirmer verifies retained published payloads after a lost ACK,
// including a canonical prefix published by an earlier generation. It never
// replays features or promotes an unpublished height.
type ExecutionConfirmer interface {
	ConfirmExecution(context.Context, []byte, func(context.Context, execwire.Ref) error) (bool, error)
}

// ExecutionStatePoint is the public block against which a state value is read.
type ExecutionStatePoint struct {
	Ref  execwire.Ref
	Root execwire.Hash
}
type ExecutionAccountView struct {
	ExecutionStatePoint
	Account execwire.Account
}
type ExecutionStorageView struct {
	ExecutionStatePoint
	Value execwire.Word
}
type ExecutionCodeView struct {
	ExecutionStatePoint
	Code []byte
}
type ExecutionStateReader interface {
	VisibleExecutionAccount(context.Context, execwire.Address) (ExecutionAccountView, error)
	VisibleExecutionStorage(context.Context, execwire.Address, execwire.Word) (ExecutionStorageView, error)
	VisibleExecutionCode(context.Context, execwire.Address) (ExecutionCodeView, error)
}

// ExecutionRewind preserves its original public tip while a newer generation
// blocks every read/publication. Resume changes data atomically, then reopens.
type ExecutionRewind struct {
	RequestGeneration uint64
	From, Target      execwire.Ref
	FromRoot, Root    execwire.Hash
}
type ExecutionRewindStore interface {
	BeginExecutionRewind(context.Context, execwire.Ref, execwire.Hash) (ExecutionRewind, error)
	ExecutionRewindStatus(context.Context) (ExecutionRewind, error)
	ResumeExecutionRewind(context.Context, UndoHook, func(context.Context, execwire.Ref) error) (*Reorg, error)
}
