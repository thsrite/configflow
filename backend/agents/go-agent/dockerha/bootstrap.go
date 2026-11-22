package dockerha

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
	"unicode"
)

// BootstrapPaths are the files ha-bootstrap reads and writes. The defaults
// are the Compose bundle's container mounts.
type BootstrapPaths struct {
	Settings string // enrollment.json from the prepared state directory
	AgentDir string // persistent Agent directory
	Setup    string // owner written by prepare.sh
}

// DefaultBootstrapPaths are the mounts of the Compose bundle's agent service.
var DefaultBootstrapPaths = BootstrapPaths{
	Settings: "/run/configflow-compose/enrollment.json",
	AgentDir: "/opt/configflow-agent",
	Setup:    "/run/configflow-compose/setup",
}

var (
	ownerPattern      = regexp.MustCompile(`\A[0-9a-f]{64}\z`)
	identityPattern   = regexp.MustCompile(`\A[A-Za-z0-9_-]{16,256}\z`)
	bundleURLPattern  = regexp.MustCompile(`\Ahttps?://.+/api/agents/docker-oxidns-ha-enroll/[0-9a-f]{32}\z`)
	bundleTokenFormat = regexp.MustCompile(`\A[A-Za-z0-9_-]{43}\z`)
)

const (
	bundleMarker = ".docker-ha-installation.json"
	agentConfig  = "config-oxidns.json"
	bundleLock   = ".compose-enrollment.lock"
	// enrollTimeout bounds the one enrollment request.
	enrollTimeout = 30 * time.Second
	// responseLimit is the largest accepted enrollment answer.
	responseLimit = 16384
)

// identityDigest is the sha256 of {"agent_id":…,"token":…} (sorted, compact)
// after both values passed the identity format check.
func identityDigest(value map[string]any) (string, error) {
	var parts [2]string
	for i, key := range []string{"agent_id", "token"} {
		text, ok := value[key].(string)
		if !ok || !identityPattern.MatchString(text) {
			return "", fail(msgIdentityPartial)
		}
		parts[i] = text
	}
	// The format check leaves only characters JSON never escapes.
	sum := sha256.Sum256([]byte(`{"agent_id":"` + parts[0] + `","token":"` + parts[1] + `"}`))
	return hex.EncodeToString(sum[:]), nil
}

// hasSpace reports whitespace in the URL (any Unicode space, including the
// C0 information separators U+001C–U+001F).
func hasSpace(text string) bool {
	return strings.IndexFunc(text, func(r rune) bool {
		return unicode.IsSpace(r) || r >= 0x1c && r <= 0x1f
	}) >= 0
}

// Bootstrap is the first step of the Compose bundle's agent container: it
// verifies the prepared owner, then either confirms the persisted identity
// (offline restart) or performs the one scoped enrollment and saves the
// identity. Status lines go to out. A *Failure is a refusal; the service must
// not start.
func Bootstrap(ctx context.Context, paths BootstrapPaths, client *http.Client, out io.Writer) error {
	err := bootstrap(ctx, paths, client, out)
	var refusal *Failure
	if err != nil && !errors.As(err, &refusal) {
		return fail(msgSaveFailed)
	}
	return err
}

