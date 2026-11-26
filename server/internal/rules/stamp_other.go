//go:build !linux

package rules

import "os"

// fileStamp is _file_stamp where inode and ctime are unavailable.
type fileStamp struct {
	ino, size    uint64
	mtime, ctime int64
}

func stampOf(info os.FileInfo) *fileStamp {
	return &fileStamp{size: uint64(info.Size()), mtime: info.ModTime().UnixNano()}
}
