package dockerha

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// settingsLimit bounds the settings object a generated command pipes in.
const settingsLimit = 65536

// EnrollClient is the enrollment client of the script install (ha-enroll):
// proxies from the environment and standard redirects, which drop the
// Authorization header when they leave the original host.
func EnrollClient() *http.Client {
	return &http.Client{Timeout: enrollTimeout}
}

// Enroll is the script install's one enrollment in a temporary container:
// settings {"url","token"} arrive on stdin, the identity is written to
// agentDir/config-oxidns.json beside the installation marker. Re-running
// the same command with the same identity keeps the files.
func Enroll(ctx context.Context, agentDir string, in io.Reader, client *http.Client, out io.Writer) error {
	settings, err := readStdinObject(in, settingsLimit)
	if err != nil {
		return fail(msgSettings)
	}
	url, urlOK := settings["url"].(string)
	token, tokenOK := settings["token"].(string)
	if !urlOK || !tokenOK || url == "" {
		return fail(msgSettings)
	}
	configPath := filepath.Join(agentDir, agentConfig)
	markerPath := filepath.Join(agentDir, bundleMarker)
	if isSymlink(configPath) || isSymlink(markerPath) {
		return fail(msgEnrollSymlink)
	}
	markerExists := exists(markerPath)
	if markerExists {
		raw, readErr := os.ReadFile(markerPath)
		marker, decodeErr := decodeObject(raw)
		if readErr != nil || decodeErr != nil || !sameValue(marker["url"], url) {
			return fail(msgEnrollForeign)
		}
	} else if exists(configPath) {
		return fail(msgEnrollExisting)
	}
	identity, err := enrollScript(ctx, client, url, token)
	if err != nil {
		return err
	}
	if exists(configPath) {
		raw, err := os.ReadFile(configPath)
		if err != nil {
			return fail(msgEnrollUnreadable)
		}
		existing, err := decodeObject(raw)
		if err != nil {
			return fail(msgEnrollUnreadable)
		}
		for _, key := range []string{"agent_id", "token"} {
			if !sameValue(existing[key], identity[key]) {
				return fail(msgEnrollDiffers)
			}
		}
		fmt.Fprintln(out, msgEnrollKept)
		return nil
	}
	if !markerExists {
		if err := createExclusive(markerPath, []field{{"url", url}}); err != nil {
			return fail(msgSaveFailed)
		}
	}
	if err := createExclusive(configPath, []field{{"agent_id", identity["agent_id"]}, {"token", identity["token"]}}); err != nil {
		return fail(msgSaveFailed)
	}
	fmt.Fprintln(out, msgEnrollDone)
	return nil
}

func enrollScript(ctx context.Context, client *http.Client, url, token string) (map[string]any, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader("{}"))
	if err != nil {
		return nil, fail(msgEnrollUnreachable)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return nil, fail(msgEnrollUnreachable)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode < 200 || response.StatusCode > 299 {
		message := msgEnrollFailedDefault
		if body, decodeErr := decodeObject(raw); err == nil && decodeErr == nil {
			if text, ok := body["message"].(string); ok {
				message = text
			}
		}
		return nil, fail(fmt.Sprintf(msgEnrollRetry, message))
	}
	if err != nil {
		return nil, fail(msgEnrollUnreachable)
	}
	identity, err := decodeObject(raw)
	if errors.Is(err, errNotJSON) {
		return nil, fail(msgEnrollUnreachable)
	}
	if err != nil {
		return nil, fail(msgEnrollInvalid)
	}
	for _, key := range []string{"agent_id", "token"} {
		if text, ok := identity[key].(string); !ok || text == "" {
			return nil, fail(msgEnrollInvalid)
		}
	}
	return identity, nil
}

// createExclusive writes a new 0600 file; it never replaces an existing one.
func createExclusive(path string, fields []field) error {
	data, err := encodeFields(fields)
	if err != nil {
		return fmt.Errorf("encode %s: %w", filepath.Base(path), err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create %s: %w", filepath.Base(path), err)
	}
	_, err = file.Write(data)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	return nil
}

// CheckEnrollment is the re-run proof of a script install whose main
// container already exists: the saved identity and marker must belong to
// this installation (enrollment URL) and hold real credentials.
func CheckEnrollment(agentDir, url string, out io.Writer) error {
	configPath := filepath.Join(agentDir, agentConfig)
	markerPath := filepath.Join(agentDir, bundleMarker)
	if isSymlink(configPath) || isSymlink(markerPath) || !isRegularFile(configPath) || !isRegularFile(markerPath) {
		return fail(msgProofMissing)
	}
	rawConfig, err1 := os.ReadFile(configPath)
	rawMarker, err2 := os.ReadFile(markerPath)
	if err1 != nil || err2 != nil {
		return fail(msgProofUnreadable)
	}
	config, err1 := decodeObject(rawConfig)
	marker, err2 := decodeObject(rawMarker)
	if errors.Is(err1, errNotJSON) || errors.Is(err2, errNotJSON) {
		return fail(msgProofUnreadable)
	}
	if err1 != nil || err2 != nil || !sameValue(marker["url"], url) {
		return fail(msgProofMismatch)
	}
	for _, key := range []string{"agent_id", "token"} {
		text, ok := config[key].(string)
		if !ok || text == "" || text == "[REDACTED]" {
			return fail(msgProofMismatch)
		}
	}
	fmt.Fprintln(out, msgProofVerified)
	return nil
}
