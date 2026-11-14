package main

import (
	"agent/routes"
	"agent/upgrade"
	"errors"
	"io"
	"log"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

const agentLogLimit = 4 << 20
const agentLogTail = 256 << 10

// Core output under OpenRC (Mihomo logs every connection at info level).
const coreLogLimit = 16 << 20
const coreLogTail = 1 << 20

// Compact in place: OpenRC keeps its descriptors open for the service lifetime.
// Do not follow symlinks or truncate pipes, journals, or container stdout.
func compactAgentLog(path string) error {
	return compactLog(path, agentLogLimit, agentLogTail)
}

func compactLog(path string, limit, keep int64) error {
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() <= limit {
		return nil
	}
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return err
	}
	defer unix.Flock(fd, unix.LOCK_UN)
	tail := make([]byte, keep)
	n, err := f.ReadAt(tail, info.Size()-keep)
	if err != nil && err != io.EOF {
		return err
	}
	// Preserve the newest diagnostics without requiring free disk for a copy.
	// As with copytruncate, concurrent log writes in this short window may be lost.
	if _, err = f.WriteAt(tail[:n], 0); err != nil {
		return err
	}
	if err = f.Truncate(int64(n)); err != nil {
		return err
	}
	return f.Sync()
}

func cleanupDisk(cfg *Config) error {
	var prune error
	if binary, err := os.Executable(); err == nil {
		prune = upgrade.PruneCompleted(binary)
	}
	routeConfig := toRoutesConfig(cfg)
	results := []error{compactAgentLog(routes.AgentLogPath(routeConfig)), routes.CleanupDisk(routeConfig), prune}
	for _, path := range routes.OpenRCCoreLogPaths(routeConfig) {
		results = append(results, compactLog(path, coreLogLimit, coreLogTail))
	}
	return errors.Join(results...)
}

func startDiskCleanup(cfg *Config) {
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for {
			if err := cleanupDisk(cfg); err != nil {
				log.Printf("Disk cleanup deferred: %v", err)
			}
			<-ticker.C
		}
	}()
}
