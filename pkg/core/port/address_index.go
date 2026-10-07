package port

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

// ContractCreation represents a contract creation event
type ContractCreation struct {
	ContractAddress common.Address `json:"contractAddress"` // 생성된 컨트랙트 주소
	Creator         common.Address `json:"creator"`         // 생성자 주소
	TransactionHash common.Hash    `json:"transactionHash"` // 생성 트랜잭션 해시
	BlockNumber     uint64         `json:"blockNumber"`     // 블록 번호
	Timestamp       uint64         `json:"timestamp"`       // 생성 시각
	BytecodeSize    int            `json:"bytecodeSize"`    // 배포된 바이트코드 크기
}

// InternalTransaction represents an internal call during transaction execution
type InternalTransaction struct {
	TransactionHash common.Hash    `json:"transactionHash"` // 원본 트랜잭션 해시
	BlockNumber     uint64         `json:"blockNumber"`     // 블록 번호
	Index           int            `json:"index"`           // 순서 인덱스
	Type            string         `json:"type"`            // CALL, DELEGATECALL, STATICCALL, CREATE, etc.
	From            common.Address `json:"from"`            // 호출자 주소
	To              common.Address `json:"to"`              // 피호출자 주소 (CREATE의 경우 생성된 주소)
	Value           *big.Int       `json:"value"`           // 전송된 ETH 양 (wei)
	Gas             uint64         `json:"gas"`             // 할당된 가스
	GasUsed         uint64         `json:"gasUsed"`         // 사용된 가스
	Input           []byte         `json:"input"`           // 호출 데이터
	Output          []byte         `json:"output"`          // 반환 데이터
	Error           string         `json:"error,omitempty"` // 에러 메시지 (실패 시)
	Depth           int            `json:"depth"`           // 호출 깊이 (0 = 루트)
}

// ERC20Transfer represents an ERC20 token transfer
type ERC20Transfer struct {
	ContractAddress common.Address `json:"contractAddress"` // 토큰 컨트랙트 주소
	From            common.Address `json:"from"`            // 발신자 주소
	To              common.Address `json:"to"`              // 수신자 주소
	Value           *big.Int       `json:"value"`           // 전송량
	TransactionHash common.Hash    `json:"transactionHash"` // 트랜잭션 해시
	BlockNumber     uint64         `json:"blockNumber"`     // 블록 번호
	LogIndex        uint           `json:"logIndex"`        // 로그 인덱스
	Timestamp       uint64         `json:"timestamp"`       // 시각
}

// ERC721Transfer represents an ERC721 NFT transfer
type ERC721Transfer struct {
	ContractAddress common.Address `json:"contractAddress"` // NFT 컨트랙트 주소
	From            common.Address `json:"from"`            // 발신자 주소
	To              common.Address `json:"to"`              // 수신자 주소
	TokenId         *big.Int       `json:"tokenId"`         // 토큰 ID
	TransactionHash common.Hash    `json:"transactionHash"` // 트랜잭션 해시
	BlockNumber     uint64         `json:"blockNumber"`     // 블록 번호
	LogIndex        uint           `json:"logIndex"`        // 로그 인덱스
	Timestamp       uint64         `json:"timestamp"`       // 시각
}

// NFTOwnership represents an NFT owned by an address
type NFTOwnership struct {
	ContractAddress common.Address `json:"contractAddress"` // NFT 컨트랙트 주소
	TokenId         *big.Int       `json:"tokenId"`         // 토큰 ID
	Owner           common.Address `json:"owner"`           // 소유자 주소
}

