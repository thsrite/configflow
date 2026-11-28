//go:build !linux

// Package pagecache gives the kernel hints about cached file contents.
package pagecache

import "os"

// Discard is a no-op outside Linux.
func Discard(file *os.File) {}
