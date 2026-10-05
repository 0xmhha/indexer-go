package testchain

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math/big"
	"os"

	"github.com/ethereum/go-ethereum/rlp"
	"github.com/golang/snappy"
)

// WriteEra1 writes blocks first..last of the chain as an era1 archive, in
// the layout go-ethereum's era builder produces: a version entry, then per
// block the compressed header, body and receipts (consensus encodings) and
// the total difficulty, then an accumulator and the block index.
func (c *Chain) WriteEra1(path string, first, last uint64) error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if first > last || last >= uint64(len(c.blocks)) {
		return fmt.Errorf("testchain: era1 range [%d, %d] outside chain of %d blocks", first, last, len(c.blocks))
	}

	var buf bytes.Buffer
	entry := func(typ uint16, data []byte) {
		var h [8]byte
		binary.LittleEndian.PutUint16(h[0:], typ)
		binary.LittleEndian.PutUint32(h[2:], uint32(len(data)))
		buf.Write(h[:])
		buf.Write(data)
	}
	compressed := func(typ uint16, v any) error {
		raw, err := rlp.EncodeToBytes(v)
		if err != nil {
			return err
		}
		var out bytes.Buffer
		w := snappy.NewBufferedWriter(&out)
		if _, err := w.Write(raw); err != nil {
			return err
		}
		if err := w.Close(); err != nil {
			return err
		}
		entry(typ, out.Bytes())
		return nil
	}

	entry(0x3265, nil) // version
	var offsets []int64
	td := new(big.Int)
	for n := first; n <= last; n++ {
		b := c.blocks[n]
		offsets = append(offsets, int64(buf.Len()))
		if err := compressed(0x03, b.Block.Header()); err != nil {
			return err
		}
		if err := compressed(0x04, b.Block.Body()); err != nil {
			return err
		}
		if err := compressed(0x05, b.Receipts); err != nil {
			return err
		}
		td.Add(td, b.Block.Difficulty())
		tdLE := make([]byte, 32)
		for i, v := range td.FillBytes(make([]byte, 32)) {
			tdLE[31-i] = v
		}
		entry(0x06, tdLE)
	}
	entry(0x07, make([]byte, 32)) // accumulator (not checked by readers here)

	indexAt := int64(buf.Len())
	index := make([]byte, 16+8*len(offsets))
	binary.LittleEndian.PutUint64(index, first)
	for i, off := range offsets {
		binary.LittleEndian.PutUint64(index[8+8*i:], uint64(off-indexAt))
	}
	binary.LittleEndian.PutUint64(index[8+8*len(offsets):], uint64(len(offsets)))
	entry(0x3266, index)
	return os.WriteFile(path, buf.Bytes(), 0o644)
}
