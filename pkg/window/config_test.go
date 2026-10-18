package window

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/core/model"
)

func layoutYAML(root string) string {
	return fmt.Sprintf(`volumes:
  shared:
    root: %q
    id: shared-fixture
    allow_directory: true
    max_bytes: 5242880
windows:
  arc:
    volume: shared
    path: %q
    max_bytes: 2097152
    segment_bytes: 131072
    max_payload_bytes: 32768
    max_segment_records: 8
    retention: 24h
`, root, filepath.Join(root, "arc"))
}

func TestLayoutStrictParsingAndPureResolution(t *testing.T) {
	root := filepath.Join(t.TempDir(), "absent-volume")
	l, err := ReadLayout(strings.NewReader(layoutYAML(root)))
	require.NoError(t, err)
	chain := model.FeedChain{ID: "5042002", GenesisHash: common.Hash{7}}
	o, err := l.OptionsFor("arc", chain)
	require.NoError(t, err)
	require.Equal(t, chain, o.Chain)
	require.Equal(t, "arc", o.Reservation)
	require.Equal(t, filepath.Join(root, "arc"), o.Path)
	require.Equal(t, 24*time.Hour, o.Retention)
	require.EqualValues(t, 32, o.MaxReaders)
	_, err = os.Stat(root)
	require.ErrorIs(t, err, os.ErrNotExist, "parsing must not initialize external storage")
	_, err = l.OptionsFor("unknown", chain)
	require.Error(t, err)
	_, err = l.OptionsFor("arc", model.FeedChain{})
	require.Error(t, err)
	for name, text := range map[string]string{
		"top-level typo":     strings.Replace(layoutYAML(root), "volumes:", "volumse:", 1),
		"volume typo":        strings.Replace(layoutYAML(root), "allow_directory:", "allow_directroy:", 1),
		"window typo":        strings.Replace(layoutYAML(root), "retention:", "retension:", 1),
		"missing volume":     strings.Replace(layoutYAML(root), "volume: shared", "volume: missing", 1),
		"short retention":    strings.Replace(layoutYAML(root), "24h", "23h", 1),
		"overflow":           strings.Replace(layoutYAML(root), "5242880", "9223372036854775808", 1),
		"multiple documents": layoutYAML(root) + "---\n{}\n",
		"oversize":           strings.Repeat("#", (1<<20)+1),
	} {
		t.Run(name, func(t *testing.T) { _, err := ReadLayout(strings.NewReader(text)); require.Error(t, err) })
	}
}

func TestLayoutSharedLimitsAndOverlaps(t *testing.T) {
	for name, mutate := range map[string]func(*Layout){
		"overcommitted": func(l *Layout) {
			for _, n := range []string{"bsc", "sepolia"} {
				w := l.Windows["arc"]
				w.Path = filepath.Join(l.Volumes["shared"].Root, n)
				l.Windows[n] = w
			}
		},
		"overlapping window":      func(l *Layout) { w := l.Windows["arc"]; w.Path += "/child"; l.Windows["bsc"] = w },
		"root window":             func(l *Layout) { w := l.Windows["arc"]; w.Path = l.Volumes["shared"].Root; l.Windows["arc"] = w },
		"relative path":           func(l *Layout) { w := l.Windows["arc"]; w.Path = "relative"; l.Windows["arc"] = w },
		"zero budget":             func(l *Layout) { v := l.Volumes["shared"]; v.MaxBytes = 0; l.Volumes["shared"] = v },
		"negative floor":          func(l *Layout) { v := l.Volumes["shared"]; v.MinFreeBytes = -1; l.Volumes["shared"] = v },
		"duplicate identity":      func(l *Layout) { v := l.Volumes["shared"]; v.Root += "-other"; l.Volumes["other"] = v },
		"overlapping volume":      func(l *Layout) { v := l.Volumes["shared"]; v.ID = "other"; v.Root += "/nested"; l.Volumes["other"] = v },
		"filesystem root overlap": func(l *Layout) { v := l.Volumes["shared"]; v.ID = "other"; v.Root = "/"; l.Volumes["other"] = v },
	} {
		t.Run(name, func(t *testing.T) {
			l, err := ReadLayout(strings.NewReader(layoutYAML(t.TempDir())))
			require.NoError(t, err)
			mutate(&l)
			require.Error(t, l.Validate())
		})
	}
	require.NoError(t, (Layout{}).Validate())
}

func TestLayoutDocumentedFourChainExample(t *testing.T) {
	f, err := os.Open("../../docs/examples/window-storage.yaml")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	l, err := ReadLayout(f)
	require.NoError(t, err)
	require.Len(t, l.Windows, 4)
	require.False(t, l.Volumes["external"].AllowDirectory)
}
