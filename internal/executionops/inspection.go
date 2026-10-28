package executionops

import (
	"github.com/0xmhha/indexer-go/execwire/retention"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/execution/checkpoint"
	"github.com/0xmhha/indexer-go/pkg/storage/postgres"
)

// CheckpointInspection is the existing execution-checkpoint inspect export.
// Supplied files are artifacts, not authenticated live database snapshots.
type CheckpointInspection struct {
	Control   port.ExecutionControl
	Bootstrap postgres.ExecutionBootstrapInfo
	Recovery  *checkpoint.RecoveryReport
	Scope     string
}

// RetentionInspection is the existing execution-retention inspect export.
type RetentionInspection struct {
	Control port.ExecutionControl
	Permit  retention.Permit
	Pending bool
	Receipt *retention.Receipt
	Scope   string
}
