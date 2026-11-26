package dockerha

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"agent/internal/atomicfile"
)

// The compact Compose installation (the panel's default, install_method
// compose_compact): every start of the Agent container runs `ha-compact`,
// which claims or verifies the three data directories for one owned
// installation, enrolls once, and then enters the ordinary OxiDNS entrypoint
// in Docker HA mode. Settings come from the container environment.

// CompactPaths are the Agent container's data directories.
type CompactPaths struct {
	AgentDir, CoreDir, HADir string
}

// DefaultCompactPaths are the compact Compose file's mounts.
var DefaultCompactPaths = CompactPaths{AgentDir: "/opt/configflow-agent", CoreDir: "/etc/oxidns", HADir: "/var/lib/configflow-ha"}

const compactLock = ".configflow-compose-compact.lock"

var (
	agentNamePattern  = regexp.MustCompile(`\A[A-Za-z0-9_-]+\z`)
	installKeyPattern = regexp.MustCompile(`\A[0-9a-f]{32}\.[A-Za-z0-9_-]{43}\z`)
)

// CompactSettings is the validated container environment.
type CompactSettings struct {
	ServerURL string
	Session   string
	Token     string
	Name      string
	IP        string
	Port      int
	Network   string // host or macvlan
}

// compactSettings validates SERVICE_TYPE, SERVER_URL, AGENT_IP, AGENT_PORT,
// AGENT_NAME and INSTALL_KEY.
func compactSettings(env func(string) (string, bool)) (CompactSettings, error) {
	get := func(key, fallback string) string {
		if value, ok := env(key); ok {
			return value
		}
		return fallback
	}
	role := get("SERVICE_TYPE", "")
	if role != "oxidns-ha-host" && role != "oxidns-ha-macvlan" {
		return CompactSettings{}, fail(msgCompactRole)
	}
	server := strings.TrimRight(get("SERVER_URL", ""), "/")
	parsed, urlErr := url.Parse(server)
	address, ipErr := netip.ParseAddr(get("AGENT_IP", ""))
	port, portErr := strconv.Atoi(strings.TrimSpace(get("AGENT_PORT", "8082")))
	if urlErr != nil || ipErr != nil || portErr != nil || !address.Is4() ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" ||
		(parsed.User != nil && (parsed.User.Username() != "" || hasPassword(parsed.User))) ||
		parsed.RawQuery != "" || parsed.Fragment != "" || hasSpace(server) ||
		address.IsUnspecified() || address.IsLoopback() || address.IsMulticast() || reservedIPv4(address) ||
		port < 1 || port > 65535 {
		return CompactSettings{}, fail(msgCompactAddress)
	}
	name := get("AGENT_NAME", "")
	if !agentNamePattern.MatchString(name) {
		return CompactSettings{}, fail(msgCompactName)
	}
	key := get("INSTALL_KEY", "")
	if key != "" && !installKeyPattern.MatchString(key) {
		return CompactSettings{}, fail(msgCompactKey)
	}
	session, token, _ := strings.Cut(key, ".")
	return CompactSettings{ServerURL: server, Session: session, Token: token, Name: name,
		IP: address.String(), Port: port, Network: strings.TrimPrefix(role, "oxidns-ha-")}, nil
}

func hasPassword(user *url.Userinfo) bool {
	password, set := user.Password()
	return set && password != ""
}

// reservedIPv4 is 240.0.0.0/4 (including the limited broadcast address).
func reservedIPv4(address netip.Addr) bool {
	return address.As4()[0] >= 240
}

// binding is the installation's identity: where it reports, as whom, on
// which address and network, and its enrollment scope.
func (s CompactSettings) binding(saved map[string]any) (map[string]any, error) {
	session := s.Session
	if session == "" {
		if saved == nil || saved["ready"] != true {
			return nil, fail(msgCompactNeedsKey)
		}
		if previous, ok := saved["binding"].(map[string]any); ok {
			session, _ = previous["session"].(string)
		}
	}
	if !hex32.MatchString(session) {
		return nil, fail(msgCompactNoScope)
	}
	return map[string]any{"server_url": s.ServerURL, "name": s.Name, "ip": s.IP, "port": s.Port,
		"network": s.Network, "session": session}, nil
}

func compactPlain(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fail(msgCompactMissing)
	}
	if !info.IsDir() {
		return fail(msgCompactNotPlain)
	}
	return nil
}

