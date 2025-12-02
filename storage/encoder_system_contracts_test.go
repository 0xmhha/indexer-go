package storage

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncodeDecode_MintEvent(t *testing.T) {
	event := &MintEvent{
		BlockNumber: 12345,
		TxHash:      common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"),
		Minter:      common.HexToAddress("0xMINTER1234567890123456789012345678901234"),
		To:          common.HexToAddress("0xTO12345678901234567890123456789012345678"),
		Amount:      big.NewInt(1000000000000000000), // 1 ETH
		Timestamp:   1234567890,
	}

	// Encode
	encoded, err := EncodeMintEvent(event)
	require.NoError(t, err)
	require.NotEmpty(t, encoded)

	// Decode
	decoded, err := DecodeMintEvent(encoded)
	require.NoError(t, err)
	assert.Equal(t, event.BlockNumber, decoded.BlockNumber)
	assert.Equal(t, event.TxHash, decoded.TxHash)
	assert.Equal(t, event.Minter, decoded.Minter)
	assert.Equal(t, event.To, decoded.To)
	assert.Equal(t, event.Amount.String(), decoded.Amount.String())
	assert.Equal(t, event.Timestamp, decoded.Timestamp)

	// Test nil event encoding
	_, err = EncodeMintEvent(nil)
	assert.Error(t, err)

	// Test empty data decoding
	_, err = DecodeMintEvent([]byte{})
	assert.Error(t, err)
}

func TestEncodeDecode_BurnEvent(t *testing.T) {
	event := &BurnEvent{
		BlockNumber:  12345,
		TxHash:       common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"),
		Burner:       common.HexToAddress("0xBURNER1234567890123456789012345678901234"),
		Amount:       big.NewInt(500000000000000000), // 0.5 ETH
		Timestamp:    1234567890,
		WithdrawalID: "withdrawal-12345",
	}

	// Encode
	encoded, err := EncodeBurnEvent(event)
	require.NoError(t, err)
	require.NotEmpty(t, encoded)

	// Decode
	decoded, err := DecodeBurnEvent(encoded)
	require.NoError(t, err)
	assert.Equal(t, event.BlockNumber, decoded.BlockNumber)
	assert.Equal(t, event.TxHash, decoded.TxHash)
	assert.Equal(t, event.Burner, decoded.Burner)
	assert.Equal(t, event.Amount.String(), decoded.Amount.String())
	assert.Equal(t, event.Timestamp, decoded.Timestamp)
	assert.Equal(t, event.WithdrawalID, decoded.WithdrawalID)

	// Test nil event encoding
	_, err = EncodeBurnEvent(nil)
	assert.Error(t, err)

	// Test empty data decoding
	_, err = DecodeBurnEvent([]byte{})
	assert.Error(t, err)
}

func TestEncodeDecode_MinterConfigEvent(t *testing.T) {
	allowance := new(big.Int)
	allowance.SetString("10000000000000000000", 10) // 10 ETH
	event := &MinterConfigEvent{
		BlockNumber: 12345,
		TxHash:      common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"),
		Minter:      common.HexToAddress("0xMINTER1234567890123456789012345678901234"),
		Allowance:   allowance,
		Action:      "configured",
		Timestamp:   1234567890,
	}

	// Encode
	encoded, err := EncodeMinterConfigEvent(event)
	require.NoError(t, err)
	require.NotEmpty(t, encoded)

	// Decode
	decoded, err := DecodeMinterConfigEvent(encoded)
	require.NoError(t, err)
	assert.Equal(t, event.BlockNumber, decoded.BlockNumber)
	assert.Equal(t, event.TxHash, decoded.TxHash)
	assert.Equal(t, event.Minter, decoded.Minter)
	assert.Equal(t, event.Allowance.String(), decoded.Allowance.String())
	assert.Equal(t, event.Action, decoded.Action)
	assert.Equal(t, event.Timestamp, decoded.Timestamp)

	// Test nil event encoding
	_, err = EncodeMinterConfigEvent(nil)
	assert.Error(t, err)

	// Test empty data decoding
	_, err = DecodeMinterConfigEvent([]byte{})
	assert.Error(t, err)
}

