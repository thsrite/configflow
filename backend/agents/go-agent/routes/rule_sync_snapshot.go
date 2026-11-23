package routes

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

const (
	ruleSnapshotFileLimit     int64 = 64 << 20
	ruleSnapshotTotalLimit    int64 = 256 << 20
	ruleSnapshotManifestLimit int64 = 4 << 20
	ruleSnapshotFileCount           = 4096
)

var (
	errRuleSnapshotInvalid     = errors.New("invalid snapshot")
	errRuleSnapshotChanged     = errors.New("snapshot changed")
	errRuleSnapshotTooLarge    = errors.New("snapshot too large")
	errRuleSnapshotUnavailable = errors.New("snapshot unavailable")
)

func ruleSnapshotFailure(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Cache-Control", "no-store")
	JsonResponse(w, status, map[string]any{"success": false, "code": code, "message": message})
}

// RuleSyncSnapshotHandler exports only already-managed files. Callers cannot
// choose a directory, filename or deployment. The snapshot pins provider bytes
// refreshed by the core so a later rule-only deployment does not restore stale
// files from the server's original publication archive.
func RuleSyncSnapshotHandler(cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodGet {
			ruleSnapshotFailure(w, http.StatusMethodNotAllowed, "INVALID_REQUEST", "Only GET is supported")
			return
		}
		if r.URL.EscapedPath() != "/api/rule-sync/snapshot" || r.URL.RawQuery != "" || r.URL.ForceQuery {
			ruleSnapshotFailure(w, http.StatusBadRequest, "INVALID_REQUEST", "Snapshot requests do not accept parameters")
			return
		}
		if r.Body != nil {
			body, err := io.ReadAll(io.LimitReader(r.Body, 1))
			if err != nil || len(body) != 0 {
				ruleSnapshotFailure(w, http.StatusBadRequest, "INVALID_REQUEST", "Snapshot requests do not accept a body")
				return
			}
		}
		if cfg.ServiceType != "mihomo" && cfg.ServiceType != "oxidns" {
			ruleSnapshotFailure(w, http.StatusBadRequest, "WRONG_SERVICE", "Rule snapshots require Mihomo or OxiDNS")
			return
		}
		release, err := AcquireServiceOperation(cfg)
		if err != nil {
			ruleSnapshotFailure(w, http.StatusConflict, "SERVICE_BUSY", "Another service operation is in progress")
			return
		}
		if err = deploymentBlocked(cfg); err != nil {
			release()
			ruleSnapshotFailure(w, http.StatusConflict, "RECOVERY_REQUIRED", "Deployment recovery is required before taking a snapshot")
			return
		}
		archive, digest, err := buildRuleSyncSnapshot(cfg, r)
		// The completed archive is immutable. Do not hold the service operation
		// lock while a slow client receives it.
		release()
		if err != nil {
			switch {
			case errors.Is(err, errRuleSnapshotUnavailable):
				ruleSnapshotFailure(w, http.StatusConflict, "SNAPSHOT_UNAVAILABLE", "No active managed deployment is available")
			case errors.Is(err, errRuleSnapshotChanged):
				ruleSnapshotFailure(w, http.StatusConflict, "SNAPSHOT_CHANGED", "Managed files changed while taking the snapshot")
			case errors.Is(err, errRuleSnapshotTooLarge):
				ruleSnapshotFailure(w, http.StatusRequestEntityTooLarge, "SNAPSHOT_TOO_LARGE", "Managed snapshot exceeds the supported size limits")
			case errors.Is(err, errRuleSnapshotInvalid):
				ruleSnapshotFailure(w, http.StatusConflict, "SNAPSHOT_INVALID", "The active manifest or managed files cannot be safely read")
			default:
				ruleSnapshotFailure(w, http.StatusInternalServerError, "SNAPSHOT_FAILED", "The managed snapshot could not be created")
			}
			return
		}
		defer os.Remove(archive.Name())
		defer archive.Close()
		info, err := archive.Stat()
		if err != nil {
			ruleSnapshotFailure(w, http.StatusInternalServerError, "SNAPSHOT_FAILED", "The managed snapshot could not be read")
			return
		}
		w.Header().Set("Content-Type", "application/gzip")
		w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
		w.Header().Set("X-Content-SHA256", digest)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(http.StatusOK)
		_, _ = io.Copy(w, archive)
	}
}

