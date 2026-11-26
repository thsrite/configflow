package mosdns

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// W11 regression: an undeclared rule-proxy URL reached rulefetch without a
// source configuration or repository and was answered 400 "Invalid remote
// URL" before any download. It must be downloaded with the global settings.
func TestRuleProxyUndeclaredURLDownloads(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("example.com\n"))
	}))
	t.Cleanup(upstream.Close)
	ctx, r := testRepo(t, map[string]any{})
	response := RuleProxy(ctx, r, RuleProxyRequest{URL: upstream.URL + "/undeclared.list", Auth: AuthRequest{Internal: true}})
	if response.Status != 200 || response.Text == "" {
		t.Fatalf("status %d text %q json %v", response.Status, response.Text, response.JSON)
	}
}
