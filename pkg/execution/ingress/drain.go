package ingress

import (
	"context"
	"github.com/0xmhha/indexer-go/execwire"
)

// DrainPoint is a joined worker boundary, never a retention/deletion permit.
// The existing maintenance coordinator must independently revalidate coverage
// and perform its fresh canonical checks before any commit.
type DrainPoint struct {
	Chain        execwire.Chain
	Ref          execwire.Ref
	Root, Digest execwire.Hash
}

type DrainProgram interface {
	Program
	Drain(context.Context) (DrainPoint, error)
	Resume(context.Context) error
}