func safeSnapshotPath(name string) bool {
	return safeRelative(name) && strings.IndexFunc(name, func(r rune) bool { return r < 0x20 || r == 0x7f }) < 0
}

// Open every component relative to a trusted directory descriptor. Lstat alone
// would leave a symlink-swap window between validation and opening the file.
func openRuleSnapshotFile(root, rel string) (*os.File, error) {
	if !safeSnapshotPath(rel) || rejectSymlinkPath(root, rel) != nil {
		return nil, errRuleSnapshotInvalid
	}
	dir, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(rel, "/")
	for _, part := range parts[:len(parts)-1] {
		next, err := unix.Openat(dir, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		unix.Close(dir)
		if err != nil {
			return nil, errRuleSnapshotInvalid
		}
		dir = next
	}
	fd, err := unix.Openat(dir, parts[len(parts)-1], unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	unix.Close(dir)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), rel)
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		return nil, errRuleSnapshotInvalid
	}
	return file, nil
}

func readRuleSnapshotFile(root, rel string, limit int64) ([]byte, error) {
	file, err := openRuleSnapshotFile(root, rel)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil {
		return nil, errRuleSnapshotInvalid
	}
	if before.Size() > limit {
		return nil, errRuleSnapshotTooLarge
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errRuleSnapshotTooLarge
	}
	after, err := file.Stat()
	if err != nil || before.Size() != after.Size() || before.ModTime() != after.ModTime() || int64(len(data)) != after.Size() {
		return nil, errRuleSnapshotChanged
	}
	return data, nil
}

func validateRuleSnapshotManifest(cfg *Config, manifest *DeploymentManifest) error {
	profile, validProfile := manifest.ProfileID.(string)
	if manifest.ProtocolVersion != 1 || !deploymentIDPattern.MatchString(manifest.DeploymentID) ||
		manifest.AgentID == "" || manifest.AgentID != cfg.AgentID || !validProfile || profile == "" ||
		manifest.ServiceType != cfg.ServiceType || manifest.ConfigPath != filepath.Base(cfg.ConfigPath) ||
		!safeSnapshotPath(manifest.ConfigPath) || len(manifest.Files) == 0 {
		return errRuleSnapshotInvalid
	}
	if len(manifest.Files) > ruleSnapshotFileCount {
		return errRuleSnapshotTooLarge
	}
	seen, configCount := map[string]bool{}, 0
	for _, entry := range manifest.Files {
		if !safeSnapshotPath(entry.Path) || seen[entry.Path] || entry.Size < 0 || !validDigest(entry.SHA256) {
			return errRuleSnapshotInvalid
		}
		seen[entry.Path] = true
		switch entry.Role {
		case "config", "provider", "rule", "custom", "resource":
		default:
			return errRuleSnapshotInvalid
		}
		if entry.Path == manifest.ConfigPath {
			configCount++
			if entry.Role != "config" || entry.Size == 0 {
				return errRuleSnapshotInvalid
			}
		} else if entry.Role == "config" {
			return errRuleSnapshotInvalid
		}
	}
	if configCount != 1 {
		return errRuleSnapshotInvalid
	}
	return nil
}

func preflightRuleSnapshotFiles(cfg *Config, manifest *DeploymentManifest) error {
	var total int64
	for _, entry := range manifest.Files {
		file, err := openRuleSnapshotFile(filepath.Dir(cfg.ConfigPath), entry.Path)
		if err != nil {
			return errRuleSnapshotInvalid
		}
		info, err := file.Stat()
		file.Close()
		if err != nil {
			return errRuleSnapshotInvalid
		}
		if entry.Role == "resource" {
			continue
		}
		if info.Size() > ruleSnapshotFileLimit {
			return errRuleSnapshotTooLarge
		}
		total += info.Size()
		if total > ruleSnapshotTotalLimit {
			return errRuleSnapshotTooLarge
		}
	}
	return nil
}

