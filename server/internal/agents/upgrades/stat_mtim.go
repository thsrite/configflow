//go:build !darwin && !freebsd && !netbsd

package upgrades

import "syscall"

func statNanos(st *syscall.Stat_t) (atime, mtime, ctime int64) {
	return st.Atim.Nano(), st.Mtim.Nano(), st.Ctim.Nano()
}
