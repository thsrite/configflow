package routes

// A deployment is a write-ahead transaction. Nothing in the live directory is
// changed before the backup is durable, and success is durable before cleanup.
import (
	"agent/upgrade"
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const deploymentDirectory = ".configflow-deployments"
const maxDeploymentSize int64 = 512 << 20
const maxExpandedSize int64 = 1 << 30
const maxDeploymentFiles = 10000

var deploymentIDPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,95}$`)
var errDeploymentBusy = errors.New("another service operation is in progress")

type DeploymentFile struct {
	Path   string `json:"path"`
	Name   string `json:"name,omitempty"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
	Role   string `json:"role"`
}
type DeploymentManifest struct {
	ProtocolVersion int         `json:"protocol_version"`
	DeploymentID    string      `json:"deployment_id"`
	AgentID         string      `json:"agent_id"`
	ProfileID       interface{} `json:"profile_id"`
	ServiceType     string      `json:"service_type"`
	ConfigPath      string      `json:"config_path"`
	FinalConfig     bool        `json:"final_config,omitempty"`
	// ReloadOnly refuses candidate activation when native hot reload is not
	// available. Rollback may still restart the previous service for recovery.
	ReloadOnly bool             `json:"reload_only,omitempty"`
	Files      []DeploymentFile `json:"files"`
}

func (m *DeploymentManifest) UnmarshalJSON(data []byte) error {
	// null must not silently disable the no-restart contract. Older manifests
	// omit the flag; a supplied flag must be an actual JSON boolean.
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if flag, exists := fields["reload_only"]; exists && string(flag) != "true" && string(flag) != "false" {
		return fmt.Errorf("reload_only must be a boolean")
	}
	if flag, exists := fields["final_config"]; exists && string(flag) != "true" && string(flag) != "false" {
		return fmt.Errorf("final_config must be a boolean")
	}
	type plainManifest DeploymentManifest
	return json.Unmarshal(data, (*plainManifest)(m))
}

type deploymentBackup struct {
	UID            int    `json:"uid"`
	GID            int    `json:"gid"`
	OwnershipKnown bool   `json:"ownership_known"`
	Path           string `json:"path"`
	Existed        bool   `json:"existed"`
	Mode           uint32 `json:"mode"`
	SHA256         string `json:"sha256,omitempty"`
}
type DeploymentState struct {
	Success              bool                 `json:"success"`
	DeploymentID         string               `json:"deployment_id"`
	Status               string               `json:"status"`
	ArchiveSHA256        string               `json:"archive_sha256"`
	ConfigVersion        string               `json:"config_version,omitempty"`
	Error                string               `json:"error,omitempty"`
	ErrorDetails         string               `json:"error_details,omitempty"`
	FailedStage          string               `json:"failed_stage,omitempty"`
	RollbackError        string               `json:"rollback_error,omitempty"`
	RollbackErrorDetails string               `json:"rollback_error_details,omitempty"`
	UpdatedAt            string               `json:"updated_at"`
	Manifest             *DeploymentManifest  `json:"-"`
	WasRunning           bool                 `json:"was_running"`
	Backups              []deploymentBackup   `json:"backups,omitempty"`
	BackupComplete       bool                 `json:"backup_complete"`
	PreviousManifest     json.RawMessage      `json:"previous_manifest,omitempty"`
	ManualRollback       bool                 `json:"manual_rollback,omitempty"`
	DNSChecks            []DeploymentDNSCheck `json:"dns_checks,omitempty"`
	DNSCheckedAt         string               `json:"dns_checked_at,omitempty"`
}

func deploymentRoot(cfg *Config) string {
	return filepath.Join(filepath.Dir(cfg.ConfigPath), deploymentDirectory)
}
func deploymentPath(cfg *Config, id string) string { return filepath.Join(deploymentRoot(cfg), id) }

// AcquireServiceOperation serializes deployment, restart, uninstall and upgrades,
// including different Agent processes pointed at the same configuration root.
func AcquireServiceOperation(cfg *Config) (func(), error) {
	return acquireServiceOperation(cfg, false)
}
func acquireServiceOperation(cfg *Config, updating bool) (func(), error) {
	if !updating && cfg.AgentConfigFile != "" {
		binary, err := os.Executable()
		if err != nil {
			return nil, err
		}
		if upgrade.Pending(binary) {
			return nil, fmt.Errorf("Agent update requires completion or recovery")
		}
	}
	root := deploymentRoot(cfg)
	if err := durableMkdirAll(root, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("invalid deployment directory")
	}
	fd, err := unix.Open(filepath.Join(root, "operation.lock"), unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return nil, err
	}
	if err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		unix.Close(fd)
		return nil, errDeploymentBusy
	}
	return func() { unix.Flock(fd, unix.LOCK_UN); unix.Close(fd) }, nil
}

// waitServiceOperation retries only lock contention. Brief holders such as the
// DNS watchdog tick on the same schedule as other periodic work and must not
// make it fail every round.
func waitServiceOperation(cfg *Config, updating bool, wait time.Duration) (func(), error) {
	release, err := acquireServiceOperation(cfg, updating)
	for deadline := time.Now().Add(wait); err == errDeploymentBusy && time.Now().Before(deadline); {
		time.Sleep(100 * time.Millisecond)
		release, err = acquireServiceOperation(cfg, updating)
	}
	return release, err
}
func durableWrite(name string, data []byte, mode os.FileMode) error {
	if err := durableMkdirAll(filepath.Dir(name), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(name), ".write-")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(tmp, name); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(name))
}

// syncDirectory is a variable so tests can count directory flushes.
var syncDirectory = func(name string) error {
	f, e := os.Open(name)
	if e != nil {
		return e
	}
	defer f.Close()
	return f.Sync()
}

