package nativestore

import (
	"context"
	"github.com/0xmhha/indexer-go/execwire/retention"
	"path/filepath"
)

// InspectMetadata reads maintenance records only; it never opens a native DB.
func InspectMetadata(ctx context.Context, path string) (retention.NativeMetadata, error) {
	return retention.InspectNativeMetadata(ctx, path, retention.NativeLayout{Fence: filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".execution-retention"), Floor: filepath.Join(path, floorName), Detached: filepath.Join(path, ".execution-retention-pending"), AllowMissingStore: true})
}
