package port

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"io"
	"math"
	"regexp"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/model"
)

// ExecutionIngressPolicy is immutable source ownership, before a native
// instance exists. Paths, quotas and current epochs are deliberately absent.
type ExecutionIngressPolicy struct {
	Version      uint8
	Module       string
	Chain        execwire.Chain
	Anchor       execwire.Ref
	AnchorRoot   execwire.Hash
	Requirements execwire.Hash
}

type ExecutionIngressBinding struct {
	Version                                                   uint8
	PolicyDigest, NativeInstance, BootstrapDigest, RawStoreID execwire.Hash
	SourcePolicyDigest                                        execwire.Hash
	NativeIdentity                                            []byte
}

type ExecutionIngressOwner struct {
	Policy        ExecutionIngressPolicy
	PolicyDigest  execwire.Hash
	Binding       *ExecutionIngressBinding
	BindingDigest execwire.Hash
}

type ExecutionIngressOwnerStore interface {
	ConfigureExecutionIngress(context.Context, *ExecutionIngressPolicy, bool) error
	BindExecutionIngress(context.Context, ExecutionIngressBinding) error
	ExecutionIngressOwner(context.Context) (ExecutionIngressOwner, error)
}

type ExecutionIngressDataset struct {
	Kind         model.DatasetKind
	Codec        string
	Reference    []byte
	Digest       execwire.Hash
	PayloadBytes uint64
	Payload      []byte `json:"-"`
}

// Payload bytes use separate SQL bytea columns; canonical metadata covers
// their exact size/digest. Original native epoch differs from E1 Ref.Generation.
type ExecutionIngressArchive struct {
	Version                                       uint8
	OwnerDigest                                   execwire.Hash
	Ref                                           execwire.Ref
	Parent, Digest, InputDigest                   execwire.Hash
	NativeEpoch, NativeReadEpoch, NativeRequestID uint64
	ObservedRawEpoch                              uint64
	Provenance                                    []byte
	Datasets                                      []ExecutionIngressDataset
}

type ExecutionIngressArchiveWriter interface {
	ApplyExecutionIngress(context.Context, ExecutionIngressArchive) error
}

type ExecutionIngressArchiveReader interface {
	ExecutionIngressArchive(context.Context, ExecutionPart) (ExecutionIngressArchive, error)
}

type ExecutionIngressInput struct {
	Chain                              execwire.Chain
	Ref                                execwire.Ref
	Parent, Digest, Root, ReceiptsRoot execwire.Hash
	BlockRLP, ReceiptsRLP              []byte
}

// Resolve completes native/raw IO before SQL. The returned release may hold
// only memory accounting; it must retain no native/file lease or child.
type ExecutionIngressOriginResolver interface {
	ResolveExecutionOrigin(context.Context, ExecutionIngressInput) (ExecutionIngressArchive, func(), error)
}

var ingressModuleName = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,63}$`)

func (p ExecutionIngressPolicy) Validate() error {
	if p.Version != 1 || !ingressModuleName.MatchString(p.Module) || p.AnchorRoot == (execwire.Hash{}) || p.Requirements == (execwire.Hash{}) {
		return ErrInvalidData
	}
	return execwire.Encode(io.Discard, execwire.Frame{Kind: execwire.Hello, Header: execwire.Header{Version: execwire.Version, Chain: p.Chain, Ref: p.Anchor}})
}
func ingressDigest(domain string, value any) (execwire.Hash, error) {
	data, err := json.Marshal(value)
	if err != nil || len(data) > 64<<10 {
		return execwire.Hash{}, ErrInvalidData
	}
	return execwire.Hash(sha256.Sum256(append([]byte(domain+"\x00"), data...))), nil
}
func (p ExecutionIngressPolicy) Digest() (execwire.Hash, error) {
	if err := p.Validate(); err != nil {
		return execwire.Hash{}, err
	}
	return ingressDigest("indexer.execution.ingress.policy.v1", p)
}
func (b ExecutionIngressBinding) Validate() error {
	if b.Version != 1 || b.PolicyDigest == (execwire.Hash{}) || b.NativeInstance == (execwire.Hash{}) || b.BootstrapDigest == (execwire.Hash{}) || b.RawStoreID == (execwire.Hash{}) || b.SourcePolicyDigest == (execwire.Hash{}) || len(b.NativeIdentity) < 2 || len(b.NativeIdentity) > 16<<10 || b.NativeIdentity[0] != '{' || !json.Valid(b.NativeIdentity) {
		return ErrInvalidData
	}
	var compact bytes.Buffer
	if json.Compact(&compact, b.NativeIdentity) != nil || !bytes.Equal(compact.Bytes(), b.NativeIdentity) {
		return ErrInvalidData
	}
	return nil
}
func (b ExecutionIngressBinding) Digest() (execwire.Hash, error) {
	if err := b.Validate(); err != nil {
		return execwire.Hash{}, err
	}
	return ingressDigest("indexer.execution.ingress.binding.v1", b)
}

func (a ExecutionIngressArchive) Validate() error {
	if a.Version != 1 || a.OwnerDigest == (execwire.Hash{}) || a.Ref.Generation == 0 || a.Ref.Generation > math.MaxInt64 || a.Ref.Height == 0 || a.Ref.Height > math.MaxInt64 || a.Ref.Hash == (execwire.Hash{}) || a.Parent == (execwire.Hash{}) || a.Digest == (execwire.Hash{}) || a.InputDigest == (execwire.Hash{}) || len(a.Provenance) < 2 || len(a.Provenance) > 16<<10 || a.Provenance[0] != '{' || !json.Valid(a.Provenance) {
		return ErrInvalidData
	}
	for _, n := range []uint64{a.NativeEpoch, a.NativeReadEpoch, a.NativeRequestID, a.ObservedRawEpoch} {
		if n == 0 || n > math.MaxInt64 {
			return ErrInvalidData
		}
	}
	kinds := []model.DatasetKind{model.DatasetHeaders, model.DatasetBodies, model.DatasetReceipts}
	if len(a.Datasets) != len(kinds) {
		return ErrInvalidData
	}
	for i, d := range a.Datasets {
		if d.Kind != kinds[i] || d.Codec == "" || len(d.Codec) > 128 || len(d.Reference) < 2 || len(d.Reference) > 4096 || d.Reference[0] != '{' || !json.Valid(d.Reference) || len(d.Payload) == 0 || len(d.Payload) > 10<<20 || d.PayloadBytes != uint64(len(d.Payload)) || d.Digest != execwire.Hash(sha256.Sum256(d.Payload)) {
			return ErrInvalidData
		}
	}
	return nil
}

func (a ExecutionIngressArchive) MetadataDigest() (execwire.Hash, error) {
	if err := a.Validate(); err != nil {
		return execwire.Hash{}, err
	}
	return ingressDigest("indexer.execution.ingress.archive.v1", a)
}
