package repo

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"configflow/internal/ctxx"
	"configflow/internal/jsonx"
	"configflow/internal/pystr"
)

var profileIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

// ValidateProfileID is ProfileRepository.validate_profile_id.
func ValidateProfileID(profileID string) error {
	// Python's fullmatch: "$" in Go's RE2 does not match before a final newline.
	if !profileIDPattern.MatchString(profileID) {
		return validationError("Invalid profile id")
	}
	return nil
}

// pyOSError is str(OSError) for a failed file operation on path.
func pyOSError(err error, path string) string {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		text := errno.Error()
		if text != "" {
			text = strings.ToUpper(text[:1]) + text[1:]
		}
		return fmt.Sprintf("[Errno %d] %s: %s", int(errno), text, jsonx.Repr(path))
	}
	return err.Error()
}

// readJSON is ProfileRepository._read_json: the JSON object in a file.
func readJSON(path string) (*jsonx.Object, error) {
	name := filepath.Base(path)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, repositoryError("Cannot read configuration " + name + ": " + pyOSError(err, path))
	}
	if message := pystr.UTF8DecodeError(data); message != "" {
		return nil, repositoryError("Cannot read configuration " + name + ": " + message)
	}
	// Text mode with universal newlines: "\r\n" and "\r" are read as "\n"
	// (only error positions can tell).
	text := strings.ReplaceAll(strings.ReplaceAll(string(data), "\r\n", "\n"), "\r", "\n")
	value, err := jsonx.Parse([]byte(text))
	if err != nil {
		return nil, repositoryError("Cannot read configuration " + name + ": " + err.Error())
	}
	object, ok := value.(*jsonx.Object)
	if !ok {
		return nil, validationError("JSON object expected in " + name)
	}
	return object, nil
}

func fsyncDir(directory string) error {
	handle, err := os.Open(directory)
	if err != nil {
		return err
	}
	defer handle.Close()
	return handle.Sync()
}

// writeTemp is _write_temp: a complete, fsynced sibling of path (mode 0600).
func writeTemp(path string, content []byte) (string, error) {
	handle, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return "", err
	}
	temporary := handle.Name()
	fail := func(err error) (string, error) {
		handle.Close()
		os.Remove(temporary)
		return "", err
	}
	if _, err := handle.Write(content); err != nil {
		return fail(err)
	}
	if err := handle.Sync(); err != nil {
		return fail(err)
	}
	if err := handle.Close(); err != nil {
		os.Remove(temporary)
		return "", err
	}
	info, err := os.Stat(temporary)
	if err != nil {
		os.Remove(temporary)
		return "", err
	}
	if info.Size() != int64(len(content)) {
		os.Remove(temporary)
		return "", repositoryError(fmt.Sprintf("Incomplete write for %s: %d of %d bytes", path, info.Size(), len(content)))
	}
	return temporary, nil
}

// writeAtomic is _write_atomic: replace path through a temporary file, then
// refresh "<path>.bak" with the previous content, also through a temporary
// file; a failed backup refresh keeps the old backup and is not an error.
func writeAtomic(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o777); err != nil {
		return err
	}
	var previous []byte
	hasPrevious := false
	if _, err := os.Stat(path); err == nil {
		if data, err := os.ReadFile(path); err == nil {
			previous, hasPrevious = data, true
		}
	}
	temporary, err := writeTemp(path, content)
	if err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		os.Remove(temporary)
		return err
	}
	if err := fsyncDir(filepath.Dir(path)); err != nil {
		return err
	}
	if !hasPrevious {
		return nil
	}
	backup := path + ".bak"
	backupTemporary, err := writeTemp(backup, previous)
	if err != nil {
		return nil
	}
	if err := os.Rename(backupTemporary, backup); err != nil {
		os.Remove(backupTemporary)
		return nil
	}
	fsyncDir(filepath.Dir(backup))
	return nil
}

// fileJSON is json.dumps(data, ensure_ascii=False, indent=2) + "\n".
func fileJSON(data any) ([]byte, error) {
	text, err := jsonx.Indent2(data)
	if err != nil {
		return nil, err
	}
	return append(text, '\n'), nil
}

func writeJSON(path string, data any) error {
	text, err := fileJSON(data)
	if err != nil {
		return err
	}
	return writeAtomic(path, text)
}

// SharedPath is shared_path: a path below DATA_DIR/shared; a relative path
// that leaves it (also through symbolic links) is refused.
func (r *Repository) SharedPath(relativePath string) (string, error) {
	if relativePath == "" || strings.ContainsRune(relativePath, 0) {
		return "", validationError("Invalid shared relative path")
	}
	root, err := resolve(filepath.Join(r.dataDir, "shared"))
	if err != nil {
		return "", err
	}
	candidate, err := resolve(joinPath(root, relativePath))
	if err != nil {
		return "", err
	}
	if !isRelativeTo(root, r.dataDir) || !isRelativeTo(candidate, root) {
		return "", validationError("Shared path escapes shared directory")
	}
	return candidate, nil
}

// joinPath is pathlib's "/" operator: an absolute right side replaces the left.
func joinPath(root, relative string) string {
	if filepath.IsAbs(relative) {
		return relative
	}
	return root + "/" + relative
}

// CacheDir is cache_dir(): DATA_DIR/shared/subscribes. A non-empty profile id
// is only validated.
func (r *Repository) CacheDir(profileID string) (string, error) {
	if profileID != "" {
		if err := ValidateProfileID(profileID); err != nil {
			return "", err
		}
	}
	return r.SharedPath("subscribes")
}

// SharedRulesDir is shared_rules_dir(): DATA_DIR/shared/rules.
func (r *Repository) SharedRulesDir() (string, error) { return r.SharedPath("rules") }