func TestEncodeDecode_Proposal(t *testing.T) {
	executedAt := uint64(1234567900)
	proposal := &Proposal{
		Contract:          common.HexToAddress("0xCONTRACT123456789012345678901234567890"),
		ProposalID:        big.NewInt(42),
		Proposer:          common.HexToAddress("0xPROPOSER123456789012345678901234567890"),
		ActionType:        [32]byte{1, 2, 3, 4, 5},
		CallData:          []byte{0x12, 0x34, 0x56, 0x78},
		MemberVersion:     big.NewInt(1),
		RequiredApprovals: 5,
		Approved:          3,
		Rejected:          1,
		Status:            ProposalStatusVoting,
		CreatedAt:         1234567890,
		ExecutedAt:        &executedAt,
		BlockNumber:       12345,
		TxHash:            common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"),
	}

	// Encode
	encoded, err := EncodeProposal(proposal)
	require.NoError(t, err)
	require.NotEmpty(t, encoded)

	// Decode
	decoded, err := DecodeProposal(encoded)
	require.NoError(t, err)
	assert.Equal(t, proposal.Contract, decoded.Contract)
	assert.Equal(t, proposal.ProposalID.String(), decoded.ProposalID.String())
	assert.Equal(t, proposal.Proposer, decoded.Proposer)
	assert.Equal(t, proposal.ActionType, decoded.ActionType)
	assert.Equal(t, proposal.CallData, decoded.CallData)
	assert.Equal(t, proposal.MemberVersion.String(), decoded.MemberVersion.String())
	assert.Equal(t, proposal.RequiredApprovals, decoded.RequiredApprovals)
	assert.Equal(t, proposal.Approved, decoded.Approved)
	assert.Equal(t, proposal.Rejected, decoded.Rejected)
	assert.Equal(t, proposal.Status, decoded.Status)
	assert.Equal(t, proposal.CreatedAt, decoded.CreatedAt)
	assert.NotNil(t, decoded.ExecutedAt)
	assert.Equal(t, *proposal.ExecutedAt, *decoded.ExecutedAt)
	assert.Equal(t, proposal.BlockNumber, decoded.BlockNumber)
	assert.Equal(t, proposal.TxHash, decoded.TxHash)

	// Test nil event encoding
	_, err = EncodeProposal(nil)
	assert.Error(t, err)

	// Test empty data decoding
	_, err = DecodeProposal([]byte{})
	assert.Error(t, err)
}

func TestEncodeDecode_Proposal_NoExecutedAt(t *testing.T) {
	proposal := &Proposal{
		Contract:          common.HexToAddress("0xCONTRACT123456789012345678901234567890"),
		ProposalID:        big.NewInt(42),
		Proposer:          common.HexToAddress("0xPROPOSER123456789012345678901234567890"),
		ActionType:        [32]byte{1, 2, 3, 4, 5},
		CallData:          []byte{0x12, 0x34, 0x56, 0x78},
		MemberVersion:     big.NewInt(1),
		RequiredApprovals: 5,
		Approved:          3,
		Rejected:          1,
		Status:            ProposalStatusVoting,
		CreatedAt:         1234567890,
		ExecutedAt:        nil,
		BlockNumber:       12345,
		TxHash:            common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"),
	}

	// Encode
	encoded, err := EncodeProposal(proposal)
	require.NoError(t, err)
	require.NotEmpty(t, encoded)

	// Decode
	decoded, err := DecodeProposal(encoded)
	require.NoError(t, err)
	assert.Nil(t, decoded.ExecutedAt)
}

