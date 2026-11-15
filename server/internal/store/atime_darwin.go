//go:build darwin

package store

import (
	"os"
	"syscall"
	"time"
)

// accessTime is st_atime of a file, which shutil.copy2 copies.
func accessTime(info os.FileInfo) time.Time {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		return time.Unix(stat.Atimespec.Sec, stat.Atimespec.Nsec)
	}
	return time.Now()
}
