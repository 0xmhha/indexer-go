package fetch

import (
	"context"
	"errors"
	"testing"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	storagepkg "github.com/0xmhha/indexer-go/pkg/storage"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

type executionConfirmationStore struct {
	*storagepkg.PebbleStorage
	ref     execwire.Ref
	payload []byte
}

func TestExecutionReorgValidationNeverAdvancesOnRPCLag(t *testing.T) {
	h := newChainHarness(t, &Config{}, nil)
	ref := execwire.Ref{Generation: 1, Height: 7, Hash: execwire.Hash{8}}
	fresh := &executionCanonicalProbe{hash: common.Hash(ref.Hash)}
	h.f.SetExecutionCanonical(fresh)
	require.NoError(t, h.f.ValidateExecutionReorg(t.Context(), ref))
	fresh.hash = common.Hash{9}
	require.ErrorIs(t, h.f.ValidateExecutionReorg(t.Context(), ref), port.ErrExecutionCanonicalLag)
	failure := errors.New("RPC unavailable")
	fresh.fail = failure
	err := h.f.ValidateExecutionReorg(t.Context(), ref)
	require.ErrorIs(t, err, port.ErrExecutionCanonicalLag)
	require.ErrorIs(t, err, failure)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, h.f.ValidateExecutionReorg(ctx, ref), context.Canceled)
	require.Equal(t, 3, fresh.calls)
}

func (s *executionConfirmationStore) ConfirmExecution(ctx context.Context, payload []byte, fresh func(context.Context, execwire.Ref) error) (bool, error) {
	s.payload = append([]byte(nil), payload...)
	if err := fresh(ctx, s.ref); err != nil {
		return false, err
	}
	return true, nil
}
func TestExecutionConfirmationUsesUncachedCanonicalAndPropagatesFailure(t *testing.T) {
	h := newChainHarness(t, &Config{}, nil)
	ref := execwire.Ref{Generation: 2, Height: 9, Hash: execwire.Hash{4}}
	h.f.storage = &executionConfirmationStore{PebbleStorage: h.db, ref: ref}
	fresh := &executionCanonicalProbe{hash: common.Hash(ref.Hash)}
	h.f.SetExecutionCanonical(fresh)
	ok, err := h.f.ConfirmExecution(t.Context(), []byte("exact opaque wire payload"))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, 1, fresh.calls)
	fresh.hash = common.Hash{5}
	ok, err = h.f.ConfirmExecution(t.Context(), nil)
	require.ErrorIs(t, err, ErrReorg)
	require.False(t, ok)
	failure := errors.New("RPC unavailable")
	fresh.fail = failure
	_, err = h.f.ConfirmExecution(t.Context(), nil)
	require.ErrorIs(t, err, failure)
	require.Equal(t, 3, fresh.calls)
}
