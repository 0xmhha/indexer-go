package postgres

import (
	"testing"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/stretchr/testify/require"
)

func TestExecutionLegacyBlockWriterCannotBypassPublication(t *testing.T) {
	s, cfg := executionFixture(t)
	require.NoError(t, s.InitExecution(t.Context(), cfg))
	_, tx, err := s.BeginBlock(t.Context())
	if tx != nil {
		defer tx.Rollback()
	}
	require.ErrorIs(t, err, port.ErrInvalidData)
}
