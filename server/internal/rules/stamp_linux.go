//go:build linux

package rules

import (
	"os"
	"syscall"
)

// fileStamp is _file_stamp: (st_ino, st_size, st_mtime_ns, st_ctime_ns).
type fileStamp struct {
	ino, size    uint64
	mtime, ctime int64
}

func stampOf(info os.FileInfo) *fileStamp {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return &fileStamp{size: uint64(info.Size()), mtime: info.ModTime().UnixNano()}
	}
	return &fileStamp{
		ino: stat.Ino, size: uint64(stat.Size),
		mtime: stat.Mtim.Nano(), ctime: stat.Ctim.Nano(),
	}
}
