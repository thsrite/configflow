package trafficstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	"configflow/internal/fsx"
	"configflow/internal/jsonstore"
)

// Only a successful full merge establishes that this day's counts, retention
// and host limit are current. Keep the digest, never the history, in the LRU.
type idleHistory struct {
	day       string
	hostLimit int
	offset    int64
	digest    [sha256.Size]byte
}

func newIdleHistory(document, status json.RawMessage, day string) *idleHistory {
	// A full merge has just serialized exactly one top-level status. A second
	// identical member in an extension would make a byte search ambiguous.
	const key = `"status":`
	field := append([]byte(key), status...)
	start := bytes.Index(document, field)
	if start < 0 || bytes.Contains(document[start+len(field):], field) {
		return nil
	}
	offset := start + len(key)
	h := sha256.New()
	h.Write(document[:offset])
	h.Write(document[offset+len(status):])
	history := &idleHistory{day: day, hostLimit: MaxHostsPerDay, offset: int64(offset)}
	h.Sum(history.digest[:0])
	return history
}

// A nil result asks Add to run its full merge, including damage checks.
func refreshIdleReport(ctx context.Context, path string, sig *jsonstore.Signature, r *Report, day, stamp string) (*Status, error) {
	e := statusIndex[path]
	if e == nil || sig == nil {
		return nil, nil
	}
	entry := e.Value.(*cacheEntry)
	history := entry.idle
	if entry.signature != *sig || history == nil || history.day != day || history.hostLimit != MaxHostsPerDay {
		return nil, nil
	}
	var status Status
	if json.Unmarshal(entry.status, &status) != nil || status.Value != r.Status {
		return nil, nil
	}
	version := r.MihomoVersion
	if len(version) != 0 && string(version) != `null` && string(version) != `""` && !bytes.Equal(version, status.MihomoVersion) {
		return nil, nil
	}
	if status.At == stamp {
		statusCache.MoveToBack(e)
		return &status, nil
	}
	status.At = stamp
	raw, err := json.Marshal(status)
	if err != nil {
		return nil, err
	}
	if len(raw) != len(entry.status) {
		return nil, nil
	}
	written, committed, err := replaceIdleStatus(ctx, path, *sig, raw, history)
	if err != nil || !committed {
		cachePop(path)
		return nil, err
	}
	if entry := rememberStatus(path, written, raw); entry != nil {
		entry.idle = history
	}
	return &status, nil
}

// Clone the known file to a private sibling, patch its fixed-size status and
// publish it with the same fsync/rename contract as a full report. On a
// filesystem without reflinks, copyFile streams the bytes with bounded memory.
func replaceIdleStatus(ctx context.Context, path string, expected jsonstore.Signature, status []byte, history *idleHistory) (*jsonstore.Signature, bool, error) {
	src, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	defer src.Close()
	info, err := src.Stat()
	if err != nil || signatureOf(info) != expected {
		return nil, false, err
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".traffic-*.tmp")
	if err != nil {
		return nil, false, err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	if err := copyFile(tmp, src); err != nil {
		return nil, false, err
	}
	if _, err := tmp.WriteAt(status, history.offset); err != nil {
		return nil, false, err
	}
	if err := tmp.Chmod(info.Mode().Perm()); err != nil {
		return nil, false, err
	}
	if err := tmp.Sync(); err != nil {
		return nil, false, err
	}
	if current := statusSignature(path); current == nil || *current != expected {
		return nil, false, nil // The full merge will open the replacement.
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return nil, false, err
	}
	if err := fsx.SyncDir(dir); err != nil {
		return nil, true, err
	}
	// Renaming changes ctime. Verify the published descriptor against the
	// known content before granting another fast merge, even if an external
	// writer restored the size and mtime. An inode replacement is separately
	// checked by rememberStatus against the path.
	sig, err := verifyIdleFile(tmp, status, history)
	return sig, true, err
}

func verifyIdleFile(f *os.File, status []byte, history *idleHistory) (*jsonstore.Signature, error) {
	before, err := f.Stat()
	if err != nil {
		return nil, err
	}
	current := make([]byte, len(status))
	if _, err := f.ReadAt(current, history.offset); err == io.EOF {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	if !bytes.Equal(current, status) {
		return nil, nil
	}
	end := history.offset + int64(len(status))
	h := sha256.New()
	unchanged := io.MultiReader(io.NewSectionReader(f, 0, history.offset), io.NewSectionReader(f, end, before.Size()-end))
	if _, err := io.Copy(h, unchanged); err != nil {
		return nil, err
	}
	after, err := f.Stat()
	if err != nil {
		return nil, err
	}
	sig := signatureOf(after)
	if sig != signatureOf(before) || !bytes.Equal(h.Sum(nil), history.digest[:]) {
		return nil, nil
	}
	return &sig, nil
}
