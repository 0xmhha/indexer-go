package postgres

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/big"
	"strconv"

	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

var (
	_ port.AddressIndexReader = (*Store)(nil)
	_ port.AddressIndexWriter = (*Store)(nil)
)

// hexOf returns bytes as a cursor value.
func hexOf(b []byte) string { return hex.EncodeToString(b) }

func u64s(v uint64) string { return strconv.FormatUint(v, 10) }

// bigOf parses a numeric column read as text.
func bigOf(s string) (*big.Int, error) {
	v, ok := new(big.Int).SetString(s, 10)
	if !ok {
		return nil, fmt.Errorf("invalid number %q", s)
	}
	return v, nil
}

// numeric returns a big integer as the text of a numeric parameter.
func numeric(v *big.Int) string {
	if v == nil {
		return "0"
	}
	return v.String()
}

// ========== Contract creations ==========

const creationColumns = "contract_address, creator, tx_hash, block_number, timestamp, bytecode_size"

// SaveContractCreation implements port.AddressIndexWriter.
func (s *Store) SaveContractCreation(ctx context.Context, c *port.ContractCreation) error {
	if c == nil {
		return fmt.Errorf("contract creation cannot be nil")
	}
	if err := s.write(); err != nil {
		return err
	}
	_, err := s.q(ctx).Exec(ctx, `INSERT INTO contract_creations (`+creationColumns+`) VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (contract_address) DO UPDATE SET creator = EXCLUDED.creator, tx_hash = EXCLUDED.tx_hash,
			block_number = EXCLUDED.block_number, timestamp = EXCLUDED.timestamp, bytecode_size = EXCLUDED.bytecode_size`,
		c.ContractAddress.Bytes(), c.Creator.Bytes(), c.TransactionHash.Bytes(), i64(c.BlockNumber), i64(c.Timestamp), c.BytecodeSize)
	return err
}

func scanCreation(row pgx.CollectableRow) (*port.ContractCreation, error) {
	var (
		addr, creator, tx []byte
		block, ts         int64
		size              int32
	)
	if err := row.Scan(&addr, &creator, &tx, &block, &ts, &size); err != nil {
		return nil, err
	}
	return &port.ContractCreation{
		ContractAddress: common.BytesToAddress(addr), Creator: common.BytesToAddress(creator),
		TransactionHash: common.BytesToHash(tx), BlockNumber: uint64(block), Timestamp: uint64(ts), BytecodeSize: int(size),
	}, nil
}

// GetContractCreation implements port.AddressIndexReader.
func (s *Store) GetContractCreation(ctx context.Context, contract common.Address) (*port.ContractCreation, error) {
	rows, err := s.q(ctx).Query(ctx, "SELECT "+creationColumns+" FROM contract_creations WHERE contract_address = $1", contract.Bytes())
	if err != nil {
		return nil, err
	}
	c, err := pgx.CollectExactlyOneRow(rows, scanCreation)
	return c, notFound(err)
}

// GetContractsByCreator implements port.AddressIndexReader.
func (s *Store) GetContractsByCreator(ctx context.Context, creator common.Address, page port.Page) ([]common.Address, string, error) {
	cs, next, err := listQuery[*port.ContractCreation]{
		list: "creator:" + creator.Hex(),
		sql:  "SELECT " + creationColumns + " FROM contract_creations WHERE creator = $1",
		args: []any{creator.Bytes()},
		keys: []keyCol{{"block_number", kindInt, false}, {"contract_address", kindBytes, false}},
		scan: scanCreation,
		keyOf: func(c *port.ContractCreation) []string {
			return []string{u64s(c.BlockNumber), hexOf(c.ContractAddress.Bytes())}
		},
	}.run(ctx, s.q(ctx), page)
	if err != nil {
		return nil, "", err
	}
	out := make([]common.Address, len(cs))
	for i, c := range cs {
		out[i] = c.ContractAddress
	}
	return out, next, nil
}

