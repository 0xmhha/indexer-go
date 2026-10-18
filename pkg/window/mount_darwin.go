package window

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func checkMount(dir *os.File, spec VolumeSpec) error {
	if spec.AllowDirectory {
		return nil
	}
	var st unix.Statfs_t
	if err := unix.Fstatfs(int(dir.Fd()), &st); err != nil {
		return err
	}
	path, err := filepath.EvalSymlinks(spec.Root)
	if err != nil {
		return err
	}
	if string(bytes.TrimRight(st.Mntonname[:], "\x00")) != path {
		return fmt.Errorf("%w: volume root is not a mount point", ErrUnavailable)
	}
	return nil
}
