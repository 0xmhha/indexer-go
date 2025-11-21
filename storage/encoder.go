package storage

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
)

// TxLocation represents the location of a transaction in the blockchain
type TxLocation struct {
	BlockHeight uint64
	TxIndex     uint64
	BlockHash   common.Hash
}

// EncodeBlock encodes a block using RLP
func EncodeBlock(block *types.Block) ([]byte, error) {
	if block == nil {
		return nil, fmt.Errorf("block cannot be nil")
	}

	var buf bytes.Buffer
	if err := rlp.Encode(&buf, block); err != nil {
		return nil, fmt.Errorf("failed to encode block: %w", err)
	}

	return buf.Bytes(), nil
}

// DecodeBlock decodes a block from RLP
func DecodeBlock(data []byte) (*types.Block, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("data cannot be empty")
	}

	var block types.Block
	if err := rlp.DecodeBytes(data, &block); err != nil {
		return nil, fmt.Errorf("failed to decode block: %w", err)
	}

	return &block, nil
}

// EncodeTransaction encodes a transaction using RLP
func EncodeTransaction(tx *types.Transaction) ([]byte, error) {
	if tx == nil {
		return nil, fmt.Errorf("transaction cannot be nil")
	}

	var buf bytes.Buffer
	if err := rlp.Encode(&buf, tx); err != nil {
		return nil, fmt.Errorf("failed to encode transaction: %w", err)
	}

	return buf.Bytes(), nil
}

// DecodeTransaction decodes a transaction from RLP
func DecodeTransaction(data []byte) (*types.Transaction, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("data cannot be empty")
	}

	var tx types.Transaction
	if err := rlp.DecodeBytes(data, &tx); err != nil {
		return nil, fmt.Errorf("failed to decode transaction: %w", err)
	}

	return &tx, nil
}

// EncodeReceipt encodes a receipt using RLP
func EncodeReceipt(receipt *types.Receipt) ([]byte, error) {
	if receipt == nil {
		return nil, fmt.Errorf("receipt cannot be nil")
	}

	var buf bytes.Buffer
	if err := rlp.Encode(&buf, receipt); err != nil {
		return nil, fmt.Errorf("failed to encode receipt: %w", err)
	}

	return buf.Bytes(), nil
}

// DecodeReceipt decodes a receipt from RLP
func DecodeReceipt(data []byte) (*types.Receipt, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("data cannot be empty")
	}

	var receipt types.Receipt
	if err := rlp.DecodeBytes(data, &receipt); err != nil {
		return nil, fmt.Errorf("failed to decode receipt: %w", err)
	}

	return &receipt, nil
}

// EncodeTxLocation encodes a TxLocation using RLP
func EncodeTxLocation(loc *TxLocation) ([]byte, error) {
	if loc == nil {
		return nil, fmt.Errorf("location cannot be nil")
	}

	var buf bytes.Buffer
	if err := rlp.Encode(&buf, loc); err != nil {
		return nil, fmt.Errorf("failed to encode location: %w", err)
	}

	return buf.Bytes(), nil
}

// DecodeTxLocation decodes a TxLocation from RLP
func DecodeTxLocation(data []byte) (*TxLocation, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("data cannot be empty")
	}

	var loc TxLocation
	if err := rlp.DecodeBytes(data, &loc); err != nil {
		return nil, fmt.Errorf("failed to decode location: %w", err)
	}

	return &loc, nil
}

