package porttest

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// verificationFixture returns a verified contract record for addr,
// verified at baseTime plus offset seconds.
func verificationFixture(addr common.Address, offset int64) *port.ContractVerification {
	return &port.ContractVerification{
		Address:              addr,
		IsVerified:           true,
		Name:                 "Token" + addr.Hex()[38:],
		CompilerVersion:      "v0.8.24+commit.e11b9ed9",
		OptimizationEnabled:  true,
		OptimizationRuns:     200,
		SourceCode:           "contract Token {}",
		ABI:                  `[{"type":"constructor","inputs":[]}]`,
		ConstructorArguments: "0x00",
		VerifiedAt:           time.Unix(int64(baseTime)+offset, 0).UTC(),
		LicenseType:          "MIT",
	}
}

// verificationAddr returns a distinct address for the i-th listed contract.
func verificationAddr(i int) common.Address {
	return common.HexToAddress(fmt.Sprintf("0x%040x", 0x7700+i))
}

// assertVerification compares two verification records.
func assertVerification(t *testing.T, want, got *port.ContractVerification) {
	t.Helper()
	require.NotNil(t, got)
	assert.True(t, want.VerifiedAt.Equal(got.VerifiedAt), "verifiedAt %v != %v", want.VerifiedAt, got.VerifiedAt)
	w, g := *want, *got
	w.VerifiedAt, g.VerifiedAt = time.Time{}, time.Time{}
	assert.Equal(t, w, g)
}

