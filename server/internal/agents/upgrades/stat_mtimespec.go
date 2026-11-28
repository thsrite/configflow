//go:build darwin || freebsd || netbsd

package upgrades

import "syscall"

func statNanos(st *syscall.Stat_t) (atime, mtime, ctime int64) {
	return st.Atimespec.Nano(), st.Mtimespec.Nano(), st.Ctimespec.Nano()
}