// ListContracts implements port.AddressIndexReader: newest deployment
// first.
func (s *Store) ListContracts(ctx context.Context, page port.Page) ([]*port.ContractCreation, string, error) {
	return listQuery[*port.ContractCreation]{
		list: "contracts",
		sql:  "SELECT " + creationColumns + " FROM contract_creations WHERE true",
		keys: []keyCol{{"block_number", kindInt, true}, {"contract_address", kindBytes, true}},
		scan: scanCreation,
		keyOf: func(c *port.ContractCreation) []string {
			return []string{u64s(c.BlockNumber), hexOf(c.ContractAddress.Bytes())}
		},
	}.run(ctx, s.q(ctx), page)
}

// GetContractsCount implements port.AddressIndexReader.
func (s *Store) GetContractsCount(ctx context.Context) (int, error) {
	var n int
	err := s.q(ctx).QueryRow(ctx, "SELECT count(*) FROM contract_creations").Scan(&n)
	return n, err
}

// ========== Internal transactions ==========

const internalColumns = "tx_hash, idx, block_number, type, from_addr, to_addr, value::text, gas, gas_used, input, output, error, depth"

// SaveInternalTransactions implements port.AddressIndexWriter: the calls
// replace the transaction's earlier ones.
func (s *Store) SaveInternalTransactions(ctx context.Context, txHash common.Hash, internals []*port.InternalTransaction) error {
	return s.inTx(ctx, func(q querier) error {
		if _, err := q.Exec(ctx, "DELETE FROM internal_transactions WHERE tx_hash = $1", txHash.Bytes()); err != nil {
			return err
		}
		batch := &pgx.Batch{}
		for _, it := range internals {
			if it == nil {
				return fmt.Errorf("internal transaction cannot be nil")
			}
			batch.Queue(`INSERT INTO internal_transactions (tx_hash, idx, block_number, type, from_addr, to_addr, value,
					gas, gas_used, input, output, error, depth)
				VALUES ($1, $2, $3, $4, $5, $6, $7::numeric, $8, $9, $10, $11, $12, $13)`,
				txHash.Bytes(), it.Index, i64(it.BlockNumber), it.Type, it.From.Bytes(), it.To.Bytes(), numeric(it.Value),
				i64(it.Gas), i64(it.GasUsed), nonNil(it.Input), nonNil(it.Output), it.Error, it.Depth)
		}
		return sendBatch(ctx, q, batch)
	})
}

func nonNil(b []byte) []byte {
	if b == nil {
		return []byte{}
	}
	return b
}

// emptyNil returns nil for an empty slice, as the Pebble store decodes it.
func emptyNil(b []byte) []byte {
	if len(b) == 0 {
		return nil
	}
	return b
}

func scanInternal(row pgx.CollectableRow) (*port.InternalTransaction, error) {
	var (
		tx, from, to, input, output []byte
		idx, depth                  int32
		block, gas, gasUsed         int64
		typ, value, errMsg          string
	)
	if err := row.Scan(&tx, &idx, &block, &typ, &from, &to, &value, &gas, &gasUsed, &input, &output, &errMsg, &depth); err != nil {
		return nil, err
	}
	v, err := bigOf(value)
	if err != nil {
		return nil, err
	}
	return &port.InternalTransaction{
		TransactionHash: common.BytesToHash(tx), BlockNumber: uint64(block), Index: int(idx), Type: typ,
		From: common.BytesToAddress(from), To: common.BytesToAddress(to), Value: v,
		Gas: uint64(gas), GasUsed: uint64(gasUsed), Input: emptyNil(input), Output: emptyNil(output), Error: errMsg, Depth: int(depth),
	}, nil
}

// GetInternalTransactions implements port.AddressIndexReader.
func (s *Store) GetInternalTransactions(ctx context.Context, txHash common.Hash) ([]*port.InternalTransaction, error) {
	rows, err := s.q(ctx).Query(ctx, "SELECT "+internalColumns+" FROM internal_transactions WHERE tx_hash = $1 ORDER BY idx", txHash.Bytes())
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanInternal)
}

