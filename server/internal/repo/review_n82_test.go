package repo

import (
	"bytes"
	"configflow/internal/model/modeltest"
	"context"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"configflow/internal/model"
	"configflow/internal/testjson"
)

// A real private snapshot exercises both repositories' writes. The database
// is copied read-only, never printed, and removed even when an assertion fails.
func TestN82ReviewPrivateRepositoryInterchange(t *testing.T) {
	python, root, snapshot := os.Getenv("CONFIGFLOW_PY"), os.Getenv("CONFIGFLOW_REPO"), os.Getenv("CONFIGFLOW_SNAPSHOT")
	if python == "" || root == "" || snapshot == "" {
		t.Skip("live private interchange requires CONFIGFLOW_PY/REPO/SNAPSHOT")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal("private directory unavailable")
	}
	runs := filepath.Join(home, "cf-prod-snapshot", "runs")
	if os.MkdirAll(runs, 0o700) != nil {
		t.Fatal("private directory unavailable")
	}
	directory, err := os.MkdirTemp(runs, "review-n82-")
	if err != nil {
		t.Fatal("private copy unavailable")
	}
	t.Cleanup(func() {
		if os.RemoveAll(directory) != nil {
			t.Error("private copy cleanup failed")
		}
	})
	run := func(mode, other string) {
		t.Helper()
		cmd := exec.Command(python, "testdata/review_n82_interchange.py", mode, directory, other)
		cmd.Env = append(os.Environ(), "CONFIGFLOW_REPO="+root, "PYTHONDONTWRITEBYTECODE=1")
		if cmd.Run() != nil {
			t.Fatal("Python repository boundary failed; contents suppressed")
		}
	}
	run("copy", filepath.Join(snapshot, DatabaseFilename))
	r, err := OpenDefault(directory)
	if err != nil {
		t.Fatal("Go cannot open private copy")
	}
	stamp, err := r.Store().Fingerprint()
	if err != nil {
		t.Fatal("private fingerprint failed")
	}
	if err := r.Update(bg, func(context.Context, *model.Document) error { return nil }); err != nil {
		t.Fatal("private no-op failed")
	}
	if after, err := r.Store().Fingerprint(); err != nil || stamp != after {
		t.Fatal("private no-op wrote rows")
	}
	docRaw, err := r.ExportAllJSON(bg, false)
	if err != nil {
		t.Fatal("private export failed")
	}
	id := parseJSONObj(t, docRaw).GetObject("profiles").Keys()[0]
	expectedPath := filepath.Join(directory, "expected.json")
	// Go and Python read the same database as the same values (O18 T1c:
	// plain records compare as values, without undeclared or empty optional
	// members). Only the name of a differing view is reported.
	dumpPath := filepath.Join(directory, "python.json")
	audit := func() {
		t.Helper()
		data, err := r.ExportAllJSON(bg, false)
		if err != nil || os.WriteFile(expectedPath, data, 0o600) != nil {
			t.Fatal("private expectation failed")
		}
		run("dump", dumpPath)
		raw, err := os.ReadFile(dumpPath)
		if err != nil {
			t.Fatal("private Python view missing")
		}
		var views map[string]any
		if json.Unmarshal(raw, &views) != nil {
			t.Fatal("private Python view unreadable")
		}
		goView := n82ThroughModel(t, data)
		for _, name := range []string{"document", "export"} {
			pyView := n82ThroughModel(t, must(json.Marshal(views[name]))(t))
			if !testjson.Equal(goView, pyView) {
				t.Fatalf("Python %s differs from Go's values; contents suppressed", name)
			}
		}
	}
	audit()
	_, err = r.UpdateProfile(bg, id, func(_ context.Context, current *model.Profile) error {
		return markerSet(&current.Loon.CustomConfig, json.RawMessage(`{"marker":true,"opaque":{"z":1.0,"a":null}}`))
	})
	if err != nil {
		t.Fatal("Go profile write failed")
	}
	audit()
	run("edit", expectedPath)
	// Opening afresh rules out a stale cache concealing a Python write.
	r, err = OpenDefault(directory)
	if err != nil {
		t.Fatal("Go cannot reopen Python-written copy")
	}
	expected, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatal("expectation missing")
	}
	actual, err := r.ExportAllJSON(bg, false)
	if err != nil {
		t.Fatal("Python→Go profile values differ; contents suppressed")
	}
	if !testjson.Equal(n82ThroughModel(t, expected), n82ThroughModel(t, actual)) {
		t.Fatal("Python→Go profile values differ; contents suppressed")
	}
	audit()
	t.Log("private ConfigStore/ProfileRepository ↔ Go repository writes and unchanged-row probe passed")
}

func TestN82ReviewCorruptOpenHasSafeRecovery(t *testing.T) {
	const secret = "synthetic-secret-never-in-startup-diagnostics"
	for _, tt := range []struct{ name, value string }{
		{"JSON", `{"token":"` + secret + `",]`},
		{"UTF8", `"` + secret + "\xff\""},
		{"large", `["` + strings.Repeat("x", 10<<20) + secret + `"`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := newRepo(t)
			// Inject an unknown member: no schema validator can hide the bad row.
			rcSQL(t, r.DatabasePath(), `INSERT INTO fields(scope,owner,field,position,value,revision) VALUES('profile','default','corrupt_future',999,CAST(X'`+hex.EncodeToString([]byte(tt.value))+`' AS TEXT),1)`)
			before := must(os.ReadFile(r.DatabasePath()))(t)
			_, err := OpenDefault(r.DataDir())
			if err == nil {
				t.Fatal("corrupt database opened")
			}
			message := err.Error()
			for _, part := range []string{r.DatabasePath(), "fields", "profile/default/corrupt_future", "字节", "请先停止服务并保留原文件", "从可用备份恢复", "最后一个能正常读取此库的版本"} {
				if !strings.Contains(message, part) {
					t.Fatalf("recovery diagnostic omitted %q", part)
				}
			}
			if len(message) > 1500 || strings.Contains(message, secret) {
				t.Fatal("startup diagnostic exposed contents")
			}
			if !bytes.Equal(before, must(os.ReadFile(r.DatabasePath()))(t)) {
				t.Fatal("failed startup changed database bytes")
			}
		})
	}
}

// n82ThroughModel reads a whole configuration document as the plain records
// hold it (modeltest.ThroughModel, empty optional members left out).
func n82ThroughModel(t *testing.T, data []byte) any {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if decoder.Decode(&value) != nil {
		t.Fatal("private document unreadable")
	}
	return testjson.WithoutEmptyMembers(modeltest.ThroughModel(value))
}
