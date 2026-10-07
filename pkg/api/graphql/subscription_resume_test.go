package graphql

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/0xmhha/indexer-go/pkg/storage"
)

// readErrorCode reads the next message and returns its extensions.code.
func readErrorCode(t *testing.T, conn interface{ ReadJSON(any) error }) (string, map[string]any) {
	t.Helper()
	var m wsMsg
	require.NoError(t, conn.ReadJSON(&m))
	require.Equal(t, "error", m.Type, "%s", m.Payload)
	var errs []struct {
		Extensions map[string]any `json:"extensions"`
	}
	require.NoError(t, json.Unmarshal(m.Payload, &errs))
	return errs[0].Extensions["code"].(string), errs[0].Extensions
}

// TestFromSequenceWithoutOutbox: a server without the outbox refuses
// fromSequence.
func TestFromSequenceWithoutOutbox(t *testing.T) {
	_, _, srv := newEngineServer(t, 16)
	conn := subscribeWS(t, srv.URL, "b", `subscription { newBlock { number } }`, map[string]any{"fromSequence": 1})
	defer func() { _ = conn.Close() }()
	code, _ := readErrorCode(t, conn)
	require.Equal(t, "RESUME_UNSUPPORTED", code)
}

// TestFromSequenceTooOld: a sequence whose events were pruned is refused
// with the oldest sequence kept; one that is kept resumes.
func TestFromSequenceTooOld(t *testing.T) {
	ctx := context.Background()
	s, err := storage.NewPebbleStorage(storage.DefaultConfig(t.TempDir()))
	require.NoError(t, err)
	defer func() { _ = s.Close() }()
	txCtx, tx, err := s.BeginBlock(ctx)
	require.NoError(t, err)
	var entries []port.OutboxEntry
	for n := uint64(1); n <= 10; n++ {
		data, err := events.MarshalEvent(&events.BlockEvent{Number: n})
		require.NoError(t, err)
		entries = append(entries, port.OutboxEntry{Type: string(events.EventTypeBlock), Data: data})
	}
	require.NoError(t, s.AppendOutbox(txCtx, entries))
	require.NoError(t, tx.Commit())
	require.NoError(t, s.PruneOutbox(ctx, 6))

	_, sub, srv := newEngineServer(t, 16)
	sub.SetOutbox(s)
	old := subscribeWS(t, srv.URL, "b", `subscription { newBlock { number } }`, map[string]any{"fromSequence": "3"})
	defer func() { _ = old.Close() }()
	code, ext := readErrorCode(t, old)
	require.Equal(t, "SEQUENCE_TOO_OLD", code)
	require.EqualValues(t, 6, ext["oldest"])

	kept := subscribeWS(t, srv.URL, "b", `subscription { newBlock { number } }`, map[string]any{"fromSequence": 8})
	defer func() { _ = kept.Close() }()
	for want := uint64(8); want <= 10; want++ {
		require.NoError(t, kept.SetReadDeadline(time.Now().Add(5*time.Second)))
		var m wsMsg
		require.NoError(t, kept.ReadJSON(&m))
		require.Equal(t, want, nextSequence(t, m))
		require.Contains(t, string(m.Payload), fmt.Sprintf(`"number":%d,`, want))
	}
}
