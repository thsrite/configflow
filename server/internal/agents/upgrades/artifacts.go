package upgrades

// Agent executables offered for web updates (backend/agents/upgrades.py:
// artifact_directory, _read_artifact, _artifact_metadata, artifacts). The
// digest of each file is streamed through a fixed buffer and cached by the
// file's identity (device, inode, size, mtime, ctime), so an in-place edit or
// a replacement is read again and a file that changes while it is read is
// never published.

import (
	"container/list"
	"context"
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

	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/resource"
)

// Arches is ARCHES, in the order artifacts() lists them.
var Arches = []string{"linux-amd64", "linux-arm64", "linux-armv7"}

// ArtifactDirectory is artifact_directory(): $AGENTS_STATIC_DIR (default
// /opt/configflow/static/agents) when it exists, else backend/static/agents
// next to the program. Like Path.exists() of Python 3.11, a stat failure
// other than "missing" (ENOENT, ENOTDIR, EBADF, ELOOP), e.g. EACCES below a
// directory without search permission, is an error.
func ArtifactDirectory(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	configured, ok := os.LookupEnv("AGENTS_STATIC_DIR")
	if !ok {
		configured = "/opt/configflow/static/agents"
	}
	if configured == "" {
		configured = "." // Path('')
	}
	exists, err := pathExists(ctx, configured)
	if err != nil {
		return "", err
	}
	if exists {
		return configured, nil
	}
	return resource.FromEnv().Backend("static/agents"), nil
}

// ignoredStatError is pathlib's _ignore_error: the errors Path.exists() and
// Path.is_file() of Python 3.11 read as "no such file"; every other OSError
// is raised. An embedded NUL (ValueError in Python, EINVAL here) is also
// "no such file".
func ignoredStatError(err error) bool {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		switch errno {
		case syscall.ENOENT, syscall.ENOTDIR, syscall.EBADF, syscall.ELOOP, syscall.EINVAL:
			return true
		}
	}
	return false
}

// pathExists is Path.exists() (symlinks followed).
func pathExists(ctx context.Context, path string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if _, err := os.Stat(path); err != nil {
		if ignoredStatError(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
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
		mtime: st.Mtim.Nano(), ctime: st.Ctim.Nano()}
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
func readArtifact(ctx context.Context, path string, expected signature) (*ArtifactInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
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
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		count, err := handle.Read(buffer)
		calls++
		if readHook != nil {
			if hookErr := readHook(path, calls, count); hookErr != nil {
				return nil, hookErr
			}
		}
		if err := ctx.Err(); err != nil {
			return nil, err
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

// isFile is Path.is_file() of Python 3.11: a regular file (symlinks
// followed); "missing" errors are false, other stat errors are raised.
func isFile(ctx context.Context, path string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	info, err := os.Stat(path)
	if err != nil {
		if ignoredStatError(err) {
			return false, nil
		}
		return false, err
	}
	return info.Mode().IsRegular(), nil
}

// artifactMetadata is _artifact_metadata(path) (POSIX: the digest cache is
// always used).
func artifactMetadata(ctx context.Context, path string) (*ArtifactInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	key, err := filepath.Abs(path)
	if err != nil {
		key = path
	}
	cacheMu.Lock()
	defer cacheMu.Unlock()
	for attempt := 0; attempt < 2; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		regular, err := isFile(ctx, path)
		if err != nil {
			return nil, err
		}
		if !regular {
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
		metadata, err := readArtifact(ctx, path, expected)
		if errors.Is(err, errChanged) {
			if attempt == 0 {
				continue
			}
			return nil, ErrArtifactChanged
		}
		if err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
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

// ArtifactsModel returns {arch: {'sha256', 'size'}} for every usable
// executable, in Arches order. Each call returns new objects. Errors are
// the OSError Python raises (unreadable file, ErrArtifactChanged).
func ArtifactsModel(ctx context.Context) (model.Map[model.UpgradeArtifact], error) {
	ctx = lifecycle.Detach(ctx)
	var result model.Map[model.UpgradeArtifact]
	if err := ctx.Err(); err != nil {
		return result, err
	}
	for _, arch := range Arches {
		// artifact_directory() is evaluated for every architecture.
		directory, err := ArtifactDirectory(ctx)
		if err != nil {
			return result, err
		}
		metadata, err := artifactMetadata(ctx, joinPath(directory, "configflow-agent-"+arch))
		if err != nil {
			return result, err
		}
		if metadata != nil {
			result.Set(arch, model.UpgradeArtifact{SHA256: metadata.SHA256, Size: model.Int(metadata.Size)})
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
