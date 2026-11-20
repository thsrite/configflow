package difftest

import (
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
	"context"
	"encoding/json"
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
	// A04 separately verifies legacy metrics migration structurally. Keep
	// nonempty monitoring history here in its current JSONL format, so the A16
	// complete-file comparison still detects every unrelated byte mutation
	// without conflating startup migration's native JSON layout with this API.
	metricsPath := filepath.Join(pyDir, "metrics", "agent-busy_metrics.json")
	rawMetrics, e := os.ReadFile(metricsPath)
	if e != nil {
		t.Fatal(e)
	}
	var metrics struct {
		Points []json.RawMessage `json:"metrics"`
	}
	if e := json.Unmarshal(rawMetrics, &metrics); e != nil {
		t.Fatal(e)
	}
	var jsonl []byte
	for _, point := range metrics.Points {
		if len(point) > 0 && point[0] == '{' {
			jsonl = append(jsonl, point...)
			jsonl = append(jsonl, '\n')
		}
	}
	if len(jsonl) == 0 {
		t.Fatal("missing synthetic metrics history")
	}
	if e := os.WriteFile(strings.TrimSuffix(metricsPath, ".json")+".jsonl", jsonl, 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.Remove(metricsPath); e != nil {
		t.Fatal(e)
	}
	r, e := repo.Open(pyDir, repo.Options{})
	if e != nil {
		t.Fatal(e)
	}
	e = r.UpdateSystem(context.Background(), func(_ context.Context, system *model.System) error {
		raw, err := json.Marshal(system)
		if err != nil {
			return err
		}
		doc, err := parsePythonJSON(raw)
		if err != nil {
			return err
		}
		records := doc.(*orderedObject).GetList("agents")
		for _, entry := range records {
			entry.(*orderedObject).Set("host", "127.0.0.1")
		}
		wrong := records[0].(*orderedObject).Clone()
		wrong.Set("id", "wrong-service")
		wrong.Set("service_type", "mosdns")
		return system.SetJSON("agents", mustRawJSON(t, append(records, wrong)))
	})
	if e != nil {
		t.Fatal(e)
	}
	copyDir(t, pyDir, goDir)
	py, stopPy := startPythonArgs(t, pyDir, "server/internal/api/agents/difftest/testdata/rulesync_app.py")
	defer stopPy()
	lifecycle.Stop(30 * time.Second)
	if e := lifecycle.Configure(lifecycle.Options{Registry: lifecycle.NewRegistry()}); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		lifecycle.Stop(30 * time.Second)
		_ = lifecycle.Configure(lifecycle.Options{Registry: lifecycle.NewRegistry()})
	})
	// These HTTP comparisons exercise save/read, not clock-driven workers.
	// Mirror the Python fixture by suppressing only the rule-sync timer driver;
	// worker admission, device I/O and Runtime cancellation have real tests.
	lifecycle.RegisterStartup(lifecycle.Startup{Name: "a16-http-no-timer", Run: func(context.Context) error {
		lifecycle.RegisterPeriodic(lifecycle.Periodic{Name: "agent-rule-sync", Start: lifecycle.AfterFirstRequest, Interval: func(context.Context) time.Duration { return 5 * time.Second }, Round: func(context.Context) error { return nil }})
		return nil
	}})
	goBase, stopGo := startGo(t, goDir)
	defer stopGo()
	pt, gt := login(t, py), login(t, goBase)
	base := "/api/agents/agent-1/rule-sync"
	J := "application/json"
	cases := []request{
		{name: "disabled", method: "GET", path: base, admin: true},
		{name: "configure", method: "PUT", path: base, admin: true, contentType: J, body: `{"library":true,"rules":false}`},
		{name: "read scopes", method: "GET", path: base, admin: true, headers: map[string]string{"X-ConfigFlow-Profile": "other"}},
		{name: "wrong service", method: "GET", path: "/api/agents/wrong-service/rule-sync", admin: true},
		{name: "missing agent", method: "GET", path: "/api/agents/missing/rule-sync", admin: true},
		{name: "missing before body", method: "PUT", path: "/api/agents/missing/rule-sync", admin: true, contentType: J, body: `null`},
		{name: "malformed", method: "PUT", path: base, admin: true, contentType: J, body: `{"library":`},
		{name: "non object", method: "PUT", path: base, admin: true, contentType: J, body: `[]`},
		{name: "missing member", method: "PUT", path: base, admin: true, contentType: J, body: `{"library":true}`},
		{name: "wrong type", method: "PUT", path: base, admin: true, contentType: J, body: `{"library":1,"rules":false}`},
		{name: "query", method: "PUT", path: base + "?extra=1", admin: true, contentType: J, body: `{"library":true,"rules":false}`},
		{name: "large", method: "PUT", path: base, admin: true, contentType: J, body: `{"library":true,"rules":false,"padding":"` + strings.Repeat("x", 1024) + `"}`},
		{name: "text plain", method: "PUT", path: base, admin: true, contentType: "text/plain", body: `{"library":true,"rules":true}`},
		{name: "extra member", method: "PUT", path: base, admin: true, contentType: J, body: `{"library":true,"rules":true,"extra":true}`},
		{name: "null member", method: "PUT", path: base, admin: true, contentType: J, body: `{"library":null,"rules":true}`},
		{name: "string member", method: "PUT", path: base, admin: true, contentType: J, body: `{"library":"true","rules":true}`},
		{name: "empty query", method: "PUT", path: base + "?", admin: true, contentType: J, body: `{"library":true,"rules":true}`},
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
