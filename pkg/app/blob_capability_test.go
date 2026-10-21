package app

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	statusapi "github.com/0xmhha/indexer-go/pkg/api/indexing"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/indexingstatus"
	"github.com/0xmhha/indexer-go/pkg/progress"
	"github.com/0xmhha/indexer-go/x/arc"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestManagedBlobCapabilityReadonlyAndLegacy(t *testing.T) {
	cfg, _, rpc := feedAppConfig(t)
	a, e := NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, e)
	t.Cleanup(a.Shutdown)
	before := rpc.Calls()
	status, e := indexingstatus.Read(t.Context(), a.storage, "", 100)
	require.NoError(t, e)
	require.NotNil(t, status.Source.BlobCapability)
	require.Equal(t, arc.BlobCapability(), *status.Source.BlobCapability)
	for _, d := range status.Source.Datasets {
		if d.Kind == model.DatasetBlobs {
			require.Equal(t, "unavailable", d.Coverage)
		}
	}
	kv := a.storage.(port.KVStore)
	raw, e := kv.Get(t.Context(), []byte(progress.DescriptorKey))
	require.NoError(t, e)
	var d model.ProjectionDescriptor
	require.NoError(t, json.Unmarshal(raw, &d))
	saved := d.BlobCapability
	// Old descriptors do not acquire policy merely from chain ID or current
	// profile code. Missing metadata remains unreported until ingest rewrites it.
	d.BlobCapability = nil
	legacy, e := json.Marshal(d)
	require.NoError(t, e)
	require.NoError(t, kv.Put(t.Context(), []byte(progress.DescriptorKey), legacy))
	status, e = indexingstatus.Read(t.Context(), a.storage, "", 100)
	require.NoError(t, e)
	require.Nil(t, status.Source.BlobCapability)
	d.BlobCapability = saved
	d.BlobCapability.SourceURL = "https://example.test/unpinned/main/pool.rs"
	require.ErrorIs(t, a.fetcher.ConfigureProjection(t.Context(), d), port.ErrInvalidData, "writer rejects invalid policy before a transaction")
	bad, e := json.Marshal(d)
	require.NoError(t, e)
	require.NoError(t, kv.Put(t.Context(), []byte(progress.DescriptorKey), bad))
	w := httptest.NewRecorder()
	statusapi.Handler(a.storage, zap.NewNop()).ServeHTTP(w, httptest.NewRequest("GET", "/indexing/status", nil))
	require.Equal(t, 500, w.Code)
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	require.NotContains(t, w.Body.String(), "pool.rs")
	require.NoError(t, kv.Put(t.Context(), []byte(progress.DescriptorKey), raw))
	require.Equal(t, before, rpc.Calls(), "policy reads do not call RPC")
}
