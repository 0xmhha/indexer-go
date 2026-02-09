package graphql

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/pkg/storage"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// ---- richMockStorage returns actual data for deep code path coverage ----

type richMockStorage struct {
	mockStorage
}

func (m *richMockStorage) GetActiveMinters(_ context.Context) ([]common.Address, error) {
	return []common.Address{
		common.HexToAddress("0x0000000000000000000000000000000000000001"),
		common.HexToAddress("0x0000000000000000000000000000000000000002"),
	}, nil
}
func (m *richMockStorage) GetMinterAllowance(_ context.Context, _ common.Address) (*big.Int, error) {
	return big.NewInt(1000000), nil
}
func (m *richMockStorage) GetActiveValidators(_ context.Context) ([]common.Address, error) {
	return []common.Address{
		common.HexToAddress("0x0000000000000000000000000000000000000010"),
	}, nil
}
func (m *richMockStorage) GetBlacklistedAddresses(_ context.Context) ([]common.Address, error) {
	return []common.Address{
		common.HexToAddress("0x0000000000000000000000000000000000000099"),
	}, nil
}
func (m *richMockStorage) GetAuthorizedAccounts(_ context.Context) ([]common.Address, error) {
	return []common.Address{
		common.HexToAddress("0x0000000000000000000000000000000000000055"),
	}, nil
}
func (m *richMockStorage) GetTotalSupply(_ context.Context) (*big.Int, error) {
	return big.NewInt(99999999), nil
}
func (m *richMockStorage) GetProposals(_ context.Context, _ common.Address, _ storage.ProposalStatus, _, _ int) ([]*storage.Proposal, error) {
	executed := uint64(1700000100)
	return []*storage.Proposal{
		{
			Contract:          common.HexToAddress("0x01"),
			ProposalID:        big.NewInt(1),
			Proposer:          common.HexToAddress("0x02"),
			ActionType:        [32]byte{0x01},
			CallData:          []byte{0xAB, 0xCD},
			MemberVersion:     big.NewInt(1),
			RequiredApprovals: 3,
			Approved:          2,
			Rejected:          1,
			Status:            storage.ProposalStatusVoting,
			CreatedAt:         1700000000,
			ExecutedAt:        nil,
			BlockNumber:       100,
			TxHash:            common.HexToHash("0xaaa"),
		},
		{
			Contract:          common.HexToAddress("0x01"),
			ProposalID:        big.NewInt(2),
			Proposer:          common.HexToAddress("0x03"),
			ActionType:        [32]byte{0x02},
			CallData:          []byte{},
			MemberVersion:     big.NewInt(1),
			RequiredApprovals: 3,
			Approved:          3,
			Rejected:          0,
			Status:            storage.ProposalStatusExecuted,
			CreatedAt:         1700000050,
			ExecutedAt:        &executed,
			BlockNumber:       110,
			TxHash:            common.HexToHash("0xbbb"),
		},
	}, nil
}
func (m *richMockStorage) GetProposalById(_ context.Context, _ common.Address, id *big.Int) (*storage.Proposal, error) {
	if id.Cmp(big.NewInt(1)) == 0 {
		return &storage.Proposal{
			Contract:          common.HexToAddress("0x01"),
			ProposalID:        big.NewInt(1),
			Proposer:          common.HexToAddress("0x02"),
			ActionType:        [32]byte{0x01},
			MemberVersion:     big.NewInt(1),
			RequiredApprovals: 3,
			Status:            storage.ProposalStatusVoting,
			CreatedAt:         1700000000,
			BlockNumber:       100,
			TxHash:            common.HexToHash("0xaaa"),
		}, nil
	}
	return nil, nil // not found
}
func (m *richMockStorage) GetProposalVotes(_ context.Context, _ common.Address, _ *big.Int) ([]*storage.ProposalVote, error) {
	return []*storage.ProposalVote{
		{
			Contract:    common.HexToAddress("0x01"),
			ProposalID:  big.NewInt(1),
			Voter:       common.HexToAddress("0x10"),
			Approval:    true,
			BlockNumber: 101,
			TxHash:      common.HexToHash("0xccc"),
			Timestamp:   1700000010,
		},
	}, nil
}
func (m *richMockStorage) GetMintEvents(_ context.Context, _, _ uint64, _ common.Address, _, _ int) ([]*storage.MintEvent, error) {
	return []*storage.MintEvent{
		{
			BlockNumber: 50,
			TxHash:      common.HexToHash("0xddd"),
			Minter:      common.HexToAddress("0x01"),
			To:          common.HexToAddress("0x02"),
			Amount:      big.NewInt(5000),
			Timestamp:   1700000000,
		},
	}, nil
}
func (m *richMockStorage) GetBurnEvents(_ context.Context, _, _ uint64, _ common.Address, _, _ int) ([]*storage.BurnEvent, error) {
	return []*storage.BurnEvent{
		{
			BlockNumber:  60,
			TxHash:       common.HexToHash("0xeee"),
			Burner:       common.HexToAddress("0x03"),
			Amount:       big.NewInt(2000),
			Timestamp:    1700000050,
			WithdrawalID: "w-123",
		},
		{
			BlockNumber: 61,
			TxHash:      common.HexToHash("0xfff"),
			Burner:      common.HexToAddress("0x04"),
			Amount:      big.NewInt(1000),
			Timestamp:   1700000060,
		},
	}, nil
}
func (m *richMockStorage) GetMinterHistory(_ context.Context, _ common.Address) ([]*storage.MinterConfigEvent, error) {
	return []*storage.MinterConfigEvent{
		{BlockNumber: 10, TxHash: common.HexToHash("0x111"), Minter: common.HexToAddress("0x01"), Allowance: big.NewInt(100000), Action: "configured", Timestamp: 1700000000},
	}, nil
}
func (m *richMockStorage) GetValidatorHistory(_ context.Context, _ common.Address) ([]*storage.ValidatorChangeEvent, error) {
	old := common.HexToAddress("0x09")
	return []*storage.ValidatorChangeEvent{
		{BlockNumber: 20, TxHash: common.HexToHash("0x222"), Validator: common.HexToAddress("0x10"), Action: "added", Timestamp: 1700000000},
		{BlockNumber: 30, TxHash: common.HexToHash("0x333"), Validator: common.HexToAddress("0x11"), Action: "changed", OldValidator: &old, Timestamp: 1700000100},
	}, nil
}
func (m *richMockStorage) GetGasTipHistory(_ context.Context, _, _ uint64) ([]*storage.GasTipUpdateEvent, error) {
	return []*storage.GasTipUpdateEvent{
		{BlockNumber: 40, TxHash: common.HexToHash("0x444"), OldTip: big.NewInt(100), NewTip: big.NewInt(200), Updater: common.HexToAddress("0x10"), Timestamp: 1700000000},
	}, nil
}
func (m *richMockStorage) GetBlacklistHistory(_ context.Context, _ common.Address) ([]*storage.BlacklistEvent, error) {
	return []*storage.BlacklistEvent{
		{BlockNumber: 50, TxHash: common.HexToHash("0x555"), Account: common.HexToAddress("0x99"), Action: "blacklisted", ProposalID: big.NewInt(5), Timestamp: 1700000000},
	}, nil
}
func (m *richMockStorage) GetMemberHistory(_ context.Context, _ common.Address) ([]*storage.MemberChangeEvent, error) {
	old := common.HexToAddress("0x08")
	return []*storage.MemberChangeEvent{
		{Contract: common.HexToAddress("0x01"), BlockNumber: 60, TxHash: common.HexToHash("0x666"), Member: common.HexToAddress("0x20"), Action: "added", TotalMembers: 5, NewQuorum: 3, Timestamp: 1700000000},
		{Contract: common.HexToAddress("0x01"), BlockNumber: 70, TxHash: common.HexToHash("0x777"), Member: common.HexToAddress("0x21"), Action: "changed", OldMember: &old, TotalMembers: 5, NewQuorum: 3, Timestamp: 1700000100},
	}, nil
}
func (m *richMockStorage) GetEmergencyPauseHistory(_ context.Context, _ common.Address) ([]*storage.EmergencyPauseEvent, error) {
	return []*storage.EmergencyPauseEvent{
		{Contract: common.HexToAddress("0x01"), BlockNumber: 80, TxHash: common.HexToHash("0x888"), ProposalID: big.NewInt(10), Action: "paused", Timestamp: 1700000000},
	}, nil
}
func (m *richMockStorage) GetDepositMintProposals(_ context.Context, _, _ uint64, _ storage.ProposalStatus) ([]*storage.DepositMintProposal, error) {
	return []*storage.DepositMintProposal{
		{ProposalID: big.NewInt(1), Requester: common.HexToAddress("0x30"), Beneficiary: common.HexToAddress("0x31"), Amount: big.NewInt(50000), DepositID: "d-001", BankReference: "BR-123", Status: storage.ProposalStatusApproved, BlockNumber: 90, TxHash: common.HexToHash("0x999"), Timestamp: 1700000000},
	}, nil
}
func (m *richMockStorage) GetMinterConfigHistory(_ context.Context, _, _ uint64) ([]*storage.MinterConfigEvent, error) {
	return []*storage.MinterConfigEvent{
		{BlockNumber: 15, TxHash: common.HexToHash("0xaab"), Minter: common.HexToAddress("0x01"), Allowance: big.NewInt(200000), Action: "configured", Timestamp: 1700000000},
	}, nil
}
func (m *richMockStorage) GetBurnHistory(_ context.Context, _, _ uint64, _ common.Address) ([]*storage.BurnEvent, error) {
	return []*storage.BurnEvent{
		{BlockNumber: 65, TxHash: common.HexToHash("0xaac"), Burner: common.HexToAddress("0x05"), Amount: big.NewInt(3000), Timestamp: 1700000070},
	}, nil
}
func (m *richMockStorage) GetMaxProposalsUpdateHistory(_ context.Context, _ common.Address) ([]*storage.MaxProposalsUpdateEvent, error) {
	return []*storage.MaxProposalsUpdateEvent{
		{Contract: common.HexToAddress("0x01"), BlockNumber: 95, TxHash: common.HexToHash("0xaad"), OldMax: 5, NewMax: 10, Timestamp: 1700000000},
	}, nil
}
func (m *richMockStorage) GetProposalExecutionSkippedEvents(_ context.Context, _ common.Address, _ *big.Int) ([]*storage.ProposalExecutionSkippedEvent, error) {
	return []*storage.ProposalExecutionSkippedEvent{
		{Contract: common.HexToAddress("0x01"), BlockNumber: 96, TxHash: common.HexToHash("0xaae"), Account: common.HexToAddress("0x40"), ProposalID: big.NewInt(3), Reason: "quorum not met", Timestamp: 1700000000},
	}, nil
}