// EncodeBalanceSnapshot encodes a BalanceSnapshot
// Format: blockNumber (8 bytes) + balance bytes length (8 bytes) + balance bytes + delta bytes length (8 bytes) + delta bytes + txHash (32 bytes)
func EncodeBalanceSnapshot(snapshot *BalanceSnapshot) ([]byte, error) {
	if snapshot == nil {
		return nil, fmt.Errorf("snapshot cannot be nil")
	}

	// Encode balance
	balanceBytes := []byte{}
	if snapshot.Balance != nil {
		balanceBytes = snapshot.Balance.Bytes()
	}

	// Encode delta
	deltaBytes := []byte{}
	deltaSign := byte(0)
	if snapshot.Delta != nil {
		if snapshot.Delta.Sign() < 0 {
			deltaSign = 1
			deltaBytes = new(big.Int).Abs(snapshot.Delta).Bytes()
		} else {
			deltaBytes = snapshot.Delta.Bytes()
		}
	}

	// Calculate total size
	totalSize := 8 + 8 + len(balanceBytes) + 1 + 8 + len(deltaBytes) + 32

	buf := make([]byte, totalSize)
	offset := 0

	// Write block number
	binary.BigEndian.PutUint64(buf[offset:offset+8], snapshot.BlockNumber)
	offset += 8

	// Write balance length
	binary.BigEndian.PutUint64(buf[offset:offset+8], uint64(len(balanceBytes)))
	offset += 8

	// Write balance bytes
	copy(buf[offset:offset+len(balanceBytes)], balanceBytes)
	offset += len(balanceBytes)

	// Write delta sign
	buf[offset] = deltaSign
	offset++

	// Write delta length
	binary.BigEndian.PutUint64(buf[offset:offset+8], uint64(len(deltaBytes)))
	offset += 8

	// Write delta bytes
	copy(buf[offset:offset+len(deltaBytes)], deltaBytes)
	offset += len(deltaBytes)

	// Write transaction hash
	copy(buf[offset:offset+32], snapshot.TxHash[:])

	return buf, nil
}

// DecodeBalanceSnapshot decodes a BalanceSnapshot
func DecodeBalanceSnapshot(data []byte) (*BalanceSnapshot, error) {
	if len(data) < 8+8+1+8+32 {
		return nil, fmt.Errorf("data too short: %d bytes", len(data))
	}

	offset := 0

	// Read block number
	blockNumber := binary.BigEndian.Uint64(data[offset : offset+8])
	offset += 8

	// Read balance length
	balanceLen := binary.BigEndian.Uint64(data[offset : offset+8])
	offset += 8

	// Read balance bytes
	var balance *big.Int
	if balanceLen > 0 {
		if offset+int(balanceLen) > len(data) {
			return nil, fmt.Errorf("invalid balance length: %d", balanceLen)
		}
		balance = new(big.Int).SetBytes(data[offset : offset+int(balanceLen)])
		offset += int(balanceLen)
	} else {
		balance = big.NewInt(0)
	}

	// Read delta sign
	if offset >= len(data) {
		return nil, fmt.Errorf("data too short for delta sign")
	}
	deltaSign := data[offset]
	offset++

	// Read delta length
	if offset+8 > len(data) {
		return nil, fmt.Errorf("data too short for delta length")
	}
	deltaLen := binary.BigEndian.Uint64(data[offset : offset+8])
	offset += 8

	// Read delta bytes
	var delta *big.Int
	if deltaLen > 0 {
		if offset+int(deltaLen) > len(data) {
			return nil, fmt.Errorf("invalid delta length: %d", deltaLen)
		}
		delta = new(big.Int).SetBytes(data[offset : offset+int(deltaLen)])
		offset += int(deltaLen)
		if deltaSign == 1 {
			delta = delta.Neg(delta)
		}
	} else {
		delta = big.NewInt(0)
	}

	// Read transaction hash
	if offset+32 > len(data) {
		return nil, fmt.Errorf("data too short for tx hash")
	}
	var txHash common.Hash
	copy(txHash[:], data[offset:offset+32])

	return &BalanceSnapshot{
		BlockNumber: blockNumber,
		Balance:     balance,
		Delta:       delta,
		TxHash:      txHash,
	}, nil
}

// EncodeBigInt encodes a big.Int to bytes
func EncodeBigInt(n *big.Int) []byte {
	if n == nil {
		return []byte{}
	}
	return n.Bytes()
}

// DecodeBigInt decodes bytes to a big.Int
func DecodeBigInt(data []byte) *big.Int {
	if len(data) == 0 {
		return big.NewInt(0)
	}
	return new(big.Int).SetBytes(data)
}

// System Contract Event Encoders

// EncodeMintEvent encodes a MintEvent using RLP
func EncodeMintEvent(event *MintEvent) ([]byte, error) {
	if event == nil {
		return nil, fmt.Errorf("event cannot be nil")
	}

	var buf bytes.Buffer
	if err := rlp.Encode(&buf, event); err != nil {
		return nil, fmt.Errorf("failed to encode mint event: %w", err)
	}

	return buf.Bytes(), nil
}

