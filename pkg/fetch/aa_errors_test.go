package fetch

import (
	"context"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	modulepkg "github.com/0xmhha/indexer-go/pkg/module"
	"github.com/0xmhha/indexer-go/pkg/userop"
)

// The account abstraction processors return storage errors, so the block
// fails and is retried instead of committing without the data (defect
// D5); only events that do not decode, or uninstalls of modules installed
// before indexing started, are skipped.

var errDisk = errors.New("disk failure")

type failingSetCode struct {
	port.SetCodeIndexWriter
	saved int
}

func (f *failingSetCode) IncrementSetCodeStats(context.Context, common.Address, bool, bool, uint64) error {
	return errDisk
}

func (f *failingSetCode) SaveSetCodeAuthorizations(_ context.Context, rs []*port.SetCodeAuthorizationRecord) error {
	f.saved += len(rs)
	return nil
}

func TestSetCodeStorageErrorFailsTheBlock(t *testing.T) {
	tx := types.NewTx(&types.SetCodeTx{
		ChainID: uint256.NewInt(1), Gas: 100000, GasFeeCap: uint256.NewInt(1), GasTipCap: uint256.NewInt(1),
		AuthList: []types.SetCodeAuthorization{{ChainID: *uint256.NewInt(1), Address: common.HexToAddress("0xbeef")}},
	})
	receipt := &types.Receipt{Status: types.ReceiptStatusSuccessful}
	err := NewSetCodeProcessor(zap.NewNop(), &failingSetCode{}).ProcessSetCodeTransactionAt(context.Background(), tx, receipt, 5, common.Hash{1}, 1000, 0)
	require.ErrorIs(t, err, errDisk)
}

type failingUserOps struct {
	UserOpIndexer
	bundlerErr, accountErr, saveAccountErr error
	accounts                               int
}

func (f *failingUserOps) GetBundlerStats(_ context.Context, a common.Address) (*userop.BundlerStats, error) {
	if f.bundlerErr != nil {
		return nil, f.bundlerErr
	}
	return &userop.BundlerStats{Address: a}, nil
}
func (f *failingUserOps) UpdateBundlerStats(context.Context, *userop.BundlerStats) error { return nil }
func (f *failingUserOps) GetSmartAccount(context.Context, common.Address) (*userop.SmartAccount, error) {
	return nil, f.accountErr
}
func (f *failingUserOps) SaveSmartAccount(context.Context, *userop.SmartAccount) error {
	f.accounts++
	return f.saveAccountErr
}

func TestUserOpStatsStorageErrorsFailTheBlock(t *testing.T) {
	bundler, sender := common.HexToAddress("0xb0"), common.HexToAddress("0x5e")
	ops := []*userop.UserOperation{{Sender: sender, Bundler: bundler}}
	counts := map[common.Address]int{bundler: 1}
	ctx := context.Background()

	s := &failingUserOps{accountErr: port.ErrNotFound}
	require.NoError(t, NewUserOpProcessor(zap.NewNop(), s).updateStats(ctx, ops, counts), "a new account is no error")
	assert.Equal(t, 1, s.accounts)

	for name, s := range map[string]*failingUserOps{
		"bundler stats read":  {bundlerErr: errDisk, accountErr: port.ErrNotFound},
		"smart account read":  {accountErr: errDisk},
		"smart account write": {accountErr: port.ErrNotFound, saveAccountErr: errDisk},
	} {
		t.Run(name, func(t *testing.T) {
			require.ErrorIs(t, NewUserOpProcessor(zap.NewNop(), s).updateStats(ctx, ops, counts), errDisk)
		})
	}
}

type failingModules struct {
	ModuleIndexer
	statsErr, removeErr error
}

func (f *failingModules) SaveInstalledModule(context.Context, *port.InstalledModule) error {
	return nil
}
func (f *failingModules) RemoveModule(context.Context, common.Address, common.Address, uint64, common.Hash) error {
	return f.removeErr
}
func (f *failingModules) GetModuleStats(_ context.Context, m common.Address) (*port.ModuleStats, error) {
	if f.statsErr != nil {
		return nil, f.statsErr
	}
	return &port.ModuleStats{Module: m}, nil
}
func (f *failingModules) UpdateModuleStats(context.Context, *port.ModuleStats) error { return nil }

func moduleBlock(topic common.Hash, data []byte) (*types.Block, []*types.Receipt) {
	b := types.NewBlockWithHeader(&types.Header{Number: big.NewInt(9), Time: 1000})
	return b, []*types.Receipt{{Status: types.ReceiptStatusSuccessful, Logs: []*types.Log{
		{Address: common.HexToAddress("0xacc"), Topics: []common.Hash{topic}, Data: data},
	}}}
}

func TestModuleEventErrors(t *testing.T) {
	ctx := context.Background()
	event := make([]byte, 64)
	event[31], event[63] = 1, 0x42

	b, rs := moduleBlock(modulepkg.ModuleInstalledSig, event)
	err := NewModuleProcessor(zap.NewNop(), &failingModules{statsErr: errDisk}).ProcessModuleEventsFromBlock(ctx, b, rs)
	require.ErrorIs(t, err, errDisk, "a storage error fails the block")

	b, rs = moduleBlock(modulepkg.ModuleInstalledSig, event[:10])
	require.NoError(t, NewModuleProcessor(zap.NewNop(), &failingModules{statsErr: errDisk}).ProcessModuleEventsFromBlock(ctx, b, rs),
		"an event that does not decode is skipped")

	b, rs = moduleBlock(modulepkg.ModuleUninstalledSig, event)
	require.NoError(t, NewModuleProcessor(zap.NewNop(), &failingModules{removeErr: port.ErrNotFound}).ProcessModuleEventsFromBlock(ctx, b, rs),
		"uninstalling a module installed before indexing started is skipped")
	require.ErrorIs(t, NewModuleProcessor(zap.NewNop(), &failingModules{removeErr: errDisk}).ProcessModuleEventsFromBlock(ctx, b, rs), errDisk)
}
