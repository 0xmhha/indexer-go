package storage

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

// RPCClient interface for querying balance from RPC
// This allows using any client implementation that provides BalanceAt method
type RPCClient interface {
	BalanceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (*big.Int, error)
}

// GenesisBalanceConfigurer is implemented by storages that can initialize
// genesis allocation balances lazily from RPC.
type GenesisBalanceConfigurer interface {
	SetGenesisBalanceResolver(client RPCClient)
}

// genesisLookupMaxBlock limits lazy genesis lookups to queries about early
// blocks, as the former GenesisInitializingStorage wrapper did.
const genesisLookupMaxBlock = 1000

// SetGenesisBalanceResolver enables lazy genesis allocation initialization:
// when GetAddressBalance finds a zero balance with no history for an early
// block, it asks client for the balance at block 0 and stores it if non-zero.
//
// This replaces the GenesisInitializingStorage wrapper, which hid every
// optional interface PebbleStorage implements outside Storage.
func (s *PebbleStorage) SetGenesisBalanceResolver(client RPCClient) {
	s.genesisMu.Lock()
	defer s.genesisMu.Unlock()
	s.genesisClient = client
	if s.genesisTried == nil {
		s.genesisTried = make(map[common.Address]bool)
	}
}

// GetAddressBalance returns the balance of addr at blockNumber (0 = latest),
// initializing a genesis allocation from RPC when a resolver is set.
func (s *PebbleStorage) GetAddressBalance(ctx context.Context, addr common.Address, blockNumber uint64) (*big.Int, error) {
	balance, err := s.getAddressBalance(ctx, addr, blockNumber)
	if err != nil || balance.Sign() != 0 || blockNumber >= genesisLookupMaxBlock {
		return balance, err
	}
	return s.maybeInitGenesisBalance(ctx, addr, blockNumber, balance), nil
}

// maybeInitGenesisBalance performs the lazy genesis lookup. Failures are
// logged and the stored balance is returned, as before.
func (s *PebbleStorage) maybeInitGenesisBalance(ctx context.Context, addr common.Address, blockNumber uint64, balance *big.Int) *big.Int {
	s.genesisMu.Lock()
	client := s.genesisClient
	tried := s.genesisTried[addr]
	s.genesisMu.Unlock()
	if client == nil || tried || s.boundTx(ctx).genesisTried(addr) {
		return balance
	}
	// Remember the attempt. Inside a block transaction it is staged and
	// published on commit, so a rolled back block is retried in full.
	s.markGenesisTried(ctx, addr)

	history, err := s.GetBalanceHistory(ctx, addr, 0, blockNumber, 1, 0)
	if err != nil {
		s.logger.Debug("failed to check balance history", zap.String("address", addr.Hex()), zap.Error(err))
		return balance
	}
	if len(history) > 0 {
		return balance
	}

	rpcBalance, err := client.BalanceAt(ctx, addr, big.NewInt(0))
	if err != nil {
		s.logger.Debug("failed to fetch genesis balance from RPC", zap.String("address", addr.Hex()), zap.Error(err))
		return balance
	}
	if rpcBalance.Sign() == 0 {
		return balance
	}

	s.logger.Info("auto-initializing genesis allocation balance",
		zap.String("address", addr.Hex()),
		zap.String("balance", rpcBalance.String()))

	// Outside a block transaction this write must not interleave with an
	// open block's read-modify-write of the same balance.
	if s.boundTx(ctx) == nil {
		s.writeMu.Lock()
		defer s.writeMu.Unlock()
	}
	if err := s.SetBalance(ctx, addr, 0, rpcBalance); err != nil {
		s.logger.Error("failed to initialize genesis balance in storage", zap.String("address", addr.Hex()), zap.Error(err))
	}
	return rpcBalance
}

func (s *PebbleStorage) markGenesisTried(ctx context.Context, addr common.Address) {
	if tx := s.boundTx(ctx); tx != nil {
		if tx.genesisSeen == nil {
			tx.genesisSeen = make(map[common.Address]bool)
		}
		tx.genesisSeen[addr] = true
		return
	}
	s.genesisMu.Lock()
	s.genesisTried[addr] = true
	s.genesisMu.Unlock()
}

// genesisTried reports whether addr was already looked up in this block.
func (tx *BlockTx) genesisTried(addr common.Address) bool {
	return tx != nil && tx.genesisSeen[addr]
}

// publishGenesisTried merges the block's lookups into the storage memo.
func (tx *BlockTx) publishGenesisTried() {
	if len(tx.genesisSeen) == 0 {
		return
	}
	tx.s.genesisMu.Lock()
	defer tx.s.genesisMu.Unlock()
	if tx.s.genesisTried == nil {
		tx.s.genesisTried = make(map[common.Address]bool)
	}
	for addr := range tx.genesisSeen {
		tx.s.genesisTried[addr] = true
	}
}
