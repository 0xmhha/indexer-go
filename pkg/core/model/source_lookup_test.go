package model

import (
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestSourceLookupDescriptorBinding(t *testing.T) {
	d := SourceLookupDescriptor{Version: 1, Profile: strings.Repeat("x", 256)}
	require.NoError(t, d.Validate())
	require.LessOrEqual(t, len(d.Definition()), 128)
	projection := ProjectionDescriptor{SourceLookup: &d, Workers: []string{SourceLookupWorker}}
	require.NoError(t, projection.ValidateSourceLookup())
	projection.BlobCapability = &BlobCapability{Profile: "other"}
	require.Error(t, projection.ValidateSourceLookup())
	projection.BlobCapability = nil
	projection.Workers = nil
	require.Error(t, projection.ValidateSourceLookup())
	projection.SourceLookup = nil
	projection.Workers = []string{SourceLookupWorker}
	require.Error(t, projection.ValidateSourceLookup())
	for _, p := range []string{"", strings.Repeat("x", 257), "a\x00b", string([]byte{255})} {
		d.Profile = p
		require.Error(t, d.Validate())
	}
}