// syncDirectories flushes each directory of a batch of renames once.
func syncDirectories(dirs map[string]bool) error {
	names := make([]string, 0, len(dirs))
	for name := range dirs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if e := syncDirectory(name); e != nil {
			return e
		}
	}
	return nil
}
func saveDeployment(cfg *Config, s *DeploymentState, status string) error {
	if cfg.Token != "" {
		for _, value := range []*string{&s.Error, &s.ErrorDetails, &s.RollbackError, &s.RollbackErrorDetails} {
			*value = strings.ReplaceAll(*value, cfg.Token, "[REDACTED]")
		}
	}
	if status == "succeeded" {
		s.Error, s.ErrorDetails, s.FailedStage = "", "", ""
		s.RollbackError, s.RollbackErrorDetails = "", ""
	}
	if status == "rolled_back" || status == "recovery_pending" {
		s.RollbackError, s.RollbackErrorDetails = "", ""
	}
	s.Status = status
	s.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	s.Success = (s.ManualRollback && status == "rolled_back") || (status != "failed" && status != "rolled_back" && status != "rollback_failed")
	b, e := json.Marshal(s)
	if e != nil {
		return e
	}
	return durableWrite(filepath.Join(deploymentPath(cfg, s.DeploymentID), "state.json"), b, 0600)
}
func loadDeployment(cfg *Config, id string) (*DeploymentState, error) {
	if !deploymentIDPattern.MatchString(id) {
		return nil, fmt.Errorf("invalid deployment ID")
	}
	b, e := os.ReadFile(filepath.Join(deploymentPath(cfg, id), "state.json"))
	if e != nil {
		return nil, e
	}
	var s DeploymentState
	if e = json.Unmarshal(b, &s); e != nil {
		return nil, e
	}
	if s.DeploymentID != id {
		return nil, fmt.Errorf("invalid deployment state")
	}
	return &s, nil
}
func deploymentBlocked(cfg *Config) error {
	entries, e := os.ReadDir(deploymentRoot(cfg))
	if e != nil {
		return e
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		s, e := loadDeployment(cfg, entry.Name())
		if e != nil {
			if os.IsNotExist(e) {
				removed, cleanupErr := removeUnstartedDeployment(cfg, entry.Name())
				if cleanupErr != nil {
					return cleanupErr
				}
				if removed {
					continue
				}
			}
			return fmt.Errorf("unreadable deployment state: %s", entry.Name())
		}
		switch s.Status {
		case "rollback_failed", "recovery_pending":
			return fmt.Errorf("deployment %s requires rollback recovery", s.DeploymentID)
		case "stopping", "backing_up", "replacing", "reloading", "starting", "checking", "rolling_back":
			return fmt.Errorf("deployment %s requires recovery", s.DeploymentID)
		}
	}
	return nil
}
func deploymentFailure(w http.ResponseWriter, code int, err error) {
	message, details := deploymentErrorFields(err)
	response := map[string]interface{}{"success": false, "message": message}
	if details != "" {
		response["error_details"] = details
	}
	JsonResponse(w, code, response)
}
func CapabilitiesHandler(cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(405)
			return
		}
		result := map[string]interface{}{"success": true, "deployment_protocols": []int{1}, "return_routing_safety_protocols": []int{1}, "service_type": cfg.ServiceType, "config_path": filepath.Base(cfg.ConfigPath)}
		result["reload_only_protocols"] = []int{1}
		result["final_config_protocols"] = []int{1}
		result["rule_sync_snapshot_protocols"] = []int{1}
		result["platform"] = gatewayOS
		if ipv6VerifySupported(cfg) {
			result["ipv6_client_probe_protocols"] = []int{1}
			if cfg.ServiceType == "mihomo" {
				result["ipv6_verification_protocols"] = []int{1}
			}
		}
		if cfg.ServiceType == "mihomo" {
			result["mihomo_resources"] = mihomoGeoAssetNames(filepath.Dir(cfg.ConfigPath))
			result["config_directory"] = filepath.Dir(cfg.ConfigPath)
			result["aio_forwarding_protocols"] = []int{1}
			result["ipv6_readiness_protocols"] = []int{1}
		}
		if cfg.ServiceType == "oxidns" {
			result["oxidns_ha_protocols"] = []int{oxidnsHAProtocol}
			result["oxidns_ha_handoff_protocols"] = []int{oxidnsHAHandoffProtocol}
			environment := haEnvironment(cfg)
			result["oxidns_ha_environment"] = environment
			if environment.Kind == "container" && environment.Ready {
				result["oxidns_ha_container_protocols"] = []int{oxidnsHAContainerProtocol}
			}
		}
		JsonResponse(w, 200, result)
	}
}
func DeploymentUploadHandler(cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(405)
			return
		}
		id := r.Header.Get("X-Deployment-ID")
		digest := strings.ToLower(r.Header.Get("X-Content-SHA256"))
		if !deploymentIDPattern.MatchString(id) || !validDigest(digest) {
			deploymentFailure(w, 400, fmt.Errorf("valid deployment ID and archive SHA-256 are required"))
			return
		}
		// Replays only inspect a durable snapshot; they need not wait for the
		// worker's operation lock and can safely report an in-flight upload.
		if prior, e := loadDeployment(cfg, id); e == nil {
			if prior.ArchiveSHA256 != digest {
				deploymentFailure(w, 409, fmt.Errorf("deployment ID already belongs to different content"))
				return
			}
			JsonResponse(w, 200, prior)
			return
		}
		release, e := AcquireServiceOperation(cfg)
		if e != nil {
			deploymentFailure(w, 409, e)
			return
		}
		handedOff := false
		defer func() {
			if !handedOff {
				release()
			}
		}()
		if s, e := loadDeployment(cfg, id); e == nil {
			if s.ArchiveSHA256 != digest {
				deploymentFailure(w, 409, fmt.Errorf("deployment ID already belongs to different content"))
				return
			}
			JsonResponse(w, 200, s)
			return
		} else if !os.IsNotExist(e) {
			deploymentFailure(w, 409, e)
			return
		}
		if e = deploymentBlocked(cfg); e != nil {
			deploymentFailure(w, 409, e)
			return
		}
		dir := deploymentPath(cfg, id)
		if e = os.Mkdir(dir, 0700); e != nil {
			deploymentFailure(w, 500, e)
			return
		}
		if e = syncDirectory(deploymentRoot(cfg)); e != nil {
			_ = os.RemoveAll(dir)
			deploymentFailure(w, 500, e)
			return
		}
		s := &DeploymentState{DeploymentID: id, ArchiveSHA256: digest}
		if e = saveDeployment(cfg, s, "receiving"); e != nil {
			_ = os.RemoveAll(dir)
			_ = syncDirectory(deploymentRoot(cfg))
			deploymentFailure(w, 500, e)
			return
		}
		fail := func(err error) {
			s.FailedStage = s.Status
			err = deploymentStageLocation(err, cfg, s.Status)
			s.Error, s.ErrorDetails = deploymentErrorFields(err)
			_ = saveDeployment(cfg, s, "failed")
			cleanupDeployments(cfg, s.DeploymentID)
			deploymentFailure(w, 400, err)
		}
		f, e := os.OpenFile(filepath.Join(dir, "bundle.tar.gz"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			fail(e)
			return
		}
		hash := sha256.New()
		n, e := io.Copy(io.MultiWriter(f, hash), io.LimitReader(r.Body, maxDeploymentSize+1))
		if e == nil {
			e = f.Sync()
		}
		ce := f.Close()
		if e == nil {
			e = ce
		}
		if e != nil {
			fail(fmt.Errorf("incomplete deployment upload"))
			return
		}
		if n > maxDeploymentSize {
			fail(fmt.Errorf("deployment archive exceeds size limit"))
			return
		}
		if hex.EncodeToString(hash.Sum(nil)) != digest {
			fail(fmt.Errorf("deployment archive SHA-256 mismatch"))
			return
		}
		if e = saveDeployment(cfg, s, "verifying"); e != nil {
			fail(e)
			return
		}
		activate := r.Header.Get("X-Activate") != "false"
		// Encode the accepted snapshot before the worker mutates state.
		JsonResponse(w, 202, map[string]interface{}{"success": true, "deployment_id": id, "status": "verifying"})
		handedOff = true
		go func() { defer release(); prepareDeployment(cfg, s, activate) }()
	}
}
func DeploymentHandler(cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tail := strings.TrimPrefix(r.URL.Path, "/api/deployments/")
		parts := strings.Split(tail, "/")
		if len(parts) < 1 || !deploymentIDPattern.MatchString(parts[0]) {
			w.WriteHeader(404)
			return
		}
		id := parts[0]
		if len(parts) == 2 && parts[1] == "rollback" && r.Method == http.MethodPost {
			manualRollbackHandler(cfg, id, w, r)
			return
		}
		if len(parts) == 1 && r.Method == http.MethodGet {
			s, e := loadDeployment(cfg, id)
			if e != nil {
				deploymentFailure(w, 404, fmt.Errorf("deployment not found"))
				return
			}
			JsonResponse(w, 200, s)
			return
		}
		if len(parts) != 2 || parts[1] != "activate" || r.Method != http.MethodPost {
			w.WriteHeader(405)
			return
		}
		release, e := AcquireServiceOperation(cfg)
		if e != nil {
			deploymentFailure(w, 409, e)
			return
		}
		handedOff := false
		defer func() {
			if !handedOff {
				release()
			}
		}()
		if e = deploymentBlocked(cfg); e != nil {
			deploymentFailure(w, 409, e)
			return
		}
		s, e := loadDeployment(cfg, id)
		if e != nil {
			deploymentFailure(w, 404, e)
			return
		}
		if s.Status == "succeeded" {
			JsonResponse(w, 200, s)
			return
		}
		if s.Status != "ready" {
			deploymentFailure(w, 409, fmt.Errorf("deployment is not ready"))
			return
		}
		// Reverify immutable staged files immediately before activation.
		if e = saveDeployment(cfg, s, "verifying"); e != nil {
			deploymentFailure(w, 500, e)
			return
		}
		JsonResponse(w, 202, map[string]interface{}{"success": true, "deployment_id": id, "status": "verifying"})
		handedOff = true
		go func() {
			defer release()
			if e := loadEffectiveManifest(cfg, s); e != nil {
				s.FailedStage = s.Status
				s.Error, s.ErrorDetails = deploymentErrorFields(deploymentStageLocation(e, cfg, s.Status))
				_ = saveDeployment(cfg, s, "failed")
				return
			}
			activateDeployment(cfg, s)
		}()
	}
}
func validDigest(v string) bool {
	b, e := hex.DecodeString(v)
	return e == nil && len(b) == 32 && len(v) == 64
}
func safeRelative(v string) bool {
	return v != "" && v != "." && v == path.Clean(v) && !strings.HasPrefix(v, "/") && !strings.Contains(v, "\\") && !strings.ContainsRune(v, 0) && v != ".." && !strings.HasPrefix(v, "../") && !strings.HasPrefix(strings.ToLower(v), deploymentDirectory) && !strings.HasPrefix(strings.ToLower(v), ".configflow-")
}
func hashFile(name string) (string, int64, error) {
	f, e := os.Open(name)
	if e != nil {
		return "", 0, e
	}
	defer f.Close()
	h := sha256.New()
	n, e := io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil)), n, e
}
func prepareDeployment(cfg *Config, s *DeploymentState, activate bool) {
	fail := func(e error) {
		s.FailedStage = s.Status
		s.Error, s.ErrorDetails = deploymentErrorFields(deploymentStageLocation(e, cfg, s.Status))
		_ = saveDeployment(cfg, s, "failed")
		cleanupDeployments(cfg, s.DeploymentID)
	}
	if e := extractDeployment(cfg, s); e != nil {
		fail(e)
		return
	}
	if e := validateStagedDeployment(cfg, s); e != nil {
		fail(e)
		return
	}
	b, e := json.Marshal(s.Manifest)
	if e != nil {
		fail(e)
		return
	}
	if e = durableWrite(filepath.Join(deploymentPath(cfg, s.DeploymentID), "effective-manifest.json"), b, 0600); e != nil {
		fail(e)
		return
	}
	if activate {
		activateDeployment(cfg, s)
		return
	}
	if e = validateDeploymentDNS(cfg, s); e != nil {
		fail(e)
		return
	}
	if e = saveDeployment(cfg, s, "ready"); e != nil {
		fail(e)
	}
}
func extractDeployment(cfg *Config, s *DeploymentState) error {
	dir := deploymentPath(cfg, s.DeploymentID)
	stage := filepath.Join(dir, "stage")
	if e := os.Mkdir(stage, 0700); e != nil {
		return e
	}
	f, e := os.Open(filepath.Join(dir, "bundle.tar.gz"))
	if e != nil {
		return e
	}
	defer f.Close()
	gz, e := gzip.NewReader(f)
	if e != nil {
		return fmt.Errorf("invalid gzip archive")
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var manifestBytes []byte
	seen := map[string]bool{}
	actual := map[string]DeploymentFile{}
	var expanded int64
	headers := 0
	for {
		h, e := tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			return fmt.Errorf("invalid tar archive: %w", e)
		}
		headers++
		if headers > maxDeploymentFiles*2 {
			return fmt.Errorf("too many archive entries")
		}
		name := h.Name
		if h.Typeflag == tar.TypeDir {
			name = strings.TrimSuffix(name, "/")
			if name != "files" && (!strings.HasPrefix(name, "files/") || !safeRelative(strings.TrimPrefix(name, "files/"))) {
				return fmt.Errorf("invalid archive directory")
			}
			continue
		}
		if h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeRegA {
			return fmt.Errorf("archive links and special files are forbidden")
		}
		if seen[name] {
			return fmt.Errorf("duplicate archive path")
		}
		seen[name] = true
		expanded += h.Size
		if h.Size < 0 || expanded > maxExpandedSize {
			return fmt.Errorf("expanded deployment exceeds size limit")
		}
		if name == "manifest.json" {
			if h.Size > 4<<20 {
				return fmt.Errorf("manifest exceeds size limit")
			}
			manifestBytes, e = io.ReadAll(tr)
			if e != nil {
				return e
			}
			continue
		}
		if !strings.HasPrefix(name, "files/") || !safeRelative(strings.TrimPrefix(name, "files/")) {
			return fmt.Errorf("invalid archive file path")
		}
		rel := strings.TrimPrefix(name, "files/")
		dest := filepath.Join(stage, filepath.FromSlash(rel))
		if e = durableMkdirAll(filepath.Dir(dest), 0700); e != nil {
			return e
		}
		out, e := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return e
		}
		hash := sha256.New()
		n, e := io.Copy(io.MultiWriter(out, hash), tr)
		if e == nil {
			e = out.Sync()
		}
		ce := out.Close()
		if e == nil {
			e = ce
		}
		if e != nil {
			return e
		}
		actual[rel] = DeploymentFile{Path: rel, Size: n, SHA256: hex.EncodeToString(hash.Sum(nil))}
	}
	// Consume gzip trailer: tar EOF alone does not verify gzip checksum/truncation.
	if n, e := io.Copy(io.Discard, io.LimitReader(gz, (1<<20)+1)); e != nil || n > 1<<20 {
		return fmt.Errorf("invalid gzip trailer")
	}
	var m DeploymentManifest
	if len(manifestBytes) == 0 || json.Unmarshal(manifestBytes, &m) != nil {
		return fmt.Errorf("invalid manifest")
	}
	if m.ProtocolVersion != 1 || m.DeploymentID != s.DeploymentID || m.ServiceType != cfg.ServiceType || m.ConfigPath != filepath.Base(cfg.ConfigPath) {
		return fmt.Errorf("deployment target or protocol does not match Agent")
	}
	if cfg.AgentID != "" && m.AgentID != cfg.AgentID {
		return fmt.Errorf("deployment Agent ID mismatch")
	}
	if len(m.Files) == 0 || len(m.Files) > maxDeploymentFiles || len(m.Files) != len(actual) {
		return fmt.Errorf("manifest file count mismatch")
	}
	listed := map[string]bool{}
	hasConfig := false
	for _, entry := range m.Files {
		if !safeRelative(entry.Path) || listed[entry.Path] || entry.Size < 0 || !validDigest(entry.SHA256) {
			return fmt.Errorf("invalid manifest file")
		}
		listed[entry.Path] = true
		a, ok := actual[entry.Path]
		if !ok || a.Size != entry.Size || a.SHA256 != entry.SHA256 {
			return fmt.Errorf("manifest checksum mismatch for %s", entry.Path)
		}
		if entry.Path == m.ConfigPath {
			hasConfig = true
		}
	}
	if !hasConfig {
		return fmt.Errorf("configuration file missing from manifest")
	}
	s.Manifest = &m
	return nil
}
func loadEffectiveManifest(cfg *Config, s *DeploymentState) error {
	b, e := os.ReadFile(filepath.Join(deploymentPath(cfg, s.DeploymentID), "effective-manifest.json"))
	if e != nil {
		return e
	}
	var m DeploymentManifest
	if e = json.Unmarshal(b, &m); e != nil {
		return e
	}
	s.Manifest = &m
	return verifyManifestFiles(filepath.Join(deploymentPath(cfg, s.DeploymentID), "stage"), &m)
}
func verifyManifestFiles(root string, m *DeploymentManifest) error {
	if len(m.Files) == 0 || len(m.Files) > maxDeploymentFiles {
		return fmt.Errorf("invalid effective manifest file count")
	}
	var total int64
	seen := map[string]bool{}
	for _, f := range m.Files {
		if f.Size < 0 || f.Size > maxExpandedSize || total > maxExpandedSize-f.Size || seen[f.Path] || !validDigest(f.SHA256) {
			return fmt.Errorf("invalid effective manifest")
		}
		total += f.Size
		seen[f.Path] = true
	}
	for _, f := range m.Files {
		if !safeRelative(f.Path) {
			return fmt.Errorf("unsafe managed file")
		}
		name := filepath.Join(root, filepath.FromSlash(f.Path))
		if e := rejectSymlinkPath(root, f.Path); e != nil {
			return e
		}
		h, n, e := hashFile(name)
		if e != nil {
			return e
		}
		if h != f.SHA256 || n != f.Size {
			return fmt.Errorf("staged file changed: %s", f.Path)
		}
	}
	return nil
}
func rejectSymlinkPath(root, rel string) error {
	current := root
	for _, part := range strings.Split(filepath.FromSlash(rel), string(os.PathSeparator)) {
		current = filepath.Join(current, part)
		info, e := os.Lstat(current)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return e
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink in managed path: %s", rel)
		}
	}
	return nil
}
func durableCopy(src, dst string, mode os.FileMode) error {
	return durableCopyOwned(src, dst, mode, -1, -1)
}
func durableCopyOwned(src, dst string, mode os.FileMode, uid, gid int) error {
	if e := copyFileOwned(src, dst, mode, uid, gid); e != nil {
		return e
	}
	return syncDirectory(filepath.Dir(dst))
}

