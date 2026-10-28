// Package diagnostics binds the product's existing maintenance format without DB access.
package diagnostics

import (
	"context"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/retention"
	"github.com/0xmhha/indexer-go/internal/nativediagnostics"
	"path/filepath"
)

func Product() nativediagnostics.Product {
	return nativediagnostics.Product{Name: "stablenet-native", InspectNative: func(ctx context.Context, path string) (retention.NativeMetadata, error) {
		return retention.InspectNativeMetadata(ctx, path, retention.NativeLayout{Fence: filepath.Join(path, ".execution-retention"), Floor: filepath.Join(path, "indexer-stablenet", "chaindata", ".execution-retention-floor")})
	}, ChainAllowed: func(c execwire.Chain) bool { return c.ID != "" }}
}
