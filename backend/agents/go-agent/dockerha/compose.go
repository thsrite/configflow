package dockerha

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"syscall"
	"unicode/utf8"

	"agent/internal/atomicfile"
)

// The Compose installation without the bundle (install_method
// compose_direct): an init container runs `ha-compose prepare` on the data
// root, then the Agent container runs `ha-compose run` before the normal
// entrypoint. Settings come from CONFIGFLOW_COMPOSE_INSTALL.

// DirectPaths are the container paths of ha-compose.
type DirectPaths struct {
	DataRoot     string // data root of the init container (prepare)
	AgentDir     string // Agent directory of the Agent container (run)
	IdentityDir  string // shared Docker volume holding host-id
	IdentityFile string // bind-mounted machine id
}

// DefaultDirectPaths are the generated Compose file's mounts.
var DefaultDirectPaths = DirectPaths{DataRoot: "/configflow-data", AgentDir: "/opt/configflow-agent",
	IdentityDir: "/run/configflow-host-identity", IdentityFile: "/run/configflow-host-machine-id"}

const (
	directMarker        = ".configflow-compose-direct.json"
	directSettingsLimit = 16384
	identityLimit       = 65536
	layoutFileSize      = 1024 * 1024
)

// directSettings validates CONFIGFLOW_COMPOSE_INSTALL.
func directSettings(raw string) (map[string]any, error) {
	if utf8.RuneCountInString(raw) > directSettingsLimit {
		return nil, fail(msgDirectSettingsTooLarge)
	}
	settings, err := decodeObject([]byte(raw))
	if errors.Is(err, errNotJSON) {
		return nil, fail(msgDirectSettingsInvalid)
	}
	owner, _ := settings["owner"].(string)
	mode, _ := settings["identity_mode"].(string)
	_, urlOK := settings["url"].(string)
	if err != nil || !isInteger(settings["protocol"], 1) || !ownerPattern.MatchString(owner) ||
		(mode != "file" && mode != "docker-volume") || !urlOK {
		return nil, fail(msgDirectSettingsProtocol)
	}
	return settings, nil
}

var setupKeys = []string{"protocol", "owner", "url", "identity_mode"}

func sameSetup(record, settings map[string]any) bool {
	for _, key := range setupKeys {
		if !sameValue(record[key], settings[key]) {
			return false
		}
	}
	return true
}

func setupFields(settings map[string]any, hostID string, ready bool) []field {
	var fields []field
	for _, key := range setupKeys {
		fields = append(fields, field{key, settings[key]})
	}
	return append(fields, field{"host_id", hostID}, field{"ready", ready})
}

// directPlain is the init container's plain-directory check.
func directPlain(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fail(msgDirectMissing)
	}
	if !info.IsDir() {
		return fail(msgDirectNotPlain)
	}
	return nil
}

// ownedTemporary reports an interrupted atomic write of this owner. Such a
// file may be incomplete; it is kept (but must still be a regular file).
func ownedTemporary(path, owner string) (bool, error) {
	pattern := regexp.MustCompile(`\A` + regexp.QuoteMeta(ownerTempPrefix(owner)) + `[a-z0-9_]{8}\z`)
	if !pattern.MatchString(filepath.Base(path)) {
		return false, nil
	}
	if _, err := readRegular(path, 65536); err != nil {
		return false, err
	}
	return true, nil
}

func readOptionalJSON(path string) (map[string]any, error) {
	if !present(path) {
		return nil, nil
	}
	return readInstallJSON(path)
}

func dirEntries(dir string) ([]os.DirEntry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", filepath.Base(dir), err)
	}
	return entries, nil
}

// checkLayout accepts empty directories pre-created by Docker; anything
// else needs this installation's record.
func checkLayout(root string, settings map[string]any) (map[string]any, error) {
	if err := directPlain(root); err != nil {
		return nil, err
	}
	record, err := readOptionalJSON(filepath.Join(root, directMarker))
	if err != nil {
		return nil, err
	}
	if len(record) > 0 && !sameSetup(record, settings) {
		return nil, fail(msgDirectForeign)
	}
	entries, err := dirEntries(root)
	if err != nil {
		return nil, err
	}
	owner := settings["owner"].(string)
	for _, entry := range entries {
		path := filepath.Join(root, entry.Name())
		if owned, err := ownedTemporary(path, owner); err != nil || owned {
			if err != nil {
				return nil, err
			}
			continue
		}
		switch entry.Name() {
		case "agent", "oxidns", "ha":
			if err := directPlain(path); err != nil {
				return nil, err
			}
			children, err := dirEntries(path)
			if err != nil {
				return nil, err
			}
			if len(record) == 0 && len(children) > 0 {
				return nil, fail(msgDirectSubdirFull)
			}
		case "compose.yaml", directMarker:
			if _, err := readRegular(path, layoutFileSize); err != nil {
				return nil, err
			}
		default:
			return nil, fail(msgDirectUnknown)
		}
	}
	return record, nil
}

// hostIdentity returns the digest of the selected host identity; with
// create, an empty Docker identity volume gets a random identity.
func hostIdentity(settings map[string]any, paths DirectPaths, create bool) (string, error) {
	var path string
	if settings["identity_mode"] == "file" {
		path = paths.IdentityFile
	} else {
		if err := directPlain(paths.IdentityDir); err != nil {
			return "", err
		}
		path = filepath.Join(paths.IdentityDir, "host-id")
		if !present(path) && create {
			if err := createHostIdentity(paths.IdentityDir, path); err != nil {
				return "", err
			}
		}
	}
	raw, err := readRegular(path, identityLimit)
	if err != nil {
		return "", err
	}
	value := trimASCII(raw)
	if len(value) < 16 || len(value) > 128 || containsLineBreak(value) {
		return "", fail(msgDirectHostID)
	}
	return hexDigest(value), nil
}