// copyFileOwned is durableCopyOwned without the final directory flush: a
// caller copying many files syncs each directory once before relying on them.
func copyFileOwned(src, dst string, mode os.FileMode, uid, gid int) error {
	if err := durableMkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	in, e := os.Open(src)
	if e != nil {
		return e
	}
	defer in.Close()
	out, e := os.CreateTemp(filepath.Dir(dst), ".configflow-copy-")
	if e != nil {
		return e
	}
	tmp := out.Name()
	defer os.Remove(tmp)
	if uid >= 0 && gid >= 0 {
		if info, statErr := out.Stat(); statErr != nil {
			e = statErr
		} else if oldUID, oldGID := fileOwnership(info); oldUID != uid || oldGID != gid {
			e = out.Chown(uid, gid)
		}
	}
	if e == nil {
		e = out.Chmod(mode)
	}
	if e == nil {
		_, e = io.Copy(out, in)
	}
	if e == nil {
		e = out.Sync()
	}
	ce := out.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return e
	}
	return os.Rename(tmp, dst)
}
func managedPaths(cfg *Config, s *DeploymentState) ([]string, error) {
	set := map[string]bool{}
	for _, f := range s.Manifest.Files {
		set[f.Path] = true
	}
	b, e := os.ReadFile(filepath.Join(deploymentRoot(cfg), "active-manifest.json"))
	if e == nil {
		var old DeploymentManifest
		if e = json.Unmarshal(b, &old); e != nil {
			return nil, fmt.Errorf("invalid active manifest")
		}
		for _, f := range old.Files {
			if !safeRelative(f.Path) {
				return nil, fmt.Errorf("invalid active manifest path")
			}
			set[f.Path] = true
		}
		s.PreviousManifest = b
	} else if !os.IsNotExist(e) {
		return nil, e
	}
	result := []string{}
	for p := range set {
		result = append(result, p)
	}
	sort.Strings(result)
	return result, nil
}
func backupDeployment(cfg *Config, s *DeploymentState) error {
	root := filepath.Dir(cfg.ConfigPath)
	paths, e := managedPaths(cfg, s)
	if e != nil {
		return e
	}
	stage := filepath.Join(deploymentPath(cfg, s.DeploymentID), "stage")
	staged := map[string]DeploymentFile{}
	for _, f := range s.Manifest.Files {
		staged[f.Path] = f
	}
	dirs := map[string]bool{}
	s.Backups = nil
	for _, rel := range paths {
		if e = rejectSymlinkPath(root, rel); e != nil {
			return e
		}
		live := filepath.Join(root, rel)
		info, e := os.Lstat(live)
		b := deploymentBackup{Path: rel}
		if e == nil {
			if !info.Mode().IsRegular() {
				return fmt.Errorf("managed path is not a regular file: %s", rel)
			}
			b.Existed = true
			b.Mode = uint32(info.Mode().Perm())
			b.UID, b.GID = fileOwnership(info)
			b.OwnershipKnown = b.UID >= 0 && b.GID >= 0
			backup := filepath.Join(deploymentPath(cfg, s.DeploymentID), "backup", rel)
			var h string
			linked := false
			if f, ok := staged[rel]; ok && f.Size == info.Size() {
				if h, _, e = hashFile(live); e != nil {
					return e
				}
				// Unchanged content: the verified stage file already holds these
				// bytes durably and is never written again, so link it instead
				// of copying the live file. Any link failure falls back to a copy.
				linked = h == f.SHA256 && linkBackup(filepath.Join(stage, rel), backup) == nil
			}
			if !linked {
				if e = deploymentBackupCopy(live, backup, info.Mode().Perm()); e != nil {
					return e
				}
				if h, _, e = hashFile(live); e != nil {
					return e
				}
			}
			bh, _, e := hashFile(backup)
			if e != nil || h != bh {
				return fmt.Errorf("backup verification failed: %s", rel)
			}
			b.SHA256 = h
			dirs[filepath.Dir(backup)] = true
		} else if !os.IsNotExist(e) {
			return e
		}
		s.Backups = append(s.Backups, b)
	}
	// Every backup entry must be durable before the journal claims them.
	if e = syncDirectories(dirs); e != nil {
		return e
	}
	s.BackupComplete = true
	return saveDeployment(cfg, s, "backing_up")
}

