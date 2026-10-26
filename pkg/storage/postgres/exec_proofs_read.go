package postgres

import (
	"context"
	"encoding/json"
	"github.com/0xmhha/indexer-go/execwire"
	stateproof "github.com/0xmhha/indexer-go/pkg/chainfeed/evmstate/proof"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"math"
)

var _ port.ExecutionProofReader = (*Store)(nil)

func (s *Store) ExecutionProof(ctx context.Context, in port.ExecutionProofQuery) (out port.ExecutionProofView, err error) {
	if in.Height > math.MaxInt64 || (in.Epoch == 0) != (in.Hash == (execwire.Hash{})) {
		return out, port.ErrInvalidData
	}
	err = s.ReadSnapshot(ctx, func(ctx context.Context) error {
		c, e := s.ExecutionStatus(ctx)
		if e != nil {
			return e
		}
		if c.Rewinding {
			return port.ErrExecutionRewinding
		}
		if c.Maintenance {
			return port.ErrExecutionMaintenance
		}
		if in.Epoch != 0 && in.Epoch != c.Generation {
			return port.ErrExecutionStale
		}
		if in.Height > c.Visible.Height {
			return port.ErrNotFound
		}
		var gen int64
		var hash, parent, digest, root []byte
		var collected bool
		q := s.q(ctx)
		if e = q.QueryRow(ctx, `SELECT generation,hash,parent,digest,root,state_proofs FROM exec_publications WHERE height=$1`, int64(in.Height)).Scan(&gen, &hash, &parent, &digest, &root, &collected); e != nil {
			return notFound(e)
		}
		if !collected {
			return port.ErrNotFound
		}
		if gen < 1 || uint64(gen) > c.Generation {
			return port.ErrExecutionConflict
		}
		p := port.ExecutionPublication{Ref: execwire.Ref{Generation: uint64(gen), Height: in.Height}, StateProofs: true}
		copy(p.Ref.Hash[:], hash)
		copy(p.Parent[:], parent)
		copy(p.Digest[:], digest)
		copy(p.Root[:], root)
		if in.Epoch != 0 && in.Hash != p.Ref.Hash {
			return port.ErrExecutionStale
		}
		record, bundle, accounts, e := readProofReceipt(ctx, q, p, c.Chain)
		if e != nil {
			return e
		}
		block, e := s.GetBlock(ctx, in.Height)
		if e != nil {
			return e
		}
		if execwire.Hash(block.Hash) != p.Ref.Hash || execwire.Hash(block.StateRoot) != p.Root {
			return port.ErrExecutionConflict
		}
		for i, account := range accounts {
			if execwire.Address(account.Address) != in.Address {
				continue
			}
			a := port.ExecutionProofAccount{Address: in.Address, Exists: account.Exists, Nonce: account.Nonce, StorageRoot: execwire.Hash(account.StorageRoot), CodeHash: execwire.Hash(account.CodeHash), Storage: []port.ExecutionProofSlot{}}
			account.Balance.ToInt().FillBytes(a.ConsensusBalance[:])
			if account.Code != nil {
				code := append([]byte{}, (*account.Code)...)
				a.Code = &code
			}
			for _, slot := range account.Storage {
				a.Storage = append(a.Storage, port.ExecutionProofSlot{Key: execwire.Word(slot.Key), Value: execwire.Word(slot.Value)})
			}
			proof := bundle.Evidence[i].Proof
			if proof.AccountProof == nil {
				proof.AccountProof = stateproof.Nodes{}
			}
			if proof.StorageProof == nil {
				proof.StorageProof = []stateproof.StorageProof{}
			}
			for j := range proof.StorageProof {
				if proof.StorageProof[j].Proof == nil {
					proof.StorageProof[j].Proof = stateproof.Nodes{}
				}
			}
			raw, e := json.Marshal(proof)
			if e != nil {
				return e
			}
			out = port.ExecutionProofView{Epoch: c.Generation, Ref: p.Ref, Root: p.Root, Digest: p.Digest, SelectionHash: record.SelectionHash, Profile: record.Profile, Provider: record.Provider, Account: a, Proof: raw}
			return nil
		}
		return port.ErrNotFound
	})
	return out, err
}
