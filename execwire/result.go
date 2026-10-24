package execwire

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
)

// MarshalResult is deterministic for the ordered journal. It never sorts an
// invalid journal: ordinals, transactions, Finalize and key ordering are checked.
func MarshalResult(r Result) ([]byte, error) {
	if err := validateResult(r); err != nil {
		return nil, err
	}
	meta, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	n := 16 + len(meta) + len(r.BlockRLP) + len(r.ReceiptsRLP)
	if len(meta) > MaxMetadataBytes || n > MaxBlockBytes {
		return nil, ErrLimit
	}
	if err := jsonBounds(meta); err != nil {
		return nil, err
	}
	p := make([]byte, 16, n)
	copy(p, "EXR1")
	binary.BigEndian.PutUint32(p[4:8], uint32(len(meta)))
	binary.BigEndian.PutUint32(p[8:12], uint32(len(r.BlockRLP)))
	binary.BigEndian.PutUint32(p[12:16], uint32(len(r.ReceiptsRLP)))
	p = append(p, meta...)
	p = append(p, r.BlockRLP...)
	p = append(p, r.ReceiptsRLP...)
	return p, nil
}

func UnmarshalResult(p []byte) (Result, error) {
	if len(p) > MaxBlockBytes {
		return Result{}, ErrLimit
	}
	if len(p) < 16 || !bytes.Equal(p[:4], []byte("EXR1")) {
		return Result{}, ErrProtocol
	}
	m, b, rp := uint64(binary.BigEndian.Uint32(p[4:8])), uint64(binary.BigEndian.Uint32(p[8:12])), uint64(binary.BigEndian.Uint32(p[12:16]))
	if m == 0 || m > MaxMetadataBytes || b == 0 || rp == 0 || 16+m+b+rp != uint64(len(p)) {
		return Result{}, ErrLimit
	}
	var r Result
	if err := parseJSON(p[16:16+m], &r); err != nil {
		return Result{}, err
	}
	r.BlockRLP = bytes.Clone(p[16+m : 16+m+b])
	r.ReceiptsRLP = bytes.Clone(p[16+m+b:])
	if err := validateResult(r); err != nil {
		return Result{}, err
	}
	return r, nil
}

func validateResult(r Result) error {
	if !validRef(r.Ref) || r.Ref.Height > 0 && r.Parent == (Hash{}) || r.StateRoot == (Hash{}) || r.ReceiptsRoot == (Hash{}) || len(r.BlockRLP) == 0 || len(r.ReceiptsRLP) == 0 {
		return fmt.Errorf("%w: result identity or roots", ErrProtocol)
	}
	if r.TxCount > 65536 || len(r.Steps) == 0 || len(r.Steps) > 65538 {
		return ErrLimit
	}
	raw := uint64(len(r.BlockRLP)) + uint64(len(r.ReceiptsRLP))
	tx, changes := uint32(0), 0
	for i, s := range r.Steps {
		if s.Ordinal != uint32(i) {
			return fmt.Errorf("%w: journal ordinal", ErrProtocol)
		}
		switch s.Cause {
		case Transaction:
			if s.TxIndex != int32(tx) || s.TxHash == (Hash{}) {
				return fmt.Errorf("%w: transaction attribution", ErrProtocol)
			}
			tx++
		case Finalize:
			if i != len(r.Steps)-1 || s.TxIndex != -1 || s.TxHash != (Hash{}) {
				return fmt.Errorf("%w: Finalize order", ErrProtocol)
			}
		case Genesis, Baseline, PreBlock:
			if i != 0 || s.TxIndex != -1 || s.TxHash != (Hash{}) || s.Cause == Genesis && r.Ref.Height != 0 {
				return fmt.Errorf("%w: baseline order", ErrProtocol)
			}
		default:
			return fmt.Errorf("%w: journal cause", ErrProtocol)
		}
		changes += len(s.Accounts) + len(s.Storage) + len(s.Code)
		if changes > 65536 {
			return ErrLimit
		}
		for j, c := range s.Accounts {
			if j > 0 && bytes.Compare(s.Accounts[j-1].Address[:], c.Address[:]) >= 0 {
				return fmt.Errorf("%w: account ordering", ErrProtocol)
			}
			if !c.Before.Exists && c.Before != (Account{}) || !c.After.Exists && c.After != (Account{}) {
				return fmt.Errorf("%w: absent account value", ErrProtocol)
			}
		}
		for j, c := range s.Storage {
			if j > 0 {
				prev := s.Storage[j-1]
				order := bytes.Compare(prev.Address[:], c.Address[:])
				if order > 0 || order == 0 && bytes.Compare(prev.Slot[:], c.Slot[:]) >= 0 {
					return fmt.Errorf("%w: slot ordering", ErrProtocol)
				}
			}
		}
		for j, c := range s.Code {
			if c.Hash == (Hash{}) || len(c.Bytes) == 0 || len(c.Bytes) > 1<<20 || j > 0 && bytes.Compare(s.Code[j-1].Hash[:], c.Hash[:]) >= 0 {
				return fmt.Errorf("%w: code bounds or ordering", ErrProtocol)
			}
			raw += uint64(len(c.Bytes))
		}
	}
	if tx != r.TxCount || r.Steps[len(r.Steps)-1].Cause != Finalize {
		return fmt.Errorf("%w: journal completeness", ErrProtocol)
	}
	if raw > MaxBlockBytes {
		return ErrLimit
	}
	return nil
}
