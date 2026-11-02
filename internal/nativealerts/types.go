// Package nativealerts owns bounded local notification state, never indexing,
// canonical reads, commits, source ACKs, GC or recovery authority. Engine and
// stores are single-owner; callers must serialize operations off producer paths.
package nativealerts

import (
	"errors"
	"time"

	"github.com/0xmhha/indexer-go/internal/nativeobserve"
)

const (
	MaxRules           = 128
	MaxQueued          = 128
	MaxDeliveries      = 256 // queued128 + at most one deferred critical per rule128
	MaxEventBytes      = 64 << 10
	MaxCheckpointBytes = 1 << 20
)

var (
	ErrRule         = errors.New("alert: invalid rule or state")
	ErrOverflow     = errors.New("alert: counter exhausted")
	ErrBackpressure = errors.New("alert: bounded queue full")
	ErrCheckpoint   = errors.New("alert: checkpoint unavailable or invalid")
	ErrDisabled     = errors.New("alert: sink disabled")
	ErrDelivery     = errors.New("alert: delivery unconfirmed")
)

type ID [32]byte
type Incarnation [16]byte
type Kind uint8

const (
	Capacity Kind = iota + 1
	Backlog
	Lag
	InputInvalid
	SampleMissing
	MaintenancePending
	DeliveryFailed
)

type Key struct {
	Resource nativeobserve.ResourceID
	Metric   nativeobserve.Metric
	Kind     Kind
}
type Level uint8

const (
	Normal Level = iota
	Warning
	Critical
)

type Quality uint8

const (
	Missing Quality = iota
	Good
	Invalid
	Stale
	Gap
	Reset
	StateLost
	Backpressured
)

type Mode uint8

const (
	High Mode = iota + 1
	Low
	BudgetBPS
)

type Rule struct {
	Key                                     Key
	Policy                                  nativeobserve.Digest
	Mode                                    Mode
	Warn, Stop, Clear                       uint64
	WarnCount, ClearCount                   int
	WarnDwell, ClearDwell, Cooldown, MaxAge time.Duration
}

// Signal is an already assessed component or derived fact. Binding must cover
// source/incarnation/run/series/definition/budget/epochs; never substitute a raw
// block height for a backlog/lag. CapacitySignal supplies the observation adapter.
type Signal struct {
	Key          Key
	Binding      ID
	Run          nativeobserve.RunID
	Sequence     uint64
	ObservedAt   time.Time
	Value, Limit nativeobserve.Number
	Quality      Quality
}
type State struct {
	Key                        Key
	Incarnation                Incarnation
	Active                     Level
	Quality                    Quality
	Revision, Reminder         uint64
	TransitionAt, LastNoticeAt time.Time
	// All following fields are transient and excluded from checkpoints.
	Binding, Policy ID
	Run             nativeobserve.RunID
	Sequence        uint64
	ObservedAt      time.Time
	Candidate       Level
	Count           int
	Since           time.Time
}
type Event struct {
	ID, Transition       ID
	Key                  Key
	Level                Level
	Revision, Reminder   uint64
	OccurredAt, NoticeAt time.Time
}
type Receipt struct{ ID ID }
