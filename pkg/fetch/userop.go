package fetch

import (
	"context"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"go.uber.org/zap"

	storagepkg "github.com/0xmhha/indexer-go/pkg/storage"
)

// EIP-4337 EntryPoint event signatures
var (
	// UserOperationEvent(bytes32 indexed userOpHash, address indexed sender, address indexed paymaster, uint256 nonce, bool success, uint256 actualGasCost, uint256 actualGasUsed)
	userOperationEventSig = crypto.Keccak256Hash([]byte("UserOperationEvent(bytes32,address,address,uint256,bool,uint256,uint256)"))

	// AccountDeployed(bytes32 indexed userOpHash, address indexed sender, address factory, address paymaster)
	accountDeployedSig = crypto.Keccak256Hash([]byte("AccountDeployed(bytes32,address,address,address)"))

	// UserOperationRevertReason(bytes32 indexed userOpHash, address indexed sender, uint256 nonce, bytes revertReason)
	userOperationRevertReasonSig = crypto.Keccak256Hash([]byte("UserOperationRevertReason(bytes32,address,uint256,bytes)"))

	// PostOpRevertReason(bytes32 indexed userOpHash, address indexed sender, uint256 nonce, bytes revertReason)
	postOpRevertReasonSig = crypto.Keccak256Hash([]byte("PostOpRevertReason(bytes32,address,uint256,bytes)"))
)

// UserOpIndexer defines the interface for indexing EIP-4337 UserOperations
type UserOpIndexer interface {
	storagepkg.UserOpIndexWriter
}

// UserOpProcessor handles processing of EIP-4337 UserOperation events from EntryPoint contracts
type UserOpProcessor struct {
	logger           *zap.Logger
	storage          UserOpIndexer
	entryPointAddrs  map[common.Address]bool
	detectBySelector bool // if true, also detect EntryPoint by event signature matching
}

// NewUserOpProcessor creates a new UserOp processor
func NewUserOpProcessor(logger *zap.Logger, storage UserOpIndexer, entryPointAddresses []common.Address) *UserOpProcessor {
	addrs := make(map[common.Address]bool, len(entryPointAddresses))
	for _, addr := range entryPointAddresses {
		addrs[addr] = true
	}

	return &UserOpProcessor{
		logger:           logger.Named("userop"),
		storage:          storage,
		entryPointAddrs:  addrs,
		detectBySelector: len(entryPointAddresses) == 0,
	}
}

// ProcessBlockReceipts processes all receipts in a block for EIP-4337 events
func (p *UserOpProcessor) ProcessBlockReceipts(
	ctx context.Context,
	block *types.Block,
	receipts []*types.Receipt,
	txByHash map[common.Hash]*types.Transaction,
) error {
	blockNumber := block.NumberU64()
	blockTime := time.Unix(int64(block.Time()), 0)

	var userOps []*storagepkg.UserOperationRecord

	for _, receipt := range receipts {
		tx, ok := txByHash[receipt.TxHash]
		if !ok {
			continue
		}

		// Get bundler address (tx.From via signer recovery)
		bundler := getBundlerAddress(tx, block)

		for _, log := range receipt.Logs {
			if len(log.Topics) == 0 {
				continue
			}

			// Check if this log is from a known EntryPoint
			if !p.isEntryPointLog(log) {
				continue
			}

			switch log.Topics[0] {
			case userOperationEventSig:
				record, err := p.parseUserOperationEvent(log, receipt, block, bundler, blockTime)
				if err != nil {
					p.logger.Warn("failed to parse UserOperationEvent",
						zap.Uint64("block", blockNumber),
						zap.String("tx", receipt.TxHash.Hex()),
						zap.Uint("logIndex", log.Index),
						zap.Error(err))
					continue
				}
				userOps = append(userOps, record)

			case accountDeployedSig:
				if err := p.processAccountDeployed(ctx, log, receipt, blockNumber, blockTime); err != nil {
					p.logger.Warn("failed to process AccountDeployed",
						zap.Uint64("block", blockNumber),
						zap.String("tx", receipt.TxHash.Hex()),
						zap.Error(err))
				}

			case userOperationRevertReasonSig:
				if err := p.processRevertReason(ctx, log, receipt, blockNumber, blockTime, storagepkg.UserOpRevertTypeExecution); err != nil {
					p.logger.Warn("failed to process UserOperationRevertReason",
						zap.Uint64("block", blockNumber),
						zap.String("tx", receipt.TxHash.Hex()),
						zap.Error(err))
				}

			case postOpRevertReasonSig:
				if err := p.processRevertReason(ctx, log, receipt, blockNumber, blockTime, storagepkg.UserOpRevertTypePostOp); err != nil {
					p.logger.Warn("failed to process PostOpRevertReason",
						zap.Uint64("block", blockNumber),
						zap.String("tx", receipt.TxHash.Hex()),
						zap.Error(err))
				}
			}
		}
	}

	// Batch save all UserOps
	if len(userOps) > 0 {
		if err := p.storage.SaveUserOps(ctx, userOps); err != nil {
			return err
		}

		// Update bundler and paymaster stats
		for _, op := range userOps {
			if err := p.storage.IncrementBundlerStats(ctx, op.Bundler, op.Success, op.ActualGasCost, op.BlockNumber); err != nil {
				p.logger.Warn("failed to increment bundler stats",
					zap.String("bundler", op.Bundler.Hex()),
					zap.Error(err))
			}

			if op.Paymaster != (common.Address{}) {
				if err := p.storage.IncrementPaymasterStats(ctx, op.Paymaster, op.Success, op.ActualGasCost, op.BlockNumber); err != nil {
					p.logger.Warn("failed to increment paymaster stats",
						zap.String("paymaster", op.Paymaster.Hex()),
						zap.Error(err))
				}
			}
		}

		p.logger.Debug("processed UserOperations",
			zap.Uint64("block", blockNumber),
			zap.Int("count", len(userOps)))
	}

	return nil
}

