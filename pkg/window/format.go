package window

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/0xmhha/indexer-go/pkg/chainfeed"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/ethereum/go-ethereum/common"
)

const (
	frameHeader = 84
	maxMeta     = 8192
	indexHeader = 48
	indexEntry  = 256
	frameMagic  = "IDXRAW01"
	indexMagic  = "IDXIND01"
)

// Order is part of the v1 disk format. Never reorder existing entries.
var datasetKinds = []model.DatasetKind{
	model.DatasetHeaders, model.DatasetBodies, model.DatasetTransactions, model.DatasetReceipts,
	model.DatasetLogs, model.DatasetAccounts, model.DatasetCode, model.DatasetStorage,
	model.DatasetTraces, model.DatasetBlobs, model.DatasetStateInputs,
}

type Reference struct {
	StoreID       common.Hash
	Block         model.FeedBlock
	Kind          model.DatasetKind
	Codec         string
	Segment       uint64
	Offset        int64
	FrameBytes    int64
	PayloadBytes  int64
	Digest        common.Hash
	FirstReceived int64 // Unix nanoseconds of the original durable receipt
}

type frameMeta struct {
	Version       int
	StoreID       common.Hash
	Block         model.FeedBlock
	Kind          model.DatasetKind
	Codec         string
	Digest        common.Hash
	FirstReceived int64
}

func makeFrame(id common.Hash, e chainfeed.Envelope, received int64) ([]byte, []byte, error) {
	meta := frameMeta{1, id, e.Block, e.Kind, e.Codec, sha256.Sum256(e.Payload), received}
	data, err := json.Marshal(meta)
	if err != nil {
		return nil, nil, err
	}
	if len(data) > maxMeta {
		return nil, nil, ErrCapacity
	}
	header := make([]byte, frameHeader)
	copy(header, frameMagic)
	binary.BigEndian.PutUint32(header[8:12], uint32(len(data)))
	binary.BigEndian.PutUint64(header[12:20], uint64(len(e.Payload)))
	h := sha256.New()
	h.Write(data)
	h.Write(e.Payload)
	copy(header[20:52], h.Sum(nil))
	headerDigest := sha256.Sum256(header[:52])
	copy(header[52:], headerDigest[:])
	return header, data, nil
}

// readFrame validates length before allocation. Recovery passes keep=false
// and hashes the payload through a fixed buffer instead of loading it in RAM.
func readFrame(f *os.File, offset, size int64, opts Options, id common.Hash, keep bool) (Reference, []byte, bool, error) {
	var ref Reference
	remaining := size - offset
	if remaining < frameHeader {
		return ref, nil, true, nil
	}
	header := make([]byte, frameHeader)
	if _, err := f.ReadAt(header, offset); err != nil {
		return ref, nil, false, err
	}
	headerDigest := sha256.Sum256(header[:52])
	if !bytes.Equal(headerDigest[:], header[52:84]) {
		return ref, nil, false, ErrCorrupt
	}
	metaLen := int64(binary.BigEndian.Uint32(header[8:12]))
	length := binary.BigEndian.Uint64(header[12:20])
	if string(header[:8]) != frameMagic || metaLen < 1 || metaLen > maxMeta || length == 0 || length > uint64(opts.MaxPayloadBytes) {
		return ref, nil, false, ErrCorrupt
	}
	total := int64(frameHeader) + metaLen + int64(length)
	if total > remaining {
		return ref, nil, true, nil
	}
	metadata := make([]byte, metaLen)
	if _, err := f.ReadAt(metadata, offset+frameHeader); err != nil {
		return ref, nil, false, err
	}
	var meta frameMeta
	if json.Unmarshal(metadata, &meta) != nil || meta.Version != 1 || meta.StoreID != id || meta.Block.Chain != opts.Chain ||
		meta.Block.Validate() != nil || !meta.Kind.Valid() || len(meta.Codec) == 0 || len(meta.Codec) > 128 || meta.FirstReceived <= 0 {
		return ref, nil, false, ErrCorrupt
	}
	whole, payloadHash := sha256.New(), sha256.New()
	whole.Write(metadata)
	reader := io.NewSectionReader(f, offset+frameHeader+metaLen, int64(length))
	var payload []byte
	var err error
	if keep {
		payload = make([]byte, int(length))
		_, err = io.ReadFull(reader, payload)
		whole.Write(payload)
		payloadHash.Write(payload)
	} else {
		_, err = io.CopyBuffer(io.MultiWriter(whole, payloadHash), reader, make([]byte, 32<<10))
	}
	if err != nil {
		return ref, nil, false, err
	}
	if !bytes.Equal(whole.Sum(nil), header[20:52]) || !bytes.Equal(payloadHash.Sum(nil), meta.Digest[:]) {
		return ref, nil, false, ErrCorrupt
	}
	ref = Reference{StoreID: id, Block: meta.Block, Kind: meta.Kind, Codec: meta.Codec, Offset: offset,
		FrameBytes: total, PayloadBytes: int64(length), Digest: meta.Digest, FirstReceived: meta.FirstReceived}
	return ref, payload, false, nil
}

func kindByte(kind model.DatasetKind) byte {
	for i, k := range datasetKinds {
		if kind == k {
			return byte(i + 1)
		}
	}
	return 0
}

