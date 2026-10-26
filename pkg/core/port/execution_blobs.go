package port

import (
	"context"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/model"
)

type ExecutionBlobReference struct {
	Index            uint32
	TransactionIndex uint32
	TransactionHash  execwire.Hash
	BlobIndex        uint32
	VersionedHash    execwire.Hash
}
type ExecutionBlob struct {
	Index                   uint32
	Blob, Commitment, Proof []byte
}

// Entries are stored separately. The small canonical metadata is authenticated
// with its publication identity and checksum before any page/proof is read.
type ExecutionBlobRecord struct {
	Chain                       execwire.Chain
	Ref                         execwire.Ref
	Parent, Digest, InputDigest execwire.Hash
	Profile, Provider           string
	References                  []ExecutionBlobReference
	Entries                     []ExecutionBlob `json:"-"`
}
type ExecutionBlobWriter interface {
	ApplyExecutionBlobs(context.Context, execwire.Chain, execwire.Result, *model.Block) error
}
type ExecutionBlobQuery struct {
	Height       uint64
	Transaction  *execwire.Hash
	After, Limit uint32
	Epoch        uint64
	Hash         execwire.Hash
}
type ExecutionBlobPage struct {
	Epoch                                         uint64
	Ref                                           execwire.Ref
	Digest                                        execwire.Hash
	Profile, Provider, Availability, Verification string
	Total                                         uint32
	References                                    []ExecutionBlobReference
	Entries                                       []ExecutionBlob
	Next                                          *uint32
}
type ExecutionBlobReader interface {
	ExecutionBlobs(context.Context, ExecutionBlobQuery) (ExecutionBlobPage, error)
}