// SharedCacheDir is shared_cache_dir().
func (r *Repository) SharedCacheDir() (string, error) { return r.CacheDir("") }

// ReadSharedJSON reads shared JSON under its bounded in-process leaf lock.
func (r *Repository) ReadSharedJSON(ctx context.Context, relativePath string) (result *jsonx.Object, err error) {
	path, err := r.SharedPath(relativePath)
	if err != nil {
		return nil, err
	}
	err = withFile(ctx, path, func() error {
		result, err = readJSON(path)
		return err
	})
	return result, err
}

// WriteSharedJSON is write_shared_json; it returns the file's path.
func (r *Repository) WriteSharedJSON(ctx context.Context, relativePath string, data any) (string, error) {
	ctx = ctxx.Detach(ctx)
	path, err := r.SharedPath(relativePath)
	if err != nil {
		return "", err
	}
	return path, withFile(ctx, path, func() error { return writeJSON(path, data) })
}

// WriteSharedText is write_shared_text; it returns the file's path.
func (r *Repository) WriteSharedText(ctx context.Context, relativePath string, content []byte) (string, error) {
	ctx = ctxx.Detach(ctx)
	path, err := r.SharedPath(relativePath)
	if err != nil {
		return "", err
	}
	return path, withFile(ctx, path, func() error { return writeAtomic(path, content) })
}

// ProfileDir is profile_dir: DATA_DIR/profiles/<id>.
func (r *Repository) ProfileDir(profileID string) (string, error) {
	if err := ValidateProfileID(profileID); err != nil {
		return "", err
	}
	root, err := resolve(r.profilesDir)
	if err != nil {
		return "", err
	}
	candidate, err := resolve(root + "/" + profileID)
	if err != nil {
		return "", err
	}
	if !isRelativeTo(root, r.dataDir) || !isRelativeTo(candidate, root) {
		return "", validationError("Profile path escapes profile directory")
	}
	return candidate, nil
}

// ProfilePath is profile_path: a path below the profile's directory.
func (r *Repository) ProfilePath(profileID, relativePath string) (string, error) {
	if relativePath == "" || strings.ContainsRune(relativePath, 0) {
		return "", validationError("Invalid profile relative path")
	}
	root, err := r.ProfileDir(profileID)
	if err != nil {
		return "", err
	}
	candidate, err := resolve(joinPath(root, relativePath))
	if err != nil {
		return "", err
	}
	if !isRelativeTo(candidate, root) {
		return "", validationError("Profile path escapes profile directory")
	}
	return candidate, nil
}

func (r *Repository) derivedDir(profileID, name string) (string, error) {
	directory, err := r.ProfileDir(profileID)
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, name), nil
}

// ProvidersDir is providers_dir.
func (r *Repository) ProvidersDir(profileID string) (string, error) {
	return r.derivedDir(profileID, "providers")
}

// RulesDir is rules_dir.
func (r *Repository) RulesDir(profileID string) (string, error) {
	return r.derivedDir(profileID, "rules")
}

// GeneratedDir is generated_dir.
func (r *Repository) GeneratedDir(profileID string) (string, error) {
	return r.derivedDir(profileID, "generated")
}

func (r *Repository) profileOperationLockPath(profileID string) (string, error) {
	if err := ValidateProfileID(profileID); err != nil {
		return "", err
	}
	return filepath.Join(r.profilesDir, "."+profileID+".operation.lock"), nil
}

// writeProfileFile is the common body of write_generated, write_profile_json
// and write_profile_text: under the profile's operation lock, check that the
// profile exists, then write under its bounded in-process file leaf lock.
func (r *Repository) writeProfileFile(ctx context.Context, profileID, path string, write func() error) error {
	ctx = ctxx.Detach(ctx)
	return r.withProfileOperation(ctx, profileID, func(held context.Context) error {
		if _, err := r.ProfileMetadata(held, profileID); err != nil {
			return err
		}
		return withFile(held, path, write)
	})
}

// WriteGenerated is write_generated: config.yaml, config.conf or loon.lcf in
// the profile's generated directory.
func (r *Repository) WriteGenerated(ctx context.Context, profileID, filename string, content []byte) (string, error) {
	ctx = ctxx.Detach(ctx)
	if filename != "config.yaml" && filename != "config.conf" && filename != "loon.lcf" {
		return "", validationError("Invalid generated filename")
	}
	directory, err := r.GeneratedDir(profileID)
	if err != nil {
		return "", err
	}
	path := filepath.Join(directory, filename)
	return path, r.writeProfileFile(ctx, profileID, path, func() error { return writeAtomic(path, content) })
}

// WriteProfileJSON is write_profile_json.
func (r *Repository) WriteProfileJSON(ctx context.Context, profileID, relativePath string, data any) (string, error) {
	ctx = ctxx.Detach(ctx)
	path, err := r.ProfilePath(profileID, relativePath)
	if err != nil {
		return "", err
	}
	return path, r.writeProfileFile(ctx, profileID, path, func() error { return writeJSON(path, data) })
}

// ReadProfileJSON is read_profile_json (no lock).
func (r *Repository) ReadProfileJSON(profileID, relativePath string) (*jsonx.Object, error) {
	path, err := r.ProfilePath(profileID, relativePath)
	if err != nil {
		return nil, err
	}
	return readJSON(path)
}

// WriteProfileText is write_profile_text.
func (r *Repository) WriteProfileText(ctx context.Context, profileID, relativePath string, content []byte) (string, error) {
	ctx = ctxx.Detach(ctx)
	path, err := r.ProfilePath(profileID, relativePath)
	if err != nil {
		return "", err
	}
	return path, r.writeProfileFile(ctx, profileID, path, func() error { return writeAtomic(path, content) })
}
