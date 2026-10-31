package nativemonitor

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/configfile"
	"github.com/stretchr/testify/require"
)

func TestManagedDiagnosticInputRefsAreAcceptedWithoutSQLGeneration(t *testing.T) {
	p, s, at := fixture()
	data, err := json.Marshal(map[string]any{"kind": "managed.raw", "basis": "fixed_managed_metadata_not_sdk_state_or_lease", "chain": s.Identity.Chain, "original_anchor": s.Identity.Anchor, "received_ref": map[string]any{"height": uint64(100), "hash": execwire.Hash{3}, "root": execwire.Hash{4}}, "raw_epoch": uint64(1)})
	require.NoError(t, err)
	var o Observation
	require.NoError(t, configfile.JSON(data, &o), "consumer must accept producer's distinct received/native/epoch metadata")
	s.Diagnostics.Report.Observations = []Observation{o}
	r := Evaluate(context.Background(), &p, &s, &at)
	require.NotEqual(t, fail, r.Status, "managed observations remain HOLD evidence, not an invalid report: %+v", r.Findings)
}
