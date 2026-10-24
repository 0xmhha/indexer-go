package retention

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
)

// ReadArtifact accepts only one bounded, strict JSON document from a regular
// file. Artifact validity/authority is checked separately by the typed caller.
func ReadArtifact(path string, v any) error {
	info, e := os.Lstat(path)
	if e != nil {
		return e
	}
	if !info.Mode().IsRegular() || info.Size() > MaxArtifact {
		return ErrUnsafe
	}
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	defer func() { _ = f.Close() }()
	opened, e := f.Stat()
	if e != nil {
		return e
	}
	if !os.SameFile(info, opened) {
		return ErrUnsafe
	}
	limited := &io.LimitedReader{R: f, N: MaxArtifact + 1}
	decoder := json.NewDecoder(limited)
	decoder.DisallowUnknownFields()
	if e = decoder.Decode(v); e != nil {
		return e
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF || limited.N == 0 {
		return ErrUnsafe
	}
	return nil
}

// WriteArtifact never overwrites prior evidence. A failed write leaves a
// private incomplete file which ReadArtifact/typed validation must reject.
func WriteArtifact(path string, v any) error {
	data, e := json.Marshal(v)
	if e != nil {
		return e
	}
	data = append(data, '\n')
	if len(data) > MaxArtifact {
		return ErrUnsafe
	}
	dir := filepath.Dir(path)
	info, e := os.Lstat(dir)
	if e != nil {
		return e
	}
	if !info.IsDir() || info.Mode().Perm() != 0700 {
		return ErrUnsafe
	}
	f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	_, writeErr := f.Write(data)
	e = errors.Join(writeErr, f.Sync(), f.Close())
	if e != nil {
		return e
	}
	d, e := os.Open(dir)
	if e != nil {
		return e
	}
	return errors.Join(d.Sync(), d.Close())
}
