package autoupdate

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// copyFile copies content, permission bits and modification time (copy2).
func copyFile(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("copy %s: %w", src, err)
	}
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("copy %s: %w", src, err)
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return fmt.Errorf("copy %s: %w", src, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return fmt.Errorf("copy %s: %w", src, err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("copy %s: %w", src, err)
	}
	if err := os.Chmod(dst, info.Mode().Perm()); err != nil {
		return fmt.Errorf("copy %s: %w", src, err)
	}
	if err := os.Chtimes(dst, info.ModTime(), info.ModTime()); err != nil {
		return fmt.Errorf("copy %s: %w", src, err)
	}
	return nil
}

// copyTree copies a directory, keeping symlinks as symlinks (copytree
// with symlinks=True); dst must not exist.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("copy tree: %w", err)
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return fmt.Errorf("copy tree: %w", err)
		}
		target := filepath.Join(dst, rel)
		switch {
		case entry.Type()&fs.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return fmt.Errorf("copy tree: %w", err)
			}
			if err := os.Symlink(link, target); err != nil {
				return fmt.Errorf("copy tree: %w", err)
			}
		case entry.IsDir():
			info, err := entry.Info()
			if err != nil {
				return fmt.Errorf("copy tree: %w", err)
			}
			if rel == "." {
				if _, err := os.Stat(target); err == nil {
					return fmt.Errorf("copy tree: %s: %w", target, fs.ErrExist)
				}
			}
			if err := os.MkdirAll(target, info.Mode().Perm()|0o700); err != nil {
				return fmt.Errorf("copy tree: %w", err)
			}
		default:
			if err := copyFile(path, target); err != nil {
				return err
			}
		}
		return nil
	})
}

// replaceDir replaces dst with a copy of src. Directories from image
// layers cannot be renamed on overlayfs (EXDEV), so delete and copy; the
// server is not running yet, and a failure is undone by restoring the
// preserved image copy.
func replaceDir(src, dst string) error {
	if info, err := os.Stat(dst); err == nil && info.IsDir() {
		if err := os.RemoveAll(dst); err != nil {
			return fmt.Errorf("replace %s: %w", dst, err)
		}
	}
	return copyTree(src, dst)
}

// replaceFile installs src at dst atomically with mode.
func replaceFile(src, dst string, mode os.FileMode) error {
	tmp := dst + ".tmp"
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("replace %s: %w", dst, err)
	}
	if err := copyFile(src, tmp); err != nil {
		return err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		return fmt.Errorf("replace %s: %w", dst, err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		return fmt.Errorf("replace %s: %w", dst, err)
	}
	return nil
}

var errCrossDevice = syscall.EXDEV

// moveDir renames src to dst, copying across file systems.
func moveDir(src, dst string) error {
	err := os.Rename(src, dst)
	if err == nil {
		return nil
	}
	if !errors.Is(err, errCrossDevice) {
		return fmt.Errorf("move %s: %w", src, err)
	}
	if err := copyTree(src, dst); err != nil {
		return err
	}
	return os.RemoveAll(src)
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// ErrUnsafeArchive identifies an archive member that would leave the
// destination or is not a plain file, directory or link.
var ErrUnsafeArchive = errors.New("unsafe archive member")

// extractTarGz unpacks a .tar.gz into dest with tarfile's "data" filter
// rules: members stay inside dest (links included), only files,
// directories and links are allowed, and setuid/setgid/sticky and
// group/other write bits are dropped.
func extractTarGz(archive, dest string) error {
	f, err := os.Open(archive)
	if err != nil {
		return fmt.Errorf("extract: %w", err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("extract: %w", err)
	}
	tr := tar.NewReader(zr)
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return fmt.Errorf("extract: %w", err)
	}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("extract: %w", err)
		}
		if hdr.Typeflag == tar.TypeXGlobalHeader || hdr.Typeflag == tar.TypeXHeader {
			continue
		}
		target, err := memberPath(dest, hdr.Name)
		if err != nil {
			return err
		}
		mode := os.FileMode(hdr.Mode).Perm()&0o755 | 0o600
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, mode|0o700); err != nil {
				return fmt.Errorf("extract: %w", err)
			}
		case tar.TypeReg:
			if err := writeMember(tr, target, mode); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if filepath.IsAbs(hdr.Linkname) {
				return fmt.Errorf("%w: %s", ErrUnsafeArchive, hdr.Name)
			}
			if _, err := memberPath(dest, filepath.Join(filepath.Dir(hdr.Name), hdr.Linkname)); err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("extract: %w", err)
			}
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				return fmt.Errorf("extract: %w", err)
			}
		case tar.TypeLink:
			source, err := memberPath(dest, hdr.Linkname)
			if err != nil {
				return err
			}
			if err := os.Link(source, target); err != nil {
				return fmt.Errorf("extract: %w", err)
			}
		default:
			return fmt.Errorf("%w: %s", ErrUnsafeArchive, hdr.Name)
		}
	}
}

func memberPath(dest, name string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(name))
	if filepath.IsAbs(name) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: %s", ErrUnsafeArchive, name)
	}
	return filepath.Join(dest, clean), nil
}

func writeMember(r io.Reader, target string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("extract: %w", err)
	}
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("extract: %w", err)
	}
	if _, err := io.Copy(out, r); err != nil {
		out.Close()
		return fmt.Errorf("extract: %w", err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("extract: %w", err)
	}
	return nil
}
