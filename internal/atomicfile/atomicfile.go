// Package atomicfile writes files atomically: a temp file in the target's directory, then a
// rename over the target, so readers see either the old file or the new one (SPEC §7.4).
package atomicfile

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Write calls write with a temp file in path's directory, then syncs it, sets perm and renames
// it to path. A done ctx writes nothing. On any error the temp file is removed and the existing
// file at path is untouched.
func Write(ctx context.Context, path string, perm os.FileMode, write func(io.Writer) error) (err error) {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	dir, name := filepath.Split(path)
	if dir == "" {
		dir = "."
	}
	tmp, err := os.CreateTemp(dir, "."+name+"-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file for %s: %w", path, err)
	}
	defer func() {
		if err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
		}
	}()
	// CreateTemp makes the file 0600.
	if err = tmp.Chmod(perm); err != nil {
		return fmt.Errorf("chmod temp file for %s: %w", path, err)
	}
	if err = write(tmp); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err = tmp.Sync(); err != nil {
		return fmt.Errorf("sync temp file for %s: %w", path, err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("close temp file for %s: %w", path, err)
	}
	if err = os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("rename temp file to %s: %w", path, err)
	}
	return nil
}
