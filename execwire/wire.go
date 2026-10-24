// Package execwire defines the bounded, versioned protocol shared by the
// execution process and the indexer. It has no dependencies outside the Go
// standard library and does not decide canonicality or acknowledge storage.
package execwire

import (
	"errors"
)

const (
	Version          = 1
	MaxFrameBytes    = 8 << 20
	MaxHeaderBytes   = 64 << 10
	MaxBlockBytes    = 64 << 20
	MaxMetadataBytes = 16 << 20
	MaxParts         = 4096
)

var (
	ErrProtocol   = errors.New("execwire: invalid protocol input")
	ErrLimit      = errors.New("execwire: resource limit")
	ErrIncomplete = errors.New("execwire: incomplete result")
)

type Hash [32]byte
type Address [20]byte
type Word = Hash

type Chain struct {
	ID      string
	Genesis Hash
}
type Ref struct {
	Generation uint64
	Height     uint64
	Hash       Hash
}
type Kind uint8

const (
	Hello Kind = iota + 1
	ResultPart
	Committed
	Rewind
	Failure
)

type Header struct {
	Version    uint16
	Chain      Chain
	Ref        Ref
	Digest     Hash
	PartDigest Hash
	Index      uint32
	Count      uint32
	Total      uint64
	Code       string
}
type Frame struct {
	Kind    Kind
	Header  Header
	Payload []byte
}

type Cause uint8

const (
	Transaction Cause = iota
	Finalize
	Genesis
	Baseline
	// PreBlock identifies system changes before the first transaction. It is
	// optional, first-only and never carries a user transaction identity.
	PreBlock
)

type Account struct {
	Exists      bool
	Nonce       uint64
	Balance     Word
	CodeHash    Hash
	StorageRoot Hash
	Extra       uint64
}
type AccountChange struct {
	Address        Address
	Before         Account
	After          Account
	Baseline       bool
	StorageCleared bool
}
type StorageChange struct {
	Address  Address
	Slot     Word
	Before   Word
	After    Word
	Baseline bool
}
type Code struct {
	Hash  Hash
	Bytes []byte
}
type Step struct {
	Ordinal  uint32
	Cause    Cause
	TxIndex  int32
	TxHash   Hash
	Accounts []AccountChange
	Storage  []StorageChange
	Code     []Code
}
type Result struct {
	Ref          Ref
	Parent       Hash
	Time         uint64
	StateRoot    Hash
	ReceiptsRoot Hash
	TxCount      uint32
	BlockRLP     []byte `json:"-"`
	ReceiptsRLP  []byte `json:"-"`
	Steps        []Step
}
