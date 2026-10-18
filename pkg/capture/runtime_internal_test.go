package capture

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/0xmhha/indexer-go/pkg/source"
	"github.com/stretchr/testify/require"
)

func TestReorgRecoveryNeverMasksIndependentFailures(t *testing.T) {
	require.True(t, recoveryOnly(errors.Join(ErrCanonical, fmt.Errorf("pipeline: %w", ErrGap))))
	require.True(t, recoveryOnly(errors.Join(ErrCanonical, fmt.Errorf("worker: %w", context.Canceled))))
	for _, err := range []error{nil, context.Canceled, errors.Join(ErrCanonical, os.ErrPermission), errors.Join(ErrCanonical, ErrFinalityConflict), ErrDeletePending, ErrStale} {
		require.False(t, recoveryOnly(err))
	}
}

func TestHintCauseKeepsIndependentFailures(t *testing.T) {
	require.Equal(t, source.ErrNotFound, hintCause(fmt.Errorf("height: %w", source.ErrNotFound)))
	require.Equal(t, ErrCanonical, hintCause(fmt.Errorf("commit: %w", ErrCanonical)))
	for _, err := range []error{errors.Join(source.ErrNotFound, os.ErrPermission), errors.Join(ErrCanonical, context.Canceled)} {
		require.Equal(t, err, hintCause(fmt.Errorf("transaction: %w", err)))
	}
	require.Nil(t, hintCause(nil))
}
