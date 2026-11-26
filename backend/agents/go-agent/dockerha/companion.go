package dockerha

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"agent/internal/atomicfile"
)

// CompanionPaths are the files ha-companion-check reads and writes. The
// defaults are the HA companion container's mounts.
type CompanionPaths struct {
	AgentDir    string // Agent data, read-only in the companion
	CoreDir     string // OxiDNS data, read-only
	HADir       string // shared HA directory, writable
	HostIDFile  string // identity mode "file"
	IdentityDir string // identity mode "volume"
	MountInfo   string
	ProcStatus  string
	NSDir       string
	// Wait bounds how long installation mode waits for the Agent to
	// finish initializing; Poll is the re-check interval.
	Wait time.Duration
	Poll time.Duration
}

// DefaultCompanionPaths are the companion container's paths.
var DefaultCompanionPaths = CompanionPaths{
	AgentDir: "/opt/configflow-agent", CoreDir: "/etc/oxidns", HADir: "/var/lib/configflow-ha",
	HostIDFile: "/run/configflow-host-machine-id", IdentityDir: "/run/configflow-host-identity",
	MountInfo: "/proc/self/mountinfo", ProcStatus: "/proc/self/status", NSDir: "/proc/self/ns",
	Wait: 30 * time.Second, Poll: 250 * time.Millisecond,
}

const (
	compactMarker   = ".configflow-compose-compact.json"
	compactIdentity = ".configflow-ha-identity"
)

var (
	hex32 = regexp.MustCompile(`\A[0-9a-f]{32}\z`)
	hex64 = regexp.MustCompile(`\A[0-9a-f]{64}\z`)
)

// asciiSpace is the whitespace trimmed from identity files.
const asciiSpace = " \t\n\r\v\f"

// CheckCompanion verifies the companion's read-only mounts and host
// identity (or, in installation mode, the compact Compose installation
// record) and publishes the evidence the main Agent checks before it lets
// the companion run keepalived. It never starts keepalived.
func CheckCompanion(ctx context.Context, paths CompanionPaths, env func(string) string) error {
	mode := env("CONFIGFLOW_DNS_HA_IDENTITY")
	if mode == "" {
		mode = "file"
	}
	var identity []byte
	var err error
	switch mode {
	case "installation":
		identity, err = installationIdentity(ctx, paths, env("INSTALL_SCOPE"))
	case "volume":
		if !readonlyMount(paths.MountInfo, paths.IdentityDir, false) || !isPlainDir(paths.IdentityDir) {
			return fail(msgCompanionVolume)
		}
		path := filepath.Join(paths.IdentityDir, "host-id")
		info, statErr := os.Lstat(path)
		if statErr != nil {
			return fmt.Errorf("host identity: %w", statErr)
		}
		if !info.Mode().IsRegular() {
			return fail(msgCompanionVolumeFile)
		}
		identity, err = os.ReadFile(path)
		identity = bytes.Trim(identity, asciiSpace)
	case "file":
		identity, err = os.ReadFile(paths.HostIDFile)
		identity = bytes.Trim(identity, asciiSpace)
	default:
		return fail(msgCompanionMode)
	}
	if err != nil {
		var refusal *Failure
		if errors.As(err, &refusal) {
			return err
		}
		return fmt.Errorf("host identity: %w", err)
	}
	if len(identity) < 16 || len(identity) > 128 || bytes.ContainsAny(identity, "\r\n") {
		return fail(msgCompanionIdentity)
	}
	return writeEvidence(paths, identity)
}

func installationIdentity(ctx context.Context, paths CompanionPaths, scope string) ([]byte, error) {
	if !hex32.MatchString(scope) {
		return nil, fail(msgCompanionScope)
	}
	if !readonlyMount(paths.MountInfo, paths.AgentDir, true) || !isPlainDir(paths.AgentDir) {
		return nil, fail(msgCompanionAgentMount)
	}
	marker := filepath.Join(paths.AgentDir, compactMarker)
	deadline := time.Now().Add(paths.Wait)
	for {
		// Atomic publication of ready=true follows all three ownership
		// records; HA/run is not written while initialization is pending.
		if present(marker) {
			record, err := readCompanionJSON(marker, msgCompanionRecordInvalid)
			if err != nil {
				return nil, err
			}
			if !ownedRecord(record, scope) {
				return nil, fail(msgCompanionRecordForeign)
			}
			switch record["ready"] {
			case true:
				return readyIdentity(paths, record)
			case false:
			default:
				return nil, fail(msgCompanionReadiness)
			}
		}
		if !time.Now().Before(deadline) {
			return nil, fail(msgCompanionTimeout)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(paths.Poll):
		}
	}
}

// ownedRecord checks the installation record against the companion's
// Compose scope: protocol 2, a 32-hex installation id, and an owner that is
// the digest of a binding for this scope.
func ownedRecord(record map[string]any, scope string) bool {
	binding, ok := record["binding"].(map[string]any)
	if !ok || !isInteger(record["protocol"], 2) || binding["session"] != scope {
		return false
	}
	id, _ := record["installation_id"].(string)
	owner, _ := record["owner"].(string)
	if !hex32.MatchString(id) || !hex64.MatchString(owner) {
		return false
	}
	digest, ok := bindingOwner(binding)
	return ok && digest == owner
}