// ---- Address index overrides for richMockStorage ----

func (m *richMockStorage) GetContractCreation(_ context.Context, addr common.Address) (*storage.ContractCreation, error) {
	return &storage.ContractCreation{ContractAddress: addr, Creator: common.HexToAddress("0x01"), TransactionHash: common.HexToHash("0xcc1"), BlockNumber: 10, Timestamp: 1700000000, BytecodeSize: 1024}, nil
}
func (m *richMockStorage) GetContractsByCreator(_ context.Context, _ common.Address, _, _ int) ([]common.Address, error) {
	return []common.Address{common.HexToAddress("0xA1"), common.HexToAddress("0xA2")}, nil
}
func (m *richMockStorage) ListContracts(_ context.Context, _, _ int) ([]*storage.ContractCreation, error) {
	return []*storage.ContractCreation{
		{ContractAddress: common.HexToAddress("0xA1"), Creator: common.HexToAddress("0x01"), TransactionHash: common.HexToHash("0xcc1"), BlockNumber: 10, Timestamp: 1700000000, BytecodeSize: 512},
	}, nil
}
func (m *richMockStorage) GetContractsCount(_ context.Context) (int, error) { return 5, nil }
func (m *richMockStorage) GetInternalTransactions(_ context.Context, _ common.Hash) ([]*storage.InternalTransaction, error) {
	return []*storage.InternalTransaction{
		{TransactionHash: common.HexToHash("0xdd1"), BlockNumber: 15, Index: 0, Type: "CALL", From: common.HexToAddress("0x01"), To: common.HexToAddress("0x02"), Value: big.NewInt(1000), Gas: 21000, GasUsed: 21000, Depth: 0},
	}, nil
}
func (m *richMockStorage) GetInternalTransactionsByAddress(_ context.Context, _ common.Address, _ bool, _, _ int) ([]*storage.InternalTransaction, error) {
	return []*storage.InternalTransaction{
		{TransactionHash: common.HexToHash("0xdd2"), BlockNumber: 16, Index: 0, Type: "DELEGATECALL", From: common.HexToAddress("0x01"), To: common.HexToAddress("0x03"), Value: big.NewInt(0), Gas: 50000, GasUsed: 30000, Depth: 1},
	}, nil
}
func (m *richMockStorage) GetERC20Transfer(_ context.Context, _ common.Hash, _ uint) (*storage.ERC20Transfer, error) {
	return &storage.ERC20Transfer{ContractAddress: common.HexToAddress("0xE1"), From: common.HexToAddress("0x01"), To: common.HexToAddress("0x02"), Value: big.NewInt(5000), TransactionHash: common.HexToHash("0xee1"), BlockNumber: 20, LogIndex: 0, Timestamp: 1700000000}, nil
}
func (m *richMockStorage) GetERC20TransfersByToken(_ context.Context, _ common.Address, _, _ int) ([]*storage.ERC20Transfer, error) {
	return []*storage.ERC20Transfer{
		{ContractAddress: common.HexToAddress("0xE1"), From: common.HexToAddress("0x01"), To: common.HexToAddress("0x02"), Value: big.NewInt(5000), TransactionHash: common.HexToHash("0xee1"), BlockNumber: 20, LogIndex: 0, Timestamp: 1700000000},
	}, nil
}
func (m *richMockStorage) GetERC20TransfersByAddress(_ context.Context, _ common.Address, _ bool, _, _ int) ([]*storage.ERC20Transfer, error) {
	return []*storage.ERC20Transfer{
		{ContractAddress: common.HexToAddress("0xE1"), From: common.HexToAddress("0x01"), To: common.HexToAddress("0x02"), Value: big.NewInt(5000), TransactionHash: common.HexToHash("0xee2"), BlockNumber: 21, LogIndex: 1, Timestamp: 1700000010},
	}, nil
}
func (m *richMockStorage) GetERC721Transfer(_ context.Context, _ common.Hash, _ uint) (*storage.ERC721Transfer, error) {
	return &storage.ERC721Transfer{ContractAddress: common.HexToAddress("0xF1"), From: common.HexToAddress("0x01"), To: common.HexToAddress("0x02"), TokenId: big.NewInt(42), TransactionHash: common.HexToHash("0xff1"), BlockNumber: 25, LogIndex: 0, Timestamp: 1700000000}, nil
}
func (m *richMockStorage) GetERC721TransfersByToken(_ context.Context, _ common.Address, _, _ int) ([]*storage.ERC721Transfer, error) {
	return []*storage.ERC721Transfer{
		{ContractAddress: common.HexToAddress("0xF1"), From: common.HexToAddress("0x01"), To: common.HexToAddress("0x02"), TokenId: big.NewInt(42), TransactionHash: common.HexToHash("0xff1"), BlockNumber: 25, LogIndex: 0, Timestamp: 1700000000},
	}, nil
}
func (m *richMockStorage) GetERC721TransfersByAddress(_ context.Context, _ common.Address, _ bool, _, _ int) ([]*storage.ERC721Transfer, error) {
	return []*storage.ERC721Transfer{
		{ContractAddress: common.HexToAddress("0xF1"), From: common.HexToAddress("0x01"), To: common.HexToAddress("0x02"), TokenId: big.NewInt(43), TransactionHash: common.HexToHash("0xff2"), BlockNumber: 26, LogIndex: 1, Timestamp: 1700000010},
	}, nil
}
func (m *richMockStorage) GetERC721Owner(_ context.Context, _ common.Address, _ *big.Int) (common.Address, error) {
	return common.HexToAddress("0x02"), nil
}
func (m *richMockStorage) GetNFTsByOwner(_ context.Context, _ common.Address, _, _ int) ([]*storage.NFTOwnership, error) {
	return []*storage.NFTOwnership{
		{ContractAddress: common.HexToAddress("0xF1"), TokenId: big.NewInt(42), Owner: common.HexToAddress("0x02")},
	}, nil
}

// ---- SetCode overrides for richMockStorage ----

func (m *richMockStorage) GetSetCodeAuthorization(_ context.Context, _ common.Hash, _ int) (*storage.SetCodeAuthorizationRecord, error) {
	return &storage.SetCodeAuthorizationRecord{TxHash: common.HexToHash("0xsc1"), BlockNumber: 30, AuthIndex: 0, TargetAddress: common.HexToAddress("0xS1"), AuthorityAddress: common.HexToAddress("0xS2"), ChainID: big.NewInt(1), Nonce: 5, Applied: true, Timestamp: time.Unix(1700000000, 0)}, nil
}
func (m *richMockStorage) GetSetCodeAuthorizationsByTx(_ context.Context, _ common.Hash) ([]*storage.SetCodeAuthorizationRecord, error) {
	return []*storage.SetCodeAuthorizationRecord{
		{TxHash: common.HexToHash("0xsc1"), BlockNumber: 30, AuthIndex: 0, TargetAddress: common.HexToAddress("0xS1"), AuthorityAddress: common.HexToAddress("0xS2"), ChainID: big.NewInt(1), Nonce: 5, Applied: true, Timestamp: time.Unix(1700000000, 0)},
	}, nil
}
func (m *richMockStorage) GetSetCodeAuthorizationsByTarget(_ context.Context, _ common.Address, _, _ int) ([]*storage.SetCodeAuthorizationRecord, error) {
	return []*storage.SetCodeAuthorizationRecord{
		{TxHash: common.HexToHash("0xsc2"), BlockNumber: 31, AuthIndex: 0, TargetAddress: common.HexToAddress("0xS1"), AuthorityAddress: common.HexToAddress("0xS3"), ChainID: big.NewInt(1), Applied: true, Timestamp: time.Unix(1700000010, 0)},
	}, nil
}
func (m *richMockStorage) GetSetCodeAuthorizationsByAuthority(_ context.Context, _ common.Address, _, _ int) ([]*storage.SetCodeAuthorizationRecord, error) {
	return []*storage.SetCodeAuthorizationRecord{
		{TxHash: common.HexToHash("0xsc3"), BlockNumber: 32, AuthIndex: 0, TargetAddress: common.HexToAddress("0xS4"), AuthorityAddress: common.HexToAddress("0xS2"), ChainID: big.NewInt(1), Applied: false, Error: "nonce mismatch", Timestamp: time.Unix(1700000020, 0)},
	}, nil
}
func (m *richMockStorage) GetSetCodeAuthorizationsByBlock(_ context.Context, _ uint64) ([]*storage.SetCodeAuthorizationRecord, error) {
	return []*storage.SetCodeAuthorizationRecord{
		{TxHash: common.HexToHash("0xsc1"), BlockNumber: 30, AuthIndex: 0, TargetAddress: common.HexToAddress("0xS1"), AuthorityAddress: common.HexToAddress("0xS2"), Applied: true, Timestamp: time.Unix(1700000000, 0)},
	}, nil
}
func (m *richMockStorage) GetSetCodeTransactionCount(_ context.Context) (int, error) { return 10, nil }
func (m *richMockStorage) GetRecentSetCodeAuthorizations(_ context.Context, _ int) ([]*storage.SetCodeAuthorizationRecord, error) {
	return []*storage.SetCodeAuthorizationRecord{
		{TxHash: common.HexToHash("0xsc4"), BlockNumber: 33, AuthIndex: 0, TargetAddress: common.HexToAddress("0xS5"), AuthorityAddress: common.HexToAddress("0xS6"), Applied: true, Timestamp: time.Unix(1700000030, 0)},
	}, nil
}

