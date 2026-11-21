package logs

import (
	"os"
	"syscall"
	"time"
)

// fileTimes returns st_mtime and st_ctime.
func fileTimes(info os.FileInfo) (modified, changed time.Time) {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return info.ModTime(), time.Unix(int64(st.Ctim.Sec), int64(st.Ctim.Nsec))
	}
	return info.ModTime(), info.ModTime()
}