// GetInternalTransactionsByAddress implements port.AddressIndexReader.
func (s *Store) GetInternalTransactionsByAddress(ctx context.Context, address common.Address, isFrom bool, page port.Page) ([]*port.InternalTransaction, string, error) {
	col, side := "to_addr", "to"
	if isFrom {
		col, side = "from_addr", "from"
	}
	return listQuery[*port.InternalTransaction]{
		list: "internal:" + side + ":" + address.Hex(),
		sql:  "SELECT " + internalColumns + " FROM internal_transactions WHERE " + col + " = $1",
		args: []any{address.Bytes()},
		keys: []keyCol{{"block_number", kindInt, false}, {"tx_hash", kindBytes, false}, {"idx", kindInt, false}},
		scan: scanInternal,
		keyOf: func(it *port.InternalTransaction) []string {
			return []string{u64s(it.BlockNumber), hexOf(it.TransactionHash.Bytes()), strconv.Itoa(it.Index)}
		},
	}.run(ctx, s.q(ctx), page)
}

// ========== ERC-20 transfers ==========

const erc20Columns = "contract, from_addr, to_addr, value::text, tx_hash, block_number, log_index, timestamp"

// SaveERC20Transfer implements port.AddressIndexWriter.
func (s *Store) SaveERC20Transfer(ctx context.Context, t *port.ERC20Transfer) error {
	if t == nil {
		return fmt.Errorf("transfer cannot be nil")
	}
	if err := s.write(); err != nil {
		return err
	}
	_, err := s.q(ctx).Exec(ctx, `INSERT INTO erc20_transfers (tx_hash, log_index, contract, from_addr, to_addr, value, block_number, timestamp)
		VALUES ($1, $2, $3, $4, $5, $6::numeric, $7, $8)
		ON CONFLICT (tx_hash, log_index) DO UPDATE SET contract = EXCLUDED.contract, from_addr = EXCLUDED.from_addr,
			to_addr = EXCLUDED.to_addr, value = EXCLUDED.value, block_number = EXCLUDED.block_number, timestamp = EXCLUDED.timestamp`,
		t.TransactionHash.Bytes(), int64(t.LogIndex), t.ContractAddress.Bytes(), t.From.Bytes(), t.To.Bytes(), numeric(t.Value),
		i64(t.BlockNumber), i64(t.Timestamp))
	return err
}

func scanERC20(row pgx.CollectableRow) (*port.ERC20Transfer, error) {
	var (
		contract, from, to, tx []byte
		value                  string
		block, ts              int64
		logIndex               int32
	)
	if err := row.Scan(&contract, &from, &to, &value, &tx, &block, &logIndex, &ts); err != nil {
		return nil, err
	}
	v, err := bigOf(value)
	if err != nil {
		return nil, err
	}
	return &port.ERC20Transfer{
		ContractAddress: common.BytesToAddress(contract), From: common.BytesToAddress(from), To: common.BytesToAddress(to),
		Value: v, TransactionHash: common.BytesToHash(tx), BlockNumber: uint64(block), LogIndex: uint(logIndex), Timestamp: uint64(ts),
	}, nil
}

// transferKeys sort transfers in block and log index order.
var transferKeys = []keyCol{{"block_number", kindInt, false}, {"log_index", kindInt, false}, {"tx_hash", kindBytes, false}}

func erc20Key(t *port.ERC20Transfer) []string {
	return []string{u64s(t.BlockNumber), strconv.FormatUint(uint64(t.LogIndex), 10), hexOf(t.TransactionHash.Bytes())}
}

// GetERC20Transfer implements port.AddressIndexReader.
func (s *Store) GetERC20Transfer(ctx context.Context, txHash common.Hash, logIndex uint) (*port.ERC20Transfer, error) {
	rows, err := s.q(ctx).Query(ctx, "SELECT "+erc20Columns+" FROM erc20_transfers WHERE tx_hash = $1 AND log_index = $2", txHash.Bytes(), int64(logIndex))
	if err != nil {
		return nil, err
	}
	t, err := pgx.CollectExactlyOneRow(rows, scanERC20)
	return t, notFound(err)
}

