//go:build darwin || linux

package nativeinput

import (
	"crypto/sha256"
	"encoding/json"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func sealedFixture(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	require.NoError(t, err)
	b, err := json.Marshal(struct {
		Data   json.RawMessage
		Digest execwire.Hash
	}{data, execwire.Hash(sha256.Sum256(data))})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, b, 0600))
}
func TestManagedArtifactsRequireExactPrivateCanonicalChecksummedFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "proof")
	want := proofFixture().Head
	sealedFixture(t, path, want)
	var got Head
	require.NoError(t, ReadArtifact(path, &got))
	require.Equal(t, want, got, "control must actually be decoded after checksum verification")
	bytes, err := os.ReadFile(path)
	require.NoError(t, err)
	for _, mode := range []string{"public", "symlink", "hardlink", "checksum", "trailing", "oversize", "missing"} {
		t.Run(mode, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "candidate")
			require.NoError(t, os.WriteFile(p, bytes, 0600))
			switch mode {
			case "public":
				require.NoError(t, os.Chmod(p, 0644))
			case "symlink":
				require.NoError(t, os.Remove(p))
				require.NoError(t, os.Symlink(path, p))
			case "hardlink":
				require.NoError(t, os.Link(p, p+".link"))
			case "checksum":
				b := append([]byte(nil), bytes...)
				b[len(b)-4] ^= 1
				require.NoError(t, os.WriteFile(p, b, 0600))
			case "trailing":
				require.NoError(t, os.WriteFile(p, append(bytes, ' '), 0600))
			case "oversize":
				require.NoError(t, os.WriteFile(p, make([]byte, MaxMeta+1), 0600))
			case "missing":
				require.NoError(t, os.Remove(p))
			}
			require.Error(t, ReadArtifact(p, &Head{}))
		})
	}
}

func TestManagedCurrentProofMustEqualSealedControlsAndImmutableInput(t *testing.T) {
	for _, change := range []string{"none", "identity", "head", "receipt", "bootstrap", "missing"} {
		t.Run(change, func(t *testing.T) {
			dir := t.TempDir()
			p := proofFixture()
			boot := []byte("owned original bootstrap bytes")
			p.Identity.BootstrapDigest = execwire.Hash(sha256.Sum256(boot))
			p.Receipt.Identity = p.Identity
			require.NoError(t, os.WriteFile(filepath.Join(dir, BootstrapName), boot, 0600))
			sealedFixture(t, filepath.Join(dir, IdentityName), p.Identity)
			sealedFixture(t, filepath.Join(dir, HeadName), p.Head)
			receipts := filepath.Join(dir, ".arc-native-import-receipts")
			require.NoError(t, os.Mkdir(receipts, 0700))
			rp := filepath.Join(receipts, "1-1.json")
			sealedFixture(t, rp, p.Receipt)
			switch change {
			case "identity":
				i := p.Identity
				i.Instance[0]++
				sealedFixture(t, filepath.Join(dir, IdentityName), i)
			case "head":
				h := p.Head
				h.ReadEpoch++
				sealedFixture(t, filepath.Join(dir, HeadName), h)
			case "receipt":
				r := *p.Receipt
				r.InputDigest[0]++
				sealedFixture(t, rp, r)
			case "bootstrap":
				require.NoError(t, os.WriteFile(filepath.Join(dir, BootstrapName), []byte("other"), 0600))
			case "missing":
				require.NoError(t, os.Remove(rp))
			}
			if change == "none" {
				require.NoError(t, VerifyCurrent(dir, p, p.Identity))
			} else {
				require.Error(t, VerifyCurrent(dir, p, p.Identity), "well-formed response is insufficient when current durable controls disagree")
			}
		})
	}
}
