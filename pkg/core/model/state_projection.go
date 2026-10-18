package model

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"

	"github.com/ethereum/go-ethereum/common"
)

// StateProjectionDescriptor pins one tracked-state reducer and its exact proof
// scope. This is independent of the base projection and its global undo.
type StateProjectionDescriptor struct {
	Version uint32                     `json:"version"`
	Name    string                     `json:"name"`
	Stream  SupplementStreamDescriptor `json:"stream"`
}

func (d ProjectionDescriptor) ValidateStateChanges() error {
	if d.StateChanges == nil {
		return nil
	}
	if d.StateProofs == nil || d.StateProofs.Validate() != nil || d.StateChanges.Validate() != nil {
		return ErrCaptureInvalid
	}
	a, _ := json.Marshal(d.StateProofs)
	b, _ := json.Marshal(d.StateChanges.Stream)
	if !bytes.Equal(a, b) {
		return ErrCaptureInvalid
	}
	return nil
}
func (d StateProjectionDescriptor) Validate() error {
	if d.Version != 1 || d.Name != "tracked."+d.Stream.Name || d.Stream.Validate() != nil || !d.Stream.ParentCheckpoint {
		return ErrCaptureInvalid
	}
	return nil
}
func (d StateProjectionDescriptor) Digest() common.Hash {
	raw, _ := json.Marshal(d)
	return sha256.Sum256(raw)
}
