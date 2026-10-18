package model

import (
	"crypto/sha256"
	"encoding/json"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestSupplementDescriptorSurvivesPersistence(t *testing.T) {
	valid := func(raw string) SupplementStreamDescriptor {
		return SupplementStreamDescriptor{Version: 1, Name: "tracked.v1", Codec: "test.v1", FromHeight: 2, Selection: json.RawMessage(raw), SelectionHash: sha256.Sum256(append([]byte("test.v1\x00"), raw...))}
	}
	d := valid(`{"escaped":"\u003c"}`)
	require.NoError(t, d.Validate())
	raw, e := json.Marshal(d)
	require.NoError(t, e)
	var got SupplementStreamDescriptor
	require.NoError(t, json.Unmarshal(raw, &got))
	require.NoError(t, got.Validate())
	require.Equal(t, d, got)
	for _, raw := range []string{`{"escaped":"<"}`, `{ "spaced": 1 }`, `{}`, `null`} {
		d := valid(raw)
		if raw == `{}` || raw == `null` {
			require.NoError(t, d.Validate())
		} else {
			require.Error(t, d.Validate())
		}
	}
	d.FromHeight = 0
	require.Error(t, d.Validate())
	d = valid(`{}`)
	d.Name = "quoted';--"
	require.Error(t, d.Validate())
}
