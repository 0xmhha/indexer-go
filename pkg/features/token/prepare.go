package token

import (
	"context"
	"errors"
	"fmt"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/feature"
	tokenmeta "github.com/0xmhha/indexer-go/pkg/token"
	"github.com/ethereum/go-ethereum/common"
)

type preparedToken struct {
	address  common.Address
	metadata *port.TokenMetadata
	err      error
}

// PrepareBlock reads contract metadata before entering the storage transaction.
// The writer still decides whether the contract already has a stored record.
func (m *metadata) PrepareBlock(ctx context.Context, b *feature.Block) (feature.PreparedHandler, error) {
	if _, ok := m.deps.Contracts.(tokenmeta.HashEthClient); !ok {
		return feature.PreparedHandler{}, nil
	}
	var tokens []preparedToken
	seen := map[common.Address]bool{}
	for _, r := range b.Receipts {
		if r == nil || r.ContractAddress == nil || seen[*r.ContractAddress] {
			continue
		}
		addr := *r.ContractAddress
		seen[addr] = true
		_, err := m.store.GetTokenMetadata(ctx, addr)
		if err == nil {
			continue
		}
		if !errors.Is(err, port.ErrNotFound) {
			return feature.PreparedHandler{}, err
		}
		tokens = append(tokens, preparedToken{address: addr})
	}
	if len(tokens) == 0 {
		return feature.PreparedHandler{}, nil
	}
	if err := m.deps.PrepareReads(ctx, len(tokens), func(ctx context.Context, i int) error {
		tokens[i].metadata, tokens[i].err = m.indexer.ReadContractAtHash(ctx, tokens[i].address, b.Model.Number, b.Model.Time, b.Model.Hash)
		return ctx.Err()
	}); err != nil {
		return feature.PreparedHandler{}, err
	}
	var size int64
	for _, token := range tokens {
		size += 128
		if token.err != nil {
			size += int64(len(token.err.Error()))
		}
		if md := token.metadata; md != nil {
			size += 256 + int64(len(md.Name)+len(md.Symbol)+len(md.BaseURI))
			if md.TotalSupply != nil {
				size += int64(len(md.TotalSupply.Bytes()))
			}
		}
	}
	return feature.PreparedHandler{ReadState: true, Bytes: size, Handler: feature.BlockHandlerFunc(func(ctx context.Context, _ *feature.Block) error {
		for _, token := range tokens {
			_, err := m.store.GetTokenMetadata(ctx, token.address)
			if err == nil {
				continue
			}
			if !errors.Is(err, port.ErrNotFound) {
				return err
			}
			if token.err != nil {
				return token.err
			}
			if token.metadata != nil {
				if err := m.store.SaveTokenMetadata(ctx, token.metadata); err != nil {
					return fmt.Errorf("save prepared token %s: %w", token.address, err)
				}
			}
		}
		return nil
	})}, nil
}
