package window

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func checkMount(_ *os.File, spec VolumeSpec) error {
	if spec.AllowDirectory {
		return nil
	}
	path, err := filepath.EvalSymlinks(spec.Root)
	if err != nil {
		return err
	}
	f, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	unescape := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) > 5 && unescape.Replace(fields[4]) == path {
			return nil
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return fmt.Errorf("%w: volume root is not a mount point", ErrUnavailable)
}
