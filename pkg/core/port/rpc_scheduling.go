package port

import "context"

type rpcMaintenanceKey struct{}

// WithRPCMaintenance marks a periodic control read as eligible for a bounded
// batching grace period. It is only a scheduling hint: the transport may ignore
// it. It never authorizes caching, changing parameters, skipping fresh checks,
// or releasing a transaction/generation fence. Commit reads remain unmarked.
func WithRPCMaintenance(ctx context.Context) context.Context {
	return context.WithValue(ctx, rpcMaintenanceKey{}, true)
}

// IsRPCMaintenance reports a scheduling hint, not a canonicality guarantee.
func IsRPCMaintenance(ctx context.Context) bool {
	v, _ := ctx.Value(rpcMaintenanceKey{}).(bool)
	return v
}
