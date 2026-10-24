package executor

import (
	"bytes"
	"context"
	"reflect"
	"testing"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

func TestReplayKeepsSuccessfulConstructorDestructionAccountWitness(t *testing.T) {
	init := append(append([]byte{0x73}, common.Address{88}.Bytes()...), 0xff)
	_, in := evmFixtureWith(t, []byte{0}, []evmTxSpec{{input: init}})
	if in.Receipts[0].Status != types.ReceiptStatusSuccessful {
		t.Fatal("fixture creation failed")
	}
	out, err := Replay(t.Context(), in)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range out.Steps[0].Accounts {
		if a.Address == execwire.Address(in.Receipts[0].ContractAddress) {
			if a.Before != (execwire.Account{}) || a.After != (execwire.Account{}) {
				t.Fatal("constructor destruction not absent at both boundaries")
			}
			return
		}
	}
	t.Fatal("successful constructor destruction lost its account witness")
}

func TestReplayEVMRevertKeepsFeesWithoutStorageOrValue(t *testing.T) {
	code := []byte{0x60, 0x03, 0x60, 0x00, 0x55, 0x60, 0x00, 0x60, 0x00, 0xfd}
	_, in := evmFixtureWith(t, code, []evmTxSpec{{to: &account, value: 1}})
	if in.Receipts[0].Status != types.ReceiptStatusFailed {
		t.Fatal("fixture did not revert")
	}
	out, err := Replay(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Steps[0].Storage) != 0 {
		t.Fatal("reverted storage became history")
	}
	for _, a := range out.Steps[0].Accounts {
		if a.Address == execwire.Address(account) {
			t.Fatal("reverted value reached recipient")
		}
	}
	if len(out.Steps[0].Accounts) < 2 {
		t.Fatal("failed transaction lost nonce or gas fees")
	}
}

func TestReplayEVMCreationThenDestructionPreservesIntermediateState(t *testing.T) {
	key, err := crypto.ToECDSA(bytes.Repeat([]byte{1}, 32))
	if err != nil {
		t.Fatal(err)
	}
	created := crypto.CreateAddress(crypto.PubkeyToAddress(key.PublicKey), 0)
	// Runtime transfers any balance and self-destructs. Init also sets slot1=2.
	runtime := append([]byte{0x73}, common.BytesToAddress([]byte{88}).Bytes()...)
	runtime = append(runtime, 0xff)
	init := []byte{0x60, 0x02, 0x60, 0x01, 0x55, 0x60, byte(len(runtime)), 0x60, 17, 0x60, 0x00, 0x39, 0x60, byte(len(runtime)), 0x60, 0x00, 0xf3}
	init = append(init, runtime...)
	disk, in := evmFixtureWith(t, []byte{0x00}, []evmTxSpec{{input: init}, {to: &created}})
	before := dump(t, disk)
	for _, receipt := range in.Receipts {
		if receipt.Status != types.ReceiptStatusSuccessful {
			t.Fatal("lifecycle fixture reverted")
		}
	}
	out, err := Replay(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Steps) != 3 || len(out.Steps[0].Code) != 1 || !bytes.Equal(out.Steps[0].Code[0].Bytes, runtime) {
		t.Fatal("creation code lost before block-level deletion")
	}
	creation, deletion := false, false
	for _, a := range out.Steps[0].Accounts {
		if a.Address == execwire.Address(created) {
			creation = !a.Before.Exists && a.After.Exists && a.After.CodeHash == execwire.Hash(crypto.Keccak256Hash(runtime))
		}
	}
	for _, a := range out.Steps[1].Accounts {
		if a.Address == execwire.Address(created) {
			deletion = a.Before.Exists && !a.After.Exists && a.StorageCleared
		}
	}
	if !creation || !deletion || len(out.Steps[0].Storage) != 1 {
		t.Fatal("transaction-level lifetime or storage clear missing")
	}
	if !reflect.DeepEqual(before, dump(t, disk)) {
		t.Fatal("intermediate replay state persisted to node DB")
	}
}
