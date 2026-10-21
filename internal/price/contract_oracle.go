package price

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"go.uber.org/zap"
)

// PriceOracleABI is the ABI for the Price Oracle contract
// This should be updated when the actual contract is deployed
const PriceOracleABI = `[
	{
		"inputs": [{"internalType": "address", "name": "token", "type": "address"}],
		"name": "getPrice",
		"outputs": [{"internalType": "uint256", "name": "", "type": "uint256"}],
		"stateMutability": "view",
		"type": "function"
	},
	{
		"inputs": [],
		"name": "getNativePrice",
		"outputs": [{"internalType": "uint256", "name": "", "type": "uint256"}],
		"stateMutability": "view",
		"type": "function"
	}
]`

// ContractOracle queries prices from a deployed Price Oracle contract
type ContractOracle struct {
	client          *ethclient.Client
	contractAddress common.Address
	abi             abi.ABI
	logger          *zap.Logger
	available       bool
}

// NewContractOracle creates a new contract-based oracle
// If the contract is not deployed or not responding, available will be false
func NewContractOracle(client *ethclient.Client, contractAddress common.Address, logger *zap.Logger) (*ContractOracle, error) {
	if logger == nil {
		logger = zap.NewNop()
	}

	parsedABI, err := abi.JSON(strings.NewReader(PriceOracleABI))
	if err != nil {
		return nil, fmt.Errorf("failed to parse ABI: %w", err)
	}

	oracle := &ContractOracle{
		client:          client,
		contractAddress: contractAddress,
		abi:             parsedABI,
		logger:          logger,
		available:       false,
	}

	// Check if contract is deployed and responding
	oracle.checkAvailability()

	return oracle, nil
}

// checkAvailability tests if the oracle contract is deployed and responding
func (o *ContractOracle) checkAvailability() {
	if o.client == nil || o.contractAddress == (common.Address{}) {
		o.available = false
		return
	}

	// Try to get the native price to test if contract is available
	ctx := context.Background()
	code, err := o.client.CodeAt(ctx, o.contractAddress, nil)
	if err != nil || len(code) == 0 {
		o.logger.Info("Price Oracle contract not deployed",
			zap.String("address", o.contractAddress.Hex()))
		o.available = false
		return
	}

	// Try a test call
	_, err = o.GetNativePrice(ctx)
	if err != nil {
		o.logger.Info("Price Oracle contract not responding",
			zap.String("address", o.contractAddress.Hex()),
			zap.Error(err))
		o.available = false
		return
	}

	o.logger.Info("Price Oracle contract available",
		zap.String("address", o.contractAddress.Hex()))
	o.available = true
}

// IsAvailable returns whether the oracle is available
func (o *ContractOracle) IsAvailable() bool {
	return o.available
}

// GetTokenPrice returns the price of a token in native coin (wei)
func (o *ContractOracle) GetTokenPrice(ctx context.Context, tokenAddress common.Address) (*big.Int, error) {
	if !o.available {
		return nil, nil
	}

	data, err := o.abi.Pack("getPrice", tokenAddress)
	if err != nil {
		return nil, fmt.Errorf("failed to pack getPrice call: %w", err)
	}

	result, err := o.client.CallContract(ctx, ethereum.CallMsg{
		To:   &o.contractAddress,
		Data: data,
	}, nil)
	if err != nil {
		o.logger.Warn("Failed to get token price",
			zap.String("token", tokenAddress.Hex()),
			zap.Error(err))
		return nil, err
	}

	if len(result) == 0 {
		return nil, nil
	}

	var price *big.Int
	err = o.abi.UnpackIntoInterface(&price, "getPrice", result)
	if err != nil {
		return nil, fmt.Errorf("failed to unpack getPrice result: %w", err)
	}

	return price, nil
}

// GetNativePrice returns the price of native coin in USD (scaled by 1e8)
func (o *ContractOracle) GetNativePrice(ctx context.Context) (*big.Int, error) {
	if !o.available && o.client != nil {
		// Re-check availability
		o.checkAvailability()
		if !o.available {
			return nil, nil
		}
	}

	if o.client == nil {
		return nil, nil
	}

	data, err := o.abi.Pack("getNativePrice")
	if err != nil {
		return nil, fmt.Errorf("failed to pack getNativePrice call: %w", err)
	}

	result, err := o.client.CallContract(ctx, ethereum.CallMsg{
		To:   &o.contractAddress,
		Data: data,
	}, nil)
	if err != nil {
		return nil, err
	}

	if len(result) == 0 {
		return nil, nil
	}

	var price *big.Int
	err = o.abi.UnpackIntoInterface(&price, "getNativePrice", result)
	if err != nil {
		return nil, fmt.Errorf("failed to unpack getNativePrice result: %w", err)
	}

	return price, nil
}

// GetTokenValue calculates the value of tokens in native coin
func (o *ContractOracle) GetTokenValue(ctx context.Context, tokenAddress common.Address, amount *big.Int, decimals uint8) (*big.Int, error) {
	if !o.available || amount == nil || amount.Sign() == 0 {
		return nil, nil
	}

	price, err := o.GetTokenPrice(ctx, tokenAddress)
	if err != nil {
		return nil, err
	}
	if price == nil {
		return nil, nil
	}

	// Calculate value: amount * price / 10^decimals
	value := new(big.Int).Mul(amount, price)
	divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)
	value.Div(value, divisor)

	return value, nil
}
