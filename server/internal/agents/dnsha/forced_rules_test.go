package dnsha

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"configflow/internal/deploy"
	"configflow/internal/jsonx"
	"configflow/internal/refresh"
)

func TestManualPairRulesForcesUnchangedLibraryMaterialization(t *testing.T) {
	f := fixtureNew(t, Options{}, true, nil)
	var content atomic.Value
	content.Store("first.example\n")
	var fetches atomic.Int32
	ruleServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fetches.Add(1)
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, content.Load().(string))
	}))
	t.Cleanup(ruleServer.Close)
	_, err := f.r.UpdateSharedTransaction(f.ctx, func(_ context.Context, shared *jsonx.Object) (*jsonx.Object, error) {
		shared.Set("rule_library", []any{jsonx.Obj("id", "library-example", "name", "Synthetic library", "source_type", "url", "url", ruleServer.URL+"/rules.txt", "behavior", "domain", "format", "text")})
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = f.r.UpdateProfileFields(f.ctx, "default", jsonx.Obj("rule_configs", []any{jsonx.Obj("id", "ruleset-example", "itemType", "ruleset", "library_rule_id", "library-example", "policy", "DIRECT", "enabled", true)}, "oxidns", jsonx.Obj("config_mode", "template", "direct_rulesets", []any{"ruleset-example"}, "local_dns", "udp://192.0.2.1:53", "remote_dns", "udp://192.0.2.2:53")), nil)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := f.s.get(f.ctx, "primary")
	if err != nil {
		t.Fatal(err)
	}
	source, revision, err := f.s.source(f.ctx, owner, true)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := deploy.PrepareContent(f.ctx, f.m, owner.target, source, deploy.PrepareOptions{BaseURL: "https://panel.example", DeploymentID: "remote-rules-baseline"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = deploy.Publish(f.ctx, f.m, "primary", prepared.Bundle, deploy.PublishOptions{Activate: true, BaseURL: "https://panel.example", SourceSnapshot: source, ResourceConfig: source, ConfigRevision: revision}); err != nil {
		t.Fatal(err)
	}
	if last := f.drain(t, 12); last.Status != "succeeded" {
		t.Fatalf("baseline: %s", last.Message)
	}
	previous := fetches.Load()
	if previous == 0 {
		t.Fatal("baseline did not download the actual remote library")
	}
	before := len(f.uploads())
	// The library scheduler refreshes its cache. The saved source remains
	// unchanged, so the manual owner must force materialization instead of
	// pinning the identical declaration's old deployment bytes, as Python does.
	content.Store("second.example\n")
	raw, err := source.Raw()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := jsonx.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	config := cfg.(*jsonx.Object)
	library := config.GetList("rule_library")[0].(*jsonx.Object)
	_, stale, err := refresh.RefreshRuleSource(f.ctx, library, "manual", refresh.RuleOptions{Config: config})
	if err != nil || stale != nil {
		t.Fatalf("library refresh: %v / %v", err, stale)
	}
	if fetches.Load() <= previous {
		t.Fatal("library scheduler did not refresh the remote bytes")
	}
	if os.Getenv("CONFIGFLOW_DIFF") == "1" {
		assertPythonForcedLibrary(t, source, prepared.Bundle)
	}
	if _, _, err = f.s.Rules(f.ctx, "primary", "POST"); err != nil {
		t.Fatal(err)
	}
	if last := f.drain(t, 12); last.Status != "succeeded" {
		t.Fatalf("rule publication: %s", last.Message)
	}
	uploads := f.uploads()
	if len(uploads) != before+2 {
		t.Fatal("rule refresh did not publish both members")
	}
	for _, upload := range uploads[before:] {
		gz, err := gzip.NewReader(bytes.NewReader(upload.Body))
		if err != nil {
			t.Fatal(err)
		}
		reader := tar.NewReader(gz)
		found := false
		for {
			header, err := reader.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(header.Name, "files/rules/") {
				continue
			}
			data, err := io.ReadAll(reader)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(data, []byte("second.example")) {
				found = true
			}
			if bytes.Contains(data, []byte("first.example")) {
				t.Fatal("peer received pinned stale rule bytes")
			}
		}
		_ = gz.Close()
		if !found {
			t.Fatal("delivered pair bundle lacked the freshly downloaded library")
		}
	}
}

func assertPythonForcedLibrary(t *testing.T, source *deploy.Source, b *deploy.Bundle) {
	t.Helper()
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := source.Raw()
	if err != nil {
		t.Fatal(err)
	}
	archive, err := b.Archive()
	if err != nil {
		t.Fatal(err)
	}
	input, err := json.Marshal(struct {
		Source   json.RawMessage `json:"source"`
		Manifest deploy.Manifest `json:"manifest"`
		Archive  []byte          `json:"archive"`
		Version  string          `json:"version"`
	}{raw, b.Manifest(), archive, b.ConfigVersion()})
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(filepath.Join(os.Getenv("HOME"), "cf-test/.venv/bin/python"), "server/internal/agents/dnsha/testdata/forced_library.py")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "PYTHONPATH=.", "DATA_DIR="+t.TempDir())
	cmd.Stdin = bytes.NewReader(input)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("Python forced library: %v %s", err, stderr.String())
	}
	var result struct{ Fresh, Old, Reload bool }
	if err = json.Unmarshal(out, &result); err != nil {
		t.Fatal(err, string(out))
	}
	if !result.Fresh || result.Old || !result.Reload {
		t.Fatal("Python forced materialization diverged", string(out))
	}
}
