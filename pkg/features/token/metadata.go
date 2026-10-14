package token

import (
	"context"
	"fmt"

	"github.com/0xmhha/indexer-go/pkg/feature"
	tokenmeta "github.com/0xmhha/indexer-go/pkg/token"
)

// MetadataName is the token.metadata feature: when a block creates a
// contract, it asks the node whether the contract is an ERC-20, ERC-721 or
// ERC-1155 token and stores its name, symbol and decimals.
const MetadataName = "token.metadata"

type metadataFeature struct{}

func (metadataFeature) Name() string       { return MetadataName }
func (metadataFeature) Requires() []string { return nil }
func (metadataFeature) DefaultOn() bool    { return true }

// OrderIndependent: one record per contract, written once.
func (metadataFeature) OrderIndependent() bool { return true }

// ReadsState implements feature.StateReader: a contract is read as of its
// creation block.
func (metadataFeature) ReadsState() bool { return true }

// metadataSettings are the feature's features.token.metadata section.
type metadataSettings struct {
	// AllowPrunedState permits latest-state fallback at startup and while
	// indexing. Otherwise missing historical state aborts the block.
	AllowPrunedState bool `yaml:"allow_pruned_state"`
}

func (metadataFeature) Register(r feature.Registrar) error {
	d := r.Deps()
	store, ok := d.Storage.(tokenmeta.TokenMetadataStore)
	if !ok {
		return fmt.Errorf("storage does not support token metadata")
	}
	if d.Contracts == nil {
		return fmt.Errorf("no node contract reader")
	}
	var st metadataSettings
	if err := d.DecodeSettings(MetadataName, &st); err != nil {
		return err
	}
	indexer := tokenmeta.NewContractIndexer(d.Contracts, store, d.Logger)
	indexer.SetAllowPrunedState(st.AllowPrunedState)
	r.OnBlock(&metadata{indexer: indexer, store: store, deps: d})
	return nil
}

func init() { feature.Register(metadataFeature{}) }

type metadata struct {
	indexer *tokenmeta.ContractIndexer
	store   tokenmeta.TokenMetadataStore
	deps    feature.Deps
}

// HandleBlock indexes the token metadata of the contracts the block
// creates, read as of the creation block. Node reads the node does not
// answer and storage errors abort the block, so it is retried.
func (m *metadata) HandleBlock(ctx context.Context, b *feature.Block) error {
	for _, r := range b.Receipts {
		if r == nil || r.ContractAddress == nil {
			continue
		}
		if err := m.indexer.IndexContract(ctx, *r.ContractAddress, b.Model.Number, b.Model.Time); err != nil {
			return err
		}
	}
	return nil
}
