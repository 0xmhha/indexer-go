// Package metadata samples only explicitly selected, bounded existing metadata.
// Matching samples are not locks, leases, authentication or atomic snapshots.
package metadata

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/configfile"
)

var ErrUnsafe = errors.New("metadata: unsafe input")
var ErrChanged = errors.New("metadata: changed observation")

type sample struct {
	path    string
	info    os.FileInfo
	data    []byte
	missing bool
}
type Reader struct {
	ctx   context.Context
	files []sample
	dirs  []sample
}

func New(ctx context.Context) *Reader { return &Reader{ctx: ctx} }
func normalized(path string) (string, error) {
	p, e := filepath.Abs(path)
	if e != nil || len(p) > 4096 {
		return "", ErrUnsafe
	}
	if runtime.GOOS == "darwin" {
		for _, v := range []string{"/var/", "/tmp/"} {
			if strings.HasPrefix(p, v) {
				p = "/private" + p
				break
			}
		}
	}
	return p, nil
}

// Directory checks each ancestor without listing any directory entries.
func (r *Reader) Directory(path string) error {
	if e := r.ctx.Err(); e != nil {
		return e
	}
	p, e := normalized(path)
	if e != nil {
		return e
	}
	if len(r.dirs) >= 32 {
		return ErrUnsafe
	}
	i, e := directoryInfo(p)
	if e != nil {
		return e
	}
	r.dirs = append(r.dirs, sample{path: p, info: i})
	return nil
}
func directoryInfo(p string) (os.FileInfo, error) {
	current := string(filepath.Separator)
	for _, part := range strings.Split(strings.TrimPrefix(p, current), current) {
		current = filepath.Join(current, part)
		i, e := os.Lstat(current)
		if e != nil {
			return nil, e
		}
		if !i.IsDir() {
			return nil, ErrUnsafe
		}
	}
	i, e := os.Lstat(p)
	if e != nil {
		return nil, e
	}
	if !owned(i, false) || i.Mode().Perm()&0022 != 0 {
		return nil, ErrUnsafe
	}
	return i, nil
}
func (r *Reader) File(path string) ([]byte, error) {
	if e := r.ctx.Err(); e != nil {
		return nil, e
	}
	if len(r.files) >= 32 {
		return nil, ErrUnsafe
	}
	p, e := normalized(path)
	if e != nil {
		return nil, e
	}
	// A missing leaf is meaningful only after its existing safe parent is checked.
	if e = r.Directory(filepath.Dir(p)); e != nil {
		return nil, e
	}
	i, e := os.Lstat(p)
	if os.IsNotExist(e) {
		r.files = append(r.files, sample{path: p, missing: true})
		return nil, e
	}
	if e != nil {
		return nil, e
	}
	if !i.Mode().IsRegular() || !owned(i, true) || i.Mode().Perm()&0077 != 0 {
		return nil, ErrUnsafe
	}
	b, e := configfile.Read(p)
	if e != nil {
		return nil, ErrUnsafe
	}
	after, e := os.Lstat(p)
	if e != nil || !same(i, after) {
		return nil, ErrChanged
	}
	r.files = append(r.files, sample{path: p, info: i, data: b})
	return b, nil
}
func same(a, b os.FileInfo) bool {
	return a != nil && b != nil && os.SameFile(a, b) && a.Mode() == b.Mode() && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime())
}
func (r *Reader) Verify() error {
	if e := r.ctx.Err(); e != nil {
		return e
	}
	for _, s := range r.dirs {
		i, e := directoryInfo(s.path)
		if e != nil || !os.SameFile(s.info, i) || i.Mode() != s.info.Mode() {
			return ErrChanged
		}
	}
	for _, s := range r.files {
		i, e := os.Lstat(s.path)
		if s.missing {
			if os.IsNotExist(e) {
				continue
			}
			return ErrChanged
		}
		if e != nil || !same(s.info, i) || !owned(i, true) {
			return ErrChanged
		}
		b, e := configfile.Read(s.path)
		if e != nil || !bytes.Equal(s.data, b) {
			return ErrChanged
		}
	}
	return r.ctx.Err()
}

// Sealed decodes canonical production digest envelopes, never raw parser errors.
func Sealed(data []byte, target any) error {
	var envelope struct {
		Data   json.RawMessage
		Digest execwire.Hash
	}
	if configfile.JSON(data, &envelope) != nil || execwire.Hash(sha256.Sum256(envelope.Data)) != envelope.Digest || configfile.JSON(envelope.Data, target) != nil {
		return ErrUnsafe
	}
	canonical, e := json.Marshal(target)
	if e != nil || !bytes.Equal(canonical, envelope.Data) {
		return ErrUnsafe
	}
	outer, e := json.Marshal(envelope)
	if e != nil || !bytes.Equal(outer, data) {
		return ErrUnsafe
	}
	return nil
}