// linkBackup hard-links an immutable staged file as a backup.
func linkBackup(src, dst string) error {
	if e := durableMkdirAll(filepath.Dir(dst), 0755); e != nil {
		return e
	}
	if e := os.Remove(dst); e != nil && !os.IsNotExist(e) {
		return e
	}
	return os.Link(src, dst)
}

// liveFileCurrent reports whether a managed file already holds f's bytes with
// the mode and owner a copy would give it, so replacing it would change only
// its inode. Like a fresh copy it gets the current modification time (cores
// schedule provider refreshes from it) and is flushed, since a core may have
// written it without fsync. Any doubt answers false and copies.
func liveFileCurrent(name string, f DeploymentFile, mode os.FileMode, uid, gid int) bool {
	if uid < 0 || gid < 0 {
		return false // A copy's owner would then come from this process.
	}
	info, e := os.Lstat(name)
	if e != nil || info.Mode() != mode || info.Size() != f.Size {
		return false
	}
	if owner, group := fileOwnership(info); owner != uid || group != gid {
		return false
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); !ok || stat.Nlink != 1 {
		return false // Replacing would detach other names for this file.
	}
	if h, n, e := hashFile(name); e != nil || h != f.SHA256 || n != f.Size {
		return false
	}
	now := time.Now()
	if os.Chtimes(name, now, now) != nil {
		return false
	}
	file, e := os.Open(name)
	if e != nil {
		return false
	}
	defer file.Close()
	return file.Sync() == nil
}
func replaceDeployment(cfg *Config, s *DeploymentState) error {
	root := filepath.Dir(cfg.ConfigPath)
	stage := filepath.Join(deploymentPath(cfg, s.DeploymentID), "stage")
	fresh := map[string]bool{}
	dirs := map[string]bool{}
	for _, f := range s.Manifest.Files {
		fresh[f.Path] = true
		if e := rejectSymlinkPath(root, f.Path); e != nil {
			return e
		}
		mode := os.FileMode(0644)
		privateAsset := cfg.ServiceType == "oxidns" && f.Role == "custom"
		if f.Path == s.Manifest.ConfigPath || cfg.ServiceType == "oxidns" {
			mode = 0600
		}
		uid, gid := -1, -1
		if info, e := os.Stat(cfg.ConfigPath); e == nil {
			uid, gid = fileOwnership(info)
		} else if info, e := os.Stat(root); e == nil {
			uid, gid = fileOwnership(info)
		}
		for _, prior := range s.Backups {
			if prior.Path == f.Path && prior.Existed {
				mode = os.FileMode(prior.Mode)
				if prior.OwnershipKnown && !privateAsset {
					uid, gid = prior.UID, prior.GID
				}
				break
			}
		}
		// Uploaded OxiDNS assets can contain TLS keys or included credentials.
		// Always install them privately for the core owner, even when replacing
		// a formerly public file. Rollback retains its recorded mode/ownership.
		if privateAsset {
			mode = 0600
		}
		if e := ensureManagedParent(root, f.Path, uid, gid); e != nil {
			return e
		}
		live := filepath.Join(root, f.Path)
		if liveFileCurrent(live, f, mode, uid, gid) {
			continue
		}
		if e := deploymentReplaceCopy(filepath.Join(stage, f.Path), live, mode, uid, gid); e != nil {
			return e
		}
		dirs[filepath.Dir(live)] = true
	}
	// A candidate can have been staged before another deployment introduced a
	// Geo database. Treat prior managed Geo resources as persistent even when
	// absent from that older candidate. Rollback still removes newly introduced
	// files using its independent Existed snapshot.
	persistent := map[string]bool{}
	var previous DeploymentManifest
	if len(s.PreviousManifest) > 0 {
		if e := json.Unmarshal(s.PreviousManifest, &previous); e != nil {
			return e
		}
		for _, entry := range previous.Files {
			if entry.Role == "resource" {
				for _, asset := range mihomoGeoAssets {
					if entry.Path == asset {
						persistent[asset] = true
					}
				}
			}
		}
	}
	for _, b := range s.Backups {
		if !fresh[b.Path] && !persistent[b.Path] {
			if e := os.Remove(filepath.Join(root, b.Path)); e != nil && !os.IsNotExist(e) {
				return e
			}
			dirs[filepath.Dir(filepath.Join(root, b.Path))] = true
		}
	}
	if e := syncDirectories(dirs); e != nil {
		return e
	}
	return verifyManifestFiles(root, s.Manifest)
}
func activateDeployment(cfg *Config, s *DeploymentState) {
	failBefore := func(e error) {
		s.FailedStage = s.Status
		s.Error, s.ErrorDetails = deploymentErrorFields(deploymentStageLocation(e, cfg, s.Status))
		_ = saveDeployment(cfg, s, "failed")
		cleanupDeployments(cfg, s.DeploymentID)
	}
	if e := loadEffectiveManifest(cfg, s); e != nil {
		failBefore(e)
		return
	}
	if e := validateDeploymentAgentIPs(cfg, s); e != nil {
		failBefore(e)
		return
	}
	if changed, e := carryPersistentGeoResources(cfg, s); e != nil {
		failBefore(e)
		return
	} else if changed {
		// Explicit activation can follow another deployment; validate the exact
		// resource snapshot now carried into this candidate before stopping.
		if e = validateStagedDeployment(cfg, s); e != nil {
			failBefore(e)
			return
		}
		data, e := json.Marshal(s.Manifest)
		if e != nil {
			failBefore(e)
			return
		}
		if e = durableWrite(filepath.Join(deploymentPath(cfg, s.DeploymentID), "effective-manifest.json"), data, 0600); e != nil {
			failBefore(e)
			return
		}
	}
	if cfg.ServiceType == "oxidns" {
		// A ready candidate may outlive the installed core or its environment.
		// Recheck with the current binary before any service/file mutation; the
		// staging validator also re-verifies the immutable manifest afterward.
		if e := validateStagedDeployment(cfg, s); e != nil {
			failBefore(e)
			return
		}
	}
	if e := validateServiceLifecycle(cfg); e != nil {
		failBefore(e)
		return
	}
	// A prepared candidate can outlive both its peer configuration and current
	// listeners. Check again before stopping or replacing any live service.
	if e := validateDeploymentDNS(cfg, s); e != nil {
		failBefore(e)
		return
	}
	if e := deploymentDiskPreflight(cfg, s); e != nil {
		failBefore(e)
		return
	}
	running, e := waitServiceState(cfg)
	if e != nil {
		failBefore(e)
		return
	}
	s.WasRunning = running
	var reloadErr error
	fail := func(e error) {
		if reloadErr != nil {
			coreName := "OxiDNS"
			if cfg.ServiceType == "mihomo" {
				coreName = "Mihomo"
			}
			e = withPreviousDiagnostic(coreName+" reload and fallback restart failed", reloadErr, e)
		}
		s.FailedStage = s.Status
		s.Error, s.ErrorDetails = deploymentErrorFields(deploymentStageLocation(e, cfg, s.Status))
		rollbackDeployment(cfg, s, true)
		cleanupDeployments(cfg, s.DeploymentID)
	}
	// Live files are untouched until replacement; while the service keeps
	// running, earlier failures need no rollback.
	reloader := prepareDeploymentReload(cfg, s, running)
	if reloader == nil && s.Manifest.ReloadOnly {
		failBefore(fmt.Errorf("native hot reload is unavailable; reload-only deployment cannot activate"))
		return
	}
	failUnchanged := fail
	if reloader != nil {
		failUnchanged = failBefore
	} else {
		if e = saveDeployment(cfg, s, "stopping"); e != nil {
			failBefore(e)
			return
		}
		if e = stopService(cfg); e != nil {
			fail(e)
			return
		}
	}
	if e = saveDeployment(cfg, s, "backing_up"); e != nil {
		failUnchanged(e)
		return
	}
	if e = backupDeployment(cfg, s); e != nil {
		failUnchanged(e)
		return
	}
	if e = saveDeployment(cfg, s, "replacing"); e != nil {
		failUnchanged(e)
		return
	}
	if e = replaceDeployment(cfg, s); e != nil {
		fail(e)
		return
	}
	if reloader != nil {
		if e = saveDeployment(cfg, s, "reloading"); e != nil {
			fail(e)
			return
		}
		if e = reloader.reload(cfg); e != nil {
			if s.Manifest.ReloadOnly {
				// Do not activate the candidate by restarting. Recovery restores the
				// previous files and may restart to resolve an uncertain core state.
				fail(fmt.Errorf("native hot reload failed: %w", e))
				return
			}
			reloadErr = e
			// A manual deployment may resolve a failed or uncertain reload by
			// applying the candidate through the usual stop/start health checks.
			log.Printf("%s release %s: reload failed (%v), restarting", cfg.ServiceType, s.DeploymentID, e)
			reloader = nil
			if e = saveDeployment(cfg, s, "stopping"); e != nil {
				fail(e)
				return
			}
			if e = stopService(cfg); e != nil {
				fail(e)
				return
			}
		}
	}
	if reloader == nil {
		if e = saveDeployment(cfg, s, "starting"); e != nil {
			fail(e)
			return
		}
		if e = startService(cfg); e != nil {
			fail(e)
			return
		}
	}
	if e = saveDeployment(cfg, s, "checking"); e != nil {
		fail(e)
		return
	}
	if e = checkDeploymentHealth(cfg); e != nil {
		fail(e)
		return
	}
	b, e := json.Marshal(s.Manifest)
	if e != nil {
		fail(e)
		return
	}
	if e = durableWrite(filepath.Join(deploymentRoot(cfg), "active-manifest.json"), b, 0600); e != nil {
		fail(e)
		return
	}
	for _, f := range s.Manifest.Files {
		if f.Path == s.Manifest.ConfigPath {
			s.ConfigVersion = f.SHA256
		}
	}
	if e = saveDeployment(cfg, s, "succeeded"); e != nil {
		s.ConfigVersion = ""
		fail(e)
		return
	}
	cleanupDeployments(cfg, s.DeploymentID)
	ReconcileLinuxGateway(cfg)
}
func rollbackDeployment(cfg *Config, s *DeploymentState, start bool) {
	s.RollbackError, s.RollbackErrorDetails = "", ""
	// Persist intent before rollback writes so an interrupted rollback is retried.
	_ = saveDeployment(cfg, s, "rolling_back")
	rollbackErr := func(e error) {
		s.ConfigVersion = ""
		s.RollbackError, s.RollbackErrorDetails = deploymentErrorFields(deploymentStageLocation(e, cfg, s.Status))
		_ = saveDeployment(cfg, s, "rollback_failed")
	}
	if start {
		if e := stopService(cfg); e != nil {
			rollbackErr(fmt.Errorf("rollback cannot stop service: %w", e))
			return
		}
	}
	if s.BackupComplete {
		root := filepath.Dir(cfg.ConfigPath)
		// Verify every backup before restoring any, preserving a usable recovery set.
		for _, b := range s.Backups {
			if !safeRelative(b.Path) {
				rollbackErr(fmt.Errorf("invalid backup path"))
				return
			}
			if b.Existed {
				h, _, e := hashFile(filepath.Join(deploymentPath(cfg, s.DeploymentID), "backup", b.Path))
				if e != nil || h != b.SHA256 {
					rollbackErr(fmt.Errorf("backup checksum mismatch: %s", b.Path))
					return
				}
			}
		}
		for _, b := range s.Backups {
			if e := rejectSymlinkPath(root, b.Path); e != nil {
				rollbackErr(e)
				return
			}
			name := filepath.Join(root, b.Path)
			if b.Existed {
				uid, gid := -1, -1
				if b.OwnershipKnown {
					uid, gid = b.UID, b.GID
				}
				if e := durableCopyOwned(filepath.Join(deploymentPath(cfg, s.DeploymentID), "backup", b.Path), name, os.FileMode(b.Mode), uid, gid); e != nil {
					rollbackErr(e)
					return
				}
			} else {
				if e := os.Remove(name); e != nil && !os.IsNotExist(e) {
					rollbackErr(e)
					return
				}
				if e := syncDirectory(filepath.Dir(name)); e != nil && !os.IsNotExist(e) {
					rollbackErr(e)
					return
				}
			}
		}
		active := filepath.Join(deploymentRoot(cfg), "active-manifest.json")
		if len(s.PreviousManifest) > 0 {
			if e := durableWrite(active, s.PreviousManifest, 0600); e != nil {
				rollbackErr(e)
				return
			}
		} else {
			if e := os.Remove(active); e != nil && !os.IsNotExist(e) {
				rollbackErr(e)
				return
			}
			if e := syncDirectory(deploymentRoot(cfg)); e != nil {
				rollbackErr(e)
				return
			}
		}
	}
	if start && s.WasRunning {
		if e := startService(cfg); e != nil {
			rollbackErr(fmt.Errorf("old service failed to start: %w", e))
			return
		}
		if e := checkDeploymentHealth(cfg); e != nil {
			rollbackErr(fmt.Errorf("old service health failed: %w", e))
			return
		}
	}
	s.ConfigVersion = ""
	if s.ManualRollback {
		if digest, _, err := hashFile(cfg.ConfigPath); err == nil {
			s.ConfigVersion = digest[:8]
		}
	}
	s.RollbackError, s.RollbackErrorDetails = "", ""
	status := "rolled_back"
	if !start && s.WasRunning {
		status = "recovery_pending"
	}
	if e := saveDeployment(cfg, s, status); e != nil {
		rollbackErr(e)
	} else if status == "rolled_back" {
		ReconcileLinuxGateway(cfg)
	}
}