// DecodeMintEvent decodes a MintEvent from RLP
func DecodeMintEvent(data []byte) (*MintEvent, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("data cannot be empty")
	}

	var event MintEvent
	if err := rlp.DecodeBytes(data, &event); err != nil {
		return nil, fmt.Errorf("failed to decode mint event: %w", err)
	}

	return &event, nil
}

// EncodeBurnEvent encodes a BurnEvent using RLP
func EncodeBurnEvent(event *BurnEvent) ([]byte, error) {
	if event == nil {
		return nil, fmt.Errorf("event cannot be nil")
	}

	var buf bytes.Buffer
	if err := rlp.Encode(&buf, event); err != nil {
		return nil, fmt.Errorf("failed to encode burn event: %w", err)
	}

	return buf.Bytes(), nil
}

// DecodeBurnEvent decodes a BurnEvent from RLP
func DecodeBurnEvent(data []byte) (*BurnEvent, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("data cannot be empty")
	}

	var event BurnEvent
	if err := rlp.DecodeBytes(data, &event); err != nil {
		return nil, fmt.Errorf("failed to decode burn event: %w", err)
	}

	return &event, nil
}

// EncodeMinterConfigEvent encodes a MinterConfigEvent using RLP
func EncodeMinterConfigEvent(event *MinterConfigEvent) ([]byte, error) {
	if event == nil {
		return nil, fmt.Errorf("event cannot be nil")
	}

	var buf bytes.Buffer
	if err := rlp.Encode(&buf, event); err != nil {
		return nil, fmt.Errorf("failed to encode minter config event: %w", err)
	}

	return buf.Bytes(), nil
}

// DecodeMinterConfigEvent decodes a MinterConfigEvent from RLP
func DecodeMinterConfigEvent(data []byte) (*MinterConfigEvent, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("data cannot be empty")
	}

	var event MinterConfigEvent
	if err := rlp.DecodeBytes(data, &event); err != nil {
		return nil, fmt.Errorf("failed to decode minter config event: %w", err)
	}

	return &event, nil
}

// EncodeProposal encodes a Proposal using custom binary format
// Format: [blockNumber(8)] [txHash(32)] [contract(20)] [proposalIDLen(8)] [proposalID]
//         [proposer(20)] [actionType(32)] [callDataLen(8)] [callData]
//         [memberVersion(bytes)] [requiredApprovals(4)] [approved(4)] [rejected(4)]
//         [status(1)] [createdAt(8)] [hasExecutedAt(1)] [executedAt(8)?]
func EncodeProposal(proposal *Proposal) ([]byte, error) {
	if proposal == nil {
		return nil, fmt.Errorf("proposal cannot be nil")
	}

	// Encode variable length fields
	proposalIDBytes := []byte{}
	if proposal.ProposalID != nil {
		proposalIDBytes = proposal.ProposalID.Bytes()
	}

	memberVersionBytes := []byte{}
	if proposal.MemberVersion != nil {
		memberVersionBytes = proposal.MemberVersion.Bytes()
	}

	// Calculate total size
	totalSize := 8 + 32 + 20 + 8 + len(proposalIDBytes) + 20 + 32 + 8 + len(proposal.CallData) +
		8 + len(memberVersionBytes) + 4 + 4 + 4 + 1 + 8 + 1
	if proposal.ExecutedAt != nil {
		totalSize += 8
	}

	buf := make([]byte, totalSize)
	offset := 0

	// Write blockNumber
	binary.BigEndian.PutUint64(buf[offset:offset+8], proposal.BlockNumber)
	offset += 8

	// Write txHash
	copy(buf[offset:offset+32], proposal.TxHash[:])
	offset += 32

	// Write contract address
	copy(buf[offset:offset+20], proposal.Contract[:])
	offset += 20

	// Write proposalID length and bytes
	binary.BigEndian.PutUint64(buf[offset:offset+8], uint64(len(proposalIDBytes)))
	offset += 8
	copy(buf[offset:offset+len(proposalIDBytes)], proposalIDBytes)
	offset += len(proposalIDBytes)

	// Write proposer
	copy(buf[offset:offset+20], proposal.Proposer[:])
	offset += 20

	// Write actionType
	copy(buf[offset:offset+32], proposal.ActionType[:])
	offset += 32

	// Write callData length and bytes
	binary.BigEndian.PutUint64(buf[offset:offset+8], uint64(len(proposal.CallData)))
	offset += 8
	copy(buf[offset:offset+len(proposal.CallData)], proposal.CallData)
	offset += len(proposal.CallData)

	// Write memberVersion length and bytes
	binary.BigEndian.PutUint64(buf[offset:offset+8], uint64(len(memberVersionBytes)))
	offset += 8
	copy(buf[offset:offset+len(memberVersionBytes)], memberVersionBytes)
	offset += len(memberVersionBytes)

	// Write requiredApprovals
	binary.BigEndian.PutUint32(buf[offset:offset+4], proposal.RequiredApprovals)
	offset += 4

	// Write approved
	binary.BigEndian.PutUint32(buf[offset:offset+4], proposal.Approved)
	offset += 4

	// Write rejected
	binary.BigEndian.PutUint32(buf[offset:offset+4], proposal.Rejected)
	offset += 4

	// Write status
	buf[offset] = byte(proposal.Status)
	offset++

	// Write createdAt
	binary.BigEndian.PutUint64(buf[offset:offset+8], proposal.CreatedAt)
	offset += 8

	// Write executedAt (optional)
	if proposal.ExecutedAt != nil {
		buf[offset] = 1
		offset++
		binary.BigEndian.PutUint64(buf[offset:offset+8], *proposal.ExecutedAt)
	} else {
		buf[offset] = 0
	}

	return buf, nil
}

