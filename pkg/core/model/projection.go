package model

import "github.com/ethereum/go-ethereum/common"

// BlockPoint is a committed position; a nil point means no completion claim.
type BlockPoint struct {
	Height uint64      `json:"height"`
	Hash   common.Hash `json:"hash"`
}

// ProjectionDescriptor declares the exact source scope; it is not a cursor.
type ProjectionDescriptor struct {
	SourceSearch   *SourceSearchDescriptor     `json:"sourceSearch,omitempty"`
	SourceLookup   *SourceLookupDescriptor     `json:"sourceLookup,omitempty"`
	BlobCapability *BlobCapability             `json:"blobCapability,omitempty"`
	CallTraces     *SupplementStreamDescriptor `json:"callTraces,omitempty"`
	Telemetry      *FeedTelemetryDescriptor    `json:"telemetry,omitempty"`
	Version        uint64                      `json:"version"`
	Anchor         FeedBlock                   `json:"anchor"`
	Plan           CapturePlan                 `json:"plan"`
	Workers        []string                    `json:"workers"`
	StateProofs    *SupplementStreamDescriptor `json:"stateProofs,omitempty"`
	StateChanges   *StateProjectionDescriptor  `json:"stateChanges,omitempty"`
}
type CompletedRange struct {
	From    uint64      `json:"from"`
	To      uint64      `json:"to"`
	EndHash common.Hash `json:"endHash"`
}

// UnitCheckpoint belongs to the projection's block transaction and undo.
// Ranges preserve holes: the highest processed height alone is not complete.
type UnitCheckpoint struct {
	Version    uint64           `json:"version"`
	Definition string           `json:"definition"`
	Epoch      uint64           `json:"epoch"`
	Chain      FeedChain        `json:"chain"`
	PlanDigest common.Hash      `json:"planDigest"`
	Start      uint64           `json:"start"`
	Ranges     []CompletedRange `json:"ranges"`
}
type ProjectionInput struct {
	Epoch    uint64
	Identity CaptureIdentity
	Anchor   FeedBlock
}
