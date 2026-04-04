package userop

import (
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// SponsorType represents how a UserOperation's gas is sponsored
type SponsorType string

const (
	SponsorWalletDeposit   SponsorType = "wallet_deposit"
	SponsorWalletBalance   SponsorType = "wallet_balance"
	SponsorPaymaster       SponsorType = "paymaster_sponsor"
	SponsorPaymasterHybrid SponsorType = "paymaster_hybrid"
)

// EntryPointVersion represents the version of the EntryPoint contract
type EntryPointVersion string

const (
	EntryPointV06 EntryPointVersion = "v0.6"
	EntryPointV07 EntryPointVersion = "v0.7"
)

// KnownEntryPoints maps known EntryPoint contract addresses to their versions
var KnownEntryPoints = map[common.Address]EntryPointVersion{
	common.HexToAddress("0x5FF137D4b0FDCD49DcA30c7CF57E578a026d2789"): EntryPointV06,
	common.HexToAddress("0x0000000071727De22E5E9d8BAf0edAc6f37da032"): EntryPointV07,
}

// Event signatures for ERC-4337 EntryPoint events
var (
	// UserOperationEvent(bytes32 indexed userOpHash, address indexed sender, address indexed paymaster, uint256 nonce, bool success, uint256 actualGasCost, uint256 actualGasUsed)
	UserOperationEventSig = crypto.Keccak256Hash([]byte("UserOperationEvent(bytes32,address,address,uint256,bool,uint256,uint256)"))

	// AccountDeployed(bytes32 indexed userOpHash, address indexed sender, address factory, address paymaster)
	AccountDeployedSig = crypto.Keccak256Hash([]byte("AccountDeployed(bytes32,address,address,address)"))

	// UserOperationRevertReason(bytes32 indexed userOpHash, address indexed sender, uint256 nonce, bytes revertReason)
	UserOperationRevertReasonSig = crypto.Keccak256Hash([]byte("UserOperationRevertReason(bytes32,address,uint256,bytes)"))
)

// UserOperation represents an ERC-4337 UserOperation that was included in a bundle
type UserOperation struct {
	// Core UserOp fields
	Hash                 common.Hash    `json:"hash"`
	Sender               common.Address `json:"sender"`
	Nonce                string         `json:"nonce"` // *big.Int serialized as string
	CallData             []byte         `json:"callData"`
	CallGasLimit         string         `json:"callGasLimit"`         // *big.Int as string
	VerificationGasLimit string         `json:"verificationGasLimit"` // *big.Int as string
	PreVerificationGas   string         `json:"preVerificationGas"`   // *big.Int as string
	MaxFeePerGas         string         `json:"maxFeePerGas"`         // *big.Int as string
	MaxPriorityFeePerGas string         `json:"maxPriorityFeePerGas"` // *big.Int as string
	Signature            []byte         `json:"signature"`

	// EntryPoint info
	EntryPoint        common.Address `json:"entryPoint"`
	EntryPointVersion string         `json:"entryPointVersion"`

	// Transaction context
	TransactionHash common.Hash `json:"transactionHash"`
	BlockNumber     uint64      `json:"blockNumber"`
	BlockHash       common.Hash `json:"blockHash"`
	BundleIndex     uint32      `json:"bundleIndex"`

	// Participants
	Bundler   common.Address  `json:"bundler"`
	Factory   *common.Address `json:"factory,omitempty"`
	Paymaster *common.Address `json:"paymaster,omitempty"`

	// Execution result
	Status        bool        `json:"status"`
	RevertReason  []byte      `json:"revertReason,omitempty"`
	GasUsed       string      `json:"gasUsed"`       // *big.Int as string
	ActualGasCost string      `json:"actualGasCost"` // *big.Int as string
	SponsorType   SponsorType `json:"sponsorType"`

	// Log range for this UserOp within the transaction
	UserLogsStartIndex uint32 `json:"userLogsStartIndex"`
	UserLogsCount      uint32 `json:"userLogsCount"`

	// Timestamp
	Timestamp time.Time `json:"timestamp"`
}

// BundlerStats represents statistics for a bundler address
type BundlerStats struct {
	Address      common.Address `json:"address"`
	TotalBundles uint64         `json:"totalBundles"`
	TotalOps     uint64         `json:"totalOps"`
}

// FactoryStats represents statistics for a factory address
type FactoryStats struct {
	Address       common.Address `json:"address"`
	TotalAccounts uint64         `json:"totalAccounts"`
}

// PaymasterStats represents statistics for a paymaster address
type PaymasterStats struct {
	Address  common.Address `json:"address"`
	TotalOps uint64         `json:"totalOps"`
}

// SmartAccount represents an ERC-4337 smart contract account
type SmartAccount struct {
	Address           common.Address  `json:"address"`
	CreationOpHash    *common.Hash    `json:"creationOpHash,omitempty"`
	CreationTxHash    *common.Hash    `json:"creationTxHash,omitempty"`
	CreationTimestamp *time.Time      `json:"creationTimestamp,omitempty"`
	Factory           *common.Address `json:"factory,omitempty"`
	TotalOps          uint64          `json:"totalOps"`
}

// IsKnownEntryPoint returns whether the given address is a known EntryPoint contract
func IsKnownEntryPoint(addr common.Address) bool {
	_, ok := KnownEntryPoints[addr]
	return ok
}

// GetEntryPointVersion returns the version of a known EntryPoint, or empty string if unknown
func GetEntryPointVersion(addr common.Address) string {
	if v, ok := KnownEntryPoints[addr]; ok {
		return string(v)
	}
	return ""
}

// DetermineSponsorType determines how a UserOperation's gas is sponsored
func DetermineSponsorType(paymaster *common.Address) SponsorType {
	if paymaster == nil || *paymaster == (common.Address{}) {
		return SponsorWalletDeposit
	}
	return SponsorPaymaster
}

// Ensure big.Int usage to avoid unused import
var _ = big.NewInt(0)
