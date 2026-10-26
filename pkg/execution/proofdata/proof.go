// Package proofdata binds bounded selected MPT evidence to E1 block-end roots.
package proofdata

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"github.com/0xmhha/indexer-go/execwire"
	stateproof "github.com/0xmhha/indexer-go/pkg/chainfeed/evmstate/proof"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
)

// Parse accepts bounded bundle JSON. Unknown/duplicate fields and competing
// header claims fail; legacy empty-list/null representations remain readable.
func Parse(raw []byte) (stateproof.Bundle, error) {
	var b stateproof.Bundle
	if len(raw) == 0 || len(raw) > stateproof.MaxBundleBytes {
		return b, stateproof.ErrProof
	}
	if e := json.Unmarshal(raw, &b); e != nil {
		return b, e
	}
	if len(b.Header) != 0 {
		return b, stateproof.ErrProof
	}
	if e := strictJSON(raw); e != nil {
		return b, e
	}
	return b, nil
}
func Decode(ctx context.Context, chain execwire.Chain, r execwire.Result) (*port.ExecutionProofRecord, stateproof.Bundle, []stateproof.Account, error) {
	var bundle stateproof.Bundle
	if e := ctx.Err(); e != nil {
		return nil, bundle, nil, e
	}
	if r.StateProofBundle == nil || execwire.ValidateStateProofBundle(r.StateProofBundle) != nil {
		return nil, bundle, nil, stateproof.ErrProof
	}
	bundle, e := Parse(r.StateProofBundle.Response)
	if e != nil {
		return nil, bundle, nil, e
	}
	accounts, e := stateproof.VerifyContext(ctx, common.Hash(r.StateRoot), bundle.Selection, bundle)
	if e != nil {
		return nil, bundle, nil, e
	}
	_, selection, e := bundle.Selection.Normalize()
	if e != nil {
		return nil, bundle, nil, e
	}
	return &port.ExecutionProofRecord{Chain: chain, Ref: r.Ref, Parent: r.Parent, Root: r.StateRoot, InputDigest: execwire.Hash(sha256.Sum256(r.StateProofBundle.Response)), SelectionHash: execwire.Hash(selection), Profile: r.StateProofBundle.Profile, Provider: r.StateProofBundle.Provider, Response: bytes.Clone(r.StateProofBundle.Response)}, bundle, accounts, nil
}
func Validate(ctx context.Context, profile string, chain execwire.Chain, r execwire.Result, required *stateproof.Selection) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	var expected common.Hash
	if required != nil {
		_, h, e := required.Normalize()
		if e != nil {
			return e
		}
		expected = h
	}
	if r.StateProofBundle == nil {
		if required != nil {
			return stateproof.ErrProof
		}
		return nil
	}
	if r.StateProofBundle.Profile != profile {
		return stateproof.ErrProof
	}
	record, _, _, e := Decode(ctx, chain, r)
	if e != nil {
		return e
	}
	if required != nil && record.SelectionHash != execwire.Hash(expected) {
		return stateproof.ErrProof
	}
	return nil
}
func VerifyRecord(ctx context.Context, r port.ExecutionProofRecord) (stateproof.Bundle, []stateproof.Account, error) {
	var b stateproof.Bundle
	if execwire.Hash(sha256.Sum256(r.Response)) != r.InputDigest {
		return b, nil, port.ErrExecutionConflict
	}
	result := execwire.Result{Ref: r.Ref, Parent: r.Parent, StateRoot: r.Root, StateProofBundle: &execwire.StateProofBundle{Version: 1, Format: execwire.StateProofFormat, Profile: r.Profile, Provider: r.Provider, Response: r.Response}}
	got, b, accounts, e := Decode(ctx, r.Chain, result)
	if e != nil {
		return b, nil, e
	}
	if got.SelectionHash != r.SelectionHash {
		return b, nil, port.ErrExecutionConflict
	}
	return b, accounts, nil
}
