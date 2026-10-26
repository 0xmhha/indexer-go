package proofdata

import (
	"bytes"
	"encoding/json"
	stateproof "github.com/0xmhha/indexer-go/pkg/chainfeed/evmstate/proof"
	"io"
)

type shape struct {
	fields map[string]*shape
	elem   *shape
}

var scalar = &shape{}

func object(fields map[string]*shape) *shape { return &shape{fields: fields} }
func array(elem *shape) *shape               { return &shape{elem: elem} }

var proofShape = object(map[string]*shape{
	"header": scalar, "version": scalar,
	"selection": object(map[string]*shape{"targets": array(object(map[string]*shape{"address": scalar, "code": scalar, "slots": array(scalar)}))}),
	"evidence": array(object(map[string]*shape{
		"code": scalar, "proof": object(map[string]*shape{"address": scalar, "accountProof": array(scalar), "balance": scalar, "codeHash": scalar, "nonce": scalar, "storageHash": scalar, "storageProof": array(object(map[string]*shape{"key": scalar, "value": scalar, "proof": array(scalar)}))}),
	})),
})

// Typed decoding bounds byte/cardinality first. This pass rejects duplicate and
// unknown keys inside custom UnmarshalJSON types, preserving legacy null lists.
func strictJSON(raw []byte) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	if e := walkJSON(d, proofShape, 0); e != nil {
		return e
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return stateproof.ErrProof
	}
	return nil
}
func walkJSON(d *json.Decoder, s *shape, depth int) error {
	if depth > 12 {
		return stateproof.ErrProof
	}
	if s.fields == nil && s.elem == nil {
		var raw json.RawMessage
		return d.Decode(&raw)
	}
	token, e := d.Token()
	if e != nil {
		return stateproof.ErrProof
	}
	if s.elem != nil {
		if token == nil {
			return nil
		}
		if token != json.Delim('[') {
			return stateproof.ErrProof
		}
		for d.More() {
			if e = walkJSON(d, s.elem, depth+1); e != nil {
				return e
			}
		}
		token, e = d.Token()
		if e != nil || token != json.Delim(']') {
			return stateproof.ErrProof
		}
		return nil
	}
	if token != json.Delim('{') {
		return stateproof.ErrProof
	}
	seen := map[string]bool{}
	for d.More() {
		token, e = d.Token()
		if e != nil {
			return stateproof.ErrProof
		}
		key, ok := token.(string)
		if !ok || seen[key] {
			return stateproof.ErrProof
		}
		next, ok := s.fields[key]
		if !ok {
			return stateproof.ErrProof
		}
		seen[key] = true
		if e = walkJSON(d, next, depth+1); e != nil {
			return e
		}
	}
	token, e = d.Token()
	if e != nil || token != json.Delim('}') {
		return stateproof.ErrProof
	}
	return nil
}
