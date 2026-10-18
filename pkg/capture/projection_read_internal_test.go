package capture

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/stretchr/testify/require"
)

func TestProjectionBufferAndMissingRecordCauses(t *testing.T) {
	b := &projectionBuffer{limit: 4}
	_, e := io.Copy(b, bytes.NewReader([]byte("oversized")))
	require.ErrorIs(t, e, port.ErrInvalidData)
	require.Empty(t, b.Bytes())
	_, e = io.Copy(b, bytes.NewReader([]byte("four")))
	require.NoError(t, e)
	_, e = b.Write([]byte("!"))
	require.ErrorIs(t, e, port.ErrInvalidData)
	require.Equal(t, []byte("four"), b.Bytes())
	require.True(t, missingProjectionRecord(fmt.Errorf("wrapped: %w", port.ErrNotFound)))
	require.False(t, missingProjectionRecord(errors.Join(port.ErrNotFound, io.ErrUnexpectedEOF)))
}