func checkCompactRecord(record map[string]any, owner, installationID string) error {
	id, _ := record["installation_id"].(string)
	if !isInteger(record["protocol"], 2) || !sameValue(record["owner"], owner) || !hex32.MatchString(id) ||
		(installationID != "" && id != installationID) {
		return fail(msgCompactForeignRecord)
	}
	return nil
}

// ignorable is an entry an unclaimed directory may hold: the installation
// lock (Agent directory only) or this owner's interrupted writes.
func ignorable(dir, agentDir, name, owner string) (bool, error) {
	if dir == agentDir && name == compactLock {
		_, err := readRegular(filepath.Join(dir, name), 65536)
		return err == nil, err
	}
	return ownedTemporary(filepath.Join(dir, name), owner)
}

// PrepareCompact claims or verifies the installation and returns the
// enrollment settings (owner, url, token) for enrollAgent.
func PrepareCompact(settings CompactSettings, paths CompactPaths) (map[string]any, error) {
	directories := []string{paths.AgentDir, paths.CoreDir, paths.HADir}
	for _, dir := range directories {
		if err := compactPlain(dir); err != nil {
			return nil, err
		}
	}
	marker := filepath.Join(paths.AgentDir, compactMarker)
	saved, err := readOptionalJSON(marker)
	if err != nil {
		return nil, err
	}
	binding, err := settings.binding(saved)
	if err != nil {
		return nil, err
	}
	owner, _ := bindingOwner(binding)
	// Reject foreign data before even creating a lock.
	if saved == nil {
		for _, dir := range directories {
			entries, err := dirEntries(dir)
			if err != nil {
				return nil, err
			}
			for _, entry := range entries {
				ok, err := ignorable(dir, paths.AgentDir, entry.Name(), owner)
				if err != nil {
					return nil, err
				}
				if ok {
					continue
				}
				// A concurrent first start may have claimed the directory
				// since the first read; its marker is checked under the lock.
				if present(marker) {
					break
				}
				return nil, fail(msgCompactHasData)
			}
		}
	}
	lock, err := openCompactLock(filepath.Join(paths.AgentDir, compactLock))
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	if saved, err = readOptionalJSON(marker); err != nil {
		return nil, err
	}
	if saved == nil {
		for _, dir := range directories {
			entries, err := dirEntries(dir)
			if err != nil {
				return nil, err
			}
			for _, entry := range entries {
				ok, err := ignorable(dir, paths.AgentDir, entry.Name(), owner)
				if err != nil {
					return nil, err
				}
				if !ok {
					return nil, fail(msgCompactUnknown)
				}
			}
		}
		var id [16]byte
		if _, err := rand.Read(id[:]); err != nil {
			return nil, fmt.Errorf("installation id: %w", err)
		}
		saved = map[string]any{"protocol": 2, "owner": owner, "binding": binding,
			"installation_id": hex.EncodeToString(id[:]), "ready": false}
		if err := composeFlavor.write(marker, recordFields(saved), false); err != nil {
			return nil, err
		}
	}
	if err := checkCompactRecord(saved, owner, ""); err != nil {
		return nil, err
	}
	if previous, ok := saved["binding"].(map[string]any); !ok || !sameBinding(previous, owner) {
		return nil, fail(msgCompactBinding)
	}
	installationID := saved["installation_id"].(string)
	complete := saved["ready"] == true
	for _, dir := range directories {
		recordPath := filepath.Join(dir, compactMarker)
		record, err := readOptionalJSON(recordPath)
		if err != nil {
			return nil, err
		}
		if record != nil {
			if err := checkCompactRecord(record, owner, installationID); err != nil {
				return nil, err
			}
		} else if complete {
			return nil, fail(msgCompactRecordLost)
		}
		if complete {
			continue
		}
		entries, err := dirEntries(dir)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if entry.Name() == compactMarker {
				continue
			}
			ok, err := ignorable(dir, paths.AgentDir, entry.Name(), owner)
			if err != nil {
				return nil, err
			}
			if ok {
				continue
			}
			if dir == paths.AgentDir && entry.Name() == compactIdentity {
				raw, err := readRegular(filepath.Join(dir, entry.Name()), 65536)
				if err != nil {
					return nil, err
				}
				if string(trimASCII(raw)) == installationID {
					continue
				}
			}
			return nil, fail(msgCompactPendingData)
		}
		if record == nil {
			fields := []field{{"protocol", saved["protocol"]}, {"owner", saved["owner"]}, {"installation_id", installationID}}
			if err := composeFlavor.write(recordPath, fields, false); err != nil {
				return nil, err
			}
		}
	}
	identityPath := filepath.Join(paths.AgentDir, compactIdentity)
	switch {
	case present(identityPath):
		raw, err := readRegular(identityPath, 65536)
		if err != nil {
			return nil, err
		}
		if string(trimASCII(raw)) != installationID {
			return nil, fail(msgCompactIdentityDiffers)
		}
	case complete:
		return nil, fail(msgCompactIdentityLost)
	default:
		if err := writeIdentity(identityPath, installationID, owner); err != nil {
			return nil, err
		}
	}
	if !complete {
		// The companion may start only after all three directories and the
		// immutable installation identity are claimed consistently.
		saved["ready"] = true
		if err := composeFlavor.write(marker, recordFields(saved), true); err != nil {
			return nil, err
		}
	}
	return map[string]any{"owner": owner,
		"url":   binding["server_url"].(string) + "/api/agents/docker-oxidns-ha-enroll/" + binding["session"].(string),
		"token": settings.Token}, nil
}

