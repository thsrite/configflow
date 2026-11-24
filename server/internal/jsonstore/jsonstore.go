// Package jsonstore is the port of backend/utils/json_store.py: one JSON file
// per id under a directory, written atomically (temp file, then rename).
//
// Files that are missing or damaged read as nil, since they hold statistics
// that can be accumulated again and must not break the main flow. Concurrency
// is the caller's business (each user keeps its own lock), as in Python.
package jsonstore

import (
	"bytes"
	"configflow/internal/fsx"
	"context"
	"crypto/sha256"

	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"configflow/internal/jsonresp"
	"configflow/internal/logx"
)

// Store keeps one `<id>.json` file per id in DataDir.
type Store struct {
	DataDir string

	// afterReplace runs between the rename and the verification of a signed
	// write; tests use it to play the part of an external writer.
	afterReplace func(path string)

	// Tests pause real I/O at publication boundaries without replacing it.
	afterTempWrite func()
	beforeCommit   func()
}

// New returns a store for dataDir (JsonFileStore.__init__).
func New(dataDir string) *Store { return &Store{DataDir: dataDir} }

// Signature identifies one version of a file: device, inode, size and the
// nanosecond mtime and ctime (st_dev, st_ino, st_size, st_mtime_ns,
// st_ctime_ns).
type Signature struct {
	Dev, Ino         uint64
	Size             int64
	MtimeNs, CtimeNs int64
}

// Path is _path: the id with "/" and "\" replaced by "_", plus ".json".
func (s *Store) Path(id string) string {
	safe := strings.NewReplacer("/", "_", "\\", "_").Replace(id)
	return filepath.Join(s.DataDir, safe+".json")
}

// Read is _read_json: the object stored for id, or nil when the file is
// missing, unreadable, not valid JSON or not an object.
func (s *Store) Read(id string, dst any) bool {
	data, err := os.ReadFile(s.Path(id))
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			logx.Get("backend.utils.json_store").Warningf("%s: %s", msgReadFailed, readErrorCategory(err))
		}
		return false
	}
	data = bytes.TrimSpace(data)
	if len(data) == 0 || data[0] != '{' {
		return false
	}
	return jsonresp.DecodeStored(data, dst) == nil
}

func readErrorCategory(err error) string {
	switch {
	case errors.Is(err, os.ErrPermission):
		return "permission"
	default:
		return "io"
	}
}

// Write is _write_json: json.dump(data, ensure_ascii=False,
// separators=(',', ':')) to `<path>.tmp`, then rename over the file. Bytes
// use native JSON, with no trailing newline.
//
// With verifySignature the descriptor stays open across the rename and the
// bytes are compared before and after it, so that a signature is only
// returned for a file nobody touched in between (rename changes ctime, so
// metadata alone cannot show an in-place edit that restored size and mtime).
// nil means "written, but no trustworthy signature". Without it the file is
// closed before the rename and the result is always nil.
//
// Write writes atomically, checking ctx at entry and immediately before
// rename. Cancellation observed by that final check leaves the existing file
// unchanged. Cancellation after the check cannot revoke an admitted rename;
// verification after a successful rename completes even if ctx is then cancelled.
// Callers supply an owner context detached from request cancellation when the
// write must outlive a disconnect.
func (s *Store) Write(ctx context.Context, id string, data any, verifySignature bool) (*Signature, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(s.DataDir, 0o777); err != nil {
		return nil, err
	}
	encoded, err := jsonresp.Encode(data)
	if err != nil {
		return nil, err
	}
	path := s.Path(id)
	temp := path + ".tmp"
	flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if verifySignature {
		flags = os.O_RDWR | os.O_CREATE | os.O_TRUNC
	}
	handle, err := os.OpenFile(temp, flags, 0o666)
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		handle.Close()
		if !committed {
			_ = os.Remove(temp)
		}
	}()
	if _, err := handle.Write(encoded); err != nil {
		return nil, err
	}
	// The temporary file is replaced by rename (not fsx.WriteFile) because
	// the signature check below reads the still open handle afterwards.
	if err := handle.Sync(); err != nil {
		return nil, err
	}
	if s.afterTempWrite != nil {
		s.afterTempWrite()
	}
	if !verifySignature {
		if err := handle.Close(); err != nil {
			return nil, err
		}
		if s.beforeCommit != nil {
			s.beforeCommit()
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := os.Rename(temp, path); err != nil {
			return nil, err
		}
		committed = true
		return nil, fsx.SyncDir(s.DataDir)
	}
	before, err := signature(handle)
	if err != nil {
		return nil, err
	}
	expected, err := fileDigest(handle)
	if err != nil {
		return nil, err
	}
	again, err := signature(handle)
	if err != nil {
		return nil, err
	}
	stableBefore := before == again
	if s.beforeCommit != nil {
		s.beforeCommit()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := os.Rename(temp, path); err != nil {
		return nil, err
	}
	committed = true
	if err := fsx.SyncDir(s.DataDir); err != nil {
		return nil, err
	}
	if s.afterReplace != nil {
		s.afterReplace(path)
	}
	written, err := signature(handle)
	if err != nil {
		return nil, err
	}
	actual, err := fileDigest(handle)
	if err != nil {
		return nil, err
	}
	verified, err := signature(handle)
	if err != nil {
		return nil, err
	}
	if !stableBefore || expected != actual || written != verified ||
		verified.Size != before.Size || verified.MtimeNs != before.MtimeNs {
		return nil, nil
	}
	return &verified, nil
}

// Remove checks ctx at entry and immediately before each unlink attempt.
// Cancellation after a check cannot revoke that attempt. A missing file is
// fine, and a directory in place of the file is an error.
func (s *Store) Remove(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path := s.Path(id)
	for {
		if s.beforeCommit != nil {
			s.beforeCommit()
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		err := syscall.Unlink(path)
		switch {
		case err == nil:
			return nil
		case err == syscall.EINTR: // PEP 475: retried
			continue
		case errors.Is(err, os.ErrNotExist):
			return nil
		}
		return &fs.PathError{Op: "remove", Path: path, Err: err}
	}
}

func signature(f *os.File) (Signature, error) {
	info, err := f.Stat()
	if err != nil {
		return Signature{}, err
	}
	return signatureOf(info), nil
}

// fileDigest hashes the descriptor's raw bytes from the start without keeping
// the body (_file_digest).
func fileDigest(f *os.File) ([sha256.Size]byte, error) {
	var sum [sha256.Size]byte
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return sum, err
	}
	digest := sha256.New()
	if _, err := io.Copy(digest, f); err != nil {
		return sum, err
	}
	copy(sum[:], digest.Sum(nil))
	return sum, nil
}
