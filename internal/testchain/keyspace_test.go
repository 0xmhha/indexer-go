package testchain

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func entries(kv ...string) []Entry {
	var out []Entry
	for i := 0; i < len(kv); i += 2 {
		out = append(out, Entry{Key: []byte(kv[i]), Value: []byte(kv[i+1])})
	}
	return out
}

func TestDiffKeyspace(t *testing.T) {
	want := entries("/a/1", "x", "/a/2", "y", "/b/1", "z")
	got := entries("/a/1", "x", "/a/3", "y", "/b/1", "CHANGED")

	require.Equal(t, []string{"- /a/2", "+ /a/3", "~ /b/1"}, DiffKeyspace(want, got, 0))
	require.Empty(t, DiffKeyspace(want, want, 0))
	require.Len(t, DiffKeyspace(want, got, 2), 2, "max limits the output")

	// Entirely missing or extra sides.
	require.Equal(t, []string{"- /a/1"}, DiffKeyspace(entries("/a/1", "x"), nil, 0))
	require.Equal(t, []string{"+ /a/1"}, DiffKeyspace(nil, entries("/a/1", "x"), 0))
}

func TestSummarizeDiff(t *testing.T) {
	lines := []string{
		"+ /index/addr/0xA/01",
		"+ /index/addr/0xA/02",
		"~ /index/balance/0xA/latest",
		"- /index/addr/0xB/01",
		"~ /meta/tc/",
	}
	require.Equal(t, []string{
		"/index/addr/ missing=1 extra=2 changed=0",
		"/index/balance/ missing=0 extra=0 changed=1",
		"/meta/tc/ missing=0 extra=0 changed=1",
	}, SummarizeDiff(lines))
}

func TestFormatKeyspaceIsStableAndReadable(t *testing.T) {
	es := []Entry{
		{Key: []byte("/data/blocks/1"), Value: []byte("v")},
		{Key: append([]byte("/bin/"), 0x00, 0xff), Value: nil},
	}
	var a, b bytes.Buffer
	require.NoError(t, FormatKeyspace(&a, es))
	require.NoError(t, FormatKeyspace(&b, es))
	require.Equal(t, a.String(), b.String())

	lines := strings.Split(strings.TrimSpace(a.String()), "\n")
	require.Len(t, lines, 2)
	require.True(t, strings.HasPrefix(lines[0], "/data/blocks/1 1 "))
	require.True(t, strings.HasPrefix(lines[1], "/bin/<00ff> 0 "), lines[1])
}
