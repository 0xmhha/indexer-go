package ingress

import (
	"context"
	"time"

	"github.com/0xmhha/indexer-go/execwire/retention"
)

// MaintenanceOptions is explicit invocation data, never automatic activation.
type MaintenanceOptions struct {
	Executable               string
	KeepBlocks, ReserveBytes uint64
	Timeout                  time.Duration
}
type MaintenanceResult struct {
	Point   DrainPoint
	Phase   string
	Permit  retention.Permit
	Receipt retention.Receipt
	Release retention.Release
	Resumed bool
}
type MaintenanceProgram interface {
	DrainProgram
	Maintain(context.Context, MaintenanceOptions) (MaintenanceResult, error)
	RecoverMaintenance(context.Context, MaintenanceOptions) (MaintenanceResult, error)
}
