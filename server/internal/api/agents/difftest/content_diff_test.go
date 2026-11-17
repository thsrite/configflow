package difftest

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	apiagents "configflow/internal/api/agents"
	"configflow/internal/deploy"
)

// This supplements the independently owned black-box matrix with A13's
// read-only HTTP boundary checks. No fields are masked in these comparisons.
func TestContentReadsAgainstFlask(t *testing.T) {
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("set CONFIGFLOW_DIFF=1 on the test server")
	}
	base := t.TempDir()
	seedDir, pyDir, goDir := filepath.Join(base, "seed"), filepath.Join(base, "python"), filepath.Join(base, "go")
	for _, dir := range []string{seedDir, pyDir, goDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	seed(t, seedDir)
	copyDir(t, seedDir, pyDir)
	copyDir(t, seedDir, goDir)
	pyBase, stopPython := startPython(t, pyDir)
	defer stopPython()
	goBase, stopGo := startGo(t, goDir)
	defer stopGo()
	pyToken, goToken := login(t, pyBase), login(t, goBase)
	page := "/api/agents/agent-1/push-diff/entries?version=x&file=rules%2Fsynthetic.list&change=added"
	cases := []request{
		{name: "list summary", path: "/api/agents?summary=1"},
		{name: "list history omitted", path: "/api/agents?history=0"},
		{name: "list history", path: "/api/agents"},
		{name: "single public record", path: "/api/agents/agent-1"},
		{name: "pending record", path: "/api/agents/agent-busy"},
		{name: "upgrading public record", path: "/api/agents/agent-upgrading"},
		{name: "bound profile ignores header", path: "/api/agents/agent-1", headers: map[string]string{"X-ConfigFlow-Profile": "other"}},
		{name: "missing record", path: "/api/agents/missing"},
		{name: "never pushed", path: "/api/agents/agent-1/push-diff"},
		{name: "pending push", path: "/api/agents/agent-busy/push-diff"},
		{name: "missing push record", path: "/api/agents/missing/push-diff"},
		{name: "expired page", path: page},
		{name: "empty page numbers", path: page + "&offset=&limit="},
		{name: "leading zero numbers", path: page + "&offset=000&limit=050"},
		{name: "missing page record", path: strings.Replace(page, "agent-1", "missing", 1)},
		{name: "validation precedes lookup", path: "/api/agents/missing/push-diff/entries"},
		{name: "empty arguments", path: "/api/agents/agent-1/push-diff/entries"},
		{name: "unknown group", path: strings.Replace(page, "change=added", "change=other", 1)},
		{name: "long version", path: strings.Replace(page, "version=x", "version="+strings.Repeat("x", 65), 1)},
		{name: "CJK query", path: page + "&q=" + url.QueryEscape(strings.Repeat("中", 200))},
		{name: "CJK query too long", path: page + "&q=" + url.QueryEscape(strings.Repeat("中", 201))},
	}
	for _, value := range []string{"-1", "+1", "1_0", "１００", "10000001"} {
		cases = append(cases, request{name: "bad offset " + value, path: page + "&offset=" + url.QueryEscape(value)})
	}
	for _, value := range []string{"0", "201", "bad"} {
		cases = append(cases, request{name: "bad limit " + value, path: page + "&limit=" + value})
	}
	for _, path := range []string{"/api/agents", "/api/agents/agent-1", "/api/agents/agent-1/push-diff", page} {
		cases = append(cases, request{name: "unauthorized", method: "GET", path: path})
	}
	for _, r := range cases {
		if r.method == "" {
			r.method, r.admin = "GET", true
		}
		t.Run(r.name, func(t *testing.T) {
			py := send(t, pyBase, r, pyToken, nil)
			goReply := send(t, goBase, r, goToken, nil)
			if diffs := compare(r, py, goReply); len(diffs) != 0 {
				t.Fatalf("%s: %s", r.path, strings.Join(diffs, "\n"))
			}
			t.Logf("same %s → %d", r.path, py.status)
		})
	}
}