func TestEncodeDecode_ProposalVote(t *testing.T) {
	vote := &ProposalVote{
		Contract:    common.HexToAddress("0xCONTRACT123456789012345678901234567890"),
		ProposalID:  big.NewInt(42),
		Voter:       common.HexToAddress("0xVOTER12345678901234567890123456789012345"),
		Approval:    true,
		BlockNumber: 12345,
		TxHash:      common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"),
		Timestamp:   1234567890,
	}

	// Encode
	encoded, err := EncodeProposalVote(vote)
	require.NoError(t, err)
	require.NotEmpty(t, encoded)

	// Decode
	decoded, err := DecodeProposalVote(encoded)
	require.NoError(t, err)
	assert.Equal(t, vote.Contract, decoded.Contract)
	assert.Equal(t, vote.ProposalID.String(), decoded.ProposalID.String())
	assert.Equal(t, vote.Voter, decoded.Voter)
	assert.Equal(t, vote.Approval, decoded.Approval)
	assert.Equal(t, vote.BlockNumber, decoded.BlockNumber)
	assert.Equal(t, vote.TxHash, decoded.TxHash)
	assert.Equal(t, vote.Timestamp, decoded.Timestamp)

	// Test nil event encoding
	_, err = EncodeProposalVote(nil)
	assert.Error(t, err)

	// Test empty data decoding
	_, err = DecodeProposalVote([]byte{})
	assert.Error(t, err)
}

func TestEncodeDecode_GasTipUpdateEvent(t *testing.T) {
	event := &GasTipUpdateEvent{
		BlockNumber: 12345,
		TxHash:      common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"),
		OldTip:      big.NewInt(1000000000), // 1 Gwei
		NewTip:      big.NewInt(2000000000), // 2 Gwei
		Updater:     common.HexToAddress("0xUPDATER123456789012345678901234567890"),
		Timestamp:   1234567890,
	}

	// Encode
	encoded, err := EncodeGasTipUpdateEvent(event)
	require.NoError(t, err)
	require.NotEmpty(t, encoded)

	// Decode
	decoded, err := DecodeGasTipUpdateEvent(encoded)
	require.NoError(t, err)
	assert.Equal(t, event.BlockNumber, decoded.BlockNumber)
	assert.Equal(t, event.TxHash, decoded.TxHash)
	assert.Equal(t, event.OldTip.String(), decoded.OldTip.String())
	assert.Equal(t, event.NewTip.String(), decoded.NewTip.String())
	assert.Equal(t, event.Updater, decoded.Updater)
	assert.Equal(t, event.Timestamp, decoded.Timestamp)

	// Test nil event encoding
	_, err = EncodeGasTipUpdateEvent(nil)
	assert.Error(t, err)

	// Test empty data decoding
	_, err = DecodeGasTipUpdateEvent([]byte{})
	assert.Error(t, err)
}

func TestEncodeDecode_BlacklistEvent(t *testing.T) {
	event := &BlacklistEvent{
		BlockNumber: 12345,
		TxHash:      common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"),
		Account:     common.HexToAddress("0xACCOUNT123456789012345678901234567890"),
		Action:      "blacklisted",
		ProposalID:  big.NewInt(42),
		Timestamp:   1234567890,
	}

	// Encode
	encoded, err := EncodeBlacklistEvent(event)
	require.NoError(t, err)
	require.NotEmpty(t, encoded)

	// Decode
	decoded, err := DecodeBlacklistEvent(encoded)
	require.NoError(t, err)
	assert.Equal(t, event.BlockNumber, decoded.BlockNumber)
	assert.Equal(t, event.TxHash, decoded.TxHash)
	assert.Equal(t, event.Account, decoded.Account)
	assert.Equal(t, event.Action, decoded.Action)
	assert.Equal(t, event.ProposalID.String(), decoded.ProposalID.String())
	assert.Equal(t, event.Timestamp, decoded.Timestamp)

	// Test nil event encoding
	_, err = EncodeBlacklistEvent(nil)
	assert.Error(t, err)

	// Test empty data decoding
	_, err = DecodeBlacklistEvent([]byte{})
	assert.Error(t, err)
}

