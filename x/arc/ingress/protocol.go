// Package arcingress owns Arc native input acquisition inside the existing App.
package arcingress

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/x/arc/nativeinput"
	"io"
	"math"
)

const ArcRevision = nativeinput.ArcRevision
const RethRevision = nativeinput.RethRevision
const maxMeta = 16 << 10
const maxPart = 10 << 20

type nativeRef = nativeinput.Ref
type nativeIdentity = nativeinput.Identity
type nativeHead = nativeinput.Head
type nativeReady = nativeinput.Ready
type importMeta = nativeinput.Meta
type importReceipt = nativeinput.Receipt

var ErrNative = errors.New("arc ingress: invalid, conflicting or unavailable native input; preserved for explicit recovery")

func positive(n uint64) bool { return n > 0 && n <= math.MaxInt64 }
func encodeInput(m importMeta, block, receipts []byte) ([]byte, execwire.Hash, error) {
	if m.Version != 1 || m.ChainID != 5042002 || m.Genesis == (execwire.Hash{}) || m.ArcRevision != ArcRevision || m.RethRevision != RethRevision || !positive(m.RequestID) || !positive(m.ImportEpoch) || !positive(m.Height) || m.Hash == (execwire.Hash{}) || m.Parent == (execwire.Hash{}) || m.ParentRoot == (execwire.Hash{}) || len(block) == 0 || len(block) > maxPart || len(receipts) == 0 || len(receipts) > maxPart {
		return nil, execwire.Hash{}, ErrNative
	}
	meta, err := json.Marshal(m)
	if err != nil || len(meta) > maxMeta {
		return nil, execwire.Hash{}, ErrNative
	}
	frame := make([]byte, 16, 16+len(meta)+len(block)+len(receipts))
	copy(frame, "AIN1")
	for i, n := range []int{len(meta), len(block), len(receipts)} {
		binary.BigEndian.PutUint32(frame[4+i*4:], uint32(n))
	}
	frame = append(frame, meta...)
	frame = append(frame, block...)
	frame = append(frame, receipts...)
	return frame, execwire.Hash(sha256.Sum256(frame)), nil
}

// Exact re-encoding rejects duplicate/unknown fields, alternate number/hex
// spellings, reordered fields, whitespace and trailing values in one bounded frame.
func strictJSON(p []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(p))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return ErrNative
	}
	canonical, err := json.Marshal(out)
	if err != nil || !bytes.Equal(canonical, p) {
		return ErrNative
	}
	return nil
}
func readReply(r io.Reader, magic string, id uint64, out any) error {
	var header [16]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return err
	}
	n := binary.BigEndian.Uint32(header[12:])
	if string(header[:4]) != magic || binary.BigEndian.Uint64(header[4:12]) != id || n == 0 || n > maxMeta {
		return ErrNative
	}
	p := make([]byte, n)
	if _, err := io.ReadFull(r, p); err != nil {
		return err
	}
	return strictJSON(p, out)
}
