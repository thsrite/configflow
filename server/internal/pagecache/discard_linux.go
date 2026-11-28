//go:build linux

// Package pagecache gives the kernel hints about cached file contents.
package pagecache

import (
	"os"

	"golang.org/x/sys/unix"
)

var fadvise = unix.Fadvise

// Discard asks Linux to release the file's cached pages after a read.
func Discard(file *os.File) {
	_ = fadvise(int(file.Fd()), 0, 0, unix.FADV_DONTNEED)
}