// RecoverDeployments explicitly restores unfinished transactions. Normal Agent
// startup uses CheckDeployments instead. startService=false is only safe while
// the core is stopped; interactive recovery uses true to manage its lifecycle.
func RecoverDeployments(cfg *Config, startService bool) error {
	release, e := AcquireServiceOperation(cfg)
	if e != nil {
		return e
	}
	defer release()
	entries, e := os.ReadDir(deploymentRoot(cfg))
	if e != nil {
		return e
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		s, e := loadDeployment(cfg, entry.Name())
		if e != nil {
			if os.IsNotExist(e) {
				removed, cleanupErr := removeUnstartedDeployment(cfg, entry.Name())
				if cleanupErr != nil {
					return cleanupErr
				}
				if removed {
					continue
				}
			}
			return fmt.Errorf("cannot read deployment recovery state %s: %w", entry.Name(), e)
		}
		switch s.Status {
		case "recovery_pending":
			if startService {
				var startupErr error
				if running, e := serviceRunning(cfg); e == nil && !running {
					startupErr = startServiceForRecovery(cfg)
				}
				// STARTING/activating is normal during boot. Let the bounded
				// health window wait for readiness instead of stranding recovery.
				if e := checkDeploymentHealth(cfg); e != nil {
					if startupErr != nil {
						e = withPreviousDiagnostic("restored service startup and health check failed", startupErr, e)
					}
					s.RollbackError, s.RollbackErrorDetails = deploymentErrorFields(deploymentStageLocation(e, cfg, s.Status))
					_ = saveDeployment(cfg, s, "rollback_failed")
					return fmt.Errorf("restored service health failed")
				}
				if e := saveDeployment(cfg, s, "rolled_back"); e != nil {
					return e
				}
			}
		case "stopping", "backing_up", "replacing", "reloading", "starting", "checking", "rolling_back", "rollback_failed":
			if s.Error == "" {
				s.Error, s.ErrorDetails = "deployment interrupted before successful confirmation", ""
			}
			rollbackDeployment(cfg, s, startService)
			if s.Status == "rollback_failed" {
				return fmt.Errorf("deployment %s recovery failed: %s", s.DeploymentID, s.RollbackError)
			}
		case "receiving", "verifying":
			s.Error, s.ErrorDetails = "deployment interrupted before activation", ""
			if e = saveDeployment(cfg, s, "failed"); e != nil {
				return e
			}
		}
	}
	return nil
}