// GetERC20TransfersByToken implements port.AddressIndexReader.
func (s *Store) GetERC20TransfersByToken(ctx context.Context, token common.Address, page port.Page) ([]*port.ERC20Transfer, string, error) {
	return listQuery[*port.ERC20Transfer]{
		list: "erc20:token:" + token.Hex(),
		sql:  "SELECT " + erc20Columns + " FROM erc20_transfers WHERE contract = $1",
		args: []any{token.Bytes()}, keys: transferKeys, scan: scanERC20, keyOf: erc20Key,
	}.run(ctx, s.q(ctx), page)
}

// GetERC20TransfersByAddress implements port.AddressIndexReader.
func (s *Store) GetERC20TransfersByAddress(ctx context.Context, address common.Address, isFrom bool, page port.Page) ([]*port.ERC20Transfer, string, error) {
	col, side := "to_addr", "to"
	if isFrom {
		col, side = "from_addr", "from"
	}
	return listQuery[*port.ERC20Transfer]{
		list: "erc20:" + side + ":" + address.Hex(),
		sql:  "SELECT " + erc20Columns + " FROM erc20_transfers WHERE " + col + " = $1",
		args: []any{address.Bytes()}, keys: transferKeys, scan: scanERC20, keyOf: erc20Key,
	}.run(ctx, s.q(ctx), page)
}

// ========== ERC-721 transfers and owners ==========

const erc721Columns = "contract, from_addr, to_addr, token_id::text, tx_hash, block_number, log_index, timestamp"

// SaveERC721Transfer implements port.AddressIndexWriter: the transfer, and
// its recipient as the token's owner (the zero address once burned).
func (s *Store) SaveERC721Transfer(ctx context.Context, t *port.ERC721Transfer) error {
	if t == nil {
		return fmt.Errorf("transfer cannot be nil")
	}
	return s.inTx(ctx, func(q querier) error {
		if _, err := q.Exec(ctx, `INSERT INTO erc721_transfers (tx_hash, log_index, contract, from_addr, to_addr, token_id, block_number, timestamp)
			VALUES ($1, $2, $3, $4, $5, $6::numeric, $7, $8)
			ON CONFLICT (tx_hash, log_index) DO UPDATE SET contract = EXCLUDED.contract, from_addr = EXCLUDED.from_addr,
				to_addr = EXCLUDED.to_addr, token_id = EXCLUDED.token_id, block_number = EXCLUDED.block_number, timestamp = EXCLUDED.timestamp`,
			t.TransactionHash.Bytes(), int64(t.LogIndex), t.ContractAddress.Bytes(), t.From.Bytes(), t.To.Bytes(), numeric(t.TokenId),
			i64(t.BlockNumber), i64(t.Timestamp)); err != nil {
			return err
		}
		_, err := q.Exec(ctx, `INSERT INTO nft_owners (contract, token_id, owner) VALUES ($1, $2::numeric, $3)
			ON CONFLICT (contract, token_id) DO UPDATE SET owner = EXCLUDED.owner`,
			t.ContractAddress.Bytes(), numeric(t.TokenId), t.To.Bytes())
		return err
	})
}

func scanERC721(row pgx.CollectableRow) (*port.ERC721Transfer, error) {
	var (
		contract, from, to, tx []byte
		id                     string
		block, ts              int64
		logIndex               int32
	)
	if err := row.Scan(&contract, &from, &to, &id, &tx, &block, &logIndex, &ts); err != nil {
		return nil, err
	}
	tokenID, err := bigOf(id)
	if err != nil {
		return nil, err
	}
	return &port.ERC721Transfer{
		ContractAddress: common.BytesToAddress(contract), From: common.BytesToAddress(from), To: common.BytesToAddress(to),
		TokenId: tokenID, TransactionHash: common.BytesToHash(tx), BlockNumber: uint64(block), LogIndex: uint(logIndex), Timestamp: uint64(ts),
	}, nil
}