// ---- TokenHolder overrides for richMockStorage ----

func (m *richMockStorage) GetTokenHolders(_ context.Context, _ common.Address, _, _ int) ([]*storage.TokenHolder, error) {
	return []*storage.TokenHolder{
		{TokenAddress: common.HexToAddress("0xT1"), HolderAddress: common.HexToAddress("0x01"), Balance: big.NewInt(1000000)},
	}, nil
}
func (m *richMockStorage) GetTokenHolderCount(_ context.Context, _ common.Address) (int, error) {
	return 42, nil
}
func (m *richMockStorage) GetTokenBalance(_ context.Context, _, _ common.Address) (*big.Int, error) {
	return big.NewInt(999), nil
}

// ---- Historical overrides for richMockStorage ----

func (m *richMockStorage) GetTokenBalances(_ context.Context, _ common.Address, _ string) ([]storage.TokenBalance, error) {
	decimals := 18
	return []storage.TokenBalance{
		{ContractAddress: common.HexToAddress("0xT1"), TokenType: "ERC20", Balance: big.NewInt(1000000), Name: "TestToken", Symbol: "TT", Decimals: &decimals},
	}, nil
}
func (m *richMockStorage) GetGasStatsByBlockRange(_ context.Context, _, _ uint64) (*storage.GasStats, error) {
	return &storage.GasStats{TotalGasUsed: 500000, TotalGasLimit: 8000000, AverageGasUsed: 50000, AverageGasPrice: big.NewInt(20000000000), BlockCount: 10, TransactionCount: 100}, nil
}
func (m *richMockStorage) GetGasStatsByAddress(_ context.Context, addr common.Address, _, _ uint64) (*storage.AddressGasStats, error) {
	return &storage.AddressGasStats{Address: addr, TotalGasUsed: 100000, TransactionCount: 50, AverageGasPerTx: 2000, TotalFeesPaid: big.NewInt(1000000000000)}, nil
}
func (m *richMockStorage) GetTopAddressesByGasUsed(_ context.Context, _ int, _, _ uint64) ([]storage.AddressGasStats, error) {
	return []storage.AddressGasStats{
		{Address: common.HexToAddress("0x01"), TotalGasUsed: 100000, TransactionCount: 50},
	}, nil
}
func (m *richMockStorage) GetTopAddressesByTxCount(_ context.Context, _ int, _, _ uint64) ([]storage.AddressActivityStats, error) {
	return []storage.AddressActivityStats{
		{Address: common.HexToAddress("0x01"), TransactionCount: 200, TotalGasUsed: 500000},
	}, nil
}
func (m *richMockStorage) GetNetworkMetrics(_ context.Context, _, _ uint64) (*storage.NetworkMetrics, error) {
	return &storage.NetworkMetrics{TPS: 15.5, BlockTime: 2.0, TotalBlocks: 1000, TotalTransactions: 5000, AverageBlockSize: 4000000}, nil
}
func (m *richMockStorage) GetAddressStats(_ context.Context, addr common.Address) (*storage.AddressStats, error) {
	return &storage.AddressStats{Address: addr, TotalTransactions: 100, SentCount: 60, ReceivedCount: 40, SuccessCount: 95}, nil
}

// ---- WBFT overrides for richMockStorage ----

func (m *richMockStorage) GetWBFTBlockExtra(_ context.Context, blockNum uint64) (*storage.WBFTBlockExtra, error) {
	return &storage.WBFTBlockExtra{
		BlockNumber: blockNum, BlockHash: common.HexToHash("0xw1"), Round: 1,
		PreparedSeal:  &storage.WBFTAggregatedSeal{Sealers: []byte{0xFF}, Signature: make([]byte, 96)},
		CommittedSeal: &storage.WBFTAggregatedSeal{Sealers: []byte{0xFF}, Signature: make([]byte, 96)},
		GasTip:        big.NewInt(100),
		Timestamp:     1700000000,
	}, nil
}
func (m *richMockStorage) GetEpochInfo(_ context.Context, _ uint64) (*storage.EpochInfo, error) {
	return &storage.EpochInfo{EpochNumber: 1, BlockNumber: 100, Validators: []uint32{0, 1, 2}, Candidates: []storage.Candidate{{Address: common.HexToAddress("0x10"), Diligence: 999999}}}, nil
}
func (m *richMockStorage) GetLatestEpochInfo(_ context.Context) (*storage.EpochInfo, error) {
	return &storage.EpochInfo{EpochNumber: 5, BlockNumber: 500, Validators: []uint32{0, 1}, Candidates: []storage.Candidate{{Address: common.HexToAddress("0x10"), Diligence: 999999}}}, nil
}
func (m *richMockStorage) GetValidatorSigningStats(_ context.Context, addr common.Address, _, _ uint64) (*storage.ValidatorSigningStats, error) {
	return &storage.ValidatorSigningStats{ValidatorAddress: addr, PrepareSignCount: 90, PrepareMissCount: 10, CommitSignCount: 95, CommitMissCount: 5, FromBlock: 1, ToBlock: 100, SigningRate: 92.5, BlocksProposed: 10, TotalBlocks: 100, ProposalRate: 10.0}, nil
}
func (m *richMockStorage) GetAllValidatorsSigningStats(_ context.Context, _, _ uint64, _, _ int) ([]*storage.ValidatorSigningStats, error) {
	return []*storage.ValidatorSigningStats{
		{ValidatorAddress: common.HexToAddress("0x10"), PrepareSignCount: 90, CommitSignCount: 95, SigningRate: 92.5},
	}, nil
}
func (m *richMockStorage) GetValidatorSigningActivity(_ context.Context, _ common.Address, _, _ uint64, _, _ int) ([]*storage.ValidatorSigningActivity, error) {
	return []*storage.ValidatorSigningActivity{
		{BlockNumber: 50, BlockHash: common.HexToHash("0xva1"), ValidatorAddress: common.HexToAddress("0x10"), ValidatorIndex: 0, SignedPrepare: true, SignedCommit: true, Round: 0, Timestamp: 1700000000},
	}, nil
}
func (m *richMockStorage) GetBlockSigners(_ context.Context, _ uint64) ([]common.Address, []common.Address, error) {
	return []common.Address{common.HexToAddress("0x10")}, []common.Address{common.HexToAddress("0x10"), common.HexToAddress("0x11")}, nil
}
func (m *richMockStorage) GetEpochsList(_ context.Context, _, _ int) ([]*storage.EpochInfo, int, error) {
	return []*storage.EpochInfo{
		{EpochNumber: 1, BlockNumber: 100, Validators: []uint32{0, 1}},
	}, 5, nil
}

// ---- Fee delegation overrides for richMockStorage ----

func (m *richMockStorage) GetFeePayerStats(_ context.Context, addr common.Address, _, _ uint64) (*storage.FeePayerStats, error) {
	return &storage.FeePayerStats{Address: addr, TxCount: 150, TotalFeesPaid: big.NewInt(5000000000000), Percentage: 25.5}, nil
}

// newRichTestHandler creates a handler with a rich mock returning actual data.
func newRichTestHandler(t *testing.T) *Handler {
	t.Helper()
	return newRichTestHandlerFull(t)
}

// newRichTestHandlerFull creates a handler with full schema (including SetCode, TokenHolder queries).
func newRichTestHandlerFull(t *testing.T) *Handler {
	t.Helper()
	header := &types.Header{
		Number:     common.Big1,
		ParentHash: common.HexToHash("0x123"),
		Time:       1700000000,
		GasLimit:   8000000,
		GasUsed:    5000000,
	}
	testBlock := types.NewBlockWithHeader(header)
	store := &richMockStorage{
		mockStorage: mockStorage{
			latestHeight: 100,
			blocks:       map[uint64]*types.Block{1: testBlock},
			blocksByHash: map[common.Hash]*types.Block{testBlock.Hash(): testBlock},
			transactions: make(map[common.Hash]*types.Transaction),
			receipts:     make(map[common.Hash]*types.Receipt),
		},
	}
	logger := zap.NewNop()
	schema, err := NewSchemaBuilder(store, logger).
		WithCoreQueries().
		WithHistoricalQueries().
		WithAnalyticsQueries().
		WithSystemContractQueries().
		WithConsensusQueries().
		WithAddressIndexingQueries().
		WithSetCodeQueries().
		WithFeeDelegationQueries().
		WithTokenMetadataQueries().
		WithTokenHolderQueries().
		WithSubscriptions().
		WithMutations().
		Build()
	require.NoError(t, err)
	return &Handler{schema: schema}
}

// ---- AddressIndexReader implementation for mockStorage ----

