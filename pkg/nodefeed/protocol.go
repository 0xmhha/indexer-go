// Package nodefeed defines versioned local execution-feed wire types.
package nodefeed

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

const Version = 1
const MaxFrame = 32 << 20

type Request struct {
	Version   int              `json:"version"`
	ChainID   uint64           `json:"chain_id"`
	Session   string           `json:"session"`
	Kind      string           `json:"kind"`
	Height    uint64           `json:"height,omitempty"`
	Hash      common.Hash      `json:"hash"`
	Addresses []common.Address `json:"addresses,omitempty"`
}
type Bundle struct {
	Height     uint64      `json:"height"`
	Hash       common.Hash `json:"hash"`
	ParentHash common.Hash `json:"parent_hash"`
	Header     []byte      `json:"header"`
	Body       []byte      `json:"body"`
	Receipts   []byte      `json:"receipts"`
}
type FeedError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *FeedError) Error() string { return "nodefeed: " + e.Code + ": " + e.Message }

type Response struct {
	Version  int            `json:"version"`
	ChainID  uint64         `json:"chain_id"`
	Error    *FeedError     `json:"error,omitempty"`
	Block    *Bundle        `json:"block,omitempty"`
	Height   *uint64        `json:"height,omitempty"`
	Balances []*hexutil.Big `json:"balances,omitempty"`
}
