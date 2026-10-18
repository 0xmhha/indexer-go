package port

import "context"

// QueryReaderLease reserves capacity inside an existing Store for short API
// reads. Context selects it only for unbound calls on that same Store. Bound
// transactions must retain priority, including canonical commit validation.
// Close is idempotent and waits for active rows to close. Contexts must not be
// used after the lease closes.
type QueryReaderLease interface {
	Context(context.Context) context.Context
	Close()
}

// QueryReaderReserver is optional. A nil lease means this Store/budget does
// not support reservation. It must not increase the configured pool limit.
type QueryReaderReserver interface {
	ReserveQueryReader(context.Context) (QueryReaderLease, error)
}
