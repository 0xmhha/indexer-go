package vmobserve

import (
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

const header = "Filesystem 1024-blocks Used Available Capacity Mounted on\n"
const report = header + "overlay 10 8 2 80% /\n"

func TestParseFreeKiBValidAndKnownZero(t *testing.T) {
	n, e := ParseFreeKiB([]byte(report))
	require.NoError(t, e)
	require.True(t, n.Present)
	require.EqualValues(t, 2048, n.Value)
	n, e = ParseFreeKiB([]byte(header + "overlay 10 10 0 100% /\n"))
	require.NoError(t, e)
	require.True(t, n.Present)
	require.Zero(t, n.Value)
	n, e = ParseFreeKiB([]byte(strings.ReplaceAll(report, "1024-blocks", "1K-blocks")))
	require.NoError(t, e)
	require.EqualValues(t, 2048, n.Value)
}
func TestParseFreeKiBRejectsUnknownInsteadOfZero(t *testing.T) {
	for _, input := range []string{"", "permission denied", "Cannot connect to daemon", strings.ReplaceAll(report, "1024-blocks", "512-blocks"), header + "overlay 10 0 -1 0% /\n", header + "overlay 10 0 +1 0% /\n", header + "overlay 10 0 1.5 0% /\n", header + "overlay 18446744073709551615 0 18446744073709551615 0% /\n", header + "overlay 10 8 3 80% /\n", header + "overlay 10 0 11 0% /\n", header + "overlay 0 0 0 0% /\n", header + "overlay 10 0 1 101% /\n", strings.ReplaceAll(report, " /\n", " /other\n"), report + "overlay 10 8 2 80% /\n", report + "extra", report + "\x00", strings.Repeat("x", MaxStdout+1)} {
		n, e := ParseFreeKiB([]byte(input))
		require.ErrorIs(t, e, ErrReport)
		require.False(t, n.Present, "invalid input became known data")
		require.Zero(t, n.Value)
	}
}
