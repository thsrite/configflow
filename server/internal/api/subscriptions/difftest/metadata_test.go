package difftest

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"configflow/internal/httpx/httpxtest"
)

func TestConfigFindingsProviderMetadataAndServedFile(t *testing.T) {
	w := newWorld(t, seedGraph(t, "", false, false), map[string]string{"s": "proxies: [{name: Synthetic, type: http, server: example.com, port: 80}]"})
	for _, path := range []string{"/api/subscriptions/s/proxies?token=c30-pull", "/api/profiles/default/subscriptions/s/proxies?token=c30-pull", "/api/subscriptions/proxies?token=c30-pull"} {
		res := w.srv.Get(path, httpxtest.Bearer(strings.TrimPrefix(w.token, "Bearer "))).WantStatus(200)
		text := string(res.Body)
		for _, prefix := range []string{"# ConfigFlow Subscription Proxies\n", "# Generated: "} {
			if !strings.Contains(text, prefix) {
				t.Errorf("%s missing %q", path, prefix)
			}
		}
		matches := regexp.MustCompile(`(?m)^# Generated: (.+)$`).FindAllStringSubmatch(text, -1)
		if len(matches) != 1 {
			t.Errorf("%s Generated count=%d", path, len(matches))
			continue
		}
		// The existing protocol appends Z to local wall time, including non-UTC TZ.
		if _, err := time.Parse("2006-01-02T15:04:05.999999Z", matches[0][1]); err != nil {
			t.Errorf("invalid timestamp: %v", err)
		}
		if strings.Contains(path, "/s/proxies") {
			for _, line := range []string{"# Subscription: Feed\n", "# ID: s\n", "# Proxies: 1\n", "# Source: "} {
				if !strings.Contains(text, line) {
					t.Errorf("missing %q", line)
				}
			}
		} else {
			for _, line := range []string{"# Subscriptions: 1\n", "# Total Proxies: 1\n", "# Subscription Details:\n", "#   - Feed: 1 proxies (updated: "} {
				if !strings.Contains(text, line) {
					t.Errorf("missing %q", line)
				}
			}
		}
	}
	var served int
	err := filepath.WalkDir(w.srv.DataDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.Contains(filepath.ToSlash(path), "providers/served/") && strings.HasSuffix(path, ".yaml") {
			served++
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if !strings.Contains(string(data), "# Subscription: Feed\n") {
				t.Errorf("served file has no metadata: %s", path)
			}
		}
		return nil
	})
	if err != nil || served == 0 {
		t.Fatalf("served files=%d err=%v", served, err)
	}
}
