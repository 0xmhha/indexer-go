package chains_test

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/core/model"
)

func TestFeeDelegationRegistry(t *testing.T) {
	const testType = 0x7e
	payer := common.Address{0xfe}
	key := model.NewExtKey("chains_test.payer")
	chains.RegisterFeeDelegation(chains.FeeDelegationScheme{
		Name:  "chains-test",
		Types: []uint8{testType},
		Of: func(tx *model.Transaction) (*chains.FeeDelegation, bool) {
			p, ok := tx.Ext.Get(key).(common.Address)
			if !ok {
				return nil, false
			}
			return &chains.FeeDelegation{Payer: p, V: big.NewInt(1)}, true
		},
	})

	sender := common.Address{0x01}
	plain := &model.Transaction{Type: 2, From: sender}
	require.False(t, chains.IsFeeDelegationType(2))
	_, ok := chains.FeeDelegationOf(plain)
	require.False(t, ok)
	require.Equal(t, sender, chains.GasPayer(plain))

	delegated := &model.Transaction{Type: testType, From: sender}
	delegated.Ext.Set(key, payer)
	require.True(t, chains.IsFeeDelegationType(testType))
	fd, ok := chains.FeeDelegationOf(delegated)
	require.True(t, ok)
	require.Equal(t, payer, fd.Payer)
	require.Equal(t, payer, chains.GasPayer(delegated))

	require.Panics(t, func() {
		chains.RegisterFeeDelegation(chains.FeeDelegationScheme{Name: "chains-test"})
	}, "duplicate name")
	require.Panics(t, func() {
		chains.RegisterFeeDelegation(chains.FeeDelegationScheme{Name: "other", Types: []uint8{testType}})
	}, "duplicate type")
}
