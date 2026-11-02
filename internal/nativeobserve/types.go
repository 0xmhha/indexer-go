// Package nativeobserve qualifies borrowed observations without acquiring data
// or granting canonical, write, ACK, GC or notification authority.
package nativeobserve

import "time"

const (
	MaxResources = 64
	MaxKeys      = 1024
)

// Fixed-size opaque identifiers cannot accidentally carry a DSN, URL or path.
type ResourceID [32]byte
type Digest [32]byte
type SourceToken [16]byte
type RunID [16]byte
type Hash [32]byte

type ResourceKind uint8

const (
	Database ResourceKind = iota + 1
	Cluster
	PGData
	Schema
	RawWindow
	NativeWindow
	SourceSpool
	Filesystem
	NativePipeline
	LegacyCapture
)

type Scope struct {
	Kind ResourceKind
	ID   ResourceID
}

type Metric uint8

const (
	DatabaseBytes Metric = iota + 1
	WALBytes
	PGDataBytes
	SchemaBytes // Reserved: no qualified measurement provider exists yet.
	RawBytes
	NativeBytes
	SourceBytes
	FreeBytes
	RawDurableHeight
	NativeExecutedHeight
	SQLVisibleHeight
	SourceACKHeight
	CaptureReceivedHeight
)

type Unit uint8

const (
	Bytes Unit = iota + 1
	Blocks
)

type Definition uint8

const (
	DatabaseLogical Definition = iota + 1
	ClusterWALLogical
	PGDataAllocated
	SchemaLogical
	RawRetained
	NativeRetained
	SourceRetained
	FilesystemAvailable
	RawDurable
	NativeExecuted
	SQLVisible
	DurableSourceACK
	CaptureReceived
)

// Missing differs from a known zero. The adapter must never cast negative SQL
// counts to uint64; ParseNumber provides a strict decimal boundary.
type Number struct {
	Present bool
	Value   uint64
}

type Source struct {
	Token       SourceToken
	Incarnation uint64
	Run         RunID
}

type Chain struct {
	ID           uint64
	Genesis      Hash
	AnchorHeight uint64
	AnchorHash   Hash
}

type Budget struct {
	Scope      Scope
	Definition Definition
	ID         Digest
	Limit      Number
}

// Binding declares exactly one observation key. A database resource shared by
// several chains has one owner and no fabricated chain/schema allocation.
type Binding struct {
	Scope      Scope
	Metric     Metric
	Unit       Unit
	Definition Definition
	Source     Source
	Series     Digest
	Policy     Digest
	Budget     Budget
	Chain      Chain
	Epochs     Epochs
}

type ReceiverGeneration uint64
type CaptureGeneration uint64
type RawEpoch uint64
type NativeEpoch uint64
type ReadEpoch uint64

// Epochs are explicitly bound by an owner, never inferred by equal numbers.
type Epochs struct {
	Receiver ReceiverGeneration
	Capture  CaptureGeneration
	Raw      RawEpoch
	Native   NativeEpoch
	Read     ReadEpoch
}

type Position struct {
	Hash Hash
	Epochs
}

type Quality uint8

const (
	Unknown Quality = iota
	Known
	Failed
	Stale
	Unsupported
)

type Coverage uint8

const (
	Unverified Coverage = iota
	Exact
	LowerBound
	Truncated
)

type Cause uint8

const (
	NoCause Cause = iota
	NotObserved
	PermissionDenied
	TimedOut
	SourceUnavailable
)

// Each component retains its own acquisition interval and original event time.
// AcquiredAt is not an ACK progress time; a restart seed may have no EventAt.
// All fields are values: callers cannot mutate a qualified sample through aliases.
type Observation struct {
	Binding    Binding
	Number     Number
	Quality    Quality
	Cause      Cause
	Coverage   Coverage
	Sequence   uint64
	StartedAt  time.Time
	AcquiredAt time.Time
	EventAt    time.Time
	Position   Position
}

// Code is allowlisted; no raw driver error or identifier is echoed in findings.
type Code string

const (
	LimitExceeded     Code = "limit_exceeded"
	InvalidBinding    Code = "invalid_binding"
	ScopeMismatch     Code = "scope_mismatch"
	DuplicateKey      Code = "duplicate_key"
	ResourceConflict  Code = "resource_conflict"
	InvalidNumber     Code = "invalid_number"
	MissingValue      Code = "missing_value"
	InvalidQuality    Code = "invalid_quality"
	SampleFailed      Code = "sample_failed"
	SampleStale       Code = "sample_stale"
	SampleUnsupported Code = "sample_unsupported"
	UnboundSample     Code = "unbound_sample"
	InvalidTime       Code = "invalid_time"
	MissingTime       Code = "missing_time"
	InvalidSequence   Code = "invalid_sequence"
	SequenceGap       Code = "sequence_gap"
	InexactValue      Code = "inexact_value"
	InvalidPosition   Code = "invalid_position"
	BudgetMissing     Code = "budget_missing"
	BudgetExceeded    Code = "budget_exceeded"
	SeriesReset       Code = "series_reset"
	InvalidInterval   Code = "invalid_interval"
	NoGrowth          Code = "growth_not_observed"
)

type Assessment struct {
	Codes []Code
}

func (a Assessment) Usable() bool { return len(a.Codes) == 0 }

type Catalog struct{ bindings []Binding }
