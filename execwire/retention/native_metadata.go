package retention

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/0xmhha/indexer-go/execwire/metadata"
)

type FileIdentity struct{ Device, Inode uint64 }
type NativeFence struct {
	Version               uint16
	Permit                Permit
	Phase                 string
	Original, Replacement FileIdentity
}
type NativeFloor struct {
	Version uint16
	Permit  Permit
}

type NativeMetadata struct {
	StorePresent    bool
	Fence           *NativeFence
	Floor           *NativeFloor
	DetachedPending *NativeFence
}

// NativeLayout selects only the owning adapter's fixed maintenance records.
type NativeLayout struct {
	Fence, Floor, Detached string
	AllowMissingStore      bool
}

// InspectNativeMetadata observes fixed maintenance records only. The DB's
// existence/format/head/state are uninspected, even if the directory exists.
// Layout paths are supplied only by the owning product adapter.
func InspectNativeMetadata(ctx context.Context, dataDir string, layout NativeLayout) (out NativeMetadata, err error) {
	if !filepath.IsAbs(dataDir) || filepath.Clean(dataDir) == string(filepath.Separator) || !filepath.IsAbs(layout.Fence) || !filepath.IsAbs(layout.Floor) || layout.Detached != "" && !filepath.IsAbs(layout.Detached) {
		return out, metadata.ErrUnsafe
	}
	r := metadata.New(ctx)
	storeErr := r.Directory(dataDir)
	if storeErr != nil {
		if !errors.Is(storeErr, os.ErrNotExist) || !layout.AllowMissingStore {
			return out, storeErr
		}
		if err = r.Directory(filepath.Dir(dataDir)); err != nil {
			return out, err
		}
	} else {
		out.StorePresent = true
	}
	marker, floorPath, detached := layout.Fence, layout.Floor, layout.Detached
	readFence := func(path string) (*NativeFence, error) {
		b, e := r.File(path)
		if os.IsNotExist(e) {
			return nil, nil
		}
		if e != nil {
			return nil, e
		}
		var f NativeFence
		if metadata.Sealed(b, &f) != nil || f.Version != 1 || f.Permit.Validate() != nil || f.Original.Inode == 0 {
			return nil, metadata.ErrUnsafe
		}
		switch f.Phase {
		case "copy":
			if f.Replacement != (FileIdentity{}) {
				return nil, metadata.ErrUnsafe
			}
		case "swap", "cleanup", "done":
			if f.Replacement.Inode == 0 || f.Replacement == f.Original {
				return nil, metadata.ErrUnsafe
			}
		default:
			return nil, metadata.ErrUnsafe
		}
		return &f, nil
	}
	out.Fence, err = readFence(marker)
	// A missing chaindata parent is uninitialized, not an absent optional floor.
	if err == nil && out.StorePresent {
		var b []byte
		b, err = r.File(floorPath)
		if err == nil {
			var f NativeFloor
			if metadata.Sealed(b, &f) != nil || f.Version != 1 || f.Permit.Validate() != nil {
				err = metadata.ErrUnsafe
			} else {
				out.Floor = &f
			}
		} else if os.IsNotExist(err) {
			if _, e := os.Lstat(filepath.Dir(floorPath)); e == nil {
				err = nil
			}
		}
	}
	if err == nil && out.StorePresent && detached != "" {
		out.DetachedPending, err = readFence(detached)
	}
	return out, errors.Join(err, r.Verify(), storeErr, ctx.Err())
}
