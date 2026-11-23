package routes

import (
	"agent/internal/logx"
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestMosdnsCacheDumpFile(t *testing.T) {
	tests := []struct {
		name     string
		config   string
		expected string
	}{
		{
			name: "persistence enabled",
			config: `plugins:
  - tag: lazy_cache
    type: cache
    args:
      size: 10240
      dump_file: ./cache.dump
      dump_interval: 300
`,
			expected: "./cache.dump",
		},
		{
			name: "persistence disabled",
			config: `plugins:
  - tag: lazy_cache
    type: cache
    args:
      size: 10240
`,
			expected: "",
		},
		{
			name: "cache disabled",
			config: `plugins:
  - tag: forward_local
    type: forward
`,
			expected: "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual, err := mosdnsCacheDumpFile(test.config)
			if err != nil {
				t.Fatalf("mosdnsCacheDumpFile returned an error: %v", err)
			}
			if actual != test.expected {
				t.Fatalf("mosdnsCacheDumpFile() = %q, want %q", actual, test.expected)
			}
		})
	}
}

func TestProviderAndRulesetDownloadLogsRedactURLCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("payload"))
	}))
	defer server.Close()

	const rawToken = "raw-secret-token"
	const encodedToken = "encoded%2520secret%252Ftoken"
	downloadURL := strings.Replace(server.URL, "://", "://user:password@", 1) +
		"/download?config_token=" + rawToken + "&authorization=" + encodedToken

	var logs bytes.Buffer
	previousWriter := logx.SetOutput(&logs)
	t.Cleanup(func() { logx.SetOutput(previousWriter) })

	dir := t.TempDir()
	if err := downloadProvider(dir, ProviderDownloadItem{
		Name: "provider", URL: downloadURL, LocalPath: filepath.Join("providers", "one.yaml"),
	}); err != nil {
		t.Fatalf("downloadProvider returned error: %v", err)
	}
	if err := downloadRuleset(dir, RulesetDownloadItem{
		Name: "ruleset", URL: downloadURL, LocalPath: filepath.Join("ruleset", "one.yaml"),
	}); err != nil {
		t.Fatalf("downloadRuleset returned error: %v", err)
	}

	got := logs.String()
	for _, secret := range []string{rawToken, encodedToken, "user", "password", rawToken[:8]} {
		if strings.Contains(got, secret) {
			t.Fatalf("download logs leaked URL credential %q: %s", secret, got)
		}
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("download logs do not show redaction marker: %s", got)
	}
}