func TestEncodeDecode_ValidatorChangeEvent(t *testing.T) {
	oldValidator := common.HexToAddress("0xOLDVALIDATOR12345678901234567890123456")
	event := &ValidatorChangeEvent{
		BlockNumber:  12345,
		TxHash:       common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"),
		Validator:    common.HexToAddress("0xVALIDATOR12345678901234567890123456789"),
		Action:       "changed",
		OldValidator: &oldValidator,
		Timestamp:    1234567890,
	}

	// Encode
	encoded, err := EncodeValidatorChangeEvent(event)
	require.NoError(t, err)
	require.NotEmpty(t, encoded)

	// Decode
	decoded, err := DecodeValidatorChangeEvent(encoded)
	require.NoError(t, err)
	assert.Equal(t, event.BlockNumber, decoded.BlockNumber)
	assert.Equal(t, event.TxHash, decoded.TxHash)
	assert.Equal(t, event.Validator, decoded.Validator)
	assert.Equal(t, event.Action, decoded.Action)
	assert.NotNil(t, decoded.OldValidator)
	assert.Equal(t, *event.OldValidator, *decoded.OldValidator)
	assert.Equal(t, event.Timestamp, decoded.Timestamp)

	// Test nil event encoding
	_, err = EncodeValidatorChangeEvent(nil)
	assert.Error(t, err)

	// Test empty data decoding
	_, err = DecodeValidatorChangeEvent([]byte{})
	assert.Error(t, err)
}

func TestEncodeDecode_ValidatorChangeEvent_NoOldValidator(t *testing.T) {
	event := &ValidatorChangeEvent{
		BlockNumber:  12345,
		TxHash:       common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"),
		Validator:    common.HexToAddress("0xVALIDATOR12345678901234567890123456789"),
		Action:       "added",
		OldValidator: nil,
		Timestamp:    1234567890,
	}

	// Encode
	encoded, err := EncodeValidatorChangeEvent(event)
	require.NoError(t, err)
	require.NotEmpty(t, encoded)

	// Decode
	decoded, err := DecodeValidatorChangeEvent(encoded)
	require.NoError(t, err)
	assert.Nil(t, decoded.OldValidator)
}

func TestEncodeDecode_MemberChangeEvent(t *testing.T) {
	oldMember := common.HexToAddress("0xOLDMEMBER12345678901234567890123456789")
	event := &MemberChangeEvent{
		Contract:     common.HexToAddress("0xCONTRACT123456789012345678901234567890"),
		BlockNumber:  12345,
		TxHash:       common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"),
		Member:       common.HexToAddress("0xMEMBER1234567890123456789012345678901"),
		Action:       "changed",
		OldMember:    &oldMember,
		TotalMembers: 10,
		NewQuorum:    7,
		Timestamp:    1234567890,
	}

	// Encode
	encoded, err := EncodeMemberChangeEvent(event)
	require.NoError(t, err)
	require.NotEmpty(t, encoded)

	// Decode
	decoded, err := DecodeMemberChangeEvent(encoded)
	require.NoError(t, err)
	assert.Equal(t, event.Contract, decoded.Contract)
	assert.Equal(t, event.BlockNumber, decoded.BlockNumber)
	assert.Equal(t, event.TxHash, decoded.TxHash)
	assert.Equal(t, event.Member, decoded.Member)
	assert.Equal(t, event.Action, decoded.Action)
	assert.NotNil(t, decoded.OldMember)
	assert.Equal(t, *event.OldMember, *decoded.OldMember)
	assert.Equal(t, event.TotalMembers, decoded.TotalMembers)
	assert.Equal(t, event.NewQuorum, decoded.NewQuorum)
	assert.Equal(t, event.Timestamp, decoded.Timestamp)

	// Test nil event encoding
	_, err = EncodeMemberChangeEvent(nil)
	assert.Error(t, err)

	// Test empty data decoding
	_, err = DecodeMemberChangeEvent([]byte{})
	assert.Error(t, err)
}

