package app

import (
	"encoding/json"
	"testing"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/feature"
	"github.com/0xmhha/indexer-go/pkg/fetch"
	"github.com/0xmhha/indexer-go/pkg/storage"
	"github.com/stretchr/testify/require"
)

func TestExecutionFeatureResumeRejectsMissingOrChangedDefinition(t *testing.T) {
	s, err := storage.NewPebbleStorage(storage.DefaultConfig(t.TempDir()))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, s.Close()) })
	c := port.ExecutionControl{Generation: 1, Visible: execwire.Ref{Generation: 1, Height: 1, Hash: execwire.Hash{2}}}
	c.Anchor = execwire.Ref{Generation: 1, Hash: execwire.Hash{1}}
	u := []feature.Unit{{Name: "balance.native", Definition: "v1"}}
	require.ErrorIs(t, validateExecutionFeatures(t.Context(), s, c, u), port.ErrExecutionCoverage)
	checkpoint := fetch.ExecutionFeatureCheckpoint{Version: 1, Definition: "v1", Ref: c.Visible, Digest: execwire.Hash{3}}
	write := func() {
		b, e := json.Marshal(checkpoint)
		require.NoError(t, e)
		require.NoError(t, s.Put(t.Context(), []byte(fetch.ExecutionFeaturePrefix+u[0].Name), b))
	}
	write()
	require.NoError(t, validateExecutionFeatures(t.Context(), s, c, u))
	checkpoint.Definition = "v0"
	write()
	require.ErrorIs(t, validateExecutionFeatures(t.Context(), s, c, u), port.ErrExecutionConflict)
	checkpoint.Definition = "v1"
	checkpoint.Ref.Height = 0
	write()
	require.ErrorIs(t, validateExecutionFeatures(t.Context(), s, c, u), port.ErrExecutionConflict)
	c.Visible = c.Anchor
	require.NoError(t, validateExecutionFeatures(t.Context(), s, c, u))
}
