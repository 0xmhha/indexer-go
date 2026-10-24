package spool

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/0xmhha/indexer-go/execwire"
)

type manifest struct {
	Version   uint16
	Chain     execwire.Chain
	Anchor    execwire.Ref
	Base      *execwire.Ref `json:",omitempty"`
	Committed execwire.Ref
	Digest    execwire.Hash
	History   []Entry
}
type ackReceipt struct {
	Version uint16
	Chain   execwire.Chain
	Entry   Entry
}
type sealedJSON struct {
	Data   json.RawMessage
	Digest execwire.Hash
}
type recordHeader struct {
	Version uint16
	Chain   execwire.Chain
	Ref     execwire.Ref
	Parent  execwire.Hash
	Digest  execwire.Hash
}

func validateAnchor(o Options) error {
	return execwire.Encode(io.Discard, execwire.Frame{Kind: execwire.Hello, Header: execwire.Header{Version: execwire.Version, Chain: o.Chain, Ref: o.Anchor}})
}
func validateACK(f execwire.Frame) error { return execwire.Encode(io.Discard, f) }
func validateReceipt(r ackReceipt) error {
	if r.Entry.Size == 0 || r.Entry.Size > execwire.MaxBlockBytes+execwire.MaxHeaderBytes+12 || r.Entry.Parent == (execwire.Hash{}) {
		return ErrCorrupt
	}
	return validateACK(execwire.Frame{Kind: execwire.Committed, Header: execwire.Header{Version: execwire.Version, Chain: r.Chain, Ref: r.Entry.Ref, Digest: r.Entry.Digest}})
}
func recordName(r execwire.Ref) string {
	return fmt.Sprintf("%016x-%016x-%s.result", r.Generation, r.Height, hex.EncodeToString(r.Hash[:]))
}
func ackName(r execwire.Ref) string { return strings.TrimSuffix(recordName(r), ".result") + ".ack" }

func defaultOps() fileOps {
	return fileOps{syncFile: func(f *os.File) error { return f.Sync() }, syncDir: func(path string) error {
		d, err := os.Open(path)
		if err != nil {
			return err
		}
		defer func() { _ = d.Close() }()
		return d.Sync()
	}, rename: os.Rename, remove: os.Remove}
}

func sealJSON(v any) ([]byte, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return json.Marshal(sealedJSON{Data: data, Digest: execwire.Hash(sha256.Sum256(data))})
}
func readSealedJSON(path string, out any) error {
	if err := regularPrivate(path); err != nil {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	p, err := io.ReadAll(io.LimitReader(f, metadataReserve+1))
	if err != nil {
		return err
	}
	if len(p) > metadataReserve {
		return ErrCorrupt
	}
	var sealed sealedJSON
	if err := strictJSON(p, &sealed); err != nil {
		return err
	}
	if execwire.Hash(sha256.Sum256(sealed.Data)) != sealed.Digest {
		return ErrCorrupt
	}
	return strictJSON(sealed.Data, out)
}
func strictJSON(p []byte, out any) error {
	decoder := json.NewDecoder(bytes.NewReader(p))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return fmt.Errorf("%w: metadata JSON", ErrCorrupt)
	}
	canonical, err := json.Marshal(out)
	if err != nil || !bytes.Equal(canonical, p) {
		return ErrCorrupt
	}
	return nil
}
func readManifest(path string) (manifest, error) {
	var m manifest
	if err := readSealedJSON(path, &m); err != nil {
		return m, err
	}
	base := m.Anchor
	if m.Version == 2 && m.Base != nil {
		base = *m.Base
	} else if m.Version != 1 || m.Base != nil {
		return manifest{}, ErrCorrupt
	}
	if len(m.History) > 128 || m.Committed.Generation != base.Generation || m.Committed.Height < base.Height || base.Height < m.Anchor.Height || base.Generation < m.Anchor.Generation {
		return manifest{}, ErrCorrupt
	}
	if base.Height == m.Anchor.Height && base.Hash != m.Anchor.Hash {
		return manifest{}, ErrCorrupt
	}
	if err := validateAnchor(Options{Chain: m.Chain, Anchor: m.Anchor}); err != nil {
		return manifest{}, ErrCorrupt
	}
	if err := validateAnchor(Options{Chain: m.Chain, Anchor: base}); err != nil {
		return manifest{}, ErrCorrupt
	}
	if len(m.History) == 0 {
		if m.Committed != base || m.Digest != (execwire.Hash{}) {
			return manifest{}, ErrCorrupt
		}
	} else {
		first := m.History[0]
		if len(m.History) < 128 && (first.Ref.Height != base.Height+1 || first.Parent != base.Hash) {
			return manifest{}, ErrCorrupt
		}
		for i, e := range m.History {
			if e.Ref.Generation != base.Generation || e.Ref.Height <= base.Height {
				return manifest{}, ErrCorrupt
			}
			if err := validateReceipt(ackReceipt{Version: 1, Chain: m.Chain, Entry: e}); err != nil {
				return manifest{}, ErrCorrupt
			}
			if i > 0 {
				prev := m.History[i-1]
				if e.Ref.Generation != prev.Ref.Generation || e.Ref.Height != prev.Ref.Height+1 || e.Parent != prev.Ref.Hash {
					return manifest{}, ErrCorrupt
				}
			}
		}
		last := m.History[len(m.History)-1]
		if last.Ref != m.Committed || last.Digest != m.Digest {
			return manifest{}, ErrCorrupt
		}
	}
	return m, nil
}
func (s *Store) saveState(ctx context.Context) error {
	p, err := sealJSON(s.state)
	if err != nil {
		return err
	}
	if len(p) > metadataReserve {
		return ErrFull
	}
	return s.atomic(ctx, "state", stateName, p)
}

