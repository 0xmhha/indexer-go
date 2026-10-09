package sdktest_test

import (
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// featureDirs lists the packages of the indexer's features.
func featureDirs(t *testing.T) []string {
	var dirs []string
	for _, root := range []string{"../../features", "../../chains/stablenet/features"} {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() && d.Name() != "testdata" {
				dirs = append(dirs, path)
			}
			return nil
		})
		require.NoError(t, err)
	}
	return dirs
}