// DecodeProposal decodes a Proposal from custom binary format
func DecodeProposal(data []byte) (*Proposal, error) {
	if len(data) < 8+32+20+8+20+32+8+8+4+4+4+1+8+1 {
		return nil, fmt.Errorf("data too short: %d bytes", len(data))
	}

	offset := 0
	proposal := &Proposal{}

	// Read blockNumber
	proposal.BlockNumber = binary.BigEndian.Uint64(data[offset : offset+8])
	offset += 8

	// Read txHash
	copy(proposal.TxHash[:], data[offset:offset+32])
	offset += 32

	// Read contract address
	copy(proposal.Contract[:], data[offset:offset+20])
	offset += 20

	// Read proposalID
	proposalIDLen := binary.BigEndian.Uint64(data[offset : offset+8])
	offset += 8
	if proposalIDLen > 0 {
		if offset+int(proposalIDLen) > len(data) {
			return nil, fmt.Errorf("invalid proposalID length: %d", proposalIDLen)
		}
		proposal.ProposalID = new(big.Int).SetBytes(data[offset : offset+int(proposalIDLen)])
		offset += int(proposalIDLen)
	} else {
		proposal.ProposalID = big.NewInt(0)
	}

	// Read proposer
	copy(proposal.Proposer[:], data[offset:offset+20])
	offset += 20

	// Read actionType
	copy(proposal.ActionType[:], data[offset:offset+32])
	offset += 32

	// Read callData
	callDataLen := binary.BigEndian.Uint64(data[offset : offset+8])
	offset += 8
	if callDataLen > 0 {
		if offset+int(callDataLen) > len(data) {
			return nil, fmt.Errorf("invalid callData length: %d", callDataLen)
		}
		proposal.CallData = make([]byte, callDataLen)
		copy(proposal.CallData, data[offset:offset+int(callDataLen)])
		offset += int(callDataLen)
	}

	// Read memberVersion
	memberVersionLen := binary.BigEndian.Uint64(data[offset : offset+8])
	offset += 8
	if memberVersionLen > 0 {
		if offset+int(memberVersionLen) > len(data) {
			return nil, fmt.Errorf("invalid memberVersion length: %d", memberVersionLen)
		}
		proposal.MemberVersion = new(big.Int).SetBytes(data[offset : offset+int(memberVersionLen)])
		offset += int(memberVersionLen)
	} else {
		proposal.MemberVersion = big.NewInt(0)
	}

	// Read requiredApprovals
	proposal.RequiredApprovals = binary.BigEndian.Uint32(data[offset : offset+4])
	offset += 4

	// Read approved
	proposal.Approved = binary.BigEndian.Uint32(data[offset : offset+4])
	offset += 4

	// Read rejected
	proposal.Rejected = binary.BigEndian.Uint32(data[offset : offset+4])
	offset += 4

	// Read status
	proposal.Status = ProposalStatus(data[offset])
	offset++

	// Read createdAt
	proposal.CreatedAt = binary.BigEndian.Uint64(data[offset : offset+8])
	offset += 8

	// Read executedAt (optional)
	hasExecutedAt := data[offset]
	offset++
	if hasExecutedAt == 1 {
		if offset+8 > len(data) {
			return nil, fmt.Errorf("data too short for executedAt")
		}
		executedAt := binary.BigEndian.Uint64(data[offset : offset+8])
		proposal.ExecutedAt = &executedAt
	}

	return proposal, nil
}

