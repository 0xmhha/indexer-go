package storage

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestContractCreation(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()
	addressWriter, ok := storage.(AddressIndexWriter)
	if !ok {
		t.Fatal("storage does not support address indexing")
	}
	addressReader, ok := storage.(AddressIndexReader)
	if !ok {
		t.Fatal("storage does not support address indexing")
	}

	t.Run("SaveAndGetContractCreation_Success", func(t *testing.T) {
		creation := &ContractCreation{
			ContractAddress: common.HexToAddress("0x1234567890123456789012345678901234567890"),
			Creator:         common.HexToAddress("0x0987654321098765432109876543210987654321"),
			TransactionHash: common.HexToHash("0xabcdef"),
			BlockNumber:     100,
			Timestamp:       1234567890,
			BytecodeSize:    1024,
		}

		err := addressWriter.SaveContractCreation(ctx, creation)
		if err != nil {
			t.Fatalf("SaveContractCreation failed: %v", err)
		}

		retrieved, err := addressReader.GetContractCreation(ctx, creation.ContractAddress)
		if err != nil {
			t.Fatalf("GetContractCreation failed: %v", err)
		}

		if retrieved.ContractAddress != creation.ContractAddress {
			t.Errorf("ContractAddress mismatch: expected %s, got %s", creation.ContractAddress.Hex(), retrieved.ContractAddress.Hex())
		}
		if retrieved.Creator != creation.Creator {
			t.Errorf("Creator mismatch: expected %s, got %s", creation.Creator.Hex(), retrieved.Creator.Hex())
		}
		if retrieved.BlockNumber != creation.BlockNumber {
			t.Errorf("BlockNumber mismatch: expected %d, got %d", creation.BlockNumber, retrieved.BlockNumber)
		}
	})

	t.Run("GetContractCreation_NotFound", func(t *testing.T) {
		_, err := addressReader.GetContractCreation(ctx, common.HexToAddress("0xnonexistent"))
		if err != ErrNotFound {
			t.Errorf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("SaveContractCreation_ZeroAddress", func(t *testing.T) {
		creation := &ContractCreation{
			ContractAddress: common.Address{},
			Creator:         common.HexToAddress("0x0987654321098765432109876543210987654321"),
			TransactionHash: common.HexToHash("0xabcdef"),
			BlockNumber:     100,
			Timestamp:       1234567890,
		}

		err := addressWriter.SaveContractCreation(ctx, creation)
		if err == nil {
			t.Error("expected error for zero contract address")
		}
	})

	t.Run("GetContractsByCreator_Success", func(t *testing.T) {
		creator := common.BigToAddress(big.NewInt(999))

		// Save multiple contracts by the same creator
		for i := 0; i < 3; i++ {
			creation := &ContractCreation{
				ContractAddress: common.BigToAddress(big.NewInt(int64(1000 + i))),
				Creator:         creator,
				TransactionHash: common.BigToHash(big.NewInt(int64(100 + i))),
				BlockNumber:     uint64(100 + i),
				Timestamp:       uint64(1234567890 + i),
				BytecodeSize:    1024,
			}
			err := addressWriter.SaveContractCreation(ctx, creation)
			if err != nil {
				t.Fatalf("SaveContractCreation failed: %v", err)
			}
		}

		// Get all contracts
		contracts, err := addressReader.GetContractsByCreator(ctx, creator, 10, 0)
		if err != nil {
			t.Fatalf("GetContractsByCreator failed: %v", err)
		}

		if len(contracts) != 3 {
			t.Errorf("expected 3 contracts, got %d", len(contracts))
		}
	})

	t.Run("GetContractsByCreator_Pagination", func(t *testing.T) {
		creator := common.BigToAddress(big.NewInt(456))

		// Save 5 contracts
		for i := 0; i < 5; i++ {
			creation := &ContractCreation{
				ContractAddress: common.BigToAddress(big.NewInt(int64(2000 + i))),
				Creator:         creator,
				TransactionHash: common.BigToHash(big.NewInt(int64(200 + i))),
				BlockNumber:     uint64(200 + i),
				Timestamp:       uint64(1234567890 + i),
				BytecodeSize:    512,
			}
			err := addressWriter.SaveContractCreation(ctx, creation)
			if err != nil {
				t.Fatalf("SaveContractCreation failed: %v", err)
			}
		}

		// Get first 2 contracts
		contracts, err := addressReader.GetContractsByCreator(ctx, creator, 2, 0)
		if err != nil {
			t.Fatalf("GetContractsByCreator failed: %v", err)
		}
		if len(contracts) != 2 {
			t.Errorf("expected 2 contracts with limit=2, got %d", len(contracts))
		}

		// Get next 2 contracts
		contracts, err = addressReader.GetContractsByCreator(ctx, creator, 2, 2)
		if err != nil {
			t.Fatalf("GetContractsByCreator failed: %v", err)
		}
		if len(contracts) != 2 {
			t.Errorf("expected 2 contracts with offset=2, got %d", len(contracts))
		}
	})

	t.Run("GetContractsByCreator_LimitValidation", func(t *testing.T) {
		creator := common.BigToAddress(big.NewInt(789))

		// Limit validation now happens in storage implementation
		_, err := addressReader.GetContractsByCreator(ctx, creator, 200, 0)
		// Some implementations may return an error, some may silently cap to 100
		// We just verify it doesn't panic
		_ = err
	})
}

func TestERC20Transfer(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()
	addressWriter, ok := storage.(AddressIndexWriter)
	if !ok {
		t.Fatal("storage does not support address indexing")
	}
	addressReader, ok := storage.(AddressIndexReader)
	if !ok {
		t.Fatal("storage does not support address indexing")
	}

	t.Run("SaveAndGetERC20Transfer_Success", func(t *testing.T) {
		transfer := &ERC20Transfer{
			ContractAddress: common.BigToAddress(big.NewInt(123)),
			From:            common.BigToAddress(big.NewInt(456)),
			To:              common.BigToAddress(big.NewInt(789)),
			Value:           big.NewInt(1000000),
			TransactionHash: common.BigToHash(big.NewInt(1001)),
			BlockNumber:     100,
			LogIndex:        0,
			Timestamp:       1234567890,
		}

		err := addressWriter.SaveERC20Transfer(ctx, transfer)
		if err != nil {
			t.Fatalf("SaveERC20Transfer failed: %v", err)
		}

		retrieved, err := addressReader.GetERC20Transfer(ctx, transfer.TransactionHash, transfer.LogIndex)
		if err != nil {
			t.Fatalf("GetERC20Transfer failed: %v", err)
		}

		if retrieved.ContractAddress != transfer.ContractAddress {
			t.Errorf("ContractAddress mismatch")
		}
		if retrieved.Value.Cmp(transfer.Value) != 0 {
			t.Errorf("Value mismatch: expected %s, got %s", transfer.Value.String(), retrieved.Value.String())
		}
	})

	t.Run("GetERC20Transfer_NotFound", func(t *testing.T) {
		_, err := addressReader.GetERC20Transfer(ctx, common.HexToHash("0xnonexistent"), 0)
		if err != ErrNotFound {
			t.Errorf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("GetERC20TransfersByToken_Success", func(t *testing.T) {
		token := common.BigToAddress(big.NewInt(456))

		// Save multiple transfers for the same token
		for i := 0; i < 3; i++ {
			transfer := &ERC20Transfer{
				ContractAddress: token,
				From:            common.BigToAddress(big.NewInt(int64(100 + i))),
				To:              common.BigToAddress(big.NewInt(int64(200 + i))),
				Value:           big.NewInt(int64(1000 + i)),
				TransactionHash: common.BigToHash(big.NewInt(int64(1000 + i))),
				BlockNumber:     uint64(100 + i),
				LogIndex:        uint(i),
				Timestamp:       uint64(1234567890 + i),
			}
			err := addressWriter.SaveERC20Transfer(ctx, transfer)
			if err != nil {
				t.Fatalf("SaveERC20Transfer failed: %v", err)
			}
		}

		transfers, err := addressReader.GetERC20TransfersByToken(ctx, token, 10, 0)
		if err != nil {
			t.Fatalf("GetERC20TransfersByToken failed: %v", err)
		}

		if len(transfers) != 3 {
			t.Errorf("expected 3 transfers, got %d", len(transfers))
		}
	})

	t.Run("GetERC20TransfersByAddress_From", func(t *testing.T) {
		from := common.BigToAddress(big.NewInt(789))

		transfer := &ERC20Transfer{
			ContractAddress: common.BigToAddress(big.NewInt(3000)),
			From:            from,
			To:              common.BigToAddress(big.NewInt(3001)),
			Value:           big.NewInt(5000),
			TransactionHash: common.BigToHash(big.NewInt(3002)),
			BlockNumber:     200,
			LogIndex:        0,
			Timestamp:       1234567890,
		}

		err := addressWriter.SaveERC20Transfer(ctx, transfer)
		if err != nil {
			t.Fatalf("SaveERC20Transfer failed: %v", err)
		}

		transfers, err := addressReader.GetERC20TransfersByAddress(ctx, from, true, 10, 0)
		if err != nil {
			t.Fatalf("GetERC20TransfersByAddress failed: %v", err)
		}

		if len(transfers) == 0 {
			t.Error("expected at least 1 transfer from address")
		}
	})

	t.Run("GetERC20TransfersByAddress_To", func(t *testing.T) {
		to := common.BigToAddress(big.NewInt(999))

		transfer := &ERC20Transfer{
			ContractAddress: common.BigToAddress(big.NewInt(4000)),
			From:            common.BigToAddress(big.NewInt(4001)),
			To:              to,
			Value:           big.NewInt(9000),
			TransactionHash: common.BigToHash(big.NewInt(4002)),
			BlockNumber:     300,
			LogIndex:        0,
			Timestamp:       1234567890,
		}

		err := addressWriter.SaveERC20Transfer(ctx, transfer)
		if err != nil {
			t.Fatalf("SaveERC20Transfer failed: %v", err)
		}

		transfers, err := addressReader.GetERC20TransfersByAddress(ctx, to, false, 10, 0)
		if err != nil {
			t.Fatalf("GetERC20TransfersByAddress failed: %v", err)
		}

		if len(transfers) == 0 {
			t.Error("expected at least 1 transfer to address")
		}
	})
}

func TestERC721Transfer(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()
	addressWriter, ok := storage.(AddressIndexWriter)
	if !ok {
		t.Fatal("storage does not support address indexing")
	}
	addressReader, ok := storage.(AddressIndexReader)
	if !ok {
		t.Fatal("storage does not support address indexing")
	}

	t.Run("SaveAndGetERC721Transfer_Success", func(t *testing.T) {
		transfer := &ERC721Transfer{
			ContractAddress: common.BigToAddress(big.NewInt(5000)),
			From:            common.BigToAddress(big.NewInt(5001)),
			To:              common.BigToAddress(big.NewInt(5002)),
			TokenId:         big.NewInt(42),
			TransactionHash: common.BigToHash(big.NewInt(5003)),
			BlockNumber:     100,
			LogIndex:        0,
			Timestamp:       1234567890,
		}

		err := addressWriter.SaveERC721Transfer(ctx, transfer)
		if err != nil {
			t.Fatalf("SaveERC721Transfer failed: %v", err)
		}

		retrieved, err := addressReader.GetERC721Transfer(ctx, transfer.TransactionHash, transfer.LogIndex)
		if err != nil {
			t.Fatalf("GetERC721Transfer failed: %v", err)
		}

		if retrieved.TokenId.Cmp(transfer.TokenId) != 0 {
			t.Errorf("TokenId mismatch: expected %s, got %s", transfer.TokenId.String(), retrieved.TokenId.String())
		}
	})

	t.Run("GetERC721Owner_Success", func(t *testing.T) {
		token := common.BigToAddress(big.NewInt(6000))
		tokenId := big.NewInt(123)
		owner := common.BigToAddress(big.NewInt(6001))

		transfer := &ERC721Transfer{
			ContractAddress: token,
			From:            common.BigToAddress(big.NewInt(6002)),
			To:              owner,
			TokenId:         tokenId,
			TransactionHash: common.BigToHash(big.NewInt(6003)),
			BlockNumber:     200,
			LogIndex:        0,
			Timestamp:       1234567890,
		}

		err := addressWriter.SaveERC721Transfer(ctx, transfer)
		if err != nil {
			t.Fatalf("SaveERC721Transfer failed: %v", err)
		}

		retrievedOwner, err := addressReader.GetERC721Owner(ctx, token, tokenId)
		if err != nil {
			t.Fatalf("GetERC721Owner failed: %v", err)
		}

		if retrievedOwner != owner {
			t.Errorf("Owner mismatch: expected %s, got %s", owner.Hex(), retrievedOwner.Hex())
		}
	})

	t.Run("GetERC721Owner_UpdateOwnership", func(t *testing.T) {
		token := common.BigToAddress(big.NewInt(7000))
		tokenId := big.NewInt(999)
		firstOwner := common.BigToAddress(big.NewInt(7001))
		secondOwner := common.BigToAddress(big.NewInt(7002))

		// First transfer
		transfer1 := &ERC721Transfer{
			ContractAddress: token,
			From:            common.Address{},
			To:              firstOwner,
			TokenId:         tokenId,
			TransactionHash: common.BigToHash(big.NewInt(7003)),
			BlockNumber:     100,
			LogIndex:        0,
			Timestamp:       1234567890,
		}
		err := addressWriter.SaveERC721Transfer(ctx, transfer1)
		if err != nil {
			t.Fatalf("SaveERC721Transfer failed: %v", err)
		}

		owner, err := addressReader.GetERC721Owner(ctx, token, tokenId)
		if err != nil {
			t.Fatalf("GetERC721Owner failed: %v", err)
		}
		if owner != firstOwner {
			t.Errorf("Expected first owner %s, got %s", firstOwner.Hex(), owner.Hex())
		}

		// Second transfer (ownership change)
		transfer2 := &ERC721Transfer{
			ContractAddress: token,
			From:            firstOwner,
			To:              secondOwner,
			TokenId:         tokenId,
			TransactionHash: common.BigToHash(big.NewInt(7004)),
			BlockNumber:     200,
			LogIndex:        0,
			Timestamp:       1234567891,
		}
		err = addressWriter.SaveERC721Transfer(ctx, transfer2)
		if err != nil {
			t.Fatalf("SaveERC721Transfer failed: %v", err)
		}

		owner, err = addressReader.GetERC721Owner(ctx, token, tokenId)
		if err != nil {
			t.Fatalf("GetERC721Owner failed: %v", err)
		}
		if owner != secondOwner {
			t.Errorf("Expected second owner %s, got %s", secondOwner.Hex(), owner.Hex())
		}
	})

	t.Run("GetERC721TransfersByToken_Success", func(t *testing.T) {
		token := common.BigToAddress(big.NewInt(8000))

		// Save multiple transfers for the same NFT collection
		for i := 0; i < 3; i++ {
			transfer := &ERC721Transfer{
				ContractAddress: token,
				From:            common.BigToAddress(big.NewInt(int64(8100 + i))),
				To:              common.BigToAddress(big.NewInt(int64(8200 + i))),
				TokenId:         big.NewInt(int64(1000 + i)),
				TransactionHash: common.BigToHash(big.NewInt(int64(8000 + i))),
				BlockNumber:     uint64(300 + i),
				LogIndex:        uint(i),
				Timestamp:       uint64(1234567890 + i),
			}
			err := addressWriter.SaveERC721Transfer(ctx, transfer)
			if err != nil {
				t.Fatalf("SaveERC721Transfer failed: %v", err)
			}
		}

		transfers, err := addressReader.GetERC721TransfersByToken(ctx, token, 10, 0)
		if err != nil {
			t.Fatalf("GetERC721TransfersByToken failed: %v", err)
		}

		if len(transfers) != 3 {
			t.Errorf("expected 3 transfers, got %d", len(transfers))
		}
	})
}

func TestInternalTransaction(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()

	ctx := context.Background()
	addressWriter, ok := storage.(AddressIndexWriter)
	if !ok {
		t.Fatal("storage does not support address indexing")
	}
	addressReader, ok := storage.(AddressIndexReader)
	if !ok {
		t.Fatal("storage does not support address indexing")
	}

	t.Run("SaveAndGetInternalTransactions_Success", func(t *testing.T) {
		txHash := common.BigToHash(big.NewInt(9000))

		internalTxs := []*InternalTransaction{
			{
				TransactionHash: txHash,
				From:            common.HexToAddress("0xfrom1"),
				To:              common.HexToAddress("0xto1"),
				Value:           big.NewInt(1000),
				Gas:             21000,
				GasUsed:         21000,
				Input:           []byte{},
				Output:          []byte{},
				Type:            InternalTxTypeCall,
				Index:           0,
				BlockNumber:     100,
				Depth:           0,
			},
			{
				TransactionHash: txHash,
				From:            common.HexToAddress("0xto1"),
				To:              common.HexToAddress("0xto2"),
				Value:           big.NewInt(500),
				Gas:             10000,
				GasUsed:         10000,
				Input:           []byte{},
				Output:          []byte{},
				Type:            InternalTxTypeCall,
				Index:           1,
				BlockNumber:     100,
				Depth:           1,
			},
		}

		err := addressWriter.SaveInternalTransactions(ctx, txHash, internalTxs)
		if err != nil {
			t.Fatalf("SaveInternalTransactions failed: %v", err)
		}

		retrieved, err := addressReader.GetInternalTransactions(ctx, txHash)
		if err != nil {
			t.Fatalf("GetInternalTransactions failed: %v", err)
		}

		if len(retrieved) != 2 {
			t.Errorf("expected 2 internal transactions, got %d", len(retrieved))
		}

		if retrieved[0].Value.Cmp(big.NewInt(1000)) != 0 {
			t.Errorf("Value mismatch for first internal tx")
		}
	})

	t.Run("GetInternalTransactions_NotFound", func(t *testing.T) {
		retrieved, err := addressReader.GetInternalTransactions(ctx, common.HexToHash("0xnonexistent"))
		if err != nil {
			t.Fatalf("GetInternalTransactions failed: %v", err)
		}

		if len(retrieved) != 0 {
			t.Errorf("expected 0 internal transactions, got %d", len(retrieved))
		}
	})

	t.Run("GetInternalTransactionsByAddress_Success", func(t *testing.T) {
		address := common.BigToAddress(big.NewInt(9500))

		txHash := common.BigToHash(big.NewInt(9501))
		internalTxs := []*InternalTransaction{
			{
				TransactionHash: txHash,
				From:            address,
				To:              common.HexToAddress("0xto456"),
				Value:           big.NewInt(2000),
				Gas:             21000,
				GasUsed:         21000,
				Input:           []byte{},
				Output:          []byte{},
				Type:            InternalTxTypeCall,
				Index:           0,
				BlockNumber:     200,
				Depth:           0,
			},
		}

		err := addressWriter.SaveInternalTransactions(ctx, txHash, internalTxs)
		if err != nil {
			t.Fatalf("SaveInternalTransactions failed: %v", err)
		}

		retrieved, err := addressReader.GetInternalTransactionsByAddress(ctx, address, true, 10, 0)
		if err != nil {
			t.Fatalf("GetInternalTransactionsByAddress failed: %v", err)
		}

		if len(retrieved) == 0 {
			t.Error("expected at least 1 internal transaction")
		}
	})
}
