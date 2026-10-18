package model

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
)

// SupplementStreamDescriptor declares a finite tracked selection and an
// explicit first height. Completion is independent of the base capture plan.
type SupplementStreamDescriptor struct {
	Version          uint32          `json:"version"`
	Name             string          `json:"name"`
	Codec            string          `json:"codec"`
	SelectionHash    common.Hash     `json:"selectionHash"`
	Selection        json.RawMessage `json:"selection"`
	FromHeight       uint64          `json:"fromHeight"`
	ParentCheckpoint bool            `json:"parentCheckpoint,omitempty"`
	Required         bool            `json:"required,omitempty"`
}

func (d SupplementStreamDescriptor) Validate() error {
	if d.Required && !d.ParentCheckpoint {
		return ErrCaptureInvalid
	}
	if d.Version != 1 || len(d.Name) == 0 || len(d.Name) > 64 || len(d.Codec) == 0 || len(d.Codec) > 128 || d.FromHeight == 0 || len(d.Selection) == 0 || len(d.Selection) > 16<<10 || !json.Valid(d.Selection) {
		return ErrCaptureInvalid
	}
	for _, c := range d.Name {
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-' {
			continue
		}
		return ErrCaptureInvalid
	}
	for _, c := range d.Codec {
		if c < 33 || c > 126 {
			return ErrCaptureInvalid
		}
	}
	// RawMessage is compacted/escaped by JSON persistence. Reject definitions
	// whose bytes would change, keeping their signed digest stable on restart.
	encoded, e := json.Marshal(d.Selection)
	if e != nil || !bytes.Equal(encoded, d.Selection) {
		return ErrCaptureInvalid
	}
	if d.SelectionHash != sha256.Sum256(append([]byte(d.Codec+"\x00"), d.Selection...)) {
		return fmt.Errorf("%w: selection digest", ErrCaptureInvalid)
	}
	return nil
}