// testContractVerification checks ContractVerificationReader and
// ContractVerificationWriter: a record comes back as stored, a missing one
// is port.ErrNotFound (false from IsContractVerified), verified contracts
// are listed in verification time order with limit/offset pagination, and
// the count matches the list.
func testContractVerification(t *testing.T, newStore NewStore) {
	ctx := context.Background()

	t.Run("NotFoundOnEmptyStore", func(t *testing.T) {
		s := open[verificationStore](t, newStore)
		_, err := s.GetContractVerification(ctx, addrC)
		assert.ErrorIs(t, err, port.ErrNotFound)
		ok, err := s.IsContractVerified(ctx, addrC)
		require.NoError(t, err)
		assert.False(t, ok)
		list, err := s.ListVerifiedContracts(ctx, 10, 0)
		require.NoError(t, err)
		assert.Empty(t, list)
		n, err := s.CountVerifiedContracts(ctx)
		require.NoError(t, err)
		assert.Equal(t, 0, n)
	})

	t.Run("RoundTrip", func(t *testing.T) {
		s := open[verificationStore](t, newStore)
		want := verificationFixture(addrC, 0)
		require.NoError(t, s.SetContractVerification(ctx, want))
		got, err := s.GetContractVerification(ctx, addrC)
		require.NoError(t, err)
		assertVerification(t, want, got)

		ok, err := s.IsContractVerified(ctx, addrC)
		require.NoError(t, err)
		assert.True(t, ok)
		ok, err = s.IsContractVerified(ctx, created)
		require.NoError(t, err)
		assert.False(t, ok, "only the stored address is verified")
	})

	t.Run("ListInVerificationOrder", func(t *testing.T) {
		s := open[verificationStore](t, newStore)
		// Verified at descending address order so that time order and
		// address order differ.
		var want []common.Address
		for i := 0; i < 5; i++ {
			a := verificationAddr(4 - i)
			want = append(want, a)
			require.NoError(t, s.SetContractVerification(ctx, verificationFixture(a, int64(i))))
		}
		list, err := s.ListVerifiedContracts(ctx, 10, 0)
		require.NoError(t, err)
		assert.Equal(t, want, list, "oldest verification first")
		n, err := s.CountVerifiedContracts(ctx)
		require.NoError(t, err)
		assert.Equal(t, 5, n)
	})

	t.Run("Pagination", func(t *testing.T) {
		s := open[verificationStore](t, newStore)
		var want []common.Address
		for i := 0; i < 5; i++ {
			a := verificationAddr(i)
			want = append(want, a)
			require.NoError(t, s.SetContractVerification(ctx, verificationFixture(a, int64(i))))
		}
		list, err := s.ListVerifiedContracts(ctx, 2, 0)
		require.NoError(t, err)
		assert.Equal(t, want[0:2], list)
		list, err = s.ListVerifiedContracts(ctx, 2, 2)
		require.NoError(t, err)
		assert.Equal(t, want[2:4], list)
		list, err = s.ListVerifiedContracts(ctx, 2, 4)
		require.NoError(t, err)
		assert.Equal(t, want[4:], list, "the last page is short")
		list, err = s.ListVerifiedContracts(ctx, 2, 5)
		require.NoError(t, err)
		assert.Empty(t, list, "offset past the end")

		list, err = s.ListVerifiedContracts(ctx, 0, 0)
		require.NoError(t, err)
		assert.Equal(t, want, list, "limit 0 uses a default limit")
		list, err = s.ListVerifiedContracts(ctx, -1, -1)
		require.NoError(t, err)
		assert.Equal(t, want, list, "a negative limit uses a default limit and a negative offset is 0")
	})

	t.Run("SetReplacesRecord", func(t *testing.T) {
		s := open[verificationStore](t, newStore)
		require.NoError(t, s.SetContractVerification(ctx, verificationFixture(addrC, 0)))
		again := verificationFixture(addrC, 0)
		again.Name = "Renamed"
		require.NoError(t, s.SetContractVerification(ctx, again))
		got, err := s.GetContractVerification(ctx, addrC)
		require.NoError(t, err)
		assertVerification(t, again, got)
		n, err := s.CountVerifiedContracts(ctx)
		require.NoError(t, err)
		assert.Equal(t, 1, n)
	})

	t.Run("ReverifyListsOnce", func(t *testing.T) {
		knownDefect(t, "re-verifying a contract at a new time keeps its old list entry, so it is listed and counted twice")
		s := open[verificationStore](t, newStore)
		require.NoError(t, s.SetContractVerification(ctx, verificationFixture(addrC, 0)))
		require.NoError(t, s.SetContractVerification(ctx, verificationFixture(addrC, 60)))
		list, err := s.ListVerifiedContracts(ctx, 10, 0)
		require.NoError(t, err)
		assert.Equal(t, []common.Address{addrC}, list)
		n, err := s.CountVerifiedContracts(ctx)
		require.NoError(t, err)
		assert.Equal(t, 1, n)
	})

	t.Run("UnverifiedRecordIsNotVerified", func(t *testing.T) {
		knownDefect(t, "a record with IsVerified false is reported, listed and counted as verified")
		s := open[verificationStore](t, newStore)
		v := verificationFixture(addrC, 0)
		v.IsVerified = false
		require.NoError(t, s.SetContractVerification(ctx, v))
		ok, err := s.IsContractVerified(ctx, addrC)
		require.NoError(t, err)
		assert.False(t, ok)
		list, err := s.ListVerifiedContracts(ctx, 10, 0)
		require.NoError(t, err)
		assert.Empty(t, list)
		n, err := s.CountVerifiedContracts(ctx)
		require.NoError(t, err)
		assert.Equal(t, 0, n)
	})

	t.Run("Delete", func(t *testing.T) {
		s := open[verificationStore](t, newStore)
		require.NoError(t, s.SetContractVerification(ctx, verificationFixture(addrC, 0)))
		require.NoError(t, s.SetContractVerification(ctx, verificationFixture(created, 1)))
		require.NoError(t, s.DeleteContractVerification(ctx, addrC))

		_, err := s.GetContractVerification(ctx, addrC)
		assert.ErrorIs(t, err, port.ErrNotFound)
		ok, err := s.IsContractVerified(ctx, addrC)
		require.NoError(t, err)
		assert.False(t, ok)
		list, err := s.ListVerifiedContracts(ctx, 10, 0)
		require.NoError(t, err)
		assert.Equal(t, []common.Address{created}, list)
		n, err := s.CountVerifiedContracts(ctx)
		require.NoError(t, err)
		assert.Equal(t, 1, n)

		assert.NoError(t, s.DeleteContractVerification(ctx, addrC), "deleting a missing record is not an error")
	})

	t.Run("RejectsNil", func(t *testing.T) {
		s := open[verificationStore](t, newStore)
		assert.Error(t, s.SetContractVerification(ctx, nil))
	})
}
