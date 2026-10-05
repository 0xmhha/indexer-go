package era

import (
	"bytes"
	"encoding/binary"
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/golang/snappy"
	"github.com/stretchr/testify/require"
)

// writeEra1 writes an era1 file holding blocks start..start+len(blocks)-1.
// Each block is header, body, receipts and total difficulty, as written by
// go-ethereum's builder; extra adds an unknown entry before the receipts.
func writeEra1(t testing.TB, path string, start uint64, blocks []RawBlock, extra bool) {
	t.Helper()
	var buf bytes.Buffer
	entry := func(typ uint16, data []byte) {
		var h [headerSize]byte
		binary.LittleEndian.PutUint16(h[0:], typ)
		binary.LittleEndian.PutUint32(h[2:], uint32(len(data)))
		buf.Write(h[:])
		buf.Write(data)
	}
	compressed := func(data []byte) []byte {
		var out bytes.Buffer
		w := snappy.NewBufferedWriter(&out)
		_, err := w.Write(data)
		require.NoError(t, err)
		require.NoError(t, w.Close())
		return out.Bytes()
	}

	entry(typeVersion, nil)
	offsets := make([]int64, len(blocks))
	for i, b := range blocks {
		offsets[i] = int64(buf.Len())
		entry(typeCompressedHeader, compressed(b.Header))
		entry(typeCompressedBody, compressed(b.Body))
		if extra {
			entry(0x7777, []byte{1, 2, 3})
		}
		entry(typeCompressedReceipts, compressed(b.Receipts))
		entry(typeTotalDifficulty, make([]byte, 32))
	}
	entry(typeAccumulator, make([]byte, 32))

	indexAt := int64(buf.Len())
	index := make([]byte, 16+8*len(blocks))
	binary.LittleEndian.PutUint64(index, start)
	for i, off := range offsets {
		binary.LittleEndian.PutUint64(index[8+8*i:], uint64(off-indexAt))
	}
	binary.LittleEndian.PutUint64(index[8+8*len(blocks):], uint64(len(blocks)))
	entry(typeBlockIndex, index)
	require.NoError(t, os.WriteFile(path, buf.Bytes(), 0o644))
}

func fakeBlocks(t testing.TB, start uint64, n int) []RawBlock {
	t.Helper()
	out := make([]RawBlock, n)
	for i := range out {
		h, err := rlp.EncodeToBytes(&types.Header{Number: new(big.Int).SetUint64(start + uint64(i)), Difficulty: big.NewInt(0)})
		require.NoError(t, err)
		out[i] = RawBlock{
			Number:   start + uint64(i),
			Header:   h,
			Body:     []byte{0xc2, 0xc0, 0xc0},
			Receipts: []byte{0xc0},
		}
	}
	return out
}

func TestFileReadsBlocks(t *testing.T) {
	for _, extra := range []bool{false, true} {
		path := filepath.Join(t.TempDir(), "test-00001-00000000.era1")
		want := fakeBlocks(t, 8192, 20)
		writeEra1(t, path, 8192, want, extra)

		f, err := Open(path)
		require.NoError(t, err)
		require.Equal(t, uint64(8192), f.Start())
		require.Equal(t, uint64(20), f.Count())
		for _, w := range want {
			got, err := f.Block(w.Number)
			require.NoError(t, err)
			require.Equal(t, w, *got)
		}
		_, err = f.Block(8191)
		require.Error(t, err)
		_, err = f.Block(8212)
		require.Error(t, err)
		require.NoError(t, f.Close())
	}
}

func TestOpenRejectsBadFiles(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.era1")
	writeEra1(t, good, 0, fakeBlocks(t, 0, 3), false)
	data, err := os.ReadFile(good)
	require.NoError(t, err)

	cases := map[string][]byte{
		"empty":      {},
		"no version": append([]byte{1, 0, 0, 0, 0, 0, 0, 0}, data[8:]...),
		"truncated":  data[:len(data)-5],
		"reserved":   append(append([]byte{}, data[:6]...), append([]byte{1, 0}, data[8:]...)...),
	}
	for name, b := range cases {
		p := filepath.Join(dir, name+".era1")
		require.NoError(t, os.WriteFile(p, b, 0o644))
		_, err := Open(p)
		require.Error(t, err, name)
	}
}

// TestRealEra1Files reads every block of the era1 files in INDEXER_ERA_DIR
// (made by `gstable export-history`) and checks that each header decodes and
// has the indexed height.
func TestRealEra1Files(t *testing.T) {
	dir := os.Getenv("INDEXER_ERA_DIR")
	if dir == "" {
		t.Skip("INDEXER_ERA_DIR not set")
	}
	paths, err := filepath.Glob(filepath.Join(dir, "*.era1"))
	require.NoError(t, err)
	require.NotEmpty(t, paths)
	for _, p := range paths {
		f, err := Open(p)
		require.NoError(t, err)
		for n := f.Start(); n < f.Start()+f.Count(); n++ {
			b, err := f.Block(n)
			require.NoError(t, err)
			var h types.Header
			require.NoError(t, rlp.DecodeBytes(b.Header, &h), "block %d", n)
			require.Equal(t, n, h.Number.Uint64())
			// Bodies may hold chain specific transaction types; here only
			// check that they are well formed RLP lists.
			kind, _, _, err := rlp.Split(b.Body)
			require.NoError(t, err, "block %d", n)
			require.Equal(t, rlp.List, kind)
		}
		t.Logf("%s: blocks %d..%d", filepath.Base(p), f.Start(), f.Start()+f.Count()-1)
		require.NoError(t, f.Close())
	}
}