func erc721Key(t *port.ERC721Transfer) []string {
	return []string{u64s(t.BlockNumber), strconv.FormatUint(uint64(t.LogIndex), 10), hexOf(t.TransactionHash.Bytes())}
}

// GetERC721Transfer implements port.AddressIndexReader.
func (s *Store) GetERC721Transfer(ctx context.Context, txHash common.Hash, logIndex uint) (*port.ERC721Transfer, error) {
	rows, err := s.q(ctx).Query(ctx, "SELECT "+erc721Columns+" FROM erc721_transfers WHERE tx_hash = $1 AND log_index = $2", txHash.Bytes(), int64(logIndex))
	if err != nil {
		return nil, err
	}
	t, err := pgx.CollectExactlyOneRow(rows, scanERC721)
	return t, notFound(err)
}

// GetERC721TransfersByToken implements port.AddressIndexReader.
func (s *Store) GetERC721TransfersByToken(ctx context.Context, token common.Address, page port.Page) ([]*port.ERC721Transfer, string, error) {
	return listQuery[*port.ERC721Transfer]{
		list: "erc721:token:" + token.Hex(),
		sql:  "SELECT " + erc721Columns + " FROM erc721_transfers WHERE contract = $1",
		args: []any{token.Bytes()}, keys: transferKeys, scan: scanERC721, keyOf: erc721Key,
	}.run(ctx, s.q(ctx), page)
}

// GetERC721TransfersByAddress implements port.AddressIndexReader.
func (s *Store) GetERC721TransfersByAddress(ctx context.Context, address common.Address, isFrom bool, page port.Page) ([]*port.ERC721Transfer, string, error) {
	col, side := "to_addr", "to"
	if isFrom {
		col, side = "from_addr", "from"
	}
	return listQuery[*port.ERC721Transfer]{
		list: "erc721:" + side + ":" + address.Hex(),
		sql:  "SELECT " + erc721Columns + " FROM erc721_transfers WHERE " + col + " = $1",
		args: []any{address.Bytes()}, keys: transferKeys, scan: scanERC721, keyOf: erc721Key,
	}.run(ctx, s.q(ctx), page)
}

// GetERC721Owner implements port.AddressIndexReader.
func (s *Store) GetERC721Owner(ctx context.Context, token common.Address, tokenID *big.Int) (common.Address, error) {
	var owner []byte
	err := s.q(ctx).QueryRow(ctx, "SELECT owner FROM nft_owners WHERE contract = $1 AND token_id = $2::numeric",
		token.Bytes(), numeric(tokenID)).Scan(&owner)
	if err != nil {
		return common.Address{}, notFound(err)
	}
	return common.BytesToAddress(owner), nil
}

// GetNFTsByOwner implements port.AddressIndexReader: in contract and token
// id order.
func (s *Store) GetNFTsByOwner(ctx context.Context, owner common.Address, page port.Page) ([]*port.NFTOwnership, string, error) {
	if owner == (common.Address{}) {
		return nil, "", nil // burned tokens have no owner
	}
	return listQuery[*port.NFTOwnership]{
		list: "nfts:" + owner.Hex(),
		sql:  "SELECT contract, token_id::text FROM nft_owners WHERE owner = $1",
		args: []any{owner.Bytes()},
		keys: []keyCol{{"nft_owners.contract", kindBytes, false}, {"nft_owners.token_id", kindNumeric, false}},
		scan: func(row pgx.CollectableRow) (*port.NFTOwnership, error) {
			var (
				contract []byte
				id       string
			)
			if err := row.Scan(&contract, &id); err != nil {
				return nil, err
			}
			tokenID, err := bigOf(id)
			if err != nil {
				return nil, err
			}
			return &port.NFTOwnership{ContractAddress: common.BytesToAddress(contract), TokenId: tokenID, Owner: owner}, nil
		},
		keyOf: func(n *port.NFTOwnership) []string {
			return []string{hexOf(n.ContractAddress.Bytes()), n.TokenId.String()}
		},
	}.run(ctx, s.q(ctx), page)
}
