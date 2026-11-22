package dockerha

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"unicode/utf8"
)

// Failure is a refusal shown to the user verbatim; the command exits 1.
type Failure struct{ Message string }

func (f *Failure) Error() string { return f.Message }

func fail(message string) error { return &Failure{Message: message} }

// errNotJSON / errNotObject distinguish a damaged file from valid JSON that
// is not an object.
var (
	errNotJSON   = errors.New("invalid JSON")
	errNotObject = errors.New("JSON value is not an object")
)

// decodeObject parses one JSON object. Numbers keep their literal text so
// values are compared and printed exactly as stored.
func decodeObject(raw []byte) (map[string]any, error) {
	raw = bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))
	if !utf8.Valid(raw) {
		return nil, errNotJSON
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, errNotJSON
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, errNotJSON
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, errNotObject
	}
	return object, nil
}

// readRegular reads a small regular file without following a final symlink.
// O_NONBLOCK makes a named pipe fail the regular-file check instead of
// blocking the container start.
func readRegular(path string, limit int64) ([]byte, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, fail(msgFileUnreadable)
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, fail(msgFileUnreadable)
	}
	if info.IsDir() {
		// The former scripts could not open a directory as a file at all.
		return nil, fail(msgFileUnreadable)
	}
	if !info.Mode().IsRegular() {
		return nil, fail(msgFileNotRegular)
	}
	raw, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, fail(msgFileUnreadable)
	}
	if int64(len(raw)) > limit {
		return nil, fail(msgFileTooLarge)
	}
	return raw, nil
}

// readInstallJSON is a bundle file or identity read: regular, at most
// 64 KiB, one JSON object.
func readInstallJSON(path string) (map[string]any, error) {
	raw, err := readRegular(path, 65536)
	if err != nil {
		return nil, err
	}
	value, err := decodeObject(raw)
	switch {
	case errors.Is(err, errNotObject):
		return nil, fail(msgFileInvalid)
	case err != nil:
		return nil, fail(msgFileCorrupt)
	}
	return value, nil
}

// present reports whether path exists, including a dangling symlink.
func present(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// exists follows symlinks, like a plain existence check.
func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func isSymlink(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode()&os.ModeSymlink != 0
}

func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// field is one member of an ordered JSON object written to disk.
type field struct {
	key   string
	value any
}

// encodeFields renders an object with its members in the given order.
func encodeFields(fields []field) ([]byte, error) {
	var out bytes.Buffer
	out.WriteByte('{')
	for i, f := range fields {
		if i > 0 {
			out.WriteString(", ")
		}
		key, err := json.Marshal(f.key)
		if err != nil {
			return nil, err
		}
		value, err := json.Marshal(f.value)
		if err != nil {
			return nil, err
		}
		out.Write(key)
		out.WriteString(": ")
		out.Write(value)
	}
	out.WriteByte('}')
	return out.Bytes(), nil
}

// writeJSON publishes fields at path through a private temporary file in the
// same directory. Without replace the publication is exclusive (a hard link
// never overwrites an unexpected identity or symlink); with replace it is an
// atomic rename. The directory is synced afterwards.
func writeJSON(path, prefix string, fields []field, replace bool) error {
	data, err := encodeFields(fields)
	if err != nil {
		return fmt.Errorf("encode %s: %w", filepath.Base(path), err)
	}
	dir := filepath.Dir(path)
	temp, err := createTemp(dir, prefix)
	if err != nil {
		return err
	}
	name := temp.Name()
	defer os.Remove(name)
	if err := writeSynced(temp, data); err != nil {
		return err
	}
	if replace {
		err = os.Rename(name, path)
	} else {
		err = os.Link(name, path)
	}
	if err != nil {
		return fmt.Errorf("publish %s: %w", filepath.Base(path), err)
	}
	return syncDir(dir)
}

func writeSynced(file *os.File, data []byte) error {
	err := file.Chmod(0o600)
	if err == nil {
		_, err = file.Write(data)
	}
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(file.Name()), err)
	}
	return nil
}

func syncDir(dir string) error {
	handle, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("open directory: %w", err)
	}
	defer handle.Close()
	if err := handle.Sync(); err != nil {
		return fmt.Errorf("sync directory: %w", err)
	}
	return nil
}

// sameValue is equality of two decoded JSON values (nil for a missing key).
func sameValue(a, b any) bool {
	left, err1 := json.Marshal(a)
	right, err2 := json.Marshal(b)
	return err1 == nil && err2 == nil && bytes.Equal(left, right)
}

// tempAlphabet and the 8-character suffix are the temporary file names an
// installation recognises as its own interrupted writes.
const tempAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789_"

// ownerTempPrefix names temporary files of the installation owner.
func ownerTempPrefix(owner string) string { return ".compose-write-" + owner + "-" }

// createTemp exclusively creates prefix + 8 random [a-z0-9_] characters in
// dir with mode 0600.
func createTemp(dir, prefix string) (*os.File, error) {
	for attempt := 0; attempt < 100; attempt++ {
		var random [8]byte
		if _, err := rand.Read(random[:]); err != nil {
			return nil, fmt.Errorf("random name: %w", err)
		}
		name := []byte(prefix)
		for _, b := range random {
			name = append(name, tempAlphabet[int(b)%len(tempAlphabet)])
		}
		file, err := os.OpenFile(filepath.Join(dir, string(name)), os.O_RDWR|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("create temporary file: %w", err)
		}
		return file, nil
	}
	return nil, fmt.Errorf("create temporary file in %s: names exhausted", dir)
}
