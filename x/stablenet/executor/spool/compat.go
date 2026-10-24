// Package spool preserves the StableNet import path for the shared durable spool.
package spool

import shared "github.com/0xmhha/indexer-go/execwire/spool"

type Store = shared.Store
type Options = shared.Options
type Entry = shared.Entry
type Status = shared.Status
type MetadataObservation = shared.MetadataObservation

var (
	Open             = shared.Open
	OpenMaintenance  = shared.OpenMaintenance
	InspectMetadata  = shared.InspectMetadata
	InspectRetention = shared.InspectRetention
	ResumeCapacity   = shared.ResumeCapacity
	ErrFull          = shared.ErrFull
	ErrConflict      = shared.ErrConflict
	ErrSequence      = shared.ErrSequence
	ErrCorrupt       = shared.ErrCorrupt
	ErrBusy          = shared.ErrBusy
	ErrClosed        = shared.ErrClosed
	ErrRecovery      = shared.ErrRecovery
)
