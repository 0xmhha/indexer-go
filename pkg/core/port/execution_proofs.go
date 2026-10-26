package port

import (
	"context"
	"encoding/json"
	"github.com/0xmhha/indexer-go/execwire"
)

type ExecutionProofRecord struct {
	Chain                                            execwire.Chain
	Ref                                              execwire.Ref
	Parent, Digest, Root, InputDigest, SelectionHash execwire.Hash
	Profile, Provider                                string
	Response                                         []byte `json:"-"`
}
type ExecutionProofQuery struct {
	Height  uint64
	Address execwire.Address
	Epoch   uint64
	Hash    execwire.Hash
}
type ExecutionProofSlot struct{ Key, Value execwire.Word }

// Balance is a consensus scalar, not profile-native currency accounting.
type ExecutionProofAccount struct {
	Address               execwire.Address
	Exists                bool
	Nonce                 uint64
	ConsensusBalance      execwire.Word
	StorageRoot, CodeHash execwire.Hash
	Code                  *[]byte
	Storage               []ExecutionProofSlot
}
type ExecutionProofView struct {
	Epoch                       uint64
	Ref                         execwire.Ref
	Root, Digest, SelectionHash execwire.Hash
	Profile, Provider           string
	Account                     ExecutionProofAccount
	Proof                       json.RawMessage
}
type ExecutionProofWriter interface {
	ApplyExecutionProofs(context.Context, execwire.Chain, execwire.Result) error
}
type ExecutionProofReader interface {
	ExecutionProof(context.Context, ExecutionProofQuery) (ExecutionProofView, error)
}
