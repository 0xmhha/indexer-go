// Package nativeinput defines the pinned Arc native import/read contracts.
package nativeinput

import "github.com/0xmhha/indexer-go/execwire"

const ArcRevision = "6e764023ee6515fe70573e123ed2db912a7207b4"
const RethRevision = "88505c7fcbfdebfd3b56d88c86b62e950043c6c4"
const MaxMeta = 16 << 10

type Ref struct {
	Height     uint64
	Hash, Root execwire.Hash
}
type Identity struct {
	Version                           uint8
	Instance                          execwire.Hash
	ChainID                           uint64
	Genesis                           execwire.Hash
	ArcRevision, RethRevision, Layout string
	BootstrapDigest                   execwire.Hash
	Anchor                            Ref
}
type Head struct {
	Version                           uint8
	Instance                          execwire.Hash
	ImportEpoch, ReadEpoch, RequestID uint64
	Tip                               Ref
}
type Ready struct {
	Version  uint8
	Identity Identity
	Head     Head
}
type Meta struct {
	Version                   uint8
	RequestID, ChainID        uint64
	Genesis                   execwire.Hash
	ArcRevision, RethRevision string
	ImportEpoch, Height       uint64
	Hash, Parent, ParentRoot  execwire.Hash
}
type Receipt struct {
	Version                                                   uint8
	Identity                                                  Identity
	RequestID, ImportEpoch, ReadEpoch, Height                 uint64
	Hash, Parent, ParentRoot, Root, ReceiptsRoot, InputDigest execwire.Hash
}

type Proof struct {
	Version  uint8
	Identity Identity
	Head     Head
	Ref      Ref
	Receipt  *Receipt
}