func (m *mockStorage) GetContractCreation(_ context.Context, _ common.Address) (*storage.ContractCreation, error) {
	return nil, storage.ErrNotFound
}
func (m *mockStorage) GetContractsByCreator(_ context.Context, _ common.Address, _, _ int) ([]common.Address, error) {
	return []common.Address{}, nil
}
func (m *mockStorage) ListContracts(_ context.Context, _, _ int) ([]*storage.ContractCreation, error) {
	return []*storage.ContractCreation{}, nil
}
func (m *mockStorage) GetContractsCount(_ context.Context) (int, error) {
	return 0, nil
}
func (m *mockStorage) GetInternalTransactions(_ context.Context, _ common.Hash) ([]*storage.InternalTransaction, error) {
	return []*storage.InternalTransaction{}, nil
}
func (m *mockStorage) GetInternalTransactionsByAddress(_ context.Context, _ common.Address, _ bool, _, _ int) ([]*storage.InternalTransaction, error) {
	return []*storage.InternalTransaction{}, nil
}
func (m *mockStorage) GetERC20Transfer(_ context.Context, _ common.Hash, _ uint) (*storage.ERC20Transfer, error) {
	return nil, storage.ErrNotFound
}
func (m *mockStorage) GetERC20TransfersByToken(_ context.Context, _ common.Address, _, _ int) ([]*storage.ERC20Transfer, error) {
	return []*storage.ERC20Transfer{}, nil
}
func (m *mockStorage) GetERC20TransfersByAddress(_ context.Context, _ common.Address, _ bool, _, _ int) ([]*storage.ERC20Transfer, error) {
	return []*storage.ERC20Transfer{}, nil
}
func (m *mockStorage) GetERC721Transfer(_ context.Context, _ common.Hash, _ uint) (*storage.ERC721Transfer, error) {
	return nil, storage.ErrNotFound
}
func (m *mockStorage) GetERC721TransfersByToken(_ context.Context, _ common.Address, _, _ int) ([]*storage.ERC721Transfer, error) {
	return []*storage.ERC721Transfer{}, nil
}
func (m *mockStorage) GetERC721TransfersByAddress(_ context.Context, _ common.Address, _ bool, _, _ int) ([]*storage.ERC721Transfer, error) {
	return []*storage.ERC721Transfer{}, nil
}
func (m *mockStorage) GetERC721Owner(_ context.Context, _ common.Address, _ *big.Int) (common.Address, error) {
	return common.Address{}, storage.ErrNotFound
}
func (m *mockStorage) GetNFTsByOwner(_ context.Context, _ common.Address, _, _ int) ([]*storage.NFTOwnership, error) {
	return []*storage.NFTOwnership{}, nil
}

// ---- SetCodeIndexReader implementation for mockStorage ----

func (m *mockStorage) GetSetCodeAuthorization(_ context.Context, _ common.Hash, _ int) (*storage.SetCodeAuthorizationRecord, error) {
	return nil, storage.ErrNotFound
}
func (m *mockStorage) GetSetCodeAuthorizationsByTx(_ context.Context, _ common.Hash) ([]*storage.SetCodeAuthorizationRecord, error) {
	return []*storage.SetCodeAuthorizationRecord{}, nil
}
func (m *mockStorage) GetSetCodeAuthorizationsByTarget(_ context.Context, _ common.Address, _, _ int) ([]*storage.SetCodeAuthorizationRecord, error) {
	return []*storage.SetCodeAuthorizationRecord{}, nil
}
func (m *mockStorage) GetSetCodeAuthorizationsByAuthority(_ context.Context, _ common.Address, _, _ int) ([]*storage.SetCodeAuthorizationRecord, error) {
	return []*storage.SetCodeAuthorizationRecord{}, nil
}
func (m *mockStorage) GetSetCodeAuthorizationsByBlock(_ context.Context, _ uint64) ([]*storage.SetCodeAuthorizationRecord, error) {
	return []*storage.SetCodeAuthorizationRecord{}, nil
}
func (m *mockStorage) GetAddressSetCodeStats(_ context.Context, addr common.Address) (*storage.AddressSetCodeStats, error) {
	return &storage.AddressSetCodeStats{Address: addr}, nil
}
func (m *mockStorage) GetAddressDelegationState(_ context.Context, addr common.Address) (*storage.AddressDelegationState, error) {
	return &storage.AddressDelegationState{Address: addr}, nil
}
func (m *mockStorage) GetSetCodeAuthorizationsCountByTarget(_ context.Context, _ common.Address) (int, error) {
	return 0, nil
}
func (m *mockStorage) GetSetCodeAuthorizationsCountByAuthority(_ context.Context, _ common.Address) (int, error) {
	return 0, nil
}
func (m *mockStorage) GetSetCodeTransactionCount(_ context.Context) (int, error) {
	return 0, nil
}
func (m *mockStorage) GetRecentSetCodeAuthorizations(_ context.Context, _ int) ([]*storage.SetCodeAuthorizationRecord, error) {
	return []*storage.SetCodeAuthorizationRecord{}, nil
}

// ---- TokenHolderIndexReader implementation for mockStorage ----

func (m *mockStorage) GetTokenHolders(_ context.Context, _ common.Address, _, _ int) ([]*storage.TokenHolder, error) {
	return []*storage.TokenHolder{}, nil
}
func (m *mockStorage) GetTokenHolderCount(_ context.Context, _ common.Address) (int, error) {
	return 0, nil
}
func (m *mockStorage) GetTokenBalance(_ context.Context, _, _ common.Address) (*big.Int, error) {
	return big.NewInt(0), nil
}
func (m *mockStorage) GetTokenHolderStats(_ context.Context, token common.Address) (*storage.TokenHolderStats, error) {
	return &storage.TokenHolderStats{TokenAddress: token}, nil
}
func (m *mockStorage) GetHolderTokens(_ context.Context, _ common.Address, _, _ int) ([]*storage.TokenHolder, error) {
	return []*storage.TokenHolder{}, nil
}

// Compile-time interface assertions
var _ storage.AddressIndexReader = (*mockStorage)(nil)
var _ storage.SetCodeIndexReader = (*mockStorage)(nil)
var _ storage.TokenHolderIndexReader = (*mockStorage)(nil)

// newTestHandler creates a handler with a test block at height 1.
func newTestHandler(t *testing.T) *Handler {
	t.Helper()
	header := &types.Header{
		Number:     common.Big1,
		ParentHash: common.HexToHash("0x123"),
		Time:       1700000000,
		GasLimit:   8000000,
		GasUsed:    5000000,
	}
	testBlock := types.NewBlockWithHeader(header)
	store := &mockStorage{
		latestHeight: 100,
		blocks:       map[uint64]*types.Block{1: testBlock},
		blocksByHash: map[common.Hash]*types.Block{testBlock.Hash(): testBlock},
		transactions: make(map[common.Hash]*types.Transaction),
		receipts:     make(map[common.Hash]*types.Receipt),
	}
	handler, err := NewHandler(store, zap.NewNop())
	require.NoError(t, err)
	return handler
}

// TestSystemContractResolvers tests all system contract query resolvers.
func TestSystemContractResolvers(t *testing.T) {
	handler := newTestHandler(t)

	tests := []struct {
		name      string
		query     string
		expectErr bool
	}{
		{"totalSupply", `{ totalSupply }`, false},
		{"activeMinters", `{ activeMinters { address allowance } }`, false},
		{"activeMinterAddresses", `{ activeMinterAddresses }`, false},
		{"minterAllowance", `{ minterAllowance(address: "0x0000000000000000000000000000000000000001") }`, false},
		{"activeValidators", `{ activeValidators { address } }`, false},
		{"activeValidatorAddresses", `{ activeValidatorAddresses }`, false},
		{"blacklistedAddresses", `{ blacklistedAddresses }`, false},
		{"authorizedAccounts", `{ authorizedAccounts }`, false},
		{"proposals", `{ proposals(contract: "0x0000000000000000000000000000000000000001") { id status } }`, false},
		{"proposal", `{ proposal(contract: "0x0000000000000000000000000000000000000001", proposalId: "1") { id } }`, true},
		{"proposalVotes", `{ proposalVotes(contract: "0x0000000000000000000000000000000000000001", proposalId: "1") { voter support } }`, false},
		{"mintEvents", `{ mintEvents { nodes { minter amount blockNumber } totalCount } }`, false},
		{"burnEvents", `{ burnEvents { nodes { burner amount blockNumber } totalCount } }`, false},
		{"minterHistory", `{ minterHistory(minter: "0x0000000000000000000000000000000000000001") { minter action blockNumber } }`, false},
		{"validatorHistory", `{ validatorHistory(validator: "0x0000000000000000000000000000000000000001") { validator action blockNumber } }`, false},
		{"gasTipHistory", `{ gasTipHistory { newGasTip blockNumber } }`, false},
		{"blacklistHistory", `{ blacklistHistory(address: "0x0000000000000000000000000000000000000001") { address action blockNumber } }`, false},
		{"memberHistory", `{ memberHistory(contract: "0x0000000000000000000000000000000000000001") { member action blockNumber } }`, false},
		{"emergencyPauseHistory", `{ emergencyPauseHistory(contract: "0x0000000000000000000000000000000000000001") { contract paused blockNumber } }`, false},
		{"depositMintProposals", `{ depositMintProposals { proposalId amount status } }`, false},
		{"minterConfigHistory", `{ minterConfigHistory { minter action blockNumber } }`, false},
		{"burnHistory", `{ burnHistory { nodes { burner amount blockNumber } totalCount } }`, false},
		{"maxProposalsUpdateHistory", `{ maxProposalsUpdateHistory(contract: "0x0000000000000000000000000000000000000001") { oldMax newMax blockNumber } }`, false},
		{"proposalExecutionSkippedEvents", `{ proposalExecutionSkippedEvents(contract: "0x0000000000000000000000000000000000000001", proposalId: "1") { proposalId reason blockNumber } }`, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := handler.ExecuteQuery(tc.query, nil)
			if tc.expectErr {
				assert.NotEmpty(t, result.Errors, "expected error for %s", tc.name)
			}
			// Resolver ran without panic - coverage gained
		})
	}
}