// sameBinding compares a stored binding with the current one through the
// owner digest (string and integer values only).
func sameBinding(stored map[string]any, owner string) bool {
	digest, ok := bindingOwner(stored)
	return ok && digest == owner
}

// recordFields orders an installation record: the known members first,
// then any other members by name.
func recordFields(record map[string]any) []field {
	known := []string{"protocol", "owner", "binding", "installation_id", "ready"}
	seen := map[string]bool{}
	var fields []field
	for _, key := range known {
		if value, ok := record[key]; ok {
			fields = append(fields, field{key, value})
			seen[key] = true
		}
	}
	var rest []string
	for key := range record {
		if !seen[key] {
			rest = append(rest, key)
		}
	}
	sort.Strings(rest)
	for _, key := range rest {
		fields = append(fields, field{key, record[key]})
	}
	return fields
}

func openCompactLock(path string) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_RDWR|syscall.O_CREAT|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open installation lock: %w", err)
	}
	file := os.NewFile(uintptr(fd), path)
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		if err != nil {
			return nil, fmt.Errorf("stat installation lock: %w", err)
		}
		return nil, fail(msgCompactLockIrregular)
	}
	if err := syscall.Flock(fd, syscall.LOCK_EX); err != nil {
		file.Close()
		return nil, fmt.Errorf("lock installation: %w", err)
	}
	return file, nil
}

// writeIdentity exclusively publishes the installation identity file.
func writeIdentity(path, value, owner string) error {
	if err := atomicfile.WriteWith(path, []byte(value+"\n"), 0o600, atomicfile.Options{
		CreateTemp: func(dir string) (*os.File, error) { return createTemp(dir, ownerTempPrefix(owner)) },
		Exclusive:  true,
	}); err != nil {
		return fmt.Errorf("publish installation identity: %w", err)
	}
	return nil
}

// RunCompact verifies or claims the installation and enrolls the Agent.
func RunCompact(ctx context.Context, settings CompactSettings, paths CompactPaths, client *http.Client, out io.Writer) error {
	enrollment, err := PrepareCompact(settings, paths)
	if err == nil {
		err = enrollAgent(ctx, paths.AgentDir, enrollment, client, out, composeFlavor)
	}
	// The compact installation words an unreadable file for its mounts.
	var refusal *Failure
	if errors.As(err, &refusal) && refusal.Message == msgFileUnreadable {
		return fail(msgCompactUnreadable)
	}
	if err != nil {
		return err
	}
	fmt.Fprintln(out, msgCompactStarting)
	return nil
}

// CompactEnvironment is the environment the entrypoint continues with: the
// installation key removed, Docker HA OxiDNS mode selected.
func CompactEnvironment(environ []string, network string) []string {
	drop := map[string]bool{"INSTALL_KEY": true, "SERVICE_TYPE": true, "CONFIGFLOW_DNS_HA": true,
		"CONFIGFLOW_DNS_HA_NETWORK": true, "CONFIGFLOW_DNS_HA_IDENTITY": true,
		"ENABLE_MIHOMO": true, "ENABLE_MOSDNS": true, "ENABLE_OXIDNS": true}
	var out []string
	for _, entry := range environ {
		key, _, _ := strings.Cut(entry, "=")
		if !drop[key] {
			out = append(out, entry)
		}
	}
	return append(out, "SERVICE_TYPE=oxidns", "CONFIGFLOW_DNS_HA=1", "CONFIGFLOW_DNS_HA_NETWORK="+network,
		"CONFIGFLOW_DNS_HA_IDENTITY=installation", "ENABLE_MIHOMO=false", "ENABLE_MOSDNS=false", "ENABLE_OXIDNS=true")
}
