// Package executionbackup authenticates explicitly supplied offline backup evidence.
// It never opens a database, starts services or grants maintenance authority.
package executionbackup

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/configfile"
	"github.com/0xmhha/indexer-go/execwire/retention"
)

const Domain = "indexer-go/postgres-backup-attestation/v1\n"
const MaxArtifactBytes = uint64(64 << 30)

type Trust struct {
	Version          uint16
	KeyID            string
	PublicKey        string
	Chain            execwire.Chain
	DatabaseIdentity string
}

type Statement struct {
	Version       uint16
	KeyID         string
	ArtifactBytes uint64
	Receipt       retention.IndexerBackup
}

type Envelope struct {
	Statement Statement
	Signature string
}

type Inputs struct{ Trust, Permit, Attestation, Artifact, Output string }

// SigningMessage defines the versioned domain and canonical typed JSON signed
// by an external provider. No signing keys are accepted by this adapter.
func SigningMessage(s Statement) ([]byte, error) {
	b, err := json.Marshal(s)
	return append([]byte(Domain), b...), err
}

func Verify(ctx context.Context, in Inputs) (retention.IndexerBackup, error) {
	var empty retention.IndexerBackup
	if ctx.Err() != nil {
		return empty, ctx.Err()
	}
	var trust Trust
	var permit retention.Permit
	var envelope Envelope
	for _, input := range []struct {
		path   string
		target any
	}{{in.Trust, &trust}, {in.Permit, &permit}, {in.Attestation, &envelope}} {
		if err := readJSON(ctx, input.path, input.target); err != nil {
			return empty, retention.ErrUnsafe
		}
	}
	pub, err := hex.DecodeString(trust.PublicKey)
	if err != nil || len(pub) != ed25519.PublicKeySize || trust.PublicKey != hex.EncodeToString(pub) {
		return empty, retention.ErrUnsafe
	}
	kid := sha256.Sum256(pub)
	s := envelope.Statement
	if trust.Version != 1 || trust.KeyID != hex.EncodeToString(kid[:]) || s.Version != 1 || s.KeyID != trust.KeyID ||
		trust.Chain != permit.Request.Chain || trust.DatabaseIdentity != s.Receipt.DatabaseIdentity ||
		!identity(trust.DatabaseIdentity) || !identity(s.Receipt.SnapshotIdentity) ||
		s.ArtifactBytes == 0 || s.ArtifactBytes > MaxArtifactBytes || s.Receipt.Validate(permit) != nil ||
		permit.Visible.Height > permit.Request.Head.Height {
		return empty, retention.ErrUnsafe
	}
	sig, err := hex.DecodeString(envelope.Signature)
	message, e := SigningMessage(s)
	if err != nil || e != nil || len(sig) != ed25519.SignatureSize || envelope.Signature != hex.EncodeToString(sig) ||
		!ed25519.Verify(ed25519.PublicKey(pub), message, sig) {
		return empty, retention.ErrUnsafe
	}
	f, err := openPrivate(in.Artifact)
	if err != nil {
		return empty, retention.ErrUnsafe
	}
	defer func() { _ = f.Close() }()
	before, err := f.Stat()
	if err != nil || before.Size() < 0 || uint64(before.Size()) != s.ArtifactBytes {
		return empty, retention.ErrUnsafe
	}
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(contextReader{ctx, f}, int64(s.ArtifactBytes)+1))
	after, e := f.Stat()
	if err != nil || e != nil || uint64(n) != s.ArtifactBytes || before.Size() != after.Size() ||
		!before.ModTime().Equal(after.ModTime()) || !privateInfo(after) ||
		!bytes.Equal(h.Sum(nil), s.Receipt.ArtifactDigest[:]) {
		return empty, retention.ErrUnsafe
	}
	if ctx.Err() != nil {
		return empty, ctx.Err()
	}
	return s.Receipt, nil
}

func identity(s string) bool {
	if s == "" || s == "HOLD" || len(s) > 256 {
		return false
	}
	for _, r := range s {
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}

func readJSON(ctx context.Context, path string, target any) error {
	f, err := openPrivate(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil || info.Size() > configfile.MaxBytes {
		return retention.ErrUnsafe
	}
	b, err := io.ReadAll(io.LimitReader(contextReader{ctx, f}, configfile.MaxBytes+1))
	if err != nil {
		return err
	}
	return configfile.JSON(b, target)
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(b []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(b)
}
