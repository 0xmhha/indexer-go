package opsguard

import (
	"errors"
	"fmt"

	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/window"
)

// Reason is why the indexer was halted.
type Reason string

const (
	// ReasonCapacity: a monitored budget reached its stop threshold.
	ReasonCapacity Reason = "capacity"
	// ReasonStorage: raw or object storage failed, for example a removed or
	// replaced volume, an identity mismatch or an I/O error.
	ReasonStorage Reason = "storage"
	// ReasonIntegrity: immutable inputs conflict, are corrupt, or need an
	// explicit rebuild. A restart would only repeat the failure.
	ReasonIntegrity Reason = "integrity"
)

// Source names what a capacity halt observed.
type Source string

const (
	SourceFilesystem Source = "filesystem"
	SourcePostgres   Source = "postgres"
	SourceRaw        Source = "raw"
)

// ProcessWide reports whether the reason stops every chain of the process.
// Capacity and storage are shared by all chains; integrity is per chain.
func (r Reason) ProcessWide() bool { return r == ReasonCapacity || r == ReasonStorage }

// HaltError is a halt decided by the Monitor.
type HaltError struct {
	Reason Reason
	Source Source
	Detail string
}

func (e *HaltError) Error() string {
	if e.Source != "" {
		return fmt.Sprintf("operations halt (%s, %s): %s", e.Reason, e.Source, e.Detail)
	}
	return fmt.Sprintf("operations halt (%s): %s", e.Reason, e.Detail)
}

// ErrHalted is returned at startup while a halt latch exists.
var ErrHalted = errors.New("opsguard: halt latch present")

// Classify reports the halt reason of err, if it has one. Errors without a
// reason, such as RPC failures or cancellation, may be retried.
func Classify(err error) (Reason, bool) {
	if err == nil {
		return "", false
	}
	var h *HaltError
	if errors.As(err, &h) {
		return h.Reason, true
	}
	switch {
	case errors.Is(err, window.ErrCapacity):
		return ReasonCapacity, true
	case errors.Is(err, window.ErrUnavailable), errors.Is(err, window.ErrCorrupt), errors.Is(err, capture.ErrObjectsUnavailable):
		return ReasonStorage, true
	case errors.Is(err, window.ErrConflict), errors.Is(err, capture.ErrConflict), errors.Is(err, capture.ErrFinalityConflict), errors.Is(err, capture.ErrRebuildRequired):
		return ReasonIntegrity, true
	}
	return "", false
}
