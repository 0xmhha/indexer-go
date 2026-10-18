package model

import (
	"crypto/sha256"
	"fmt"
)

const SourceSearchWorker = "source.search.v1"

// SourceSearchDescriptor declares the separate, opt-in address/topic index.
type SourceSearchDescriptor struct {
	Version uint32 `json:"version"`
	Profile string `json:"profile"`
}

func (d SourceSearchDescriptor) Validate() error {
	return SourceLookupDescriptor(d).Validate()
}
func (d SourceSearchDescriptor) Definition() string {
	return fmt.Sprintf("source-search-v1-%x", sha256.Sum256([]byte(d.Profile)))
}
func (d ProjectionDescriptor) ValidateSourceSearch() error {
	selected := false
	for _, w := range d.Workers {
		if w == SourceSearchWorker {
			selected = true
		}
	}
	if d.SourceSearch == nil {
		if selected {
			return ErrCaptureInvalid
		}
		return nil
	}
	if !selected || d.SourceSearch.Validate() != nil {
		return ErrCaptureInvalid
	}
	if d.BlobCapability != nil && d.BlobCapability.Profile != d.SourceSearch.Profile {
		return ErrCaptureInvalid
	}
	if d.SourceLookup != nil && d.SourceLookup.Profile != d.SourceSearch.Profile {
		return ErrCaptureInvalid
	}
	if d.CallTraces != nil {
		v, e := CallTraceDefinitionFor(*d.CallTraces)
		if e != nil || v.Profile != d.SourceSearch.Profile {
			return ErrCaptureInvalid
		}
	}
	return nil
}
