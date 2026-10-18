package model

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"unicode/utf8"
)

const SourceLookupWorker = "source.lookup.v1"

// SourceLookupDescriptor binds the opt-in derived index to its decoding profile.
// Its completion is independent of source capture and grants no GC permission.
type SourceLookupDescriptor struct {
	Version uint32 `json:"version"`
	Profile string `json:"profile"`
}

func (d SourceLookupDescriptor) Validate() error {
	if d.Version != 1 || len(d.Profile) == 0 || len(d.Profile) > 256 || !utf8.ValidString(d.Profile) || strings.ContainsRune(d.Profile, 0) {
		return ErrCaptureInvalid
	}
	return nil
}
func (d SourceLookupDescriptor) Definition() string {
	return fmt.Sprintf("source-lookup-v1-%x", sha256.Sum256([]byte(d.Profile)))
}
func (d ProjectionDescriptor) ValidateSourceLookup() error {
	selected := false
	for _, w := range d.Workers {
		if w == SourceLookupWorker {
			selected = true
		}
	}
	if d.SourceLookup == nil {
		if selected {
			return ErrCaptureInvalid
		}
		return nil
	}
	if !selected || d.SourceLookup.Validate() != nil {
		return ErrCaptureInvalid
	}
	if d.BlobCapability != nil && d.BlobCapability.Profile != d.SourceLookup.Profile {
		return ErrCaptureInvalid
	}
	if d.CallTraces != nil {
		t, e := CallTraceDefinitionFor(*d.CallTraces)
		if e != nil || t.Profile != d.SourceLookup.Profile {
			return ErrCaptureInvalid
		}
	}
	return nil
}