// isEntryPointLog checks if a log is from a known EntryPoint contract.
// If no EntryPoint addresses are configured, uses event signature detection.
func (p *UserOpProcessor) isEntryPointLog(log *types.Log) bool {
	if len(p.entryPointAddrs) > 0 {
		return p.entryPointAddrs[log.Address]
	}

	// Fallback: detect by event signature
	if p.detectBySelector && len(log.Topics) > 0 {
		switch log.Topics[0] {
		case userOperationEventSig, accountDeployedSig, userOperationRevertReasonSig, postOpRevertReasonSig:
			return true
		}
	}

	return false
}

// parseUserOperationEvent parses a UserOperationEvent log into a record.
// Event: UserOperationEvent(bytes32 indexed userOpHash, address indexed sender, address indexed paymaster, uint256 nonce, bool success, uint256 actualGasCost, uint256 actualGasUsed)
func (p *UserOpProcessor) parseUserOperationEvent(
	log *types.Log,
	receipt *types.Receipt,
	block *types.Block,
	bundler common.Address,
	blockTime time.Time,
) (*storagepkg.UserOperationRecord, error) {
	if len(log.Topics) < 4 {
		return nil, errInsufficientTopics{expected: 4, got: len(log.Topics)}
	}

	userOpHash := log.Topics[1]
	sender := common.BytesToAddress(log.Topics[2].Bytes())
	paymaster := common.BytesToAddress(log.Topics[3].Bytes())

	// Decode non-indexed data: nonce (uint256), success (bool), actualGasCost (uint256), actualGasUsed (uint256)
	if len(log.Data) < 128 {
		return nil, errInsufficientData{expected: 128, got: len(log.Data)}
	}

	nonce := new(big.Int).SetBytes(log.Data[0:32])
	success := new(big.Int).SetBytes(log.Data[32:64]).Sign() != 0
	actualGasCost := new(big.Int).SetBytes(log.Data[64:96])
	actualUserOpFeePerGas := new(big.Int).SetBytes(log.Data[96:128])

	return &storagepkg.UserOperationRecord{
		UserOpHash:            userOpHash,
		TxHash:                receipt.TxHash,
		BlockNumber:           block.NumberU64(),
		BlockHash:             block.Hash(),
		TxIndex:               uint64(receipt.TransactionIndex),
		LogIndex:              uint64(log.Index),
		Sender:                sender,
		Paymaster:             paymaster,
		Nonce:                 nonce,
		Success:               success,
		ActualGasCost:         actualGasCost,
		ActualUserOpFeePerGas: actualUserOpFeePerGas,
		Bundler:               bundler,
		EntryPoint:            log.Address,
		Timestamp:             blockTime,
	}, nil
}