func createHostIdentity(dir, path string) error {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return fmt.Errorf("random host identity: %w", err)
	}
	// Another project may have initialized this shared Docker volume.
	err := atomicfile.WriteWith(path, []byte(hex.EncodeToString(random[:])+"\n"), 0o600, atomicfile.Options{
		CreateTemp: func(d string) (*os.File, error) { return createTemp(d, ".host-id-") },
		Exclusive:  true,
	})
	if errors.Is(err, os.ErrExist) {
		return syncDir(dir)
	}
	if err != nil {
		return fmt.Errorf("publish host identity: %w", err)
	}
	return nil
}

// PrepareDirect is `ha-compose prepare` in the init container.
func PrepareDirect(settings map[string]any, paths DirectPaths, out io.Writer) error {
	root := paths.DataRoot
	record, err := checkLayout(root, settings)
	if err != nil {
		return err
	}
	// Identity failures must not reserve an otherwise unused data directory.
	hostID, err := hostIdentity(settings, paths, true)
	if err != nil {
		return err
	}
	marker := filepath.Join(root, directMarker)
	owner := composeFlavor
	if len(record) == 0 {
		if err := owner.write(marker, setupFields(settings, hostID, false), false); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
	}
	lock, err := lockExisting(marker)
	if err != nil {
		return err
	}
	defer lock.Close()
	if record, err = checkLayout(root, settings); err != nil {
		return err
	}
	if len(record) == 0 || !sameValue(record["host_id"], hostID) {
		return fail(msgDirectHostMismatch)
	}
	for _, name := range []string{"agent", "oxidns", "ha"} {
		dir := filepath.Join(root, name)
		if !present(dir) {
			if err := os.Mkdir(dir, 0o700); err != nil {
				return fmt.Errorf("create %s: %w", name, err)
			}
		}
		if err := directPlain(dir); err != nil {
			return err
		}
	}
	setup := filepath.Join(root, "agent", directMarker)
	switch {
	case present(setup):
		saved, err := readInstallJSON(setup)
		if err != nil {
			return err
		}
		if !sameSetup(saved, settings) || !sameValue(saved["host_id"], hostID) || saved["ready"] != true {
			return fail(msgDirectSetupBroken)
		}
	case truthyJSON(record["ready"]):
		return fail(msgDirectSetupLost)
	default:
		// Before completion only empty core/HA dirs and our residue are allowed.
		for _, name := range []string{"oxidns", "ha"} {
			entries, err := dirEntries(filepath.Join(root, name))
			if err != nil {
				return err
			}
			if len(entries) > 0 {
				return fail(msgDirectPending)
			}
		}
		entries, err := dirEntries(filepath.Join(root, "agent"))
		if err != nil {
			return err
		}
		for _, entry := range entries {
			owned, err := ownedTemporary(filepath.Join(root, "agent", entry.Name()), settings["owner"].(string))
			if err != nil {
				return err
			}
			if !owned {
				return fail(msgDirectPending)
			}
		}
		if err := owner.write(setup, setupFields(settings, hostID, true), false); err != nil {
			return err
		}
	}
	if record["ready"] != true {
		if err := owner.write(marker, setupFields(settings, hostID, true), true); err != nil {
			return err
		}
	}
	fmt.Fprintln(out, msgDirectPrepared)
	return nil
}

// RunDirect is `ha-compose run` in the Agent container: the prepared
// installation and host identity are verified, then the Agent enrolls (or
// confirms its saved identity).
func RunDirect(ctx context.Context, settings map[string]any, paths DirectPaths, client *http.Client, out io.Writer) error {
	setup, err := readInstallJSON(filepath.Join(paths.AgentDir, directMarker))
	if err != nil {
		return err
	}
	if !sameSetup(setup, settings) || setup["ready"] != true {
		return fail(msgDirectNotReady)
	}
	hostID, err := hostIdentity(settings, paths, false)
	if err != nil {
		return err
	}
	if !sameValue(setup["host_id"], hostID) {
		return fail(msgDirectRunHost)
	}
	return enrollAgent(ctx, paths.AgentDir, settings, client, out, composeFlavor)
}

// lockExisting opens an existing record without following a symlink and
// takes an exclusive lock on it.
func lockExisting(path string) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_RDWR|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", filepath.Base(path), err)
	}
	file := os.NewFile(uintptr(fd), path)
	if err := syscall.Flock(fd, syscall.LOCK_EX); err != nil {
		file.Close()
		return nil, fmt.Errorf("lock %s: %w", filepath.Base(path), err)
	}
	return file, nil
}

// truthyJSON is JSON truthiness of a decoded value.
func truthyJSON(value any) bool {
	switch v := value.(type) {
	case nil:
		return false
	case bool:
		return v
	case string:
		return v != ""
	case json.Number:
		f, err := v.Float64()
		return err != nil || f != 0
	case []any:
		return len(v) > 0
	case map[string]any:
		return len(v) > 0
	}
	return true
}

func trimASCII(raw []byte) []byte {
	start, end := 0, len(raw)
	for start < end && isASCIISpace(raw[start]) {
		start++
	}
	for end > start && isASCIISpace(raw[end-1]) {
		end--
	}
	return raw[start:end]
}

func isASCIISpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\v' || b == '\f'
}

func containsLineBreak(value []byte) bool {
	for _, b := range value {
		if b == '\n' || b == '\r' {
			return true
		}
	}
	return false
}
