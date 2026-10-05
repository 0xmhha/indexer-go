// Package era reads era1 history archives, the block export format of
// go-ethereum based clients (`export-history`). The files hold raw consensus
// encodings, so building the chain-neutral model is left to the chain
// profile (chains.BinaryProfile).
package era

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/golang/snappy"
)

// e2store entry types used by era1.
const (
	typeVersion            uint16 = 0x3265
	typeCompressedHeader   uint16 = 0x03
	typeCompressedBody     uint16 = 0x04
	typeCompressedReceipts uint16 = 0x05
	typeTotalDifficulty    uint16 = 0x06
	typeAccumulator        uint16 = 0x07
	typeBlockIndex         uint16 = 0x3266
)

// headerSize is the size of an e2store entry header: type (u16 LE), data
// length (u32 LE) and two reserved zero bytes.
const headerSize = 8

// ErrFormat reports a file that is not a valid era1 archive.
var ErrFormat = errors.New("era: invalid era1 file")

// RawBlock is one block of an era1 file: the RLP of its header, body and
// receipts (consensus encoding), decompressed.
type RawBlock struct {
	Number   uint64
	Header   []byte
	Body     []byte
	Receipts []byte
}

// File is an open era1 file. Its methods are safe for concurrent use.
type File struct {
	f       *os.File
	start   uint64  // first block number
	offsets []int64 // absolute file offset of each block's header entry
}

// Open opens an era1 file and reads its block index.
func Open(path string) (*File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	e, err := open(f)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return e, nil
}

func open(f *os.File) (*File, error) {
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := st.Size()

	// The file starts with a version entry.
	typ, length, err := readHeader(f, 0)
	if err != nil {
		return nil, err
	}
	if typ != typeVersion || length != 0 {
		return nil, fmt.Errorf("%w: missing version entry", ErrFormat)
	}

	// The block index is the last entry: start (u64), one offset (i64) per
	// block relative to the index entry, count (u64).
	if size < headerSize+24 {
		return nil, fmt.Errorf("%w: too small", ErrFormat)
	}
	var b [8]byte
	if _, err := f.ReadAt(b[:], size-8); err != nil {
		return nil, err
	}
	count := binary.LittleEndian.Uint64(b[:])
	if count == 0 || count > uint64(size)/8 {
		return nil, fmt.Errorf("%w: block count %d", ErrFormat, count)
	}
	indexAt := size - headerSize - 16 - int64(count)*8
	if indexAt < headerSize {
		return nil, fmt.Errorf("%w: block index out of range", ErrFormat)
	}
	typ, length, err = readHeader(f, indexAt)
	if err != nil {
		return nil, err
	}
	if typ != typeBlockIndex || int64(length) != 16+int64(count)*8 {
		return nil, fmt.Errorf("%w: bad block index entry", ErrFormat)
	}
	index := make([]byte, length)
	if _, err := f.ReadAt(index, indexAt+headerSize); err != nil {
		return nil, err
	}
	e := &File{f: f, start: binary.LittleEndian.Uint64(index[:8]), offsets: make([]int64, count)}
	for i := range e.offsets {
		rel := int64(binary.LittleEndian.Uint64(index[8+i*8:]))
		abs := indexAt + rel
		if abs < headerSize || abs >= indexAt {
			return nil, fmt.Errorf("%w: block %d offset out of range", ErrFormat, e.start+uint64(i))
		}
		e.offsets[i] = abs
	}
	return e, nil
}

// Start returns the first block number in the file.
func (e *File) Start() uint64 { return e.start }

// Count returns the number of blocks in the file.
func (e *File) Count() uint64 { return uint64(len(e.offsets)) }

// Contains reports whether block n is in the file.
func (e *File) Contains(n uint64) bool { return n >= e.start && n-e.start < e.Count() }

// Close closes the file.
func (e *File) Close() error { return e.f.Close() }

// Block reads block n. Entries are header, body, receipts and total
// difficulty in this order; unknown entry types in between are skipped.
func (e *File) Block(n uint64) (*RawBlock, error) {
	if !e.Contains(n) {
		return nil, fmt.Errorf("era: block %d not in file [%d, %d]", n, e.start, e.start+e.Count()-1)
	}
	rb := &RawBlock{Number: n}
	off := e.offsets[n-e.start]
	for rb.Header == nil || rb.Body == nil || rb.Receipts == nil {
		typ, length, err := readHeader(e.f, off)
		if err != nil {
			return nil, fmt.Errorf("era: block %d: %w", n, err)
		}
		var dst *[]byte
		switch typ {
		case typeCompressedHeader:
			dst = &rb.Header
		case typeCompressedBody:
			dst = &rb.Body
		case typeCompressedReceipts:
			dst = &rb.Receipts
		case typeTotalDifficulty:
			// Only proof-of-work chains need it; the next block starts here.
			return nil, fmt.Errorf("%w: block %d is incomplete", ErrFormat, n)
		case typeAccumulator, typeBlockIndex, typeVersion:
			return nil, fmt.Errorf("%w: block %d is incomplete", ErrFormat, n)
		}
		if dst != nil {
			if *dst != nil {
				return nil, fmt.Errorf("%w: block %d has a duplicate entry %#x", ErrFormat, n, typ)
			}
			data, err := e.decompress(off+headerSize, length)
			if err != nil {
				return nil, fmt.Errorf("era: block %d entry %#x: %w", n, typ, err)
			}
			*dst = data
		}
		off += headerSize + int64(length)
	}
	return rb, nil
}

// decompress reads a snappy framed entry body.
func (e *File) decompress(off int64, length uint32) ([]byte, error) {
	r := snappy.NewReader(io.NewSectionReader(e.f, off, int64(length)))
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func readHeader(r io.ReaderAt, off int64) (typ uint16, length uint32, err error) {
	var h [headerSize]byte
	if _, err := r.ReadAt(h[:], off); err != nil {
		if errors.Is(err, io.EOF) {
			err = fmt.Errorf("%w: truncated entry at %d", ErrFormat, off)
		}
		return 0, 0, err
	}
	if h[6] != 0 || h[7] != 0 {
		return 0, 0, fmt.Errorf("%w: reserved bytes set at %d", ErrFormat, off)
	}
	return binary.LittleEndian.Uint16(h[0:2]), binary.LittleEndian.Uint32(h[2:6]), nil
}