func compareRef(a, b Reference) int {
	if a.Block.Number < b.Block.Number {
		return -1
	}
	if a.Block.Number > b.Block.Number {
		return 1
	}
	if c := bytes.Compare(a.Block.Hash[:], b.Block.Hash[:]); c != 0 {
		return c
	}
	return int(kindByte(a.Kind)) - int(kindByte(b.Kind))
}

func encodeRef(dst []byte, r Reference) {
	binary.BigEndian.PutUint64(dst[:8], r.Block.Number)
	copy(dst[8:40], r.Block.Hash[:])
	dst[40] = kindByte(r.Kind)
	for i, v := range []int64{r.Offset, r.FrameBytes, r.PayloadBytes, r.FirstReceived} {
		binary.BigEndian.PutUint64(dst[41+i*8:49+i*8], uint64(v))
	}
	copy(dst[73:105], r.Digest[:])
	dst[105] = byte(len(r.Codec))
	copy(dst[106:234], r.Codec)
}

func decodeRef(src []byte, opts Options, storeID common.Hash, seg uint64) (Reference, error) {
	if len(src) != indexEntry || src[40] < 1 || int(src[40]) > len(datasetKinds) || src[105] < 1 || src[105] > 128 {
		return Reference{}, ErrCorrupt
	}
	r := Reference{StoreID: storeID, Block: model.FeedBlock{Chain: opts.Chain, Number: binary.BigEndian.Uint64(src[:8]), Hash: common.BytesToHash(src[8:40])},
		Kind: datasetKinds[src[40]-1], Segment: seg, Codec: string(src[106 : 106+int(src[105])]), Digest: common.BytesToHash(src[73:105])}
	r.Offset = int64(binary.BigEndian.Uint64(src[41:49]))
	r.FrameBytes = int64(binary.BigEndian.Uint64(src[49:57]))
	r.PayloadBytes = int64(binary.BigEndian.Uint64(src[57:65]))
	r.FirstReceived = int64(binary.BigEndian.Uint64(src[65:73]))
	if r.Block.Validate() != nil || r.Offset < 0 || r.PayloadBytes <= 0 || r.PayloadBytes > opts.MaxPayloadBytes ||
		r.FrameBytes < frameHeader+r.PayloadBytes || r.FrameBytes > frameHeader+maxMeta+r.PayloadBytes || r.FirstReceived <= 0 {
		return Reference{}, ErrCorrupt
	}
	return r, nil
}

func makeIndex(refs []Reference) ([]byte, common.Hash) {
	refs = append([]Reference(nil), refs...)
	sort.Slice(refs, func(i, j int) bool { return compareRef(refs[i], refs[j]) < 0 })
	data := make([]byte, indexHeader+len(refs)*indexEntry)
	copy(data, indexMagic)
	binary.BigEndian.PutUint64(data[8:16], uint64(len(refs)))
	for i, r := range refs {
		encodeRef(data[indexHeader+i*indexEntry:], r)
	}
	digest := sha256.Sum256(data[indexHeader:])
	copy(data[16:indexHeader], digest[:])
	return data, digest
}

type segment struct {
	ID          uint64
	Count       int
	DataBytes   int64
	IndexDigest common.Hash
	MinHeight   uint64
	MaxHeight   uint64
	Newest      int64
	leases      int
}

func summarize(id uint64, size int64, refs []Reference, digest common.Hash) segment {
	s := segment{ID: id, Count: len(refs), DataBytes: size, IndexDigest: digest, MinHeight: ^uint64(0)}
	for _, r := range refs {
		if r.Block.Number < s.MinHeight {
			s.MinHeight = r.Block.Number
		}
		if r.Block.Number > s.MaxHeight {
			s.MaxHeight = r.Block.Number
		}
		if r.FirstReceived > s.Newest {
			s.Newest = r.FirstReceived
		}
	}
	return s
}

func (s *Store) indexRefs(seg *segment) ([]Reference, error) {
	data, err := readSmall(s.root, segmentName(seg.ID, "idx"), int64(indexHeader+s.opts.MaxSegmentRecords*indexEntry))
	if err != nil {
		return nil, err
	}
	if len(data) != indexHeader+seg.Count*indexEntry || string(data[:8]) != indexMagic || binary.BigEndian.Uint64(data[8:16]) != uint64(seg.Count) {
		return nil, ErrCorrupt
	}
	digest := sha256.Sum256(data[indexHeader:])
	if digest != seg.IndexDigest || !bytes.Equal(data[16:indexHeader], digest[:]) {
		return nil, ErrCorrupt
	}
	refs := make([]Reference, 0, seg.Count)
	for i := 0; i < seg.Count; i++ {
		r, err := decodeRef(data[indexHeader+i*indexEntry:indexHeader+(i+1)*indexEntry], s.opts, s.id, seg.ID)
		if err != nil || r.Offset > seg.DataBytes-r.FrameBytes {
			return nil, ErrCorrupt
		}
		if i > 0 && compareRef(refs[i-1], r) >= 0 {
			return nil, fmt.Errorf("%w: duplicate or unordered index", ErrCorrupt)
		}
		refs = append(refs, r)
	}
	return refs, nil
}