// EncodeProposalVote encodes a ProposalVote using RLP
func EncodeProposalVote(vote *ProposalVote) ([]byte, error) {
	if vote == nil {
		return nil, fmt.Errorf("vote cannot be nil")
	}

	var buf bytes.Buffer
	if err := rlp.Encode(&buf, vote); err != nil {
		return nil, fmt.Errorf("failed to encode proposal vote: %w", err)
	}

	return buf.Bytes(), nil
}

// DecodeProposalVote decodes a ProposalVote from RLP
func DecodeProposalVote(data []byte) (*ProposalVote, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("data cannot be empty")
	}

	var vote ProposalVote
	if err := rlp.DecodeBytes(data, &vote); err != nil {
		return nil, fmt.Errorf("failed to decode proposal vote: %w", err)
	}

	return &vote, nil
}

// EncodeGasTipUpdateEvent encodes a GasTipUpdateEvent using RLP
func EncodeGasTipUpdateEvent(event *GasTipUpdateEvent) ([]byte, error) {
	if event == nil {
		return nil, fmt.Errorf("event cannot be nil")
	}

	var buf bytes.Buffer
	if err := rlp.Encode(&buf, event); err != nil {
		return nil, fmt.Errorf("failed to encode gas tip update event: %w", err)
	}

	return buf.Bytes(), nil
}

// DecodeGasTipUpdateEvent decodes a GasTipUpdateEvent from RLP
func DecodeGasTipUpdateEvent(data []byte) (*GasTipUpdateEvent, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("data cannot be empty")
	}

	var event GasTipUpdateEvent
	if err := rlp.DecodeBytes(data, &event); err != nil {
		return nil, fmt.Errorf("failed to decode gas tip update event: %w", err)
	}

	return &event, nil
}

// EncodeBlacklistEvent encodes a BlacklistEvent using RLP
func EncodeBlacklistEvent(event *BlacklistEvent) ([]byte, error) {
	if event == nil {
		return nil, fmt.Errorf("event cannot be nil")
	}

	var buf bytes.Buffer
	if err := rlp.Encode(&buf, event); err != nil {
		return nil, fmt.Errorf("failed to encode blacklist event: %w", err)
	}

	return buf.Bytes(), nil
}

// DecodeBlacklistEvent decodes a BlacklistEvent from RLP
func DecodeBlacklistEvent(data []byte) (*BlacklistEvent, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("data cannot be empty")
	}

	var event BlacklistEvent
	if err := rlp.DecodeBytes(data, &event); err != nil {
		return nil, fmt.Errorf("failed to decode blacklist event: %w", err)
	}

	return &event, nil
}

// EncodeValidatorChangeEvent encodes a ValidatorChangeEvent using custom binary format
// Format: [blockNumber(8)] [txHash(32)] [validator(20)] [actionLen(8)] [action] [hasOldValidator(1)] [oldValidator(20)?] [timestamp(8)]
func EncodeValidatorChangeEvent(event *ValidatorChangeEvent) ([]byte, error) {
	if event == nil {
		return nil, fmt.Errorf("event cannot be nil")
	}

	totalSize := 8 + 32 + 20 + 8 + len(event.Action) + 1 + 8
	if event.OldValidator != nil {
		totalSize += 20
	}

	buf := make([]byte, totalSize)
	offset := 0

	// Write blockNumber
	binary.BigEndian.PutUint64(buf[offset:offset+8], event.BlockNumber)
	offset += 8

	// Write txHash
	copy(buf[offset:offset+32], event.TxHash[:])
	offset += 32

	// Write validator
	copy(buf[offset:offset+20], event.Validator[:])
	offset += 20

	// Write action length and string
	binary.BigEndian.PutUint64(buf[offset:offset+8], uint64(len(event.Action)))
	offset += 8
	copy(buf[offset:offset+len(event.Action)], []byte(event.Action))
	offset += len(event.Action)

	// Write oldValidator (optional)
	if event.OldValidator != nil {
		buf[offset] = 1
		offset++
		copy(buf[offset:offset+20], (*event.OldValidator)[:])
		offset += 20
	} else {
		buf[offset] = 0
		offset++
	}

	// Write timestamp
	binary.BigEndian.PutUint64(buf[offset:offset+8], event.Timestamp)

	return buf, nil
}

