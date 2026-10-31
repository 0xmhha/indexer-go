// Package nativemonitor evaluates submitted observations without probing a host,
// database, peer or RPC endpoint. Its findings grant no operational authority.
package nativemonitor

import (
	"time"

	"github.com/0xmhha/indexer-go/execwire"
)

type Identity struct {
	Profile string         `json:"profile"`
	Chain   execwire.Chain `json:"chain"`
	Anchor  execwire.Ref   `json:"original_anchor"`
}

// Nil numeric fields mean unobserved, never a measured zero or a default limit.
type Policy struct {
	Version           int       `json:"version"`
	Expected          *Identity `json:"expected"`
	MaxAgeSeconds     *uint64   `json:"max_age_seconds"`
	CapacityWarnBPS   *uint64   `json:"capacity_warn_bps"`
	CapacityStopBPS   *uint64   `json:"capacity_stop_bps"`
	MinFreeBytes      *uint64   `json:"min_free_bytes"`
	BacklogWarnBlocks *uint64   `json:"backlog_warn_blocks"`
	BacklogStopBlocks *uint64   `json:"backlog_stop_blocks"`
	LagWarnSeconds    *uint64   `json:"lag_warn_seconds"`
	LagStopSeconds    *uint64   `json:"lag_stop_seconds"`
	RequiredStorage   []string  `json:"required_storage"`
}

type Pipeline struct {
	Generation           *uint64 `json:"generation"`
	Head                 *uint64 `json:"head_height"`
	Received             *uint64 `json:"received_height"`
	Processed            *uint64 `json:"processed_height"`
	ACK                  *uint64 `json:"ack_height"`
	OldestPendingSeconds *uint64 `json:"oldest_pending_seconds"`
	ACKLagSeconds        *uint64 `json:"ack_lag_seconds"`
	WorkerLagSeconds     *uint64 `json:"worker_lag_seconds"`
}

type Storage struct {
	Kind  string  `json:"kind"`
	Used  *uint64 `json:"used_bytes"`
	Limit *uint64 `json:"limit_bytes"`
	Free  *uint64 `json:"free_bytes"`
}

type Metrics struct {
	ObservedAt *time.Time `json:"observed_at"`
	Pipeline   *Pipeline  `json:"pipeline"`
	Storage    []Storage  `json:"storage"`
}

type DiagnosticSample struct {
	ObservedAt *time.Time        `json:"observed_at"`
	Report     *DiagnosticReport `json:"report"`
}

type Snapshot struct {
	Version     int               `json:"version"`
	Identity    *Identity         `json:"identity"`
	Diagnostics *DiagnosticSample `json:"diagnostics"`
	Metrics     *Metrics          `json:"metrics"`
}

// DiagnosticReport consumes the version1 JSON contract of execution-diagnose.
// Keeping the consumer DTO independent prevents linking runtime DB/chain code.
// Actual product-output compatibility is tested separately.
type DiagnosticReport struct {
	Version       int           `json:"version"`
	Profile       string        `json:"profile"`
	Status        string        `json:"status"`
	Scope         string        `json:"scope"`
	Authority     string        `json:"start_ack_deletion_gc_recovery_restore_activation"`
	LiveFreshness string        `json:"live_canonical_freshness"`
	FullState     string        `json:"full_state_reconstruction"`
	Consistency   string        `json:"cross_store_consistency"`
	Findings      []Finding     `json:"findings"`
	Observations  []Observation `json:"observations"`
}

type Observation struct {
	Kind               string         `json:"kind"`
	Basis              string         `json:"basis"`
	Chain              execwire.Chain `json:"chain"`
	Anchor             execwire.Ref   `json:"original_anchor"`
	ACK                *execwire.Ref  `json:"ack,omitempty"`
	Base               *execwire.Ref  `json:"retained_base,omitempty"`
	Visible            *execwire.Ref  `json:"public_ref,omitempty"`
	Parent             *execwire.Ref  `json:"parent_header,omitempty"`
	Root               *execwire.Hash `json:"root,omitempty"`
	Generation         *uint64        `json:"generation,omitempty"`
	Floor              *uint64        `json:"retained_through,omitempty"`
	Phase              string         `json:"phase,omitempty"`
	TransitionPending  *bool          `json:"transition_pending,omitempty"`
	MaintenancePending *bool          `json:"maintenance_pending,omitempty"`
	HeaderAvailable    *bool          `json:"header_available,omitempty"`
	StateAvailable     *bool          `json:"state_available,omitempty"`
	Received           *InputRef      `json:"received_ref,omitempty"`
	NativeTip          *InputRef      `json:"native_tip,omitempty"`
	RawEpoch           *uint64        `json:"raw_epoch,omitempty"`
	NativeEpoch        *uint64        `json:"native_epoch,omitempty"`
	NativeReadEpoch    *uint64        `json:"native_read_epoch,omitempty"`
}

type InputRef struct {
	Height uint64        `json:"height"`
	Hash   execwire.Hash `json:"hash"`
	Root   execwire.Hash `json:"root"`
}

type Finding struct {
	Field  string `json:"field"`
	Status string `json:"status"`
	Code   string `json:"code"`
}

type Report struct {
	Version              int       `json:"version"`
	Status               string    `json:"status"`
	AlertLevel           string    `json:"alert_level"`
	Scope                string    `json:"scope"`
	OperationalReadiness string    `json:"operational_readiness"`
	LiveFreshness        string    `json:"live_canonical_freshness"`
	Authenticity         string    `json:"snapshot_authenticity"`
	Actuation            string    `json:"actuation"`
	Findings             []Finding `json:"findings"`
}