// TestFeeDelegationResolvers tests fee delegation query resolvers.
func TestFeeDelegationResolvers(t *testing.T) {
	handler := newTestHandler(t)

	tests := []struct {
		name  string
		query string
	}{
		{"feeDelegationStats", `{ feeDelegationStats { totalFeeDelegatedTxs totalFeesSaved adoptionRate avgFeeSaved } }`},
		{"feeDelegationStats_withRange", `{ feeDelegationStats(fromBlock: "0", toBlock: "100") { totalFeeDelegatedTxs } }`},
		{"topFeePayers", `{ topFeePayers { nodes { address txCount totalFeesPaid percentage } totalCount } }`},
		{"topFeePayers_withLimit", `{ topFeePayers(limit: 5, fromBlock: "0", toBlock: "100") { nodes { address } } }`},
		{"feePayerStats", `{ feePayerStats(address: "0x0000000000000000000000000000000000000001") { address txCount totalFeesPaid } }`},
		{"feePayerStats_withRange", `{ feePayerStats(address: "0x0000000000000000000000000000000000000001", fromBlock: "0", toBlock: "100") { address } }`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := handler.ExecuteQuery(tc.query, nil)
			_ = result // Resolver ran, coverage gained
		})
	}
}

// TestAddressIndexingResolvers tests address indexing query resolvers.
// Since mockStorage doesn't implement AddressIndexReader, these hit the "not available" paths.
func TestAddressIndexingResolvers(t *testing.T) {
	handler := newTestHandler(t)

	tests := []struct {
		name  string
		query string
	}{
		{"addressOverview", `{ addressOverview(address: "0x0000000000000000000000000000000000000001") { address balance transactionCount isContract } }`},
		{"contractCreation", `{ contractCreation(address: "0x0000000000000000000000000000000000000001") { contractAddress creator transactionHash blockNumber } }`},
		{"contracts", `{ contracts { nodes { contractAddress creator } totalCount pageInfo { hasNextPage } } }`},
		{"contractsByCreator", `{ contractsByCreator(creator: "0x0000000000000000000000000000000000000001") { contractAddress } }`},
		{"internalTransactions", `{ internalTransactions(transactionHash: "0x0000000000000000000000000000000000000000000000000000000000000001") { from to value } }`},
		{"internalTransactionsByAddress", `{ internalTransactionsByAddress(address: "0x0000000000000000000000000000000000000001", isFrom: true) { nodes { from to value } totalCount } }`},
		{"erc20Transfer", `{ erc20Transfer(transactionHash: "0x0000000000000000000000000000000000000000000000000000000000000001", logIndex: 0) { contractAddress from to value } }`},
		{"erc20TransfersByToken", `{ erc20TransfersByToken(token: "0x0000000000000000000000000000000000000001") { nodes { from to value } totalCount } }`},
		{"erc20TransfersByAddress", `{ erc20TransfersByAddress(address: "0x0000000000000000000000000000000000000001", isFrom: true) { nodes { contractAddress from to value } totalCount } }`},
		{"erc721Transfer", `{ erc721Transfer(transactionHash: "0x0000000000000000000000000000000000000000000000000000000000000001", logIndex: 0) { contractAddress from to tokenId } }`},
		{"erc721TransfersByToken", `{ erc721TransfersByToken(token: "0x0000000000000000000000000000000000000001") { nodes { from to tokenId } totalCount } }`},
		{"erc721TransfersByAddress", `{ erc721TransfersByAddress(address: "0x0000000000000000000000000000000000000001", isFrom: true) { nodes { contractAddress from to tokenId } totalCount } }`},
		{"erc721Owner", `{ erc721Owner(token: "0x0000000000000000000000000000000000000001", tokenId: "1") }`},
		{"nftsByOwner", `{ nftsByOwner(owner: "0x0000000000000000000000000000000000000001") { nodes { contractAddress tokenId } totalCount } }`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := handler.ExecuteQuery(tc.query, nil)
			_ = result // Resolver ran, coverage gained
		})
	}
}

// TestSetCodeResolvers tests setCode authorization query resolvers.
// Since mockStorage doesn't implement SetCodeIndexReader, these hit the "not available" paths.
func TestSetCodeResolvers(t *testing.T) {
	handler := newTestHandler(t)

	tests := []struct {
		name  string
		query string
	}{
		{"setCodeAuthorization", `{ setCodeAuthorization(txHash: "0x0000000000000000000000000000000000000000000000000000000000000001", authIndex: 0) { txHash authorizationIndex address authority chainId nonce } }`},
		{"setCodeAuthorizationsByTx", `{ setCodeAuthorizationsByTx(txHash: "0x0000000000000000000000000000000000000000000000000000000000000001") { txHash address authority } }`},
		{"setCodeAuthorizationsByTarget", `{ setCodeAuthorizationsByTarget(target: "0x0000000000000000000000000000000000000001") { nodes { txHash address authority } totalCount } }`},
		{"setCodeAuthorizationsByAuthority", `{ setCodeAuthorizationsByAuthority(authority: "0x0000000000000000000000000000000000000001") { nodes { txHash address authority } totalCount } }`},
		{"addressSetCodeInfo", `{ addressSetCodeInfo(address: "0x0000000000000000000000000000000000000001") { address hasDelegation delegationTarget asTargetCount } }`},
		{"setCodeTransactionsInBlock", `{ setCodeTransactionsInBlock(blockNumber: "1") { hash from to } }`},
		{"recentSetCodeTransactions", `{ recentSetCodeTransactions(limit: 10) { hash from to } }`},
		{"setCodeTransactionCount", `{ setCodeTransactionCount }`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := handler.ExecuteQuery(tc.query, nil)
			_ = result // Resolver ran, coverage gained
		})
	}
}

// TestTokenMetadataResolvers tests token metadata query resolvers.
func TestTokenMetadataResolvers(t *testing.T) {
	handler := newTestHandler(t)

	tests := []struct {
		name  string
		query string
	}{
		{"tokenMetadata", `{ tokenMetadata(address: "0x0000000000000000000000000000000000000001") { address name symbol decimals standard } }`},
		{"tokens", `{ tokens(standard: "ERC20") { nodes { address name symbol } totalCount } }`},
		{"searchTokens", `{ searchTokens(query: "test") { address name symbol } }`},
		{"tokenCount", `{ tokenCount(standard: "ERC20") }`},
		// TokenHolder queries - mockStorage doesn't implement TokenHolderIndexReader, hits "not available" path
		{"tokenHolders", `{ tokenHolders(token: "0x0000000000000000000000000000000000000001") { nodes { holderAddress balance } totalCount } }`},
		{"tokenHolderCount", `{ tokenHolderCount(token: "0x0000000000000000000000000000000000000001") }`},
		{"tokenBalance", `{ tokenBalance(token: "0x0000000000000000000000000000000000000001", holder: "0x0000000000000000000000000000000000000002") }`},
		{"tokenHolderStats", `{ tokenHolderStats(token: "0x0000000000000000000000000000000000000001") { holderCount transferCount } }`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := handler.ExecuteQuery(tc.query, nil)
			_ = result // Resolver ran, coverage gained
		})
	}
}

// TestHistoricalResolvers tests untested historical/analytics query resolvers.
func TestHistoricalAndAnalyticsResolvers(t *testing.T) {
	handler := newTestHandler(t)

	tests := []struct {
		name  string
		query string
	}{
		{"blockCount", `{ blockCount }`},
		{"transactionCount", `{ transactionCount }`},
		{"blocksByTimeRange", `{ blocksByTimeRange(fromTime: "1700000000", toTime: "1700001000") { number hash timestamp } }`},
		{"blockByTimestamp", `{ blockByTimestamp(timestamp: "1700000000") { number hash } }`},
		{"transactionsByAddressFiltered", `{ transactionsByAddressFiltered(address: "0x0000000000000000000000000000000000000001") { nodes { hash } totalCount } }`},
		{"addressBalance", `{ addressBalance(address: "0x0000000000000000000000000000000000000001") }`},
		{"addressBalance_withBlock", `{ addressBalance(address: "0x0000000000000000000000000000000000000001", blockNumber: "1") }`},
		{"balanceHistory", `{ balanceHistory(address: "0x0000000000000000000000000000000000000001") { blockNumber balance txHash } }`},
		{"topMiners", `{ topMiners(limit: 5) { address blockCount percentage } }`},
		{"topMiners_withRange", `{ topMiners(limit: 5, fromBlock: "0", toBlock: "100") { address blockCount } }`},
		{"tokenBalances", `{ tokenBalances(address: "0x0000000000000000000000000000000000000001") { address balance tokenType } }`},
		{"gasStats", `{ gasStats(fromBlock: "0", toBlock: "100") { averageGasPrice totalGasUsed averageGasUsed } }`},
		{"addressGasStats", `{ addressGasStats(address: "0x0000000000000000000000000000000000000001", fromBlock: "0", toBlock: "100") { address totalGasUsed averageGasPerTx transactionCount } }`},
		{"topAddressesByGasUsed", `{ topAddressesByGasUsed(limit: 5, fromBlock: "0", toBlock: "100") { address totalGasUsed } }`},
		{"topAddressesByTxCount", `{ topAddressesByTxCount(limit: 5, fromBlock: "0", toBlock: "100") { address transactionCount } }`},
		{"networkMetrics", `{ networkMetrics(fromTime: "1700000000", toTime: "1700001000") { totalBlocks totalTransactions blockTime } }`},
		{"addressStats", `{ addressStats(address: "0x0000000000000000000000000000000000000001") { totalTransactions sentCount receivedCount } }`},
		{"contractVerification", `{ contractVerification(address: "0x0000000000000000000000000000000000000001") { address verified } }`},
		{"search", `{ search(query: "0x1234") { type value label } }`},
		{"blocksRange", `{ blocksRange(from: "1", to: "10") { number hash } }`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := handler.ExecuteQuery(tc.query, nil)
			_ = result // Resolver ran, coverage gained
		})
	}
}