// DecodeValidatorChangeEvent decodes a ValidatorChangeEvent from custom binary format
func DecodeValidatorChangeEvent(data []byte) (*ValidatorChangeEvent, error) {
	if len(data) < 8+32+20+8+1+8 {
		return nil, fmt.Errorf("data too short: %d bytes", len(data))
	}

	offset := 0
	event := &ValidatorChangeEvent{}

	// Read blockNumber
	event.BlockNumber = binary.BigEndian.Uint64(data[offset : offset+8])
	offset += 8

	// Read txHash
	copy(event.TxHash[:], data[offset:offset+32])
	offset += 32

	// Read validator
	copy(event.Validator[:], data[offset:offset+20])
	offset += 20

	// Read action
	actionLen := binary.BigEndian.Uint64(data[offset : offset+8])
	offset += 8
	if offset+int(actionLen) > len(data) {
		return nil, fmt.Errorf("invalid action length: %d", actionLen)
	}
	event.Action = string(data[offset : offset+int(actionLen)])
	offset += int(actionLen)

	// Read oldValidator (optional)
	hasOldValidator := data[offset]
	offset++
	if hasOldValidator == 1 {
		if offset+20 > len(data) {
			return nil, fmt.Errorf("data too short for oldValidator")
		}
		var oldValidator common.Address
		copy(oldValidator[:], data[offset:offset+20])
		event.OldValidator = &oldValidator
		offset += 20
	}

	// Read timestamp
	if offset+8 > len(data) {
		return nil, fmt.Errorf("data too short for timestamp")
	}
	event.Timestamp = binary.BigEndian.Uint64(data[offset : offset+8])

	return event, nil
}

// EncodeMemberChangeEvent encodes a MemberChangeEvent using custom binary format
// Format: [contract(20)] [blockNumber(8)] [txHash(32)] [member(20)] [actionLen(8)] [action]
//         [hasOldMember(1)] [oldMember(20)?] [totalMembers(8)] [newQuorum(4)] [timestamp(8)]
func EncodeMemberChangeEvent(event *MemberChangeEvent) ([]byte, error) {
	if event == nil {
		return nil, fmt.Errorf("event cannot be nil")
	}

	totalSize := 20 + 8 + 32 + 20 + 8 + len(event.Action) + 1 + 8 + 4 + 8
	if event.OldMember != nil {
		totalSize += 20
	}

	buf := make([]byte, totalSize)
	offset := 0

	// Write contract
	copy(buf[offset:offset+20], event.Contract[:])
	offset += 20

	// Write blockNumber
	binary.BigEndian.PutUint64(buf[offset:offset+8], event.BlockNumber)
	offset += 8

	// Write txHash
	copy(buf[offset:offset+32], event.TxHash[:])
	offset += 32

	// Write member
	copy(buf[offset:offset+20], event.Member[:])
	offset += 20

	// Write action length and string
	binary.BigEndian.PutUint64(buf[offset:offset+8], uint64(len(event.Action)))
	offset += 8
	copy(buf[offset:offset+len(event.Action)], []byte(event.Action))
	offset += len(event.Action)

	// Write oldMember (optional)
	if event.OldMember != nil {
		buf[offset] = 1
		offset++
		copy(buf[offset:offset+20], (*event.OldMember)[:])
		offset += 20
	} else {
		buf[offset] = 0
		offset++
	}

	// Write totalMembers
	binary.BigEndian.PutUint64(buf[offset:offset+8], event.TotalMembers)
	offset += 8

	// Write newQuorum
	binary.BigEndian.PutUint32(buf[offset:offset+4], event.NewQuorum)
	offset += 4

	// Write timestamp
	binary.BigEndian.PutUint64(buf[offset:offset+8], event.Timestamp)

	return buf, nil
}