func readyIdentity(paths CompanionPaths, record map[string]any) ([]byte, error) {
	// A helper may have been given a different core/HA bind path than the
	// Agent; check every owner before creating HA/run.
	for _, dir := range []string{paths.CoreDir, paths.HADir} {
		info, err := os.Lstat(dir)
		if err != nil {
			return nil, fmt.Errorf("data directory: %w", err)
		}
		if !info.IsDir() {
			return nil, fail(msgCompanionDataSymlink)
		}
		data, err := readCompanionJSON(filepath.Join(dir, compactMarker), msgCompanionDataInvalid)
		if err != nil {
			return nil, err
		}
		if !isInteger(data["protocol"], 2) || !sameValue(data["owner"], record["owner"]) ||
			!sameValue(data["installation_id"], record["installation_id"]) {
			return nil, fail(msgCompanionDataForeign)
		}
	}
	raw, err := readCompanionRegular(filepath.Join(paths.AgentDir, compactIdentity), 128)
	if err != nil {
		return nil, err
	}
	identity := bytes.Trim(raw, asciiSpace)
	if string(identity) != record["installation_id"].(string) {
		return nil, fail(msgCompanionIdentityRecord)
	}
	return identity, nil
}

// isInteger reports a JSON integer (not a boolean or a float) equal to want.
func isInteger(value any, want int) bool {
	switch v := value.(type) {
	case int:
		return v == want
	case json.Number:
		return integerText.MatchString(string(v)) && string(v) == strconv.Itoa(want)
	}
	return false
}

// readCompanionRegular is the companion's regular-file read: no final
// symlink, no device or pipe, bounded size.
func readCompanionRegular(path string, limit int64) ([]byte, error) {
	raw, err := readRegular(path, limit)
	var refusal *Failure
	if errors.As(err, &refusal) {
		switch refusal.Message {
		case msgFileNotRegular:
			return nil, fail(msgCompanionNotRegular)
		case msgFileTooLarge:
			return nil, fail(msgCompanionTooLarge)
		}
		return nil, fmt.Errorf("read %s: missing, unreadable or a symlink", filepath.Base(path))
	}
	return raw, err
}

func readCompanionJSON(path, invalid string) (map[string]any, error) {
	raw, err := readCompanionRegular(path, 65536)
	if err != nil {
		return nil, err
	}
	value, err := decodeObject(raw)
	if errors.Is(err, errNotJSON) {
		return nil, fail(invalid)
	}
	if err != nil {
		// Valid JSON that is not an object fails the record checks.
		return map[string]any{}, nil
	}
	return value, nil
}

func isPlainDir(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.IsDir()
}

// readonlyMount reports whether the first mount at path in mountinfo is
// read-only; persistent also rejects tmpfs and ramfs.
func readonlyMount(mountinfo, path string, persistent bool) bool {
	raw, err := os.ReadFile(mountinfo)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(raw), "\n") {
		left, right, found := strings.Cut(line, " - ")
		fields, filesystem := strings.Fields(left), strings.Fields(right)
		if !found || len(fields) < 6 || unescapeMount(fields[4]) != path {
			continue
		}
		readonly := false
		for _, option := range strings.Split(fields[5], ",") {
			readonly = readonly || option == "ro"
		}
		return readonly && len(filesystem) > 0 &&
			(!persistent || (filesystem[0] != "tmpfs" && filesystem[0] != "ramfs"))
	}
	return false
}

// unescapeMount decodes the octal escapes the kernel uses in mountinfo.
func unescapeMount(value string) string {
	return strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`).Replace(value)
}

type namespaceID struct {
	Device uint64 `json:"device"`
	Inode  uint64 `json:"inode"`
}

type companionEvidence struct {
	Protocol int         `json:"protocol"`
	Mode     string      `json:"mode"`
	HostID   string      `json:"host_id"`
	CapEff   string      `json:"cap_eff"`
	Network  namespaceID `json:"network_namespace"`
	PID      namespaceID `json:"pid_namespace"`
}

func writeEvidence(paths CompanionPaths, identity []byte) error {
	dir := filepath.Join(paths.HADir, "run")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create HA run directory: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("restrict HA run directory: %w", err)
	}
	capEff, err := effectiveCapabilities(paths.ProcStatus)
	if err != nil {
		return err
	}
	evidence := companionEvidence{Protocol: 1, Mode: "host", HostID: hexDigest(identity), CapEff: capEff}
	if evidence.Network, err = namespace(paths.NSDir, "net"); err != nil {
		return err
	}
	if evidence.PID, err = namespace(paths.NSDir, "pid"); err != nil {
		return err
	}
	data, err := json.Marshal(evidence)
	if err != nil {
		return fmt.Errorf("encode evidence: %w", err)
	}
	if err := atomicfile.WriteWith(filepath.Join(dir, "companion.json"), data, 0o600, atomicfile.Options{
		Pattern: ".companion-*",
	}); err != nil {
		return fmt.Errorf("publish evidence: %w", err)
	}
	return nil
}

func effectiveCapabilities(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read process status: %w", err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "CapEff:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "CapEff:")), nil
		}
	}
	return "", errors.New("process status has no CapEff line")
}

func namespace(dir, name string) (namespaceID, error) {
	info, err := os.Stat(filepath.Join(dir, name))
	if err != nil {
		return namespaceID{}, fmt.Errorf("stat %s namespace: %w", name, err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return namespaceID{}, fmt.Errorf("stat %s namespace: no inode", name)
	}
	return namespaceID{Device: uint64(stat.Dev), Inode: stat.Ino}, nil
}

func hexDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