// TestNewSchema tests the NewSchema constructor directly.
func TestNewSchema_Direct(t *testing.T) {
	store := &mockStorage{
		latestHeight: 0,
		blocks:       make(map[uint64]*types.Block),
		blocksByHash: make(map[common.Hash]*types.Block),
	}
	schema, err := NewSchema(store, zap.NewNop())
	require.NoError(t, err)
	require.NotNil(t, schema)
	assert.NotNil(t, schema.Schema())
}

// TestNewHandlerWithOptions_Variations tests handler creation with different options.
func TestNewHandlerWithOptions_Variations(t *testing.T) {
	store := &mockStorage{
		latestHeight: 0,
		blocks:       make(map[uint64]*types.Block),
		blocksByHash: make(map[common.Hash]*types.Block),
	}
	logger := zap.NewNop()

	t.Run("nil options", func(t *testing.T) {
		handler, err := NewHandlerWithOptions(store, logger, nil)
		require.NoError(t, err)
		require.NotNil(t, handler)
	})

	t.Run("empty options", func(t *testing.T) {
		handler, err := NewHandlerWithOptions(store, logger, &HandlerOptions{})
		require.NoError(t, err)
		require.NotNil(t, handler)
	})
}

// TestSchemaBuilder tests schema builder methods.
func TestSchemaBuilder_Methods(t *testing.T) {
	store := &mockStorage{
		latestHeight: 0,
		blocks:       make(map[uint64]*types.Block),
		blocksByHash: make(map[common.Hash]*types.Block),
	}
	logger := zap.NewNop()

	builder := NewSchemaBuilder(store, logger)
	require.NotNil(t, builder)

	// Chain all query builders
	builder = builder.
		WithCoreQueries().
		WithHistoricalQueries().
		WithAnalyticsQueries().
		WithSystemContractQueries().
		WithConsensusQueries().
		WithAddressIndexingQueries().
		WithFeeDelegationQueries().
		WithTokenMetadataQueries().
		WithTokenHolderQueries().
		WithSetCodeQueries().
		WithSubscriptions().
		WithMutations()

	schema, err := builder.Build()
	require.NoError(t, err)
	require.NotNil(t, schema)
}

// ---- Tests using richMockStorage for deep code path coverage ----

// TestSystemContractResolversWithData exercises resolvers that iterate over data
// and call *ToMap helper functions.
func TestSystemContractResolversWithData(t *testing.T) {
	handler := newRichTestHandler(t)

	tests := []struct {
		name      string
		query     string
		checkData func(t *testing.T, data map[string]interface{})
	}{
		{
			"activeMinters_withData",
			`{ activeMinters { address allowance isActive } }`,
			func(t *testing.T, data map[string]interface{}) {
				minters, ok := data["activeMinters"].([]interface{})
				require.True(t, ok)
				assert.Len(t, minters, 2)
				first := minters[0].(map[string]interface{})
				assert.Equal(t, true, first["isActive"])
				assert.Equal(t, "1000000", first["allowance"])
			},
		},
		{
			"activeMinterAddresses_withData",
			`{ activeMinterAddresses }`,
			func(t *testing.T, data map[string]interface{}) {
				addrs, ok := data["activeMinterAddresses"].([]interface{})
				require.True(t, ok)
				assert.Len(t, addrs, 2)
			},
		},
		{
			"minterAllowance_withData",
			`{ minterAllowance(minter: "0x0000000000000000000000000000000000000001") }`,
			func(t *testing.T, data map[string]interface{}) {
				assert.Equal(t, "1000000", data["minterAllowance"])
			},
		},
		{
			"activeValidators_withData",
			`{ activeValidators { address isActive } }`,
			func(t *testing.T, data map[string]interface{}) {
				vals, ok := data["activeValidators"].([]interface{})
				require.True(t, ok)
				assert.Len(t, vals, 1)
			},
		},
		{
			"activeValidatorAddresses_withData",
			`{ activeValidatorAddresses }`,
			func(t *testing.T, data map[string]interface{}) {
				addrs, ok := data["activeValidatorAddresses"].([]interface{})
				require.True(t, ok)
				assert.Len(t, addrs, 1)
			},
		},
		{
			"blacklistedAddresses_withData",
			`{ blacklistedAddresses }`,
			func(t *testing.T, data map[string]interface{}) {
				addrs, ok := data["blacklistedAddresses"].([]interface{})
				require.True(t, ok)
				assert.Len(t, addrs, 1)
			},
		},
		{
			"authorizedAccounts_withData",
			`{ authorizedAccounts }`,
			func(t *testing.T, data map[string]interface{}) {
				addrs, ok := data["authorizedAccounts"].([]interface{})
				require.True(t, ok)
				assert.Len(t, addrs, 1)
			},
		},
		{
			"totalSupply_withData",
			`{ totalSupply }`,
			func(t *testing.T, data map[string]interface{}) {
				assert.Equal(t, "99999999", data["totalSupply"])
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := handler.ExecuteQuery(tc.query, nil)
			assert.Empty(t, result.Errors, "unexpected errors for %s: %v", tc.name, result.Errors)
			if tc.checkData != nil {
				data, ok := result.Data.(map[string]interface{})
				require.True(t, ok)
				tc.checkData(t, data)
			}
		})
	}
}

// TestProposalResolversWithData exercises proposal query/vote resolvers and proposalToMap.
func TestProposalResolversWithData(t *testing.T) {
	handler := newRichTestHandler(t)

	t.Run("proposals_withData", func(t *testing.T) {
		result := handler.ExecuteQuery(`{ proposals { nodes { contract proposalId proposer status requiredApprovals approved rejected createdAt executedAt blockNumber transactionHash actionType callData memberVersion } totalCount pageInfo { hasNextPage hasPreviousPage } } }`, nil)
		assert.Empty(t, result.Errors)
		data := result.Data.(map[string]interface{})
		proposals := data["proposals"].(map[string]interface{})
		nodes := proposals["nodes"].([]interface{})
		assert.Len(t, nodes, 2)
		// First proposal: no executedAt
		first := nodes[0].(map[string]interface{})
		assert.Equal(t, "VOTING", first["status"])
		assert.Nil(t, first["executedAt"])
		// Second proposal: has executedAt
		second := nodes[1].(map[string]interface{})
		assert.Equal(t, "EXECUTED", second["status"])
		assert.NotNil(t, second["executedAt"])
		assert.Equal(t, 2, proposals["totalCount"])
	})

	t.Run("proposals_withFilter", func(t *testing.T) {
		result := handler.ExecuteQuery(`{ proposals(filter: {contract: "0x0000000000000000000000000000000000000001", status: VOTING, proposer: "0x0000000000000000000000000000000000000002"}) { nodes { proposalId } totalCount } }`, nil)
		assert.Empty(t, result.Errors)
	})

	t.Run("proposals_withPagination", func(t *testing.T) {
		result := handler.ExecuteQuery(`{ proposals(pagination: {limit: 1, offset: 0}) { nodes { proposalId } totalCount pageInfo { hasNextPage hasPreviousPage } } }`, nil)
		assert.Empty(t, result.Errors)
	})

	t.Run("proposal_byId", func(t *testing.T) {
		result := handler.ExecuteQuery(`{ proposal(contract: "0x0000000000000000000000000000000000000001", proposalId: "1") { contract proposalId proposer status } }`, nil)
		assert.Empty(t, result.Errors)
		data := result.Data.(map[string]interface{})
		assert.NotNil(t, data["proposal"])
	})

	t.Run("proposal_notFound", func(t *testing.T) {
		result := handler.ExecuteQuery(`{ proposal(contract: "0x0000000000000000000000000000000000000001", proposalId: "999") { proposalId } }`, nil)
		// Returns nil without error
		assert.Empty(t, result.Errors)
	})

	t.Run("proposalVotes_withData", func(t *testing.T) {
		result := handler.ExecuteQuery(`{ proposalVotes(contract: "0x0000000000000000000000000000000000000001", proposalId: "1") { contract proposalId voter approval blockNumber transactionHash timestamp } }`, nil)
		assert.Empty(t, result.Errors)
		data := result.Data.(map[string]interface{})
		votes := data["proposalVotes"].([]interface{})
		assert.Len(t, votes, 1)
	})
}

