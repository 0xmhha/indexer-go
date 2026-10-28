package retention

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/metadata"
)

func nativeSeal(t *testing.T, path string, v any) []byte {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	data, e := json.Marshal(struct {
		Data   json.RawMessage
		Digest execwire.Hash
	}{b, execwire.Hash(sha256.Sum256(b))})
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(path, data, 0600); e != nil {
		t.Fatal(e)
	}
	return data
}
func TestNativeMetadataPrivateStrictReadOnlyBoundaries(t *testing.T) {
	for _, profile := range []string{"arc-native", "stablenet-native"} {
		t.Run(profile, func(t *testing.T) {
			dir, e := filepath.EvalSymlinks(t.TempDir())
			if e != nil {
				t.Fatal(e)
			}
			native := filepath.Join(dir, "native")
			if e = os.Mkdir(native, 0700); e != nil {
				t.Fatal(e)
			}
			marker := filepath.Join(dir, ".native.execution-retention")
			floorDir := native
			if profile == "stablenet-native" {
				marker = filepath.Join(native, ".execution-retention")
				floorDir = filepath.Join(native, "indexer-stablenet", "chaindata")
				if e = os.MkdirAll(floorDir, 0700); e != nil {
					t.Fatal(e)
				}
			}
			layout := NativeLayout{Fence: marker, Floor: filepath.Join(floorDir, ".execution-retention-floor")}
			if profile == "arc-native" {
				layout.Detached = filepath.Join(native, ".execution-retention-pending")
				layout.AllowMissingStore = true
			}
			out, e := InspectNativeMetadata(t.Context(), native, layout)
			if e != nil || !out.StorePresent || out.Fence != nil || out.Floor != nil {
				t.Fatal(out, e)
			}
			p := Permit{Version: 1, Coverage: "full", BaselineDigest: execwire.Hash{8}, Token: execwire.Hash{7}, Request: validRequest(), Visible: execwire.Ref{Generation: 2, Height: 90001, Hash: execwire.Hash{8}}, Root: execwire.Hash{9}}
			f := NativeFence{Version: 1, Permit: p, Phase: "done", Original: FileIdentity{Inode: 1}, Replacement: FileIdentity{Inode: 2}}
			before := nativeSeal(t, marker, f)
			i, e := os.Stat(marker)
			if e != nil {
				t.Fatal(e)
			}
			nativeSeal(t, filepath.Join(floorDir, ".execution-retention-floor"), NativeFloor{Version: 1, Permit: p})
			if profile == "arc-native" {
				nativeSeal(t, filepath.Join(native, ".execution-retention-pending"), f)
			}
			out, e = InspectNativeMetadata(t.Context(), native, layout)
			if e != nil || out.Fence == nil || out.Floor == nil || out.Floor.Permit != p {
				t.Fatal(out, e)
			}
			after, e := os.ReadFile(marker)
			if e != nil || string(before) != string(after) {
				t.Fatal("metadata modified")
			}
			j, e := os.Stat(marker)
			if e != nil || !os.SameFile(i, j) {
				t.Fatal("inode replaced")
			}
			for _, b := range []string{`{`, `{"Data":null}`, `{"Data":{},"data":{}}`, strings.Repeat("x", (1<<20)+1)} {
				if e = os.WriteFile(marker, []byte(b), 0600); e != nil {
					t.Fatal(e)
				}
				if _, e = InspectNativeMetadata(t.Context(), native, layout); e == nil {
					t.Fatal("corruption accepted")
				}
			}
			for _, bad := range []any{struct {
				Version               uint16
				Permit                Permit
				Phase                 string
				Original, Replacement FileIdentity
				Unknown               string
			}{1, p, "done", f.Original, f.Replacement, "secret"}, NativeFence{Version: 1, Permit: p, Phase: "resume", Original: f.Original}, NativeFence{Version: 2, Permit: p, Phase: "done", Original: f.Original}} {
				nativeSeal(t, marker, bad)
				if _, e = InspectNativeMetadata(t.Context(), native, layout); e == nil {
					t.Fatal("unsupported metadata accepted")
				}
			}
			if e = os.Remove(marker); e != nil {
				t.Fatal(e)
			}
			if e = os.Symlink(filepath.Join(floorDir, ".execution-retention-floor"), marker); e != nil {
				t.Fatal(e)
			}
			if _, e = InspectNativeMetadata(t.Context(), native, layout); !errors.Is(e, metadata.ErrUnsafe) {
				t.Fatal("unsafe marker link", e)
			}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			if _, e = InspectNativeMetadata(ctx, native, layout); !errors.Is(e, context.Canceled) {
				t.Fatal(e)
			}
		})
	}
}