func TestContentChangesAgainstFlask(t *testing.T) {
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("set CONFIGFLOW_DIFF=1 on the test server")
	}
	base := t.TempDir()
	seedDir, pyDir, goDir := filepath.Join(base, "seed"), filepath.Join(base, "python"), filepath.Join(base, "go")
	for _, dir := range []string{seedDir, pyDir, goDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	seed(t, seedDir)
	seedContentBaseline(t, seedDir)
	copyDir(t, seedDir, pyDir)
	copyDir(t, seedDir, goDir)
	pyBase, stopPython := startPythonArgs(t, pyDir, "server/internal/api/agents/difftest/testdata/content_app.py")
	defer stopPython()
	candidate := contentDiffBundle(t, true)
	restore := apiagents.SetContentPrepare(func(_ context.Context, _ deploy.Target, _ *deploy.Source, origin, id string) (*deploy.Bundle, error) {
		if origin != "https://original.example.test/prefix" || !strings.HasPrefix(id, "check-") {
			t.Error("preparation lost publication identity")
		}
		return candidate, nil
	})
	defer restore()
	goBase, stopGo := startGo(t, goDir)
	defer stopGo()
	pyToken, goToken := login(t, pyBase), login(t, goBase)
	diffRequest := request{name: "full changed comparison", method: "GET", path: "/api/agents/agent-1/push-diff", admin: true, mask: []string{"checked_at", "version"}}
	settle := func(base, token string) (response, string) {
		deadline := time.Now().Add(10 * time.Second)
		for {
			r := send(t, base, diffRequest, token, nil)
			var v struct {
				Status, Version string
				CheckedAt       string `json:"checked_at"`
				Content         string
			}
			if err := json.Unmarshal(r.body, &v); err != nil || r.status != 200 {
				t.Fatalf("invalid comparison: %d %s", r.status, r.body)
			}
			if v.Status != "checking" {
				if v.Status != "changed" || v.Content != "full" || len(v.Version) != 16 || v.CheckedAt == "" {
					t.Fatalf("expected full comparison: %s", r.body)
				}
				if strings.Contains(string(r.body), agentToken) || !strings.Contains(string(r.body), "[REDACTED]") {
					t.Fatalf("stored credential escaped: %s", r.body)
				}
				return r, v.Version
			}
			if time.Now().After(deadline) {
				t.Fatal("comparison did not settle")
			}
			time.Sleep(time.Millisecond)
		}
	}
	pyReply, pyVersion := settle(pyBase, pyToken)
	goReply, goVersion := settle(goBase, goToken)
	if diffs := compare(diffRequest, pyReply, goReply); len(diffs) != 0 {
		t.Fatal(strings.Join(diffs, "\n"))
	}
	pyValues, goValues := map[string]string{"version": pyVersion}, map[string]string{"version": goVersion}
	page := "/api/agents/agent-1/push-diff/entries?version={version}&file=rules%2Fexample.list&change=added"
	cases := []request{
		{name: "first page", path: page},
		{name: "second page", path: page + "&offset=50&limit=50"},
		{name: "past end", path: page + "&offset=100"},
		{name: "CJK search", path: page + "&q=" + url.QueryEscape("新-001")},
		{name: "stored token search", path: page + "&q=" + agentToken},
		{name: "removed rules", path: strings.Replace(page, "change=added", "change=removed", 1)},
		{name: "absent group", path: strings.Replace(page, "change=added", "change=modified", 1)},
		{name: "absent file", path: strings.Replace(page, "example.list", "absent.list", 1)},
		{name: "replaced version", path: strings.Replace(page, "{version}", "old", 1)},
		{name: "cached diff", path: diffRequest.path, mask: diffRequest.mask},
		{name: "list uses cached status", path: "/api/agents?history=0", mask: []string{"*.content_sync.checked_at"}},
		{name: "detail uses cached status", path: "/api/agents/agent-1", mask: []string{"content_sync.checked_at"}},
	}
	for _, r := range cases {
		r.method, r.admin = "GET", true
		if strings.Contains(r.path, "/entries?") {
			r.mask = []string{"version"}
		}
		t.Run(r.name, func(t *testing.T) {
			py := send(t, pyBase, r, pyToken, pyValues)
			native := send(t, goBase, r, goToken, goValues)
			if diffs := compare(r, py, native); len(diffs) != 0 {
				t.Fatal(strings.Join(diffs, "\n"))
			}
			if r.name == "cached diff" && (string(py.body) != string(pyReply.body) || string(native.body) != string(goReply.body)) {
				t.Fatal("cache changed comparison identity")
			}
			t.Logf("same %s → %d", r.name, py.status)
		})
	}
}
