package difftest

import (
	"configflow/internal/lifecycle"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRuleSyncHTTPAgainstFlask(t *testing.T) {
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("set CONFIGFLOW_DIFF=1")
	}
	root := t.TempDir()
	pyDir, goDir := filepath.Join(root, "python"), filepath.Join(root, "go")
	seed(t, pyDir)
	copyDir(t, pyDir, goDir)
	py, stopPy := startPythonArgs(t, pyDir, "server/internal/api/agents/difftest/testdata/rulesync_app.py")
	defer stopPy()
	lifecycle.Stop(30 * time.Second)
	if e := lifecycle.Configure(lifecycle.Options{Mode: lifecycle.ModeManual}); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { lifecycle.Stop(30 * time.Second); _ = lifecycle.Configure(lifecycle.Options{}) })
	goBase, stopGo := startGo(t, goDir)
	defer stopGo()
	pt, gt := login(t, py), login(t, goBase)
	base := "/api/agents/agent-1/rule-sync"
	J := "application/json"
	cases := []request{
		{name: "disabled", method: "GET", path: base, admin: true},
		{name: "configure", method: "PUT", path: base, admin: true, contentType: J, body: `{"library":true,"rules":false}`},
		{name: "read scopes", method: "GET", path: base, admin: true, headers: map[string]string{"X-ConfigFlow-Profile": "other"}},
		{name: "missing agent", method: "GET", path: "/api/agents/missing/rule-sync", admin: true},
		{name: "missing before body", method: "PUT", path: "/api/agents/missing/rule-sync", admin: true, contentType: J, body: `null`},
		{name: "malformed", method: "PUT", path: base, admin: true, contentType: J, body: `{"library":`},
		{name: "non object", method: "PUT", path: base, admin: true, contentType: J, body: `[]`},
		{name: "missing member", method: "PUT", path: base, admin: true, contentType: J, body: `{"library":true}`},
		{name: "wrong type", method: "PUT", path: base, admin: true, contentType: J, body: `{"library":1,"rules":false}`},
		{name: "query", method: "PUT", path: base + "?extra=1", admin: true, contentType: J, body: `{"library":true,"rules":false}`},
		{name: "large", method: "PUT", path: base, admin: true, contentType: J, body: `{"library":true,"rules":false,"padding":"` + strings.Repeat("x", 1024) + `"}`},
		{name: "disable", method: "PUT", path: base, admin: true, contentType: J, body: `{"library":false,"rules":false}`},
		{name: "read auth", method: "GET", path: base},
		{name: "write auth", method: "PUT", path: base, contentType: J, body: `{"library":true,"rules":true}`},
	}
	values := []map[string]string{{}, {}}
	for _, r := range cases {
		t.Run(r.name, func(t *testing.T) {
			a := send(t, py, r, pt, nil)
			b := send(t, goBase, r, gt, nil)
			if diffs := compare(r, a, b); len(diffs) > 0 {
				t.Fatalf("%s", strings.Join(diffs, "\n"))
			}
			if a.status == 200 && a.header.Get("Cache-Control") != "no-store" {
				t.Fatal("missing no-store")
			}
			comparePublicationState(t, pyDir, goDir, values)
		})
	}
}
