package address

import (
	"context"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/feature"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
	"testing"
)

type creationProbe struct {
	port.Writer
	port.AddressIndexWriter
	created []common.Address
}

func (p *creationProbe) AddTransactionToAddressIndex(context.Context, common.Address, common.Hash) error {
	return nil
}
func (p *creationProbe) SaveContractCreation(_ context.Context, c *port.ContractCreation) error {
	p.created = append(p.created, c.ContractAddress)
	return nil
}
func TestCreationRequiresSuccessfulReceiptAndNonzeroAddress(t *testing.T) {
	valid, failed, zero := common.Address{1}, common.Address{2}, common.Address{}
	p := &creationProbe{}
	h := handler{txIndex: p, creations: p}
	b := &feature.Block{Model: &model.Block{Number: 1, Transactions: []*model.Transaction{{Hash: common.Hash{1}}, {Hash: common.Hash{2}}, {Hash: common.Hash{3}}}}, Receipts: []*model.Receipt{{TxHash: common.Hash{1}, Status: 1, ContractAddress: &valid}, {TxHash: common.Hash{2}, Status: 0, ContractAddress: &failed}, {TxHash: common.Hash{3}, Status: 1, ContractAddress: &zero}}}
	b.Geth = types.NewBlockWithHeader(&types.Header{}).WithBody(types.Body{Transactions: types.Transactions{types.NewTx(&types.LegacyTx{}), types.NewTx(&types.LegacyTx{}), types.NewTx(&types.LegacyTx{})}})
	b.GethReceipts = types.Receipts{&types.Receipt{}, &types.Receipt{}, &types.Receipt{}}
	require.NoError(t, h.HandleBlock(t.Context(), b))
	require.Equal(t, []common.Address{valid}, p.created)
}
