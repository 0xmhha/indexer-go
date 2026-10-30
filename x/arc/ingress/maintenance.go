package arcingress

import (
	"context"
	"github.com/0xmhha/indexer-go/execwire/retention"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/execution/ingress"
	"github.com/0xmhha/indexer-go/pkg/window"
)

type MaintenanceStore interface {
	RawRetentionStore
	ExecutionStatus(context.Context) (port.ExecutionControl, error)
	ExecutionRetentionStatus(context.Context) (retention.Permit, bool, *retention.Receipt, error)
	BeginExecutionRetention(context.Context, retention.Request, func(context.Context) error) (retention.Permit, error)
	WithExecutionRetention(context.Context, retention.Permit, func(context.Context) error) error
	CompleteExecutionRetention(context.Context, retention.Permit, retention.Receipt, func(context.Context) error) error
	WithExecutionRetentionRelease(context.Context, retention.Permit, retention.Receipt, func(context.Context) error) error
}
type maintenanceCoordinator struct {
	cfg     ingress.Config
	deps    ingress.Dependencies
	raw     window.Options
	options ingress.MaintenanceOptions
	store   MaintenanceStore
	after   func(string) error
}
