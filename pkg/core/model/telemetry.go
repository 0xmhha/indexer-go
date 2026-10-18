package model

import (
	"encoding/hex"
	"math"
)

// FeedTelemetryDescriptor identifies one live process, never durable coverage.
// Only the identifier is stored with the existing projection descriptor.
type FeedTelemetryDescriptor struct {
	RunID                   string  `json:"runId"`
	ReceiveIntervalTargetMS float64 `json:"receiveIntervalTargetMs"`
}

func (d ProjectionDescriptor) ValidateTelemetry() error {
	if d.Telemetry == nil {
		return nil
	}
	v := d.Telemetry
	b, e := hex.DecodeString(v.RunID)
	if e != nil || len(b) != 16 || len(v.RunID) != 32 || math.IsNaN(v.ReceiveIntervalTargetMS) || math.IsInf(v.ReceiveIntervalTargetMS, 0) || v.ReceiveIntervalTargetMS <= 0 || v.ReceiveIntervalTargetMS > 60000 {
		return ErrCaptureInvalid
	}
	return nil
}