func (s *Store) atomic(ctx context.Context, kind, name string, parts ...[]byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f, err := os.CreateTemp(s.path, ".tmp-"+kind+"-")
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	for _, p := range parts {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := f.Write(p)
		if err != nil {
			return err
		}
		if n != len(p) {
			return io.ErrShortWrite
		}
	}
	if err := s.ops.syncFile(f); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.ops.rename(f.Name(), filepath.Join(s.path, name)); err != nil {
		return err
	}
	if err := s.ops.syncDir(s.path); err != nil {
		s.recovery = true
		return err
	}
	return nil
}

func recordBytes(chain execwire.Chain, e Entry, p []byte) ([]byte, error) {
	h, err := json.Marshal(recordHeader{Version: 1, Chain: chain, Ref: e.Ref, Parent: e.Parent, Digest: e.Digest})
	if err != nil {
		return nil, err
	}
	if len(h) > execwire.MaxHeaderBytes || len(p) > execwire.MaxBlockBytes {
		return nil, ErrFull
	}
	out := make([]byte, 12, 12+len(h))
	copy(out, "EXS1")
	binary.BigEndian.PutUint32(out[4:8], uint32(len(h)))
	binary.BigEndian.PutUint32(out[8:12], uint32(len(p)))
	return append(out, h...), nil
}
func headerFromFile(f *os.File, chain execwire.Chain) (Entry, uint32, error) {
	var prefix [12]byte
	if _, err := io.ReadFull(f, prefix[:]); err != nil {
		return Entry{}, 0, fmt.Errorf("%w: record prefix", ErrCorrupt)
	}
	hn, pn := binary.BigEndian.Uint32(prefix[4:8]), binary.BigEndian.Uint32(prefix[8:12])
	if !bytes.Equal(prefix[:4], []byte("EXS1")) || hn == 0 || hn > execwire.MaxHeaderBytes || pn == 0 || pn > execwire.MaxBlockBytes {
		return Entry{}, 0, ErrCorrupt
	}
	info, err := f.Stat()
	if err != nil {
		return Entry{}, 0, err
	}
	size := uint64(12) + uint64(hn) + uint64(pn)
	if uint64(info.Size()) != size {
		return Entry{}, 0, ErrCorrupt
	}
	data := make([]byte, int(hn))
	if _, err := io.ReadFull(f, data); err != nil {
		return Entry{}, 0, ErrCorrupt
	}
	var h recordHeader
	if err := strictJSON(data, &h); err != nil {
		return Entry{}, 0, err
	}
	if h.Version != 1 || h.Chain != chain || h.Parent == (execwire.Hash{}) {
		return Entry{}, 0, ErrConflict
	}
	e := Entry{Ref: h.Ref, Parent: h.Parent, Digest: h.Digest, Size: size}
	if err := validateReceipt(ackReceipt{Version: 1, Chain: chain, Entry: e}); err != nil {
		return Entry{}, 0, ErrCorrupt
	}
	return e, pn, nil
}
func readEntry(path string, chain execwire.Chain) (Entry, error) {
	if err := regularPrivate(path); err != nil {
		return Entry{}, err
	}
	f, err := os.Open(path)
	if err != nil {
		return Entry{}, err
	}
	defer func() { _ = f.Close() }()
	e, _, err := headerFromFile(f, chain)
	return e, err
}
func readResult(ctx context.Context, path string, chain execwire.Chain, expected Entry) (execwire.Result, error) {
	if err := regularPrivate(path); err != nil {
		return execwire.Result{}, err
	}
	f, err := os.Open(path)
	if err != nil {
		return execwire.Result{}, err
	}
	defer func() { _ = f.Close() }()
	e, n, err := headerFromFile(f, chain)
	if err != nil {
		return execwire.Result{}, err
	}
	if e != expected {
		return execwire.Result{}, ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return execwire.Result{}, err
	}
	p := make([]byte, int(n))
	if _, err := io.ReadFull(f, p); err != nil {
		return execwire.Result{}, ErrCorrupt
	}
	if execwire.Hash(sha256.Sum256(p)) != e.Digest {
		return execwire.Result{}, ErrCorrupt
	}
	r, err := execwire.UnmarshalResult(p)
	if err != nil {
		return execwire.Result{}, fmt.Errorf("%w: result encoding", ErrCorrupt)
	}
	if r.Ref != e.Ref || r.Parent != e.Parent {
		return execwire.Result{}, ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return execwire.Result{}, err
	}
	return r, nil
}
func regularPrivate(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || !owned(info) {
		return fmt.Errorf("%w: file ownership or permissions", ErrConflict)
	}
	return nil
}
func (s *Store) usage() (uint64, int, int, error) {
	names, err := os.ReadDir(s.path)
	if err != nil {
		return 0, 0, 0, err
	}
	var bytes uint64
	records, quarantine := 0, 0
	for _, name := range names {
		path := filepath.Join(s.path, name.Name())
		if err := regularPrivate(path); err != nil {
			return 0, 0, 0, err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return 0, 0, 0, err
		}
		bytes += charged(info)
		if name.Name() != stateName && name.Name() != lockName && name.Name() != transitionName && !strings.HasSuffix(name.Name(), ".ack") {
			records++
		}
		if strings.HasPrefix(name.Name(), ".quarantine-") {
			quarantine++
		}
	}
	return bytes, records, quarantine, nil
}