func deploymentDiskPreflight(cfg *Config, s *DeploymentState) error {
	root := filepath.Dir(cfg.ConfigPath)
	available, e := deploymentFreeBytes(root)
	if e != nil {
		return fmt.Errorf("cannot determine free deployment disk space")
	}
	paths, e := managedPaths(cfg, s)
	if e != nil {
		return e
	}
	var needed int64 = 16 << 20 // journal, directory entries, and copy-on-write headroom
	for _, rel := range paths {
		if e := rejectSymlinkPath(root, rel); e != nil {
			return e
		}
		info, e := os.Lstat(filepath.Join(root, rel))
		if e == nil {
			if !info.Mode().IsRegular() {
				return fmt.Errorf("managed path is not a regular file: %s", rel)
			}
			needed += info.Size()
		} else if !os.IsNotExist(e) {
			return e
		}
	}
	for _, f := range s.Manifest.Files {
		needed += f.Size
	}
	if uint64(needed) > available {
		return fmt.Errorf("insufficient disk space for verified backup and replacement")
	}
	return nil
}

// Retain the active/current transactions and two recent completed ones. Ready and unresolved
// journals are never pruned: they may still be activated or needed for recovery.
func cleanupDeployments(cfg *Config, current string) {
	_ = cleanupDeploymentPayloads(cfg) // Caller already holds the service operation lock.
	var active DeploymentManifest
	if b, e := os.ReadFile(filepath.Join(deploymentRoot(cfg), "active-manifest.json")); e == nil {
		if json.Unmarshal(b, &active) != nil {
			return
		}
	}
	entries, e := os.ReadDir(deploymentRoot(cfg))
	if e != nil {
		return
	}
	type candidate struct{ id, updated string }
	var completed []candidate
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == current || entry.Name() == active.DeploymentID {
			continue
		}
		s, e := loadDeployment(cfg, entry.Name())
		if e != nil {
			continue
		}
		switch s.Status {
		case "succeeded", "failed", "rolled_back":
			completed = append(completed, candidate{s.DeploymentID, s.UpdatedAt})
		}
	}
	sort.Slice(completed, func(i, j int) bool { return completed[i].updated > completed[j].updated })
	for i := 2; i < len(completed); i++ {
		_ = os.RemoveAll(deploymentPath(cfg, completed[i].id))
	}
	_ = syncDirectory(deploymentRoot(cfg))
}

