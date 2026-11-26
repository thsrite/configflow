//go:build !linux && !darwin

package store

import (
	"os"
	"time"
)

// accessTime is st_atime where the platform exposes it; elsewhere now.
func accessTime(os.FileInfo) time.Time { return time.Now() }