func buildRuleSyncSnapshot(cfg *Config, request *http.Request) (_ *os.File, _ string, resultErr error) {
	manifestBytes, err := readRuleSnapshotFile(deploymentRoot(cfg), "active-manifest.json", ruleSnapshotManifestLimit)
	if os.IsNotExist(err) {
		return nil, "", errRuleSnapshotUnavailable
	}
	if err != nil {
		return nil, "", err
	}
	var manifest DeploymentManifest
	if json.Unmarshal(manifestBytes, &manifest) != nil {
		return nil, "", errRuleSnapshotInvalid
	}
	if err = validateRuleSnapshotManifest(cfg, &manifest); err != nil {
		return nil, "", err
	}
	if err = preflightRuleSnapshotFiles(cfg, &manifest); err != nil {
		return nil, "", err
	}
	archive, err := os.CreateTemp(deploymentRoot(cfg), "snapshot-*.tar.gz")
	if err != nil {
		return nil, "", err
	}
	defer func() {
		if resultErr != nil {
			archive.Close()
			os.Remove(archive.Name())
		}
	}()
	hash := sha256.New()
	compressed := gzip.NewWriter(io.MultiWriter(archive, hash))
	tarWriter := tar.NewWriter(compressed)
	defer compressed.Close()
	defer tarWriter.Close()
	write := func(name string, data []byte) error {
		if err := tarWriter.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
			return err
		}
		_, err := tarWriter.Write(data)
		return err
	}
	root, total := filepath.Dir(cfg.ConfigPath), int64(0)
	files := make([]DeploymentFile, 0, len(manifest.Files))
	for _, entry := range manifest.Files {
		if err := request.Context().Err(); err != nil {
			return nil, "", err
		}
		if entry.Role == "resource" {
			// Agent-carried Geo databases are preserved by deployment activation,
			// not part of the server publication baseline. Still validate paths
			// and regular-file identity before excluding them from this export.
			file, err := openRuleSnapshotFile(root, entry.Path)
			if err != nil {
				return nil, "", errRuleSnapshotInvalid
			}
			file.Close()
			continue
		}
		data, err := readRuleSnapshotFile(root, entry.Path, ruleSnapshotFileLimit)
		if err != nil {
			if errors.Is(err, errRuleSnapshotTooLarge) || errors.Is(err, errRuleSnapshotChanged) {
				return nil, "", err
			}
			return nil, "", errRuleSnapshotInvalid
		}
		digest := sha256Hex(data)
		if entry.Path == manifest.ConfigPath && (digest != entry.SHA256 || int64(len(data)) != entry.Size) {
			return nil, "", errRuleSnapshotChanged
		}
		total += int64(len(data))
		if total > ruleSnapshotTotalLimit {
			return nil, "", errRuleSnapshotTooLarge
		}
		entry.Size, entry.SHA256 = int64(len(data)), digest
		files = append(files, entry)
		if err = write("files/"+entry.Path, data); err != nil {
			return nil, "", err
		}
	}
	// Re-read against the captured digests before releasing the operation lock.
	// This also detects native periodic downloads that do not use the Agent lock.
	for _, entry := range files {
		if err := request.Context().Err(); err != nil {
			return nil, "", err
		}
		data, err := readRuleSnapshotFile(root, entry.Path, ruleSnapshotFileLimit)
		if err != nil || sha256Hex(data) != entry.SHA256 || int64(len(data)) != entry.Size {
			return nil, "", errRuleSnapshotChanged
		}
	}
	latestManifest, err := readRuleSnapshotFile(deploymentRoot(cfg), "active-manifest.json", ruleSnapshotManifestLimit)
	if err != nil || !bytes.Equal(latestManifest, manifestBytes) {
		return nil, "", errRuleSnapshotChanged
	}
	manifest.Files = files
	manifestBytes, err = json.Marshal(manifest)
	if err != nil {
		return nil, "", err
	}
	if err = write("manifest.json", manifestBytes); err != nil {
		return nil, "", err
	}
	if err = tarWriter.Close(); err != nil {
		return nil, "", err
	}
	if err = compressed.Close(); err != nil {
		return nil, "", err
	}
	if _, err = archive.Seek(0, io.SeekStart); err != nil {
		return nil, "", err
	}
	return archive, hex.EncodeToString(hash.Sum(nil)), nil
}
