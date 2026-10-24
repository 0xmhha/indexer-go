// Package exporter preserves the StableNet import path for shared supervision.
package exporter

import shared "github.com/0xmhha/indexer-go/execwire/exporter"

type Client = shared.Client
type Collector = shared.Collector
type Backend = shared.Backend
type CanonicalChange = shared.CanonicalChange

var (
	ErrCanonicalChanged   = shared.ErrCanonicalChanged
	ErrCheckpointRecovery = shared.ErrCheckpointRecovery
)
