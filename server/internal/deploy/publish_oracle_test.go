package deploy

import (
	"bytes"
	"configflow/internal/agents/agentfake"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestPythonPublishTranscripts(t *testing.T) {
	python := filepath.Join(os.Getenv("HOME"), "cf-test/.venv/bin/python")
	if _, e := os.Stat(python); e != nil {
		t.Fatal("Python reference unavailable", e)
	}
	cases := []struct {
		name     string
		options  agentfake.Options
		activate bool
		actions  []string
	}{
		{"staged-activate-rollback", agentfake.Options{}, false, []string{"publish", "poll", "activate", "rollback"}},
		{"auto-success-replay", agentfake.Options{}, true, []string{"publish", "poll", "publish"}},
		{"auto-rollback", agentfake.Options{ActivationOutcome: "rolled_back"}, true, []string{"publish", "poll"}},
		{"rollback-failure-recover", agentfake.Options{ActivationOutcome: "rollback_failed"}, true, []string{"publish", "poll", "recover"}},
		{"old-recover", agentfake.Options{ActivationOutcome: "rollback_failed", RecoverUnsupported: true}, true, []string{"publish", "poll", "recover"}},
		{"old-capabilities", agentfake.Options{Capabilities: json.RawMessage(`{"service_type":"mihomo"}`)}, false, []string{"publish", "poll"}},
		{"service-mismatch", agentfake.Options{Capabilities: json.RawMessage(`{"deployment_protocols":[1],"service_type":"mosdns"}`)}, true, []string{"publish"}},
		{"lost-upload", agentfake.Options{Script: []agentfake.Fault{{Path: "/api/deployments", LoseResponse: true}}}, true, []string{"publish", "poll"}},
		{"busy-then-accept", agentfake.Options{Script: []agentfake.Fault{{Path: "/api/deployments", Times: 2, Code: 409, Body: json.RawMessage(`{"success":false,"message":"another service operation is in progress"}`)}}}, false, []string{"publish", "poll"}},
		{"refused-upload", agentfake.Options{Script: []agentfake.Fault{{Path: "/api/deployments", Code: 403, Body: json.RawMessage(`{"success":false,"message":"refused"}`)}}}, false, []string{"publish", "poll"}},
		{"invalid-status", agentfake.Options{Script: []agentfake.Fault{{Path: "/api/deployments", Code: 202, Body: json.RawMessage(`null`)}}}, false, []string{"publish"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Two independent instances of the same fake, never one shared journal.
			py := newPublish(t, tc.options, nil)
			goSide := newPublish(t, tc.options, nil)
			agent := py.agent(t)
			source, _ := py.source.Raw()
			input, _ := json.Marshal(map[string]any{"agent": agent, "source": source, "actions": tc.actions, "activate": tc.activate})
			cmd := exec.Command(python, "testdata/gen_a14_publish.py")
			cmd.Stdin = bytes.NewReader(input)
			root, _ := filepath.Abs("../../..")
			cmd.Env = append(os.Environ(), "PYTHONPATH="+root)
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			raw, e := cmd.Output()
			if e != nil {
				t.Fatalf("Python: %v %s", e, stderr.String())
			}
			var expected any
			if e = json.Unmarshal(raw, &expected); e != nil {
				t.Fatal(e, string(raw))
			}
			b := goSide.bundle(t, "oracle")
			answers := []any{}
			for _, action := range tc.actions {
				var result any
				switch action {
				case "publish":
					result, e = PublishDirect(goSide.ctx, goSide.m, "publish-agent", b, PublishOptions{Activate: tc.activate, ConfigRevision: &Revision{Value: 7}, SourceSnapshot: goSide.source, BaseURL: "https://panel.example.com"})
				case "poll":
					result, e = goSide.m.GetDeploymentModel(goSide.ctx, "publish-agent", "oracle")
				case "activate":
					result, e = goSide.m.ActivateCandidateModel(goSide.ctx, "publish-agent", "oracle")
				case "rollback":
					result, e = goSide.m.RollbackDeploymentModel(goSide.ctx, "publish-agent", "oracle")
				case "recover":
					result, e = goSide.m.RecoverDeploymentModel(goSide.ctx, "publish-agent", "oracle")
				}
				if e != nil {
					t.Fatal(e)
				}
				answers = append(answers, map[string]any{"result": result, "agent": goSide.agent(t)})
			}
			files := map[string]any{}
			dir := goSide.m.Repository().DataDir()
			e = filepath.WalkDir(filepath.Join(dir, ".agent-rule-sync"), func(path string, entry os.DirEntry, e error) error {
				if os.IsNotExist(e) {
					return nil
				}
				if e != nil {
					return e
				}
				if entry.IsDir() {
					return nil
				}
				rel, _ := filepath.Rel(dir, path)
				if entry.Name() == "receipt.json" {
					raw, e := os.ReadFile(path)
					if e != nil {
						return e
					}
					var v any
					if e = json.Unmarshal(raw, &v); e != nil {
						return e
					}
					files[rel] = v
				} else if strings.HasSuffix(entry.Name(), ".gz") {
					files[rel] = "archive"
				}
				return nil
			})
			if e != nil {
				t.Fatal(e)
			}
			actual := nativeValue(t, map[string]any{"answers": answers, "files": files})
			normalizePublish(expected)
			normalizePublish(actual)
			if !reflect.DeepEqual(actual, expected) {
				t.Fatal(firstDifference("", actual, expected))
			}
			compareTranscripts(t, py.fake.Calls(), goSide.fake.Calls())
		})
	}
}
func nativeValue(t *testing.T, v any) any {
	t.Helper()
	raw, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	var out any
	if e = json.Unmarshal(raw, &out); e != nil {
		t.Fatal(e)
	}
	return out
}
func normalizePublish(v any) {
	switch x := v.(type) {
	case []any:
		for _, item := range x {
			normalizePublish(item)
		}
	case map[string]any:
		for key, value := range x {
			switch key {
			case "created_at", "updated_at", "agent_updated_at", "pushed_at", "at":
				if _, ok := value.(string); ok {
					x[key] = "@time"
				}
			case "port":
				x[key] = "@port"
			case "sha256", "source_sha256":
				if key == "source_sha256" || strings.HasPrefix(fmt.Sprint(value), "v1:") == false { // archive digests only: file metadata has a path
					if _, file := x["path"]; !file {
						if _, proof := x["role"]; !proof {
							x[key] = "@archive-sha"
						}
					}
				}
			case "message":
				if strings.HasPrefix(fmt.Sprint(value), "正在上传发布包（") {
					x[key] = "正在上传发布包（@files 个文件，@archive-bytes 字节）"
				}
			case "error_details":
				if strings.Contains(fmt.Sprint(value), "EOF") || strings.Contains(fmt.Sprint(value), "RemoteDisconnected") {
					x[key] = "@lost-response"
				}
			}
			normalizePublish(x[key])
		}
		// File names embed each side's archive digest; retain count and all metadata.
		for key, value := range x {
			if strings.HasPrefix(filepath.Base(key), "bundle-") && strings.HasSuffix(key, ".tar.gz") {
				delete(x, key)
				x[filepath.Join(filepath.Dir(key), "bundle-@archive.tar.gz")] = value
			}
		}
	}
}
func compareTranscripts(t *testing.T, a, b []agentfake.Call) {
	t.Helper()
	if len(a) != len(b) {
		t.Fatalf("request count %d != %d", len(a), len(b))
	}
	for i, left := range a {
		right := b[i]
		if left.Method != right.Method || left.Path != right.Path {
			t.Fatal("request sequence", i, left.Method, left.Path, right.Method, right.Path)
		}
		for _, k := range []string{"Authorization", "Content-Type", "X-Deployment-ID", "X-Activate"} {
			if left.Headers.Get(k) != right.Headers.Get(k) {
				t.Fatalf("header %s differs", k)
			}
		}
		if left.Path == "/api/deployments" {
			if left.Headers.Get("X-Content-SHA256") != digest(left.Body) || right.Headers.Get("X-Content-SHA256") != digest(right.Body) {
				t.Fatal("wrong body digest")
			}
			if !reflect.DeepEqual(left.Manifest, right.Manifest) || !reflect.DeepEqual(left.FileDigests, right.FileDigests) {
				t.Fatalf("upload manifest/files differ: %#v %#v", left.Manifest, right.Manifest)
			}
		} else if !bytes.Equal(left.Body, right.Body) {
			t.Fatalf("body bytes differ at %d", i)
		}
	}
}

func firstDifference(path string, a, b any) string {
	if reflect.DeepEqual(a, b) {
		return ""
	}
	if x, ok := a.(map[string]any); ok {
		if y, ok := b.(map[string]any); ok {
			keys := []string{}
			seen := map[string]bool{}
			for k := range x {
				keys = append(keys, k)
				seen[k] = true
			}
			for k := range y {
				if !seen[k] {
					keys = append(keys, k)
				}
			}
			sort.Strings(keys)
			for _, k := range keys {
				av, ap := x[k]
				bv, bp := y[k]
				if ap != bp {
					return fmt.Sprintf("%s/%s presence got=%v want=%v", path, k, ap, bp)
				}
				if d := firstDifference(path+"/"+k, av, bv); d != "" {
					return d
				}
			}
		}
	}
	if x, ok := a.([]any); ok {
		if y, ok := b.([]any); ok {
			if len(x) != len(y) {
				return fmt.Sprintf("%s length %d != %d", path, len(x), len(y))
			}
			for i := range x {
				if d := firstDifference(fmt.Sprintf("%s/%d", path, i), x[i], y[i]); d != "" {
					return d
				}
			}
		}
	}
	return fmt.Sprintf("%s got=%v want=%v", path, a, b)
}
