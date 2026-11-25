// Package fsutil holds small file system helpers shared by the scanners.
package fsutil

import (
	"context"
	"io/fs"
	"path/filepath"
)

// DirSize adds up the size of the regular files under dir without following
// symlinks. Unreadable entries are ignored. It stops early when ctx is done.
func DirSize(ctx context.Context, dir string) int64 {
	var total int64

	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if err != nil {
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}

			return nil
		}

		if !d.Type().IsRegular() {
			return nil
		}

		if info, infoErr := d.Info(); infoErr == nil {
			total += info.Size()
		}

		return nil
	})

	return total
}