// Persist every new directory entry, not just a file's immediate parent. This
// keeps transaction journals and nested backups reachable after power loss.
func durableMkdirAll(name string, mode os.FileMode) error {
	name = filepath.Clean(name)
	if info, e := os.Lstat(name); e == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("not a directory: %s", name)
		}
		return nil
	} else if !os.IsNotExist(e) {
		return e
	}
	parent := filepath.Dir(name)
	if parent == name {
		return fmt.Errorf("cannot create directory root")
	}
	if e := durableMkdirAll(parent, mode); e != nil {
		return e
	}
	if e := os.Mkdir(name, mode); e != nil && !os.IsExist(e) {
		return e
	}
	return syncDirectory(parent)
}

func startServiceForRecovery(cfg *Config) error { return startService(cfg) }

func fileOwnership(info os.FileInfo) (int, int) {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		return int(stat.Uid), int(stat.Gid)
	}
	return -1, -1
}
func ensureManagedParent(root, rel string, uid, gid int) error {
	current := root
	for _, part := range strings.Split(filepath.Dir(filepath.FromSlash(rel)), string(os.PathSeparator)) {
		if part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, e := os.Lstat(current)
		if e == nil {
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("invalid managed parent directory")
			}
			continue
		}
		if !os.IsNotExist(e) {
			return e
		}
		if e = os.Mkdir(current, 0755); e != nil {
			return e
		}
		if uid >= 0 && gid >= 0 {
			if info, e := os.Stat(current); e != nil {
				return e
			} else if oldUID, oldGID := fileOwnership(info); oldUID != uid || oldGID != gid {
				if e = os.Chown(current, uid, gid); e != nil {
					return e
				}
			}
		}
		if e = syncDirectory(current); e != nil {
			return e
		}
		if e = syncDirectory(filepath.Dir(current)); e != nil {
			return e
		}
	}
	return nil
}

