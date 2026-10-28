package executionbackup

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/retention"
)

type fixture struct {
	in       Inputs
	trust    Trust
	p        retention.Permit
	envelope Envelope
	key      ed25519.PrivateKey
	data     []byte
}

func save(t *testing.T, path string, v any) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
}

func setup(t *testing.T) fixture {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	kid := sha256.Sum256(pub)
	f := fixture{key: key, data: []byte("owned fake snapshot plus WAL; not a real PostgreSQL backup")}
	f.in = Inputs{Trust: filepath.Join(dir, "trust.json"), Permit: filepath.Join(dir, "permit.json"), Attestation: filepath.Join(dir, "attestation.json"), Artifact: filepath.Join(dir, "database.backup")}
	c := execwire.Chain{ID: "1", Genesis: execwire.Hash{1}}
	r := retention.Request{Version: 1, Chain: c, Anchor: execwire.Ref{Generation: 1, Hash: c.Genesis}, ACK: execwire.Ref{Generation: 2, Height: 90000, Hash: execwire.Hash{2}}, Tail: execwire.Ref{Generation: 2, Height: 90002, Hash: execwire.Hash{3}}, Head: execwire.Ref{Height: 90003, Hash: execwire.Hash{4}}, HeadRoot: execwire.Hash{5}, SourceDigest: execwire.Hash{6}, KeepBlocks: 86400, Through: 3600}
	f.p = retention.Permit{Version: 1, Coverage: "full", BaselineDigest: execwire.Hash{7}, Token: execwire.Hash{8}, Request: r, Visible: execwire.Ref{Generation: 2, Height: 90001, Hash: execwire.Hash{9}}, Root: execwire.Hash{10}}
	pd, err := retention.Digest(f.p)
	if err != nil {
		t.Fatal(err)
	}
	f.trust = Trust{Version: 1, KeyID: hex.EncodeToString(kid[:]), PublicKey: hex.EncodeToString(pub), Chain: c, DatabaseIdentity: "owned/schema"}
	f.envelope.Statement = Statement{Version: 1, KeyID: f.trust.KeyID, ArtifactBytes: uint64(len(f.data)), Receipt: retention.IndexerBackup{Version: 1, PermitDigest: pd, ArtifactDigest: execwire.Hash(sha256.Sum256(f.data)), PublicationDigest: execwire.Hash{11}, DatabaseIdentity: f.trust.DatabaseIdentity, SnapshotIdentity: "owned-snapshot", WALBoundary: "0/ABC"}}
	f.sign(t)
	save(t, f.in.Trust, f.trust)
	save(t, f.in.Permit, f.p)
	if err = os.WriteFile(f.in.Artifact, f.data, 0600); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *fixture) sign(t *testing.T) {
	t.Helper()
	b, err := SigningMessage(f.envelope.Statement)
	if err != nil {
		t.Fatal(err)
	}
	f.envelope.Signature = hex.EncodeToString(ed25519.Sign(f.key, b))
	save(t, f.in.Attestation, f.envelope)
}

func TestBackupAuthenticatesBoundReceiptAndPreservesInputs(t *testing.T) {
	f := setup(t)
	before := map[string][]byte{}
	identities := map[string]os.FileInfo{}
	for _, path := range []string{f.in.Trust, f.in.Permit, f.in.Attestation, f.in.Artifact} {
		var err error
		before[path], err = os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		identities[path], err = os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
	}
	got, err := Verify(t.Context(), f.in)
	if err != nil || got != f.envelope.Statement.Receipt {
		t.Fatalf("valid authenticated receipt refused: %v", err)
	}
	for path, want := range before {
		b, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(b, want) {
			t.Fatal("input bytes changed")
		}
		info, err := os.Stat(path)
		if err != nil || !os.SameFile(info, identities[path]) {
			t.Fatal("input replaced")
		}
	}
	entries, err := os.ReadDir(filepath.Dir(f.in.Trust))
	if err != nil || len(entries) != 4 {
		t.Fatal("verification created state")
	}
}

