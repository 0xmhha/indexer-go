package window

import (
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestLayoutOptionsPreserveExplicitClockWithoutConfigCapability(t *testing.T) {
	layout, err := ReadLayout(strings.NewReader(layoutYAML(t.TempDir())))
	require.NoError(t, err)
	base := time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)
	var offset, calls atomic.Int64
	clock := func() time.Time { calls.Add(1); return base.Add(time.Duration(offset.Load())) }
	layout.Now = clock
	options, err := layout.OptionsFor("arc", model.FeedChain{ID: "5042002", GenesisHash: common.Hash{1}})
	require.NoError(t, err)
	require.Zero(t, calls.Load(), "pure validation must not call the embedding clock")
	require.Equal(t, base, options.Now())
	offset.Store(int64(25 * time.Hour))
	require.Equal(t, base.Add(25*time.Hour), options.Now())
	require.Equal(t, 24*time.Hour, options.Retention)
	encoded, err := json.Marshal(layout)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "Now")
	encoded, err = yaml.Marshal(layout)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "now:")
	_, err = ReadLayout(strings.NewReader(layoutYAML(t.TempDir()) + "now: 2026-10-31T00:00:00Z\n"))
	require.Error(t, err, "clock is not a YAML/operator capability")
}
