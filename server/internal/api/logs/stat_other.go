//go:build !linux

package logs

import (
	"os"
	"time"
)

// fileTimes returns st_mtime and, where st_ctime is not available, st_mtime.
func fileTimes(info os.FileInfo) (modified, changed time.Time) {
	return info.ModTime(), info.ModTime()
}
