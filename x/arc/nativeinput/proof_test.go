package nativeinput

import (
	"bytes"
	"encoding/json"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/stretchr/testify/require"
	"testing"
)

func proofFixture() Proof {
	i := Identity{Version: 1, Instance: execwire.Hash{3}, ChainID: 5042002, Genesis: execwire.Hash{1}, ArcRevision: ArcRevision, RethRevision: RethRevision, Layout: "reth-native-v2", BootstrapDigest: execwire.Hash{6}, Anchor: Ref{Hash: execwire.Hash{1}, Root: execwire.Hash{2}}}
	r := &Receipt{Version: 1, Identity: i, RequestID: 1, ImportEpoch: 1, ReadEpoch: 2, Height: 1, Hash: execwire.Hash{4}, Parent: i.Anchor.Hash, ParentRoot: i.Anchor.Root, Root: execwire.Hash{5}, ReceiptsRoot: execwire.Hash{7}, InputDigest: execwire.Hash{8}}
	return Proof{Version: 1, Identity: i, Head: Head{Version: 1, Instance: i.Instance, ImportEpoch: 1, ReadEpoch: 2, RequestID: 1, Tip: Ref{1, r.Hash, r.Root}}, Ref: Ref{1, r.Hash, r.Root}, Receipt: r}
}
func TestManagedPayloadBindsOriginalAIN1BytesAndKeepsE1GenerationIndependent(t *testing.T) {
	p := proofFixture()
	r := execwire.Result{Ref: execwire.Ref{Generation: 7, Height: p.Ref.Height, Hash: p.Ref.Hash}, Parent: p.Receipt.Parent, StateRoot: p.Ref.Root, ReceiptsRoot: p.Receipt.ReceiptsRoot, BlockRLP: []byte{0xc0}, ReceiptsRLP: []byte{0xc0}}
	m := Meta{Version: 1, RequestID: p.Receipt.RequestID, ChainID: p.Identity.ChainID, Genesis: p.Identity.Genesis, ArcRevision: p.Identity.ArcRevision, RethRevision: p.Identity.RethRevision, ImportEpoch: p.Receipt.ImportEpoch, Height: p.Ref.Height, Hash: p.Ref.Hash, Parent: p.Receipt.Parent, ParentRoot: p.Receipt.ParentRoot}
	digest, err := Digest(m, r.BlockRLP, r.ReceiptsRLP)
	require.NoError(t, err)
	require.NotEqual(t, execwire.Hash{}, digest)
	p.Receipt.InputDigest = digest
	require.NoError(t, ValidatePayload(p, r), "receiver-owned generation is not native import epoch")
	r.BlockRLP = []byte{0xc1, 0xc0}
	require.Error(t, ValidatePayload(p, r), "EXR1 body must equal original native input bytes")
	r.BlockRLP = []byte{0xc0}
	r.Parent[0]++
	require.Error(t, ValidatePayload(p, r))
}
func TestManagedProofRequiresExactIdentityCompletedHeadAndSelectedReceipt(t *testing.T) {
	p := proofFixture()
	require.NoError(t, ValidateProof(p, p.Identity))
	for _, mutate := range []func(*Proof){func(p *Proof) { p.Identity.Instance[0]++ }, func(p *Proof) { p.Head.Instance[0]++ }, func(p *Proof) { p.Head.ReadEpoch++ }, func(p *Proof) { p.Head.Tip.Hash[0]++ }, func(p *Proof) { p.Receipt = nil }, func(p *Proof) { p.Receipt.InputDigest = execwire.Hash{} }, func(p *Proof) { p.Receipt.Root[0]++ }, func(p *Proof) { p.Receipt.ImportEpoch++ }, func(p *Proof) { p.Ref.Height++ }} {
		p := proofFixture()
		expected := p.Identity
		mutate(&p)
		require.Error(t, ValidateProof(p, expected), "well-formed metadata alone cannot establish a native read")
	}
}
func TestManagedProofCanonicalBoundedJSONRejectsDuplicateUnknownAndTrailing(t *testing.T) {
	p := proofFixture()
	encoded, err := json.Marshal(p)
	require.NoError(t, err)
	var decoded Proof
	require.NoError(t, Decode(encoded, &decoded))
	require.Equal(t, p, decoded)
	for _, b := range [][]byte{append(bytes.Clone(encoded), ' '), bytes.Replace(encoded, []byte(`"Version":1`), []byte(`"Version":1,"Version":1`), 1), bytes.Replace(encoded, []byte(`"Version":1`), []byte(`"Other":1`), 1), append(bytes.Clone(encoded), encoded...), bytes.Repeat([]byte{' '}, MaxMeta+1)} {
		require.Error(t, Decode(b, &Proof{}))
	}
}
