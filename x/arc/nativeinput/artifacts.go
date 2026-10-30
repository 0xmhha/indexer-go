package nativeinput

import (
	"crypto/sha256"
	"encoding/json"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/x/arc/nativestore"
	"io"
	"os"
)

// PrivateBytes reads an exact bounded owned artifact without following links.
func PrivateBytes(path string) ([]byte, error) {
	if err := nativestore.PrivateFile(path); err != nil {
		return nil, err
	}
	f, err := openArtifact(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	actual, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !os.SameFile(info, actual) || !info.Mode().IsRegular() || info.Size() > MaxMeta {
		return nil, ErrProof
	}
	if err = nativestore.PrivateFile(path); err != nil {
		return nil, err
	}
	p, err := io.ReadAll(io.LimitReader(f, MaxMeta+1))
	if err != nil {
		return nil, err
	}
	after, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if len(p) > MaxMeta || !os.SameFile(info, after) || nativestore.PrivateFile(path) != nil {
		return nil, ErrProof
	}
	return p, nil
}

// ReadArtifact verifies canonical sealed control bytes before decoding.
func ReadArtifact(path string, out any) error {
	p, err := PrivateBytes(path)
	if err != nil {
		return err
	}
	var envelope struct {
		Data   json.RawMessage
		Digest execwire.Hash
	}
	if Decode(p, &envelope) != nil || execwire.Hash(sha256.Sum256(envelope.Data)) != envelope.Digest {
		return ErrProof
	}
	return Decode(envelope.Data, out)
}