func TestBackupRejectsForgedReplayAndInvalidSignedClaims(t *testing.T) {
	cases := map[string]func(*fixture){
		"tamper": func(f *fixture) {
			f.envelope.Statement.Receipt.WALBoundary = "0/DEF"
			save(t, f.in.Attestation, f.envelope)
		},
		"wrong-key": func(f *fixture) { _, k, _ := ed25519.GenerateKey(rand.Reader); f.key = k; f.sign(t) },
		"domain": func(f *fixture) {
			b, _ := json.Marshal(f.envelope.Statement)
			f.envelope.Signature = hex.EncodeToString(ed25519.Sign(f.key, b))
			save(t, f.in.Attestation, f.envelope)
		},
		"permit-replay":    func(f *fixture) { f.p.Token[0]++; save(t, f.in.Permit, f.p) },
		"wrong-chain":      func(f *fixture) { f.trust.Chain.ID = "2"; save(t, f.in.Trust, f.trust) },
		"wrong-database":   func(f *fixture) { f.trust.DatabaseIdentity = "foreign/schema"; save(t, f.in.Trust, f.trust) },
		"key-id":           func(f *fixture) { f.trust.KeyID = strings.Repeat("0", 64); save(t, f.in.Trust, f.trust) },
		"bad-version":      func(f *fixture) { f.envelope.Statement.Version = 2; f.sign(t) },
		"bad-wal":          func(f *fixture) { f.envelope.Statement.Receipt.WALBoundary = "0/1; SELECT"; f.sign(t) },
		"partial-baseline": func(f *fixture) { f.p.Coverage = "selected"; save(t, f.in.Permit, f.p) },
		"zero-size":        func(f *fixture) { f.envelope.Statement.ArtifactBytes = 0; f.sign(t) },
		"size-limit":       func(f *fixture) { f.envelope.Statement.ArtifactBytes = MaxArtifactBytes + 1; f.sign(t) },
		"size-mismatch":    func(f *fixture) { f.envelope.Statement.ArtifactBytes++; f.sign(t) },
		"digest": func(f *fixture) {
			f.data[0] ^= 1
			if err := os.WriteFile(f.in.Artifact, f.data, 0600); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			f := setup(t)
			mutate(&f)
			if _, err := Verify(t.Context(), f.in); err == nil {
				t.Fatal("unsafe evidence authenticated")
			}
		})
	}
}

