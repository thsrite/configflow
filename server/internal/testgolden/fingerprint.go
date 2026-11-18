package testgolden

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// Fingerprint computes the module-external reference inputs without Python.
// Gate scripts may export its result as PYREF_INPUT_FINGERPRINT before go test;
// this environment input becomes part of Go's otherwise module-local test cache.
func Fingerprint() (string, error) {
	root, err := Root()
	if err != nil {
		return "", err
	}
	h := sha256.New()
	err = filepath.WalkDir(filepath.Join(root, "backend"), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "__pycache__" || d.Name() == "go-agent" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".py") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		fmt.Fprintf(h, "%s\x00%s\x00", filepath.ToSlash(rel), digest(data))
		return nil
	})
	if err != nil {
		return "", err
	}
	dep, err := dependencies()
	if err != nil {
		return "", err
	}
	fmt.Fprintf(h, "venv\x00%s\x00", dep)
	return hex.EncodeToString(h.Sum(nil)), nil
}

var processFingerprint = sync.OnceValues(Fingerprint)

func cacheGuard() error {
	// Getenv is deliberately unconditional: testing logs this read as a cache
	// input, including when the caller accidentally omits the wrapper.
	supplied := os.Getenv("PYREF_INPUT_FINGERPRINT")
	if supplied == "" {
		uncached := false
		flag.Visit(func(f *flag.Flag) {
			if f.Name == "test.count" {
				count, err := strconv.Atoi(f.Value.String())
				uncached = err == nil && count > 0
			}
		})
		if uncached {
			return nil
		}
		return fmt.Errorf("cached tests require tools/pyref -test -- <go test arguments>, or explicit -count=1; PYREF_INPUT_FINGERPRINT is missing")
	}
	current, err := processFingerprint()
	if err != nil {
		return err
	}
	if current != supplied {
		return fmt.Errorf("Python 参考已变化，请重新录制: stale PYREF_INPUT_FINGERPRINT; recompute via tools/pyref")
	}
	return nil
}