// AddressIndexReader defines read operations for address indexing
type AddressIndexReader interface {
	// Contract Creation queries
	//
	// GetContractCreation retrieves contract creation information by contract address.
	// Returns ErrNotFound if the contract was not created or not indexed.
	GetContractCreation(ctx context.Context, contractAddress common.Address) (*ContractCreation, error)

	// GetContractsByCreator returns one page of the contracts created by a
	// specific address, in deployment block order (see Page).
	// Returns empty slice if no contracts found.
	GetContractsByCreator(ctx context.Context, creator common.Address, page Page) ([]common.Address, string, error)

	// ListContracts returns one page of all deployed contracts, newest
	// deployment block first (see Page).
	ListContracts(ctx context.Context, page Page) ([]*ContractCreation, string, error)

	// GetContractsCount returns the total number of deployed contracts.
	GetContractsCount(ctx context.Context) (int, error)

	// Internal Transaction queries
	//
	// GetInternalTransactions retrieves all internal transactions for a given transaction hash.
	// Returns empty slice if no internal transactions found or tracing is disabled.
	GetInternalTransactions(ctx context.Context, txHash common.Hash) ([]*InternalTransaction, error)

	// GetInternalTransactionsByAddress returns one page of the internal calls
	// involving a specific address, in block order and, within a transaction,
	// in call order (see Page). Limit and Offset count calls, and a page may
	// end between two calls of one transaction.
	// If isFrom is true, returns calls where address is the caller.
	// If isFrom is false, returns calls where address is the callee.
	GetInternalTransactionsByAddress(ctx context.Context, address common.Address, isFrom bool, page Page) ([]*InternalTransaction, string, error)

	// ERC20 Transfer queries
	//
	// GetERC20Transfer retrieves a specific ERC20 transfer by transaction hash and log index.
	// Returns ErrNotFound if the transfer does not exist.
	GetERC20Transfer(ctx context.Context, txHash common.Hash, logIndex uint) (*ERC20Transfer, error)

	// GetERC20TransfersByToken returns one page of the ERC20 transfers of a
	// specific token contract, in block and log index order (see Page).
	GetERC20TransfersByToken(ctx context.Context, tokenAddress common.Address, page Page) ([]*ERC20Transfer, string, error)

	// GetERC20TransfersByAddress returns one page of the ERC20 transfers
	// involving a specific address, in block and log index order (see Page).
	// If isFrom is true, returns transfers where address is the sender.
	// If isFrom is false, returns transfers where address is the recipient.
	GetERC20TransfersByAddress(ctx context.Context, address common.Address, isFrom bool, page Page) ([]*ERC20Transfer, string, error)

	// ERC721 Transfer queries
	//
	// GetERC721Transfer retrieves a specific ERC721 transfer by transaction hash and log index.
	// Returns ErrNotFound if the transfer does not exist.
	GetERC721Transfer(ctx context.Context, txHash common.Hash, logIndex uint) (*ERC721Transfer, error)

	// GetERC721TransfersByToken returns one page of the ERC721 transfers of a
	// specific token contract, in block and log index order (see Page).
	GetERC721TransfersByToken(ctx context.Context, tokenAddress common.Address, page Page) ([]*ERC721Transfer, string, error)

	// GetERC721TransfersByAddress returns one page of the ERC721 transfers
	// involving a specific address, in block and log index order (see Page).
	// If isFrom is true, returns transfers where address is the sender.
	// If isFrom is false, returns transfers where address is the recipient.
	GetERC721TransfersByAddress(ctx context.Context, address common.Address, isFrom bool, page Page) ([]*ERC721Transfer, string, error)

	// GetERC721Owner retrieves the current owner of a specific NFT token.
	// Returns ErrNotFound if the token has not been transferred or does not exist.
	GetERC721Owner(ctx context.Context, tokenAddress common.Address, tokenId *big.Int) (common.Address, error)

	// GetNFTsByOwner returns one page of the NFTs a specific address owns,
	// in a fixed order (see Page). A cursor stays valid when the token it
	// ended at leaves the owner.
	// Returns empty slice if no NFTs found.
	GetNFTsByOwner(ctx context.Context, owner common.Address, page Page) ([]*NFTOwnership, string, error)
}

// AddressIndexWriter defines write operations for address indexing
type AddressIndexWriter interface {
	// Contract Creation operations
	//
	// SaveContractCreation saves contract creation information.
	// Returns error if storage operation fails.
	SaveContractCreation(ctx context.Context, creation *ContractCreation) error

	// Internal Transaction operations
	//
	// SaveInternalTransactions saves all internal transactions for a given transaction hash.
	// The internals slice must be ordered by execution order (index field).
	// Returns error if storage operation fails.
	SaveInternalTransactions(ctx context.Context, txHash common.Hash, internals []*InternalTransaction) error

	// ERC20 Transfer operations
	//
	// SaveERC20Transfer saves an ERC20 token transfer.
	// Returns error if storage operation fails.
	SaveERC20Transfer(ctx context.Context, transfer *ERC20Transfer) error

	// ERC721 Transfer operations
	//
	// SaveERC721Transfer saves an ERC721 NFT transfer.
	// Also updates the current owner index for the token.
	// Returns error if storage operation fails.
	SaveERC721Transfer(ctx context.Context, transfer *ERC721Transfer) error
}

// AddressIndexReaderWriter combines AddressIndexReader and AddressIndexWriter
type AddressIndexReaderWriter interface {
	AddressIndexReader
	AddressIndexWriter
}

// SetCodeIndexReaderWriter combines SetCodeIndexReader and SetCodeIndexWriter
type SetCodeIndexReaderWriter interface {
	SetCodeIndexReader
	SetCodeIndexWriter
}

// FullAddressIndexer combines all address indexing interfaces including SetCode
type FullAddressIndexer interface {
	AddressIndexReader
	AddressIndexWriter
	SetCodeIndexReader
	SetCodeIndexWriter
}

// ERC20 Transfer event topic (keccak256("Transfer(address,address,uint256)"))
const ERC20TransferTopic = "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"

// Internal transaction types
const (
	InternalTxTypeCall         = "CALL"
	InternalTxTypeDelegateCall = "DELEGATECALL"
	InternalTxTypeStaticCall   = "STATICCALL"
	InternalTxTypeCreate       = "CREATE"
	InternalTxTypeCreate2      = "CREATE2"
	InternalTxTypeSelfDestruct = "SELFDESTRUCT"
)
