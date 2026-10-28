package nodestore

import (
	"context"
	"github.com/0xmhha/indexer-go/execwire/retention"
	"path/filepath"
)

// InspectMetadata reads maintenance records only; it never opens a native DB.
func InspectMetadata(ctx context.Context, path string) (retention.NativeMetadata, error) {
	return retention.InspectNativeMetadata(ctx, path, retention.NativeLayout{Fence: filepath.Join(path, fenceName), Floor: filepath.Join(path, "indexer-stablenet", "chaindata", floorName)})
}