func TestEncodeDecode_MemberChangeEvent_NoOldMember(t *testing.T) {
	event := &MemberChangeEvent{
		Contract:     common.HexToAddress("0xCONTRACT123456789012345678901234567890"),
		BlockNumber:  12345,
		TxHash:       common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"),
		Member:       common.HexToAddress("0xMEMBER1234567890123456789012345678901"),
		Action:       "added",
		OldMember:    nil,
		TotalMembers: 10,
		NewQuorum:    7,
		Timestamp:    1234567890,
	}

	// Encode
	encoded, err := EncodeMemberChangeEvent(event)
	require.NoError(t, err)
	require.NotEmpty(t, encoded)

	// Decode
	decoded, err := DecodeMemberChangeEvent(encoded)
	require.NoError(t, err)
	assert.Nil(t, decoded.OldMember)
}

func TestEncodeDecode_EmergencyPauseEvent(t *testing.T) {
	event := &EmergencyPauseEvent{
		Contract:    common.HexToAddress("0xCONTRACT123456789012345678901234567890"),
		BlockNumber: 12345,
		TxHash:      common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"),
		ProposalID:  big.NewInt(42),
		Action:      "paused",
		Timestamp:   1234567890,
	}

	// Encode
	encoded, err := EncodeEmergencyPauseEvent(event)
	require.NoError(t, err)
	require.NotEmpty(t, encoded)

	// Decode
	decoded, err := DecodeEmergencyPauseEvent(encoded)
	require.NoError(t, err)
	assert.Equal(t, event.Contract, decoded.Contract)
	assert.Equal(t, event.BlockNumber, decoded.BlockNumber)
	assert.Equal(t, event.TxHash, decoded.TxHash)
	assert.Equal(t, event.ProposalID.String(), decoded.ProposalID.String())
	assert.Equal(t, event.Action, decoded.Action)
	assert.Equal(t, event.Timestamp, decoded.Timestamp)

	// Test nil event encoding
	_, err = EncodeEmergencyPauseEvent(nil)
	assert.Error(t, err)

	// Test empty data decoding
	_, err = DecodeEmergencyPauseEvent([]byte{})
	assert.Error(t, err)
}

func TestEncodeDecode_DepositMintProposal(t *testing.T) {
	proposal := &DepositMintProposal{
		ProposalID:    big.NewInt(42),
		Requester:     common.HexToAddress("0xREQUESTER12345678901234567890123456"),
		Beneficiary:   common.HexToAddress("0xBENEFICIARY1234567890123456789012345"),
		Amount:        big.NewInt(1000000000000000000), // 1 ETH
		DepositID:     "deposit-12345",
		BankReference: "bank-ref-001",
		Status:        ProposalStatusVoting,
		BlockNumber:   12345,
		TxHash:        common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"),
		Timestamp:     1234567890,
	}

	// Encode
	encoded, err := EncodeDepositMintProposal(proposal)
	require.NoError(t, err)
	require.NotEmpty(t, encoded)

	// Decode
	decoded, err := DecodeDepositMintProposal(encoded)
	require.NoError(t, err)
	assert.Equal(t, proposal.ProposalID.String(), decoded.ProposalID.String())
	assert.Equal(t, proposal.Requester, decoded.Requester)
	assert.Equal(t, proposal.Beneficiary, decoded.Beneficiary)
	assert.Equal(t, proposal.Amount.String(), decoded.Amount.String())
	assert.Equal(t, proposal.DepositID, decoded.DepositID)
	assert.Equal(t, proposal.BankReference, decoded.BankReference)
	assert.Equal(t, proposal.Status, decoded.Status)
	assert.Equal(t, proposal.BlockNumber, decoded.BlockNumber)
	assert.Equal(t, proposal.TxHash, decoded.TxHash)
	assert.Equal(t, proposal.Timestamp, decoded.Timestamp)

	// Test nil event encoding
	_, err = EncodeDepositMintProposal(nil)
	assert.Error(t, err)

	// Test empty data decoding
	_, err = DecodeDepositMintProposal([]byte{})
	assert.Error(t, err)
}