// processAccountDeployed parses and saves an AccountDeployed event.
// Event: AccountDeployed(bytes32 indexed userOpHash, address indexed sender, address factory, address paymaster)
func (p *UserOpProcessor) processAccountDeployed(
	ctx context.Context,
	log *types.Log,
	receipt *types.Receipt,
	blockNumber uint64,
	blockTime time.Time,
) error {
	if len(log.Topics) < 3 {
		return errInsufficientTopics{expected: 3, got: len(log.Topics)}
	}

	userOpHash := log.Topics[1]
	sender := common.BytesToAddress(log.Topics[2].Bytes())

	// Non-indexed: factory (address), paymaster (address)
	if len(log.Data) < 64 {
		return errInsufficientData{expected: 64, got: len(log.Data)}
	}

	factory := common.BytesToAddress(log.Data[12:32])
	paymaster := common.BytesToAddress(log.Data[44:64])

	record := &storagepkg.AccountDeployedRecord{
		UserOpHash:  userOpHash,
		Sender:      sender,
		Factory:     factory,
		Paymaster:   paymaster,
		TxHash:      receipt.TxHash,
		BlockNumber: blockNumber,
		LogIndex:    uint64(log.Index),
		Timestamp:   blockTime,
	}

	return p.storage.SaveAccountDeployed(ctx, record)
}

// processRevertReason parses and saves a revert reason event.
// Event: UserOperationRevertReason(bytes32 indexed userOpHash, address indexed sender, uint256 nonce, bytes revertReason)
// Event: PostOpRevertReason(bytes32 indexed userOpHash, address indexed sender, uint256 nonce, bytes revertReason)
func (p *UserOpProcessor) processRevertReason(
	ctx context.Context,
	log *types.Log,
	receipt *types.Receipt,
	blockNumber uint64,
	blockTime time.Time,
	revertType string,
) error {
	if len(log.Topics) < 3 {
		return errInsufficientTopics{expected: 3, got: len(log.Topics)}
	}

	userOpHash := log.Topics[1]
	sender := common.BytesToAddress(log.Topics[2].Bytes())

	// Non-indexed: nonce (uint256), revertReason (bytes)
	// ABI encoding: nonce(32) + offset(32) + length(32) + data(variable)
	if len(log.Data) < 96 {
		return errInsufficientData{expected: 96, got: len(log.Data)}
	}

	nonce := new(big.Int).SetBytes(log.Data[0:32])

	// Decode dynamic bytes: offset at [32:64], length at offset, data follows
	var revertReason []byte
	offset := new(big.Int).SetBytes(log.Data[32:64]).Uint64()
	if offset+32 <= uint64(len(log.Data)) {
		length := new(big.Int).SetBytes(log.Data[offset : offset+32]).Uint64()
		dataStart := offset + 32
		if dataStart+length <= uint64(len(log.Data)) {
			revertReason = make([]byte, length)
			copy(revertReason, log.Data[dataStart:dataStart+length])
		}
	}

	record := &storagepkg.UserOpRevertRecord{
		UserOpHash:   userOpHash,
		Sender:       sender,
		Nonce:        nonce,
		RevertReason: revertReason,
		TxHash:       receipt.TxHash,
		BlockNumber:  blockNumber,
		LogIndex:     uint64(log.Index),
		RevertType:   revertType,
		Timestamp:    blockTime,
	}

	return p.storage.SaveUserOpRevert(ctx, record)
}

// getBundlerAddress recovers the sender (bundler) of the transaction.
func getBundlerAddress(tx *types.Transaction, block *types.Block) common.Address {
	signer := types.LatestSignerForChainID(tx.ChainId())
	from, err := types.Sender(signer, tx)
	if err != nil {
		return common.Address{}
	}
	return from
}

// Error helpers
type errInsufficientTopics struct {
	expected, got int
}

func (e errInsufficientTopics) Error() string {
	return "insufficient log topics: expected " + intStr(e.expected) + ", got " + intStr(e.got)
}

type errInsufficientData struct {
	expected, got int
}

func (e errInsufficientData) Error() string {
	return "insufficient log data: expected " + intStr(e.expected) + " bytes, got " + intStr(e.got)
}

func intStr(n int) string {
	return big.NewInt(int64(n)).String()
}