// DecodeMemberChangeEvent decodes a MemberChangeEvent from custom binary format
func DecodeMemberChangeEvent(data []byte) (*MemberChangeEvent, error) {
	if len(data) < 20+8+32+20+8+1+8+4+8 {
		return nil, fmt.Errorf("data too short: %d bytes", len(data))
	}

	offset := 0
	event := &MemberChangeEvent{}

	// Read contract
	copy(event.Contract[:], data[offset:offset+20])
	offset += 20

	// Read blockNumber
	event.BlockNumber = binary.BigEndian.Uint64(data[offset : offset+8])
	offset += 8

	// Read txHash
	copy(event.TxHash[:], data[offset:offset+32])
	offset += 32

	// Read member
	copy(event.Member[:], data[offset:offset+20])
	offset += 20

	// Read action
	actionLen := binary.BigEndian.Uint64(data[offset : offset+8])
	offset += 8
	if offset+int(actionLen) > len(data) {
		return nil, fmt.Errorf("invalid action length: %d", actionLen)
	}
	event.Action = string(data[offset : offset+int(actionLen)])
	offset += int(actionLen)

	// Read oldMember (optional)
	hasOldMember := data[offset]
	offset++
	if hasOldMember == 1 {
		if offset+20 > len(data) {
			return nil, fmt.Errorf("data too short for oldMember")
		}
		var oldMember common.Address
		copy(oldMember[:], data[offset:offset+20])
		event.OldMember = &oldMember
		offset += 20
	}

	// Read totalMembers
	if offset+8 > len(data) {
		return nil, fmt.Errorf("data too short for totalMembers")
	}
	event.TotalMembers = binary.BigEndian.Uint64(data[offset : offset+8])
	offset += 8

	// Read newQuorum
	if offset+4 > len(data) {
		return nil, fmt.Errorf("data too short for newQuorum")
	}
	event.NewQuorum = binary.BigEndian.Uint32(data[offset : offset+4])
	offset += 4

	// Read timestamp
	if offset+8 > len(data) {
		return nil, fmt.Errorf("data too short for timestamp")
	}
	event.Timestamp = binary.BigEndian.Uint64(data[offset : offset+8])

	return event, nil
}

// EncodeEmergencyPauseEvent encodes an EmergencyPauseEvent using RLP
func EncodeEmergencyPauseEvent(event *EmergencyPauseEvent) ([]byte, error) {
	if event == nil {
		return nil, fmt.Errorf("event cannot be nil")
	}

	var buf bytes.Buffer
	if err := rlp.Encode(&buf, event); err != nil {
		return nil, fmt.Errorf("failed to encode emergency pause event: %w", err)
	}

	return buf.Bytes(), nil
}

// DecodeEmergencyPauseEvent decodes an EmergencyPauseEvent from RLP
func DecodeEmergencyPauseEvent(data []byte) (*EmergencyPauseEvent, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("data cannot be empty")
	}

	var event EmergencyPauseEvent
	if err := rlp.DecodeBytes(data, &event); err != nil {
		return nil, fmt.Errorf("failed to decode emergency pause event: %w", err)
	}

	return &event, nil
}

// EncodeDepositMintProposal encodes a DepositMintProposal using RLP
func EncodeDepositMintProposal(proposal *DepositMintProposal) ([]byte, error) {
	if proposal == nil {
		return nil, fmt.Errorf("proposal cannot be nil")
	}

	var buf bytes.Buffer
	if err := rlp.Encode(&buf, proposal); err != nil {
		return nil, fmt.Errorf("failed to encode deposit mint proposal: %w", err)
	}

	return buf.Bytes(), nil
}

// DecodeDepositMintProposal decodes a DepositMintProposal from RLP
func DecodeDepositMintProposal(data []byte) (*DepositMintProposal, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("data cannot be empty")
	}

	var proposal DepositMintProposal
	if err := rlp.DecodeBytes(data, &proposal); err != nil {
		return nil, fmt.Errorf("failed to decode deposit mint proposal: %w", err)
	}

	return &proposal, nil
}
