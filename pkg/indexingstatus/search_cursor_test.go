package indexingstatus

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestSourceSearchCursorScopeAndBounds(t *testing.T) {
	scope := capture.SearchScope{Epoch: 1, EndHash: common.Hash{1}, Root: common.Hash{2}, Filter: common.Hash{3}, Definition: "source-search-v1-fixture"}
	raw := encodeSearchCursor(scope, capture.SearchPosition{Height: 9, Index: 3})
	got, e := DecodeSearchCursor(raw)
	require.NoError(t, e)
	require.Equal(t, scope, got.Scope)
	require.EqualValues(t, 9, got.After.Height)
	b, e := base64.RawURLEncoding.DecodeString(raw)
	require.NoError(t, e)
	var doc map[string]any
	require.NoError(t, json.Unmarshal(b, &doc))
	doc["extra"] = 1
	unknown, _ := json.Marshal(doc)
	for _, bad := range []string{"", strings.Repeat("a", MaxSearchCursorBytes+1), raw + "=", base64.RawURLEncoding.EncodeToString(unknown), base64.RawURLEncoding.EncodeToString(append(b, ' ')), base64.RawURLEncoding.EncodeToString(append(b, []byte(` {}`)...))} {
		_, e := DecodeSearchCursor(bad)
		require.Error(t, e)
	}
}