func TestBackupStrictPrivateFiles(t *testing.T) {
	for _, kind := range []string{"unknown", "duplicate", "null", "trailing", "oversize", "public", "hardlink", "symlink", "parent-link", "directory", "missing"} {
		t.Run(kind, func(t *testing.T) {
			f := setup(t)
			b, err := os.ReadFile(f.in.Attestation)
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "unknown":
				b = append([]byte(`{"extra":1,`), b[1:]...)
			case "duplicate":
				b = append([]byte(`{"Signature":"",`), b[1:]...)
			case "null":
				b = []byte(`{"Statement":null,"Signature":""}`)
			case "trailing":
				b = append(b, []byte(` {}`)...)
			case "oversize":
				b = bytes.Repeat([]byte(" "), (1<<20)+1)
			case "public":
				if err = os.Chmod(f.in.Attestation, 0644); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				if err = os.Link(f.in.Artifact, f.in.Artifact+".link"); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				p := f.in.Artifact + ".link"
				if err = os.Symlink(f.in.Artifact, p); err != nil {
					t.Fatal(err)
				}
				f.in.Artifact = p
			case "parent-link":
				p := filepath.Join(filepath.Dir(f.in.Artifact), "link")
				if err = os.Symlink(filepath.Dir(f.in.Artifact), p); err != nil {
					t.Fatal(err)
				}
				f.in.Artifact = filepath.Join(p, "database.backup")
			case "directory":
				f.in.Artifact = filepath.Dir(f.in.Artifact)
			case "missing":
				f.in.Artifact += ".absent"
			}
			if kind == "unknown" || kind == "duplicate" || kind == "null" || kind == "trailing" || kind == "oversize" {
				if err = os.WriteFile(f.in.Attestation, b, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err = Verify(t.Context(), f.in); err == nil {
				t.Fatal("unsafe input accepted")
			}
		})
	}
}

func TestBackupCommandStatusRedactionCancellationAndNoOverwrite(t *testing.T) {
	f := setup(t)
	args := []string{"-trust", f.in.Trust, "-permit", f.in.Permit, "-attestation", f.in.Attestation, "-artifact", f.in.Artifact}
	var out bytes.Buffer
	if code := Run(t.Context(), args, &out); code != 0 || !strings.Contains(out.String(), `"status":"PASS"`) {
		t.Fatalf("verify command: %d %s", code, &out)
	}
	if strings.Contains(out.String(), f.trust.DatabaseIdentity) || strings.Contains(out.String(), f.in.Artifact) {
		t.Fatal("private input leaked")
	}
	output := filepath.Join(filepath.Dir(f.in.Artifact), "receipt.json")
	if code := Run(t.Context(), append(args, "-output", output), &out); code != 0 {
		t.Fatal("receipt export failed", code)
	}
	var receipt retention.IndexerBackup
	if err := retention.ReadArtifact(output, &receipt); err != nil || receipt != f.envelope.Statement.Receipt {
		t.Fatal("incompatible receipt")
	}
	before, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if code := Run(t.Context(), append(args, "-output", output), &out); code != 1 {
		t.Fatal("output overwrite accepted")
	}
	after, err := os.ReadFile(output)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("existing output changed")
	}
	for _, a := range [][]string{nil, {"-trust", "HOLD"}} {
		out.Reset()
		if code := Run(t.Context(), a, &out); code != 2 {
			t.Fatal("missing input must HOLD", code)
		}
	}
	for _, a := range [][]string{{"-unknown", f.in.Artifact}, append(args, "extra")} {
		out.Reset()
		if code := Run(t.Context(), a, &out); code != 1 || strings.Contains(out.String(), f.in.Artifact) {
			t.Fatal("invalid flags leaked or succeeded")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err = Verify(ctx, f.in); err == nil {
		t.Fatal("canceled verification succeeded")
	}
	if code := Run(ctx, args, &out); code != 1 {
		t.Fatal("canceled command succeeded")
	}
	if code := Run(t.Context(), args, failingWriter{}); code != 1 {
		t.Fatal("output failure ignored")
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, os.ErrClosed }

func TestBackupExportFailurePreservesEvidenceAndLeavesNoPartialReceipt(t *testing.T) {
	for _, kind := range []string{"public-parent", "parent-link", "existing-link", "canceled", "invalid-evidence"} {
		t.Run(kind, func(t *testing.T) {
			f := setup(t)
			parent := filepath.Join(filepath.Dir(f.in.Artifact), "output")
			if err := os.Mkdir(parent, 0700); err != nil {
				t.Fatal(err)
			}
			output := filepath.Join(parent, "receipt.json")
			ctx := t.Context()
			switch kind {
			case "public-parent":
				if err := os.Chmod(parent, 0755); err != nil {
					t.Fatal(err)
				}
			case "parent-link":
				link := parent + ".link"
				if err := os.Symlink(parent, link); err != nil {
					t.Fatal(err)
				}
				output = filepath.Join(link, "receipt.json")
			case "existing-link":
				if err := os.Symlink(f.in.Artifact, output); err != nil {
					t.Fatal(err)
				}
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "invalid-evidence":
				f.envelope.Signature = "00"
				save(t, f.in.Attestation, f.envelope)
			}
			var out bytes.Buffer
			args := []string{"-trust", f.in.Trust, "-permit", f.in.Permit, "-attestation", f.in.Attestation, "-artifact", f.in.Artifact, "-output", output}
			if Run(ctx, args, &out) != 1 {
				t.Fatal("unsafe export accepted")
			}
			entries, err := os.ReadDir(parent)
			want := 0
			if kind == "existing-link" {
				want = 1
			}
			if err != nil || len(entries) != want {
				t.Fatal("partial export leaked or existing evidence changed")
			}
			data, err := os.ReadFile(f.in.Artifact)
			if err != nil || !bytes.Equal(data, f.data) {
				t.Fatal("source artifact changed")
			}
		})
	}
}

type cancelReader struct{ cancel context.CancelFunc }

func (r cancelReader) Read(b []byte) (int, error) { r.cancel(); b[0] = 1; return 1, nil }

func TestBackupStreamingCancellationStopsBeforeNextRead(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	r := contextReader{ctx: ctx, r: cancelReader{cancel}}
	var b [4]byte
	if n, err := r.Read(b[:]); n != 1 || err != nil {
		t.Fatal(n, err)
	}
	if n, err := r.Read(b[:]); n != 0 || !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation ignored", n, err)
	}
}
