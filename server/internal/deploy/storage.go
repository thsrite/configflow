package deploy

import (
	"configflow/internal/fsx"
	"configflow/internal/lifecycle"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"syscall"
)

// L4 files are leaf stores: these functions never enter the repository while
// holding a store mutex. Atomic writes retain old bytes until rename succeeds.
func atomicWrite(ctx context.Context, path string, data []byte) error {
	if err := fsx.WriteFile(ctx, path, data, fsx.Options{Pattern: ".write-*"}); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("write deployment file: %w", err)
	}
	return nil
}
func readPrivate(path string, limit int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, fmt.Errorf("invalid private deployment file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("private deployment file too large")
	}
	return data, nil
}
func privateDir(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("invalid private deployment directory")
	}
	return nil
}

type folderInfo struct {
	path  string
	size  int64
	mtime int64
}

func privateFolders(root string) ([]folderInfo, error) {
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := []folderInfo{}
	for _, e := range entries {
		if !e.IsDir() || e.Type()&os.ModeSymlink != 0 {
			continue
		}
		p := filepath.Join(root, e.Name())
		info, err := e.Info()
		if err != nil {
			return nil, err
		}
		f := folderInfo{path: p, mtime: info.ModTime().UnixNano()}
		children, err := os.ReadDir(p)
		if err != nil {
			return nil, err
		}
		for _, child := range children {
			info, err := child.Info()
			if err != nil {
				return nil, err
			}
			if info.Mode().IsRegular() {
				f.size += info.Size()
			}
		}
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].mtime < out[j].mtime })
	return out, nil
}

// Stamp identifies an atomically replaced receipt without reading its contents.
type Stamp struct {
	Inode   uint64
	Size    int64
	MtimeNS int64
}

func fileStamp(path string) (Stamp, bool) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return Stamp{}, false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return Stamp{}, false
	}
	return Stamp{stat.Ino, info.Size(), info.ModTime().UnixNano()}, true
}

var versionPattern = regexp.MustCompile(`^(?:[0-9a-f]{8}|[0-9a-f]{64})$`)

type leafLocks struct {
	sync.Mutex
	byKey map[string]*leafLock
}
type leafLock struct {
	mu   sync.Mutex
	refs int
}

func (l *leafLocks) lock(key string) func() {
	l.Lock()
	if l.byKey == nil {
		l.byKey = map[string]*leafLock{}
	}
	entry := l.byKey[key]
	if entry == nil {
		entry = &leafLock{}
		l.byKey[key] = entry
	}
	entry.refs++
	l.Unlock()
	entry.mu.Lock()
	return func() {
		entry.mu.Unlock()
		l.Lock()
		defer l.Unlock()
		entry.refs--
		if entry.refs == 0 {
			delete(l.byKey, key)
		}
	}
}

// WriteFileAtomic publishes one complete private file by fsync and rename.
func WriteFileAtomic(ctx context.Context, path string, data []byte) error {
	return atomicWrite(lifecycle.Detach(ctx), path, data)
}