var deploymentFreeBytes = func(root string) (uint64, error) {
	var stats unix.Statfs_t
	if e := unix.Statfs(root, &stats); e != nil {
		return 0, e
	}
	return uint64(stats.Bavail) * uint64(stats.Bsize), nil
}
var deploymentBackupCopy = func(src, dst string, mode os.FileMode) error { return copyFileOwned(src, dst, mode, -1, -1) }
var deploymentReplaceCopy = copyFileOwned

// The first durable receiving record precedes archive reception and all live
// writes. A crash between mkdir and that first rename can therefore only leave
// an empty transaction directory or an initial .write-* temporary state file.
// Never discard an existing/corrupt state, a received bundle, stage or backup.
// Call only while holding the service operation lock.
func removeUnstartedDeployment(cfg *Config, id string) (bool, error) {
	if !deploymentIDPattern.MatchString(id) {
		return false, nil
	}
	dir := deploymentPath(cfg, id)
	if _, e := os.Lstat(filepath.Join(dir, "state.json")); e == nil {
		return false, nil
	} else if !os.IsNotExist(e) {
		return false, e
	}
	entries, e := os.ReadDir(dir)
	if e != nil {
		return false, e
	}
	for _, entry := range entries {
		info, e := entry.Info()
		if e != nil {
			return false, e
		}
		if !strings.HasPrefix(entry.Name(), ".write-") || !info.Mode().IsRegular() || info.Size() > 64<<10 {
			return false, nil
		}
		// A complete temporary record from a later phase cannot be an initial
		// interrupted write, even if somebody removed its corresponding state file.
		data, e := os.ReadFile(filepath.Join(dir, entry.Name()))
		if e != nil {
			return false, e
		}
		var s DeploymentState
		if json.Unmarshal(data, &s) == nil && (s.Status != "receiving" || s.DeploymentID != id || s.BackupComplete || len(s.Backups) > 0) {
			return false, nil
		}
	}
	for _, entry := range entries {
		if e := os.Remove(filepath.Join(dir, entry.Name())); e != nil {
			return false, e
		}
	}
	if e := syncDirectory(dir); e != nil {
		return false, e
	}
	if e := os.Remove(dir); e != nil {
		return false, e
	}
	if e := syncDirectory(deploymentRoot(cfg)); e != nil {
		return false, e
	}
	return true, nil
}