func bootstrap(ctx context.Context, paths BootstrapPaths, client *http.Client, out io.Writer) error {
	settings, err := readInstallJSON(paths.Settings)
	if err != nil {
		return err
	}
	owner, _ := settings["owner"].(string)
	if !ownerPattern.MatchString(owner) {
		return fail(msgOwnerInvalid)
	}
	setup, err := readRegular(paths.Setup, 65536)
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(setup)) != owner {
		return fail(msgNotPrepared)
	}
	if info, err := os.Lstat(paths.AgentDir); err != nil || !info.IsDir() {
		return fail(msgAgentDirInvalid)
	}
	lock, err := openLock(filepath.Join(paths.AgentDir, bundleLock))
	if err != nil {
		return err
	}
	defer lock.Close()

	configPath := filepath.Join(paths.AgentDir, agentConfig)
	markerPath := filepath.Join(paths.AgentDir, bundleMarker)
	var receipt map[string]any
	if present(markerPath) {
		if receipt, err = readInstallJSON(markerPath); err != nil {
			return err
		}
		if !sameValue(receipt["owner"], owner) || !sameValue(receipt["url"], settings["url"]) {
			return fail(msgForeignInstall)
		}
	}
	if present(configPath) {
		existing, err := readInstallJSON(configPath)
		if err != nil {
			return err
		}
		if len(receipt) == 0 {
			return fail(msgIdentityMismatch)
		}
		digest, err := identityDigest(existing)
		if err != nil {
			return err
		}
		if !sameValue(receipt["identity_digest"], digest) {
			return fail(msgIdentityMismatch)
		}
		fmt.Fprintln(out, msgIdentityVerified)
		return nil
	}
	if len(receipt) == 0 {
		// Reserve ownership before the HTTP request; retries retain its scope.
		if err := writeJSON(markerPath, []field{{"owner", owner}, {"url", settings["url"]}}, false); err != nil {
			return err
		}
	}
	url, _ := settings["url"].(string)
	if !bundleURLPattern.MatchString(url) || hasSpace(url) {
		return fail(msgURLInvalid)
	}
	token, _ := settings["token"].(string)
	if !bundleTokenFormat.MatchString(token) {
		return fail(msgTokenInvalid)
	}
	identity, err := enrollOnce(ctx, client, url, token)
	if err != nil {
		return err
	}
	digest, err := identityDigest(identity)
	if err != nil {
		return err
	}
	if len(receipt) > 0 && receipt["identity_digest"] != nil && !sameValue(receipt["identity_digest"], digest) {
		return fail(msgDifferentServer)
	}
	if err := writeJSON(markerPath, []field{{"owner", owner}, {"url", url}, {"identity_digest", digest}}, true); err != nil {
		return err
	}
	if err := writeJSON(configPath, []field{{"agent_id", identity["agent_id"]}, {"token", identity["token"]}}, false); err != nil {
		return err
	}
	fmt.Fprintln(out, msgIdentitySaved)
	return nil
}

// openLock opens and exclusively locks the enrollment lock file.
func openLock(path string) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_RDWR|syscall.O_CREAT|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open enrollment lock: %w", err)
	}
	file := os.NewFile(uintptr(fd), path)
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("stat enrollment lock: %w", err)
	}
	if !info.Mode().IsRegular() {
		file.Close()
		return nil, fail(msgLockNotRegular)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		file.Close()
		return nil, fmt.Errorf("lock enrollment: %w", err)
	}
	return file, nil
}

// BundleClient is the enrollment client of ha-bootstrap: no proxy, no
// redirects (the scoped bearer capability is never sent to another URL).
func BundleClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	return &http.Client{
		Transport: transport,
		Timeout:   enrollTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// enrollOnce posts {} with the bearer capability and returns the identity
// object of a successful answer.
func enrollOnce(ctx context.Context, client *http.Client, url, token string) (map[string]any, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader("{}"))
	if err != nil {
		return nil, fail(msgEnrollIncomplete)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return nil, fail(msgEnrollIncomplete)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return nil, fail(fmt.Sprintf(msgEnrollRejected, response.StatusCode))
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, responseLimit+1))
	if err != nil {
		return nil, fail(msgEnrollIncomplete)
	}
	if len(raw) > responseLimit {
		return nil, fail(msgResponseTooLarge)
	}
	identity, err := decodeObject(raw)
	if errors.Is(err, errNotJSON) {
		return nil, fail(msgEnrollIncomplete)
	}
	if err != nil || identity["success"] != true {
		return nil, fail(msgResponseInvalid)
	}
	return identity, nil
}

// readStdinObject decodes the one JSON object a generated command pipes in.
func readStdinObject(in io.Reader, limit int64) (map[string]any, error) {
	raw, err := io.ReadAll(io.LimitReader(in, limit+1))
	if err != nil || int64(len(raw)) > limit {
		return nil, errNotJSON
	}
	return decodeObject(bytes.TrimSpace(raw))
}

// jsonText is a decoded JSON string or number as text.
func jsonText(value any) (string, bool) {
	switch v := value.(type) {
	case string:
		return v, true
	case json.Number:
		return v.String(), true
	}
	return "", false
}
