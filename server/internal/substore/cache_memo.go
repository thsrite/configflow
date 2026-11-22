package substore

import (
	"io/fs"
	"os"
	"slices"
	"sync"
	"syscall"

	"configflow/internal/model"
)

// Decoded node caches are kept per cache file and reused while the file is
// unchanged (O10 R8): decoding one subscription's nodes costs several MB per
// read, and every generated configuration, provider and scheduler round
// reads them. A file is identified by its size, modification time and inode
// (writers replace the file atomically, which gives it a new inode), checked
// with one stat before and one after reading. Only successful decodes are
// kept, so a malformed file is read and reported every time as before.

type cacheFileKey struct {
	size, mtime int64
	dev, ino    uint64
}

func fileKeyOf(info fs.FileInfo) cacheFileKey {
	key := cacheFileKey{size: info.Size(), mtime: info.ModTime().UnixNano()}
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		key.dev, key.ino = uint64(st.Dev), uint64(st.Ino)
	}
	return key
}

type cacheMemoEntry struct {
	key     cacheFileKey
	cache   *Cache        // decoded file; never modified, never handed out
	summary *CacheSummary // envelope summary; never modified, never handed out
	encoded []byte        // model encoding of cache; never modified, never handed out
}

var cacheMemo = struct {
	sync.Mutex
	entries map[string]cacheMemoEntry
}{entries: map[string]cacheMemoEntry{}}

// statCacheFile returns the file's identity, or ok=false when it is missing.
func statCacheFile(path string) (cacheFileKey, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return cacheFileKey{}, false
	}
	return fileKeyOf(info), true
}

func memoLookup(path string, key cacheFileKey) cacheMemoEntry {
	cacheMemo.Lock()
	defer cacheMemo.Unlock()
	entry, ok := cacheMemo.entries[path]
	if !ok || entry.key != key {
		return cacheMemoEntry{}
	}
	return entry
}

// memoStore keeps a decode made from a file whose identity did not change
// while it was read.
func memoStore(path string, before cacheFileKey, update func(*cacheMemoEntry)) {
	after, ok := statCacheFile(path)
	if !ok || after != before {
		return
	}
	cacheMemo.Lock()
	defer cacheMemo.Unlock()
	entry := cacheMemo.entries[path]
	if entry.key != before {
		entry = cacheMemoEntry{key: before}
	}
	update(&entry)
	cacheMemo.entries[path] = entry
}

func memoForget(path string) {
	cacheMemo.Lock()
	delete(cacheMemo.entries, path)
	cacheMemo.Unlock()
}

// clonePointer copies the value p points to. (The constraint is spelled out:
// subs' boundary test keeps the identifier `any` out of the native owners.)
func clonePointer[T interface{}](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

// cloneCache gives the caller its own record: every struct and slice it may
// assign to or append to is new. Fields, Map and Dynamic values are
// copy-on-write and Raw is immutable, so they are shared.
func cloneCache(c *Cache) *Cache {
	if c == nil {
		return nil
	}
	out := *c
	out.Count = clonePointer(c.Count)
	if c.Nodes != nil {
		out.Nodes = make([]model.Node, len(c.Nodes))
		for i, node := range c.Nodes {
			node.ProxyString = clonePointer(node.ProxyString)
			node.Enabled = clonePointer(node.Enabled)
			node.DialerRef = clonePointer(node.DialerRef)
			out.Nodes[i] = node
		}
	}
	if c.Metadata != nil {
		metadata := *c.Metadata
		if metadata.Traffic != nil {
			traffic := *metadata.Traffic
			traffic.Upload, traffic.Download = clonePointer(traffic.Upload), clonePointer(traffic.Download)
			traffic.Total, traffic.Expire = clonePointer(traffic.Total), clonePointer(traffic.Expire)
			metadata.Traffic = &traffic
		}
		if metadata.Hosts != nil {
			hosts := *metadata.Hosts
			hosts.Skipped = slices.Clone(hosts.Skipped)
			metadata.Hosts = &hosts
		}
		if metadata.Dialers != nil {
			dialers := *metadata.Dialers
			dialers.Unresolved = slices.Clone(dialers.Unresolved)
			metadata.Dialers = &dialers
		}
		out.Metadata = &metadata
	}
	return &out
}

func cloneSummary(s *CacheSummary) *CacheSummary {
	if s == nil {
		return nil
	}
	out := *s
	out.UpdatedAt, out.URL = clonePointer(s.UpdatedAt), clonePointer(s.URL)
	return &out
}