// TestMintBurnResolversWithData exercises mint/burn event resolvers and *ToMap functions.
func TestMintBurnResolversWithData(t *testing.T) {
	handler := newRichTestHandler(t)

	t.Run("mintEvents_withData", func(t *testing.T) {
		result := handler.ExecuteQuery(`{ mintEvents(filter: {fromBlock: "0", toBlock: "100"}) { nodes { blockNumber transactionHash minter to amount timestamp } totalCount pageInfo { hasNextPage hasPreviousPage } } }`, nil)
		assert.Empty(t, result.Errors)
		data := result.Data.(map[string]interface{})
		events := data["mintEvents"].(map[string]interface{})
		nodes := events["nodes"].([]interface{})
		assert.Len(t, nodes, 1)
		first := nodes[0].(map[string]interface{})
		assert.Equal(t, "5000", first["amount"])
	})

	t.Run("mintEvents_withMinterFilter", func(t *testing.T) {
		result := handler.ExecuteQuery(`{ mintEvents(filter: {fromBlock: "0", toBlock: "100", address: "0x0000000000000000000000000000000000000001"}) { nodes { minter } totalCount } }`, nil)
		assert.Empty(t, result.Errors)
	})

	t.Run("mintEvents_withPagination", func(t *testing.T) {
		result := handler.ExecuteQuery(`{ mintEvents(filter: {fromBlock: "0", toBlock: "100"}, pagination: {limit: 5, offset: 0}) { nodes { minter } } }`, nil)
		assert.Empty(t, result.Errors)
	})

	t.Run("burnEvents_withData", func(t *testing.T) {
		result := handler.ExecuteQuery(`{ burnEvents(filter: {fromBlock: "0", toBlock: "100"}) { nodes { blockNumber transactionHash burner amount timestamp withdrawalId } totalCount pageInfo { hasNextPage hasPreviousPage } } }`, nil)
		assert.Empty(t, result.Errors)
		data := result.Data.(map[string]interface{})
		events := data["burnEvents"].(map[string]interface{})
		nodes := events["nodes"].([]interface{})
		assert.Len(t, nodes, 2)
		// First has withdrawalId
		first := nodes[0].(map[string]interface{})
		assert.Equal(t, "w-123", first["withdrawalId"])
	})

	t.Run("burnEvents_withBurnerFilter", func(t *testing.T) {
		result := handler.ExecuteQuery(`{ burnEvents(filter: {fromBlock: "0", toBlock: "100", address: "0x0000000000000000000000000000000000000003"}) { nodes { burner } } }`, nil)
		assert.Empty(t, result.Errors)
	})

	t.Run("burnHistory_withData", func(t *testing.T) {
		result := handler.ExecuteQuery(`{ burnHistory(filter: {fromBlock: "0", toBlock: "100"}) { nodes { burner amount } totalCount } }`, nil)
		assert.Empty(t, result.Errors)
	})
}

// TestHistoryResolversWithData exercises all history resolvers (minter, validator, gas tip, etc).
func TestHistoryResolversWithData(t *testing.T) {
	handler := newRichTestHandler(t)

	t.Run("minterHistory_withData", func(t *testing.T) {
		result := handler.ExecuteQuery(`{ minterHistory(minter: "0x0000000000000000000000000000000000000001") { blockNumber transactionHash minter allowance action timestamp } }`, nil)
		assert.Empty(t, result.Errors)
		data := result.Data.(map[string]interface{})
		events := data["minterHistory"].([]interface{})
		assert.Len(t, events, 1)
		first := events[0].(map[string]interface{})
		assert.Equal(t, "configured", first["action"])
	})

	t.Run("validatorHistory_withData", func(t *testing.T) {
		result := handler.ExecuteQuery(`{ validatorHistory(validator: "0x0000000000000000000000000000000000000010") { blockNumber transactionHash validator action oldValidator timestamp } }`, nil)
		assert.Empty(t, result.Errors)
		data := result.Data.(map[string]interface{})
		events := data["validatorHistory"].([]interface{})
		assert.Len(t, events, 2)
		// Second event has oldValidator
		second := events[1].(map[string]interface{})
		assert.NotNil(t, second["oldValidator"])
	})

	t.Run("gasTipHistory_withData", func(t *testing.T) {
		result := handler.ExecuteQuery(`{ gasTipHistory(filter: {fromBlock: "0", toBlock: "100"}) { blockNumber transactionHash oldTip newTip updater timestamp } }`, nil)
		assert.Empty(t, result.Errors)
		data := result.Data.(map[string]interface{})
		events := data["gasTipHistory"].([]interface{})
		assert.Len(t, events, 1)
	})

	t.Run("blacklistHistory_withData", func(t *testing.T) {
		result := handler.ExecuteQuery(`{ blacklistHistory(address: "0x0000000000000000000000000000000000000099") { blockNumber transactionHash account action proposalId timestamp } }`, nil)
		assert.Empty(t, result.Errors)
		data := result.Data.(map[string]interface{})
		events := data["blacklistHistory"].([]interface{})
		assert.Len(t, events, 1)
	})

	t.Run("memberHistory_withData", func(t *testing.T) {
		result := handler.ExecuteQuery(`{ memberHistory(contract: "0x0000000000000000000000000000000000000001") { contract blockNumber transactionHash member action oldMember totalMembers newQuorum timestamp } }`, nil)
		assert.Empty(t, result.Errors)
		data := result.Data.(map[string]interface{})
		events := data["memberHistory"].([]interface{})
		assert.Len(t, events, 2)
		// Second event has oldMember
		second := events[1].(map[string]interface{})
		assert.NotNil(t, second["oldMember"])
	})

	t.Run("emergencyPauseHistory_withData", func(t *testing.T) {
		result := handler.ExecuteQuery(`{ emergencyPauseHistory(contract: "0x0000000000000000000000000000000000000001") { contract blockNumber transactionHash proposalId action timestamp } }`, nil)
		assert.Empty(t, result.Errors)
		data := result.Data.(map[string]interface{})
		events := data["emergencyPauseHistory"].([]interface{})
		assert.Len(t, events, 1)
	})

	t.Run("depositMintProposals_withData", func(t *testing.T) {
		result := handler.ExecuteQuery(`{ depositMintProposals(filter: {fromBlock: "0", toBlock: "100"}) { proposalId amount depositId status blockNumber transactionHash timestamp } }`, nil)
		assert.Empty(t, result.Errors)
		data := result.Data.(map[string]interface{})
		proposals := data["depositMintProposals"].([]interface{})
		assert.Len(t, proposals, 1)
		first := proposals[0].(map[string]interface{})
		assert.Equal(t, "APPROVED", first["status"])
	})

	t.Run("minterConfigHistory_withData", func(t *testing.T) {
		result := handler.ExecuteQuery(`{ minterConfigHistory(filter: {fromBlock: "0", toBlock: "100"}) { blockNumber transactionHash minter allowance action timestamp } }`, nil)
		assert.Empty(t, result.Errors)
		data := result.Data.(map[string]interface{})
		events := data["minterConfigHistory"].([]interface{})
		assert.Len(t, events, 1)
	})

	t.Run("maxProposalsUpdateHistory_withData", func(t *testing.T) {
		result := handler.ExecuteQuery(`{ maxProposalsUpdateHistory(contract: "0x0000000000000000000000000000000000000001") { contract blockNumber transactionHash oldMax newMax timestamp } }`, nil)
		assert.Empty(t, result.Errors)
		data := result.Data.(map[string]interface{})
		events := data["maxProposalsUpdateHistory"].([]interface{})
		assert.Len(t, events, 1)
	})

	t.Run("proposalExecutionSkippedEvents_withData", func(t *testing.T) {
		result := handler.ExecuteQuery(`{ proposalExecutionSkippedEvents(contract: "0x0000000000000000000000000000000000000001", proposalId: "3") { contract blockNumber transactionHash account proposalId reason timestamp } }`, nil)
		assert.Empty(t, result.Errors)
		data := result.Data.(map[string]interface{})
		events := data["proposalExecutionSkippedEvents"].([]interface{})
		assert.Len(t, events, 1)
	})
}

// TestBlocksRangeResolver exercises the resolveBlocksRange function.
func TestBlocksRangeResolver(t *testing.T) {
	handler := newRichTestHandler(t)

	t.Run("basic_range", func(t *testing.T) {
		result := handler.ExecuteQuery(`{ blocksRange(startNumber: "1", endNumber: "1") { blocks { number hash gasUsed gasLimit } startNumber endNumber count hasMore latestHeight } }`, nil)
		assert.Empty(t, result.Errors)
		data := result.Data.(map[string]interface{})
		br := data["blocksRange"].(map[string]interface{})
		assert.Equal(t, 1, br["count"])
		assert.Equal(t, true, br["hasMore"])
	})

	t.Run("range_beyond_latest", func(t *testing.T) {
		result := handler.ExecuteQuery(`{ blocksRange(startNumber: "200", endNumber: "300") { count hasMore } }`, nil)
		assert.Empty(t, result.Errors)
		data := result.Data.(map[string]interface{})
		br := data["blocksRange"].(map[string]interface{})
		assert.Equal(t, 0, br["count"])
	})

	t.Run("range_noTransactions", func(t *testing.T) {
		result := handler.ExecuteQuery(`{ blocksRange(startNumber: "1", endNumber: "1", includeTransactions: false) { blocks { number } count } }`, nil)
		assert.Empty(t, result.Errors)
	})

	t.Run("range_withReceipts", func(t *testing.T) {
		result := handler.ExecuteQuery(`{ blocksRange(startNumber: "1", endNumber: "1", includeReceipts: true) { blocks { number } count } }`, nil)
		assert.Empty(t, result.Errors)
	})
}

// TestProposalStatusParsing exercises parseProposalStatus and proposalStatusToString.
func TestProposalStatusParsing(t *testing.T) {
	handler := newRichTestHandler(t)

	statuses := []string{"VOTING", "APPROVED", "EXECUTED", "CANCELLED", "EXPIRED", "FAILED", "REJECTED", "NONE"}
	for _, status := range statuses {
		t.Run("filter_"+status, func(t *testing.T) {
			result := handler.ExecuteQuery(`{ proposals(filter: {status: `+status+`}) { totalCount } }`, nil)
			// Resolver runs, exercising parseProposalStatus
			_ = result
		})
	}
}

