package porttest

import (
	"context"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// abiFixture returns a small ABI JSON document labelled by name.
func abiFixture(name string) []byte {
	return []byte(`[{"type":"function","name":"` + name + `","inputs":[],"outputs":[]}]`)
}

// testABI checks ABIReader and ABIWriter: an ABI comes back byte for byte
// under its contract address, a missing ABI is port.ErrNotFound from GetABI
// and false from HasABI, SetABI replaces, DeleteABI removes, and ListABIs
// lists exactly the addresses that have an ABI.
func testABI(t *testing.T, newStore NewStore) {
	ctx := context.Background()

	t.Run("NotFoundOnEmptyStore", func(t *testing.T) {
		s := open[abiStore](t, newStore)
		_, err := s.GetABI(ctx, addrC)
		assert.ErrorIs(t, err, port.ErrNotFound)
		has, err := s.HasABI(ctx, addrC)
		require.NoError(t, err)
		assert.False(t, has)
		list, err := s.ListABIs(ctx)
		require.NoError(t, err)
		assert.Empty(t, list)
	})

	t.Run("RoundTrip", func(t *testing.T) {
		s := open[abiStore](t, newStore)
		require.NoError(t, s.SetABI(ctx, addrC, abiFixture("c")))
		require.NoError(t, s.SetABI(ctx, created, abiFixture("created")))

		got, err := s.GetABI(ctx, addrC)
		require.NoError(t, err)
		assert.Equal(t, abiFixture("c"), got)
		got, err = s.GetABI(ctx, created)
		require.NoError(t, err)
		assert.Equal(t, abiFixture("created"), got)

		has, err := s.HasABI(ctx, addrC)
		require.NoError(t, err)
		assert.True(t, has)
		has, err = s.HasABI(ctx, unknown)
		require.NoError(t, err)
		assert.False(t, has, "only the stored addresses have an ABI")
		_, err = s.GetABI(ctx, unknown)
		assert.ErrorIs(t, err, port.ErrNotFound)
	})

	t.Run("ReturnedBytesAreACopy", func(t *testing.T) {
		s := open[abiStore](t, newStore)
		in := abiFixture("c")
		require.NoError(t, s.SetABI(ctx, addrC, in))
		in[0] = 'X'
		got, err := s.GetABI(ctx, addrC)
		require.NoError(t, err)
		assert.Equal(t, abiFixture("c"), got, "changing the argument after SetABI does not change the stored ABI")
		got[0] = 'Y'
		again, err := s.GetABI(ctx, addrC)
		require.NoError(t, err)
		assert.Equal(t, abiFixture("c"), again, "changing a returned ABI does not change the stored ABI")
	})

	t.Run("ListABIs", func(t *testing.T) {
		s := open[abiStore](t, newStore)
		for _, a := range []common.Address{created, addrC, addrA} {
			require.NoError(t, s.SetABI(ctx, a, abiFixture(a.Hex())))
		}
		list, err := s.ListABIs(ctx)
		require.NoError(t, err)
		assert.ElementsMatch(t, []common.Address{addrA, addrC, created}, list)

		require.NoError(t, s.SetABI(ctx, addrC, abiFixture("again")))
		list, err = s.ListABIs(ctx)
		require.NoError(t, err)
		assert.Len(t, list, 3, "replacing an ABI does not list its address twice")
	})

	t.Run("SetABIReplaces", func(t *testing.T) {
		s := open[abiStore](t, newStore)
		require.NoError(t, s.SetABI(ctx, addrC, abiFixture("old")))
		require.NoError(t, s.SetABI(ctx, addrC, abiFixture("new")))
		got, err := s.GetABI(ctx, addrC)
		require.NoError(t, err)
		assert.Equal(t, abiFixture("new"), got)
	})

	t.Run("DeleteABI", func(t *testing.T) {
		s := open[abiStore](t, newStore)
		require.NoError(t, s.SetABI(ctx, addrC, abiFixture("c")))
		require.NoError(t, s.SetABI(ctx, created, abiFixture("created")))
		require.NoError(t, s.DeleteABI(ctx, addrC))

		_, err := s.GetABI(ctx, addrC)
		assert.ErrorIs(t, err, port.ErrNotFound)
		has, err := s.HasABI(ctx, addrC)
		require.NoError(t, err)
		assert.False(t, has)
		list, err := s.ListABIs(ctx)
		require.NoError(t, err)
		assert.Equal(t, []common.Address{created}, list)

		assert.NoError(t, s.DeleteABI(ctx, addrC), "deleting a missing ABI is not an error")
		assert.NoError(t, s.DeleteABI(ctx, unknown))
	})

	t.Run("RejectsEmptyABI", func(t *testing.T) {
		s := open[abiStore](t, newStore)
		assert.Error(t, s.SetABI(ctx, addrC, nil))
		assert.Error(t, s.SetABI(ctx, addrC, []byte{}))
		has, err := s.HasABI(ctx, addrC)
		require.NoError(t, err)
		assert.False(t, has, "a rejected ABI is not stored")
	})
}
