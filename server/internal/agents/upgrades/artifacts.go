package upgrades

// Agent executables offered for web updates (backend/agents/upgrades.py:
// artifact_directory, _read_artifact, _artifact_metadata, artifacts). The
// digest of each file is streamed through a fixed buffer and cached by the
// file's identity (device, inode, size, mtime, ctime), so an in-place edit or
// a replacement is read again and a file that changes while it is read is
// never published.

import (
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"configflow/internal/jsonx"
	"configflow/internal/resource"
)

// Arches is ARCHES, in the order artifacts() lists them.
var Arches = []string{"linux-amd64", "linux-arm64", "linux-armv7"}

// ArtifactDirectory is artifact_directory(): $AGENTS_STATIC_DIR (default
// /opt/configflow/static/agents) when it exists, else backend/static/agents
// next to the program.
func ArtifactDirectory() string {
	configured, ok := os.LookupEnv("AGENTS_STATIC_DIR")
	if !ok {
		configured = "/opt/configflow/static/agents"
	}
	if configured == "" {
		configured = "." // Path('')
	}
	if _, err := os.Stat(configured); err == nil {
		return configured
	}
	return resource.FromEnv().Backend("static/agents")
}

// joinPath is Path(directory) / name without cleaning ".." (as pathlib).
func joinPath(directory, name string) string {
	trimmed := strings.TrimRight(directory, "/")
	if trimmed == "" && strings.HasPrefix(directory, "/") {
		return "/" + name
	}
	return trimmed + "/" + name
}

// Limits of upgrades.py; variables so tests can lower them as the Python
// tests monkeypatch them.
var (
	maxArtifactBytes   int64 = 128 * 1024 * 1024
	artifactReadBytes        = 256 * 1024
	artifactCacheLimit       = 12
)

// ErrArtifactChanged is the OSError raised when a file changed during both
// reads.
var ErrArtifactChanged = errors.New("Agent artifact changed while being read; please retry")

var errChanged = errors.New("artifact changed") // _ArtifactChanged

type signature struct {
	dev, ino, size, mtime, ctime int64
}

func signatureOf(info fs.FileInfo) signature {
	st, _ := info.Sys().(*syscall.Stat_t)
	if st == nil {
		return signature{size: info.Size(), mtime: info.ModTime().UnixNano()}
	}
	return signature{dev: int64(st.Dev), ino: int64(st.Ino), size: st.Size,
		mtime: st.Mtim.Sec*1e9 + st.Mtim.Nsec, ctime: st.Ctim.Sec*1e9 + st.Ctim.Nsec}
}

// ArtifactInfo is one artifact's {'sha256', 'size'}.
type ArtifactInfo struct {
	SHA256 string
	Size   int64
}

type cacheEntry struct {
	key      string
	expected signature
	info     ArtifactInfo
}

var (
	cacheMu    sync.Mutex
	cacheOrder = list.New() // oldest first, like an OrderedDict
	cacheIndex = map[string]*list.Element{}
)

// Test seams: openHook runs before a file is opened for reading, readHook
// after every read (calls counts reads of this open, n is the byte count, 0
// at end of file); an error from readHook is the read's error.
var (
	openHook func(path string)
	readHook func(path string, calls, n int) error
)

func cachePop(key string) {
	if element, ok := cacheIndex[key]; ok {
		cacheOrder.Remove(element)
		delete(cacheIndex, key)
	}
}

// readArtifact is _read_artifact(path, expected): the digest and size of an
// ELF file within the size limit, nil for any other file, errChanged when the
// path and the bytes read did not describe one stable file.
func readArtifact(path string, expected signature) (*ArtifactInfo, error) {
	if openHook != nil {
		openHook(path)
	}
	handle, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer handle.Close()
	info, err := handle.Stat()
	if err != nil {
		return nil, err
	}
	if signatureOf(info) != expected {
		return nil, errChanged
	}
	digest := sha256.New()
	var size int64
	var magic []byte
	buffer := make([]byte, artifactReadBytes)
	calls := 0
	for {
		count, err := handle.Read(buffer)
		calls++
		if readHook != nil {
			if hookErr := readHook(path, calls, count); hookErr != nil {
				return nil, hookErr
			}
		}
		if count > 0 {
			if len(magic) < 4 {
				magic = append(magic, buffer[:min(4-len(magic), count)]...)
			}
			size += int64(count)
			digest.Write(buffer[:count])
		}
		if err == io.EOF || (err == nil && count == 0) {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	info, err = handle.Stat()
	if err != nil {
		return nil, err
	}
	afterRead := signatureOf(info)
	pathInfo, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, errChanged
		}
		return nil, err
	}
	afterPath := signatureOf(pathInfo)
	if expected != afterRead || expected != afterPath || size != expected.size {
		return nil, errChanged
	}
	if string(magic) != "\x7fELF" || size > maxArtifactBytes {
		return nil, nil
	}
	return &ArtifactInfo{SHA256: hex.EncodeToString(digest.Sum(nil)), Size: size}, nil
}

// isFile is Path.is_file(): a regular file (symlinks followed), false on
// any error.
func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// artifactMetadata is _artifact_metadata(path) (POSIX: the digest cache is
// always used).
func artifactMetadata(path string) (*ArtifactInfo, error) {
	key, err := filepath.Abs(path)
	if err != nil {
		key = path
	}
	cacheMu.Lock()
	defer cacheMu.Unlock()
	for attempt := 0; attempt < 2; attempt++ {
		if !isFile(path) {
			cachePop(key)
			return nil, nil
		}
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		expected := signatureOf(info)
		if element, ok := cacheIndex[key]; ok {
			entry := element.Value.(*cacheEntry)
			if entry.expected == expected {
				cacheOrder.MoveToBack(element)
				result := entry.info
				return &result, nil
			}
		}
		cachePop(key)
		metadata, err := readArtifact(path, expected)
		if errors.Is(err, errChanged) {
			if attempt == 0 {
				continue
			}
			return nil, ErrArtifactChanged
		}
		if err != nil {
			return nil, err
		}
		if metadata != nil {
			cacheIndex[key] = cacheOrder.PushBack(&cacheEntry{key: key, expected: expected, info: *metadata})
			for cacheOrder.Len() > artifactCacheLimit {
				oldest := cacheOrder.Front()
				cacheOrder.Remove(oldest)
				delete(cacheIndex, oldest.Value.(*cacheEntry).key)
			}
		}
		return metadata, nil
	}
	return nil, nil
}

// Artifacts is artifacts(): {arch: {'sha256', 'size'}} for every usable
// executable, in Arches order. Each call returns new objects. Errors are
// the OSError Python raises (unreadable file, ErrArtifactChanged).
func Artifacts() (*jsonx.Object, error) {
	result := jsonx.NewObject()
	directory := ArtifactDirectory()
	for _, arch := range Arches {
		metadata, err := artifactMetadata(joinPath(directory, "configflow-agent-"+arch))
		if err != nil {
			return nil, err
		}
		if metadata != nil {
			result.Set(arch, jsonx.Obj("sha256", metadata.SHA256, "size", jsonx.Int(metadata.Size)))
		}
	}
	return result, nil
}

// ResetArtifactCache empties the digest cache (tests).
func ResetArtifactCache() {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	cacheOrder.Init()
	cacheIndex = map[string]*list.Element{}
}

func cacheLen() int {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	return cacheOrder.Len()
}
