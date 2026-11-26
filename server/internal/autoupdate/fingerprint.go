package autoupdate

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// serverIgnoredDirs are test-only packages left out of the server fingerprint.
var serverIgnoredDirs = map[string]bool{
	"testdata": true, "difftest": true, "installertest": true, "yamlxtest": true,
	"testgolden": true, "testjson": true, "fuzz": true,
}

var precompressExts = []string{".js", ".mjs", ".css", ".html", ".svg", ".json", ".webmanifest"}

// FrontendInput selects frontend build inputs: what reaches the image build
// (.dockerignore drops *.md and dotfiles).
func FrontendInput(name string) bool {
	return !strings.HasSuffix(name, ".md") && !strings.HasPrefix(name, ".")
}

// GoModuleInput selects the inputs of the Agent and node-probe modules.
func GoModuleInput(name string) bool {
	return strings.HasSuffix(name, ".go") || name == "go.mod" || name == "go.sum"
}

func serverInput(name string) bool {
	return !strings.HasPrefix(name, ".") && !strings.HasSuffix(name, "_test.go") &&
		!strings.HasSuffix(name, ".md") && !strings.HasSuffix(name, ".py")
}

// walkInputs feeds the digest with "<relative path>" + sha256(content) for
// every selected file below dir, in os.walk order (top-down, sorted, a
// directory's files before its subdirectories; symlinked directories are
// listed but not entered). skipDir prunes directories by name.
func walkInputs(digest hash.Hash, root, dir string, include func(string) bool, skipDir func(string) bool) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil // os.walk ignores unreadable directories
	}
	var files, dirs []string
	for _, entry := range entries {
		name := entry.Name()
		isDir := entry.IsDir()
		if entry.Type()&fs.ModeSymlink != 0 {
			if info, err := os.Stat(filepath.Join(dir, name)); err == nil && info.IsDir() {
				// Listed as a directory, never descended into.
				continue
			}
		}
		if isDir {
			if !skipDir(name) {
				dirs = append(dirs, name)
			}
		} else if include(name) {
			files = append(files, name)
		}
	}
	sort.Strings(files)
	sort.Strings(dirs)
	for _, name := range files {
		if err := addFile(digest, root, filepath.Join(dir, name)); err != nil {
			return err
		}
	}
	for _, name := range dirs {
		if err := walkInputs(digest, root, filepath.Join(dir, name), include, skipDir); err != nil {
			return err
		}
	}
	return nil
}

func addFile(digest hash.Hash, root, path string) error {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return fmt.Errorf("fingerprint %s: %w", path, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("fingerprint: %w", err)
	}
	sum := sha256.Sum256(data)
	digest.Write([]byte(filepath.ToSlash(rel)))
	digest.Write(sum[:])
	return nil
}

func skipBuildDir(name string) bool {
	return name == "node_modules" || name == "dist" || strings.HasPrefix(name, ".")
}

// TreeHash fingerprints the files that affect a build, so only changed
// components are rebuilt. Only build inputs count: the fingerprint taken
// from the Docker build context equals the one taken from fetched source.
func TreeHash(path string, include func(string) bool) (string, error) {
	digest := sha256.New()
	if err := walkInputs(digest, path, path, include, skipBuildDir); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

// ServerTreeHash fingerprints the Go server build inputs: go.mod, go.sum,
// cmd/, internal/ and local modules under third_party/ (go:embed assets
// included, tests excluded).
func ServerTreeHash(path string) (string, error) {
	digest := sha256.New()
	for _, name := range []string{"go.mod", "go.sum"} {
		full := filepath.Join(path, name)
		if info, err := os.Stat(full); err == nil && info.Mode().IsRegular() {
			if err := addFile(digest, path, full); err != nil {
				return "", err
			}
		}
	}
	skip := func(name string) bool { return strings.HasPrefix(name, ".") || serverIgnoredDirs[name] }
	for _, sub := range []string{"cmd", "internal", "third_party"} {
		base := filepath.Join(path, sub)
		if info, err := os.Stat(base); err != nil || !info.IsDir() {
			continue
		}
		if err := walkInputs(digest, path, base, serverInput, skip); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

// PrecompressDir writes deterministic .gz siblings (level 9, no name, zero
// mtime) of static assets above 1 KiB, for the Go edge's gzip_static.
func PrecompressDir(dir string) error {
	return filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() || !hasAnySuffix(entry.Name(), precompressExts) {
			return nil
		}
		info, err := os.Stat(path)
		if err != nil || info.IsDir() || info.Size() <= 1024 {
			return nil
		}
		return gzipFile(path, path+".gz")
	})
}

func hasAnySuffix(name string, suffixes []string) bool {
	for _, suffix := range suffixes {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

func gzipFile(src, dst string) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("precompress: %w", err)
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("precompress: %w", err)
	}
	defer func() {
		if cerr := out.Close(); err == nil && cerr != nil {
			err = fmt.Errorf("precompress: %w", cerr)
		}
	}()
	zw, err := gzip.NewWriterLevel(out, gzip.BestCompression)
	if err != nil {
		return fmt.Errorf("precompress: %w", err)
	}
	if _, err := io.Copy(zw, in); err != nil {
		return fmt.Errorf("precompress %s: %w", src, err)
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("precompress %s: %w", src, err)
	}
	return nil
}