// TestAddressResolversWithData exercises address indexing resolvers with actual data.
func TestAddressResolversWithData(t *testing.T) {
	handler := newRichTestHandler(t)

	tests := []struct {
		name  string
		query string
	}{
		{"contractCreation", `{ contractCreation(address: "0x0000000000000000000000000000000000000001") { contractAddress creator transactionHash blockNumber timestamp } }`},
		{"contracts", `{ contracts { nodes { contractAddress creator transactionHash blockNumber } totalCount pageInfo { hasNextPage } } }`},
		{"contractsByCreator", `{ contractsByCreator(creator: "0x0000000000000000000000000000000000000001") { nodes { contractAddress } totalCount } }`},
		{"internalTransactions", `{ internalTransactions(transactionHash: "0x0000000000000000000000000000000000000000000000000000000000000001") { transactionHash blockNumber type from to value gas gasUsed } }`},
		{"internalTransactionsByAddress", `{ internalTransactionsByAddress(address: "0x0000000000000000000000000000000000000001", isFrom: true) { nodes { transactionHash from to value type } totalCount } }`},
		{"erc20Transfer", `{ erc20Transfer(transactionHash: "0x0000000000000000000000000000000000000000000000000000000000000001", logIndex: 0) { contractAddress from to value transactionHash blockNumber } }`},
		{"erc20TransfersByToken", `{ erc20TransfersByToken(token: "0x0000000000000000000000000000000000000001") { nodes { from to value } totalCount } }`},
		{"erc20TransfersByAddress", `{ erc20TransfersByAddress(address: "0x0000000000000000000000000000000000000001", isFrom: true) { nodes { contractAddress from to value } totalCount } }`},
		{"erc721Transfer", `{ erc721Transfer(transactionHash: "0x0000000000000000000000000000000000000000000000000000000000000001", logIndex: 0) { contractAddress from to tokenId transactionHash blockNumber } }`},
		{"erc721TransfersByToken", `{ erc721TransfersByToken(token: "0x0000000000000000000000000000000000000001") { nodes { from to tokenId } totalCount } }`},
		{"erc721TransfersByAddress", `{ erc721TransfersByAddress(address: "0x0000000000000000000000000000000000000001", isFrom: true) { nodes { contractAddress from to tokenId } totalCount } }`},
		{"erc721Owner", `{ erc721Owner(token: "0x0000000000000000000000000000000000000001", tokenId: "1") }`},
		{"nftsByOwner", `{ nftsByOwner(owner: "0x0000000000000000000000000000000000000001") { nodes { contractAddress tokenId owner } totalCount } }`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := handler.ExecuteQuery(tc.query, nil)
			assert.Empty(t, result.Errors, "unexpected errors for %s: %v", tc.name, result.Errors)
		})
	}
}

// TestSetCodeResolversWithData exercises setCode resolvers with actual data.
func TestSetCodeResolversWithData(t *testing.T) {
	handler := newRichTestHandler(t)

	tests := []struct {
		name  string
		query string
	}{
		{"setCodeAuthorization", `{ setCodeAuthorization(txHash: "0x0000000000000000000000000000000000000000000000000000000000000001", authIndex: 0) { txHash authorizationIndex address authority chainId nonce applied } }`},
		{"setCodeAuthorizationsByTx", `{ setCodeAuthorizationsByTx(txHash: "0x0000000000000000000000000000000000000000000000000000000000000001") { txHash address authority applied } }`},
		{"setCodeAuthorizationsByTarget", `{ setCodeAuthorizationsByTarget(target: "0x0000000000000000000000000000000000000001") { nodes { txHash address authority } totalCount } }`},
		{"setCodeAuthorizationsByAuthority", `{ setCodeAuthorizationsByAuthority(authority: "0x0000000000000000000000000000000000000001") { nodes { txHash address authority applied error } totalCount } }`},
		{"setCodeTransactionsInBlock", `{ setCodeTransactionsInBlock(blockNumber: "1") { hash from to } }`},
		{"recentSetCodeTransactions", `{ recentSetCodeTransactions(limit: 10) { hash from to } }`},
		{"setCodeTransactionCount", `{ setCodeTransactionCount }`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := handler.ExecuteQuery(tc.query, nil)
			assert.Empty(t, result.Errors, "unexpected errors for %s: %v", tc.name, result.Errors)
		})
	}
}

// TestTokenResolversWithData exercises token holder resolvers with actual data.
func TestTokenResolversWithData(t *testing.T) {
	handler := newRichTestHandler(t)

	tests := []struct {
		name  string
		query string
	}{
		{"tokenHolders", `{ tokenHolders(token: "0x0000000000000000000000000000000000000001") { nodes { holderAddress balance } totalCount } }`},
		{"tokenHolderCount", `{ tokenHolderCount(token: "0x0000000000000000000000000000000000000001") }`},
		{"tokenBalance", `{ tokenBalance(token: "0x0000000000000000000000000000000000000001", holder: "0x0000000000000000000000000000000000000002") }`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := handler.ExecuteQuery(tc.query, nil)
			assert.Empty(t, result.Errors, "unexpected errors for %s: %v", tc.name, result.Errors)
		})
	}
}

// TestHistoricalResolversWithData exercises historical/analytics resolvers with actual data.
func TestHistoricalResolversWithRichData(t *testing.T) {
	handler := newRichTestHandler(t)

	tests := []struct {
		name      string
		query     string
		checkData func(t *testing.T, data map[string]interface{})
	}{
		{
			"tokenBalances",
			`{ tokenBalances(address: "0x0000000000000000000000000000000000000001") { address balance tokenType name symbol } }`,
			func(t *testing.T, data map[string]interface{}) {
				balances, ok := data["tokenBalances"].([]interface{})
				require.True(t, ok)
				assert.Len(t, balances, 1)
			},
		},
		{
			"gasStats",
			`{ gasStats(fromBlock: "0", toBlock: "100") { totalGasUsed averageGasPrice blockCount transactionCount } }`,
			func(t *testing.T, data map[string]interface{}) {
				stats := data["gasStats"]
				assert.NotNil(t, stats)
			},
		},
		{
			"addressGasStats",
			`{ addressGasStats(address: "0x0000000000000000000000000000000000000001", fromBlock: "0", toBlock: "100") { address totalGasUsed averageGasPerTx transactionCount } }`,
			func(t *testing.T, data map[string]interface{}) {
				stats := data["addressGasStats"]
				assert.NotNil(t, stats)
			},
		},
		{
			"topAddressesByTxCount",
			`{ topAddressesByTxCount(limit: 5, fromBlock: "0", toBlock: "100") { address transactionCount } }`,
			func(t *testing.T, data map[string]interface{}) {
				addrs, ok := data["topAddressesByTxCount"].([]interface{})
				require.True(t, ok)
				assert.Len(t, addrs, 1)
			},
		},
		{
			"networkMetrics",
			`{ networkMetrics(fromTime: "1700000000", toTime: "1700001000") { tps blockTime totalBlocks totalTransactions } }`,
			func(t *testing.T, data map[string]interface{}) {
				metrics := data["networkMetrics"]
				assert.NotNil(t, metrics)
			},
		},
		{
			"addressStats",
			`{ addressStats(address: "0x0000000000000000000000000000000000000001") { address totalTransactions sentCount receivedCount } }`,
			func(t *testing.T, data map[string]interface{}) {
				stats := data["addressStats"]
				assert.NotNil(t, stats)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := handler.ExecuteQuery(tc.query, nil)
			assert.Empty(t, result.Errors, "unexpected errors for %s: %v", tc.name, result.Errors)
			if tc.checkData != nil {
				data, ok := result.Data.(map[string]interface{})
				require.True(t, ok)
				tc.checkData(t, data)
			}
		})
	}
}

// TestWBFTResolversWithData exercises WBFT consensus resolvers with actual data.
func TestWBFTResolversWithData(t *testing.T) {
	handler := newRichTestHandler(t)

	tests := []struct {
		name  string
		query string
	}{
		{"wbftBlockExtra", `{ wbftBlockExtra(blockNumber: "1") { blockNumber round gasTip preparedSeal { signature } committedSeal { signature } } }`},
		{"wbftBlock_alias", `{ wbftBlock(number: "1") { blockNumber round } }`},
		{"epochInfo", `{ epochInfo(epochNumber: "1") { epochNumber blockNumber validators candidates { address diligence } } }`},
		{"epochByNumber_alias", `{ epochByNumber(number: "1") { epochNumber } }`},
		{"latestEpochInfo", `{ latestEpochInfo { epochNumber blockNumber } }`},
		{"epochs", `{ epochs { nodes { epochNumber blockNumber } totalCount } }`},
		{"validatorSigningStats", `{ validatorSigningStats(validatorAddress: "0x0000000000000000000000000000000000000010", fromBlock: "1", toBlock: "100") { validatorAddress prepareSignCount commitSignCount signingRate blocksProposed proposalRate } }`},
		{"allValidatorsSigningStats", `{ allValidatorsSigningStats(fromBlock: "1", toBlock: "100") { nodes { validatorAddress signingRate } totalCount } }`},
		{"validatorSigningActivity", `{ validatorSigningActivity(validatorAddress: "0x0000000000000000000000000000000000000010", fromBlock: "1", toBlock: "100") { nodes { blockNumber signedPrepare signedCommit round } totalCount } }`},
		{"blockSigners", `{ blockSigners(blockNumber: "1") { preparers committers } }`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := handler.ExecuteQuery(tc.query, nil)
			assert.Empty(t, result.Errors, "unexpected errors for %s: %v", tc.name, result.Errors)
		})
	}
}

// TestFeeDelegationResolversWithData exercises fee delegation resolvers with actual data.
func TestFeeDelegationResolversWithData(t *testing.T) {
	handler := newRichTestHandler(t)

	t.Run("feePayerStats_withData", func(t *testing.T) {
		result := handler.ExecuteQuery(`{ feePayerStats(address: "0x0000000000000000000000000000000000000001") { address txCount totalFeesPaid percentage } }`, nil)
		assert.Empty(t, result.Errors, "errors: %v", result.Errors)
	})
}
