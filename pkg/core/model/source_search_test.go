package model

import (
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestSourceSearchDescriptorBinding(t *testing.T) {
	v := SourceSearchDescriptor{Version: 1, Profile: strings.Repeat("x", 256)}
	require.NoError(t, v.Validate())
	require.LessOrEqual(t, len(v.Definition()), 128)
	d := ProjectionDescriptor{SourceSearch: &v, Workers: []string{SourceSearchWorker}}
	require.NoError(t, d.ValidateSourceSearch())
	d.SourceLookup = &SourceLookupDescriptor{Version: 1, Profile: "other"}
	require.Error(t, d.ValidateSourceSearch())
	d.SourceLookup = nil
	d.BlobCapability = &BlobCapability{Profile: "other"}
	require.Error(t, d.ValidateSourceSearch())
	d.BlobCapability = nil
	d.Workers = nil
	require.Error(t, d.ValidateSourceSearch())
	d.SourceSearch = nil
	d.Workers = []string{SourceSearchWorker}
	require.Error(t, d.ValidateSourceSearch())
}
