// Package venueabi reads the pinned venue ABI fixtures in
// pkg/features/dex/testdata/venues for the DEX conformance tests. It lives
// beside testchain, outside pkg/features, because it reads files.
package venueabi

import (
	"bytes"
	"os"
	"path/filepath"

	"github.com/ethereum/go-ethereum/accounts/abi"
)

// Load parses dir/venue/file.
func Load(dir, venue, file string) (abi.ABI, error) {
	data, err := os.ReadFile(filepath.Join(dir, venue, file))
	if err != nil {
		return abi.ABI{}, err
	}
	return abi.JSON(bytes.NewReader(data))
}

// Words is the number of 32-byte head words a value of type typ occupies in
// ABI encoding: one for a dynamic value (its offset), the sum of the
// components for a static tuple or array.
func Words(typ abi.Type) int {
	switch typ.T {
	case abi.TupleTy:
		if dynamic(typ) {
			return 1
		}
		n := 0
		for _, el := range typ.TupleElems {
			n += Words(*el)
		}
		return n
	case abi.ArrayTy:
		if dynamic(typ) {
			return 1
		}
		return typ.Size * Words(*typ.Elem)
	default:
		return 1
	}
}

// ArgsWords is the head words of a list of arguments.
func ArgsWords(args abi.Arguments) int {
	n := 0
	for _, a := range args {
		n += Words(a.Type)
	}
	return n
}

func dynamic(typ abi.Type) bool {
	switch typ.T {
	case abi.StringTy, abi.BytesTy, abi.SliceTy:
		return true
	case abi.TupleTy:
		for _, el := range typ.TupleElems {
			if dynamic(*el) {
				return true
			}
		}
	case abi.ArrayTy:
		return dynamic(*typ.Elem)
	}
	return false
}
