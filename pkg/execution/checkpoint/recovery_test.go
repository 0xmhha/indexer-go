package checkpoint

import (
	"github.com/0xmhha/indexer-go/execwire"
	format "github.com/0xmhha/indexer-go/execwire/checkpoint"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestCheckpointRecoveryKeepsGenerationAndParentBoundaries(t *testing.T) {
	ref := execwire.Ref{Generation: 2, Height: 10, Hash: execwire.Hash{1}}
	c := port.ExecutionControl{Generation: 2, Visible: ref, Root: execwire.Hash{2}, BaselineReady: true}
	c.Chain = execwire.Chain{ID: "1", Genesis: execwire.Hash{3}}
	c.Anchor = execwire.Ref{Generation: 1, Hash: c.Chain.Genesis}
	src := format.SourceObservation{Chain: c.Chain, Anchor: c.Anchor, Committed: ref, Parent: ref, ParentRoot: c.Root, HeaderAvailable: true, StateAvailable: true}
	require.Equal(t, "compatible_observation", Diagnose(c, src).Status)
	for _, change := range []func(*format.SourceObservation){func(s *format.SourceObservation) { s.Committed.Generation++ }, func(s *format.SourceObservation) { s.Committed.Height++ }, func(s *format.SourceObservation) { s.StateAvailable = false }, func(s *format.SourceObservation) { s.ParentRoot[0]++ }, func(s *format.SourceObservation) { s.Anchor.Hash[0]++ }, func(s *format.SourceObservation) { s.TransitionPending = true }, func(s *format.SourceObservation) { s.MaintenancePending = true }} {
		s := src
		change(&s)
		r := Diagnose(c, s)
		require.Equal(t, "blocked", r.Status)
		require.NotEmpty(t, r.Findings)
	}
	c.Maintenance = true
	require.Equal(t, "blocked", Diagnose(c, src).Status)
	c.Maintenance = false
	// A lost ACK can leave the source behind; only normal handshake/fresh gates
	// may confirm the surviving prefix. Diagnosis itself changes neither side.
	src.Committed.Height--
	require.Equal(t, "compatible_observation", Diagnose(c, src).Status)
	require.EqualValues(t, 2, c.Generation)
}
