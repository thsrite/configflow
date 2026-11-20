package difftest

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"configflow/internal/agents/agentfake"
	"configflow/internal/deploy"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/store"
	"configflow/internal/yamlx"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

func publishSeed(t *testing.T, dir string, port int) {
	t.Helper()
	r, e := repo.Open(dir, repo.Options{})
	if e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	e = r.UpdateSystem(ctx, func(_ context.Context, s *model.System) error {
		if err := s.SetJSON("system_config", mustRawJSON(t, oObj("server_domain", "https://panel.example.com"))); err != nil {
			return err
		}
		return s.SetJSON("agents", mustRawJSON(t, []any{oObj("id", "a14-agent", "name", "Synthetic", "token", "a14-synthetic-token", "host", "127.0.0.1", "port", port, "service_type", "mihomo", "profile_id", "default", "deployment_method", "docker", "version", "1.8.60-go", "enabled", true)}))
	})
	if e != nil {
		t.Fatal(e)
	}
	_, e = r.UpdateProfile(ctx, "default", func(_ context.Context, p *model.Profile) error {
		if err := p.SetJSON("mihomo", mustRawJSON(t, oObj("custom_config", "mode: rule\n"))); err != nil {
			return err
		}
		if err := p.SetJSON("proxy_groups", json.RawMessage(`[]`)); err != nil {
			return err
		}
		return p.SetJSON("rule_configs", json.RawMessage(`[]`))
	})
	if e != nil {
		t.Fatal(e)
	}
}
func TestPublishHTTPAgainstFlask(t *testing.T) {
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("set CONFIGFLOW_DIFF=1")
	}
	root := t.TempDir()
	pyDir, goDir := filepath.Join(root, "python"), filepath.Join(root, "go")
	pf, gf := agentfake.New(agentfake.Options{AgentID: "a14-agent", Token: "a14-synthetic-token"}), agentfake.New(agentfake.Options{AgentID: "a14-agent", Token: "a14-synthetic-token"})
	ps, gs := httptest.NewServer(pf), httptest.NewServer(gf)
	defer ps.Close()
	defer gs.Close()
	port := func(s *httptest.Server) int {
		_, p, _ := net.SplitHostPort(strings.TrimPrefix(s.URL, "http://"))
		n, _ := strconv.Atoi(p)
		return n
	}
	publishSeed(t, pyDir, port(ps))
	copyDir(t, pyDir, goDir)
	gr, e := repo.Open(goDir, repo.Options{})
	if e != nil {
		t.Fatal(e)
	}
	e = gr.UpdateSystem(context.Background(), func(_ context.Context, s *model.System) error {
		return s.Agents[0].SetJSON("port", mustRawJSON(t, port(gs)))
	})
	if e != nil {
		t.Fatal(e)
	}
	// Keep initial write counts identical; only target port differs.
	pr, e := repo.Open(pyDir, repo.Options{})
	if e != nil {
		t.Fatal(e)
	}
	e = pr.UpdateSystem(context.Background(), func(_ context.Context, s *model.System) error { return nil })
	if e != nil {
		t.Fatal(e)
	}
	py, stopPy := startPythonArgs(t, pyDir, "server/internal/api/agents/difftest/testdata/publish_app.py")
	defer stopPy()
	goBase, stopGo := startGo(t, goDir)
	defer stopGo()
	pt, gt := login(t, py), login(t, goBase)
	base := "/api/agents/a14-agent"
	J := "application/json"
	cases := []request{
		{name: "pull missing token", method: "GET", path: base + "/config"},
		{name: "pull wrong token", method: "GET", path: base + "/config?token=bad"},
		{name: "pull", method: "GET", path: base + "/config?token=a14-synthetic-token"},
		{name: "missing push", method: "POST", path: "/api/agents/missing/push-config", body: `{}`, contentType: J, admin: true},
		{name: "body", method: "POST", path: base + "/push-config", body: `[1]`, contentType: J, admin: true},
		{name: "guard", method: "POST", path: base + "/push-config", body: `{"expected_service_type":"mihomo"}`, contentType: J, admin: true},
		{name: "restart", method: "POST", path: base + "/push-config", body: `{"restart":null}`, contentType: J, admin: true},
		{name: "invalid id", method: "POST", path: base + "/push-config", body: `{"deployment_id":"../bad"}`, contentType: J, admin: true},
		{name: "generate guard", method: "POST", path: base + "/generate-config", body: `{}`, contentType: J, admin: true},
		{name: "generate missing", method: "POST", path: "/api/agents/missing/generate-config", body: `{}`, contentType: J, admin: true},
		{name: "generate", method: "POST", path: base + "/generate-config", body: `{"expected_profile_id":"default","expected_service_type":"mihomo","base_url":"https://panel.example.com"}`, contentType: J, admin: true},
	}
	ids := []map[string]string{{}, {}}
	values := []map[string]string{{}, {}}
	calls := 0
	comparePair := func(r request, paths ...string) {
		t.Helper()
		defer comparePublicationState(t, pyDir, goDir, values)
		rpy, rgo := r, r
		if len(paths) == 2 {
			rpy.path = paths[0]
			rgo.path = paths[1]
		}
		a, b := send(t, py, rpy, pt, nil), send(t, goBase, rgo, gt, nil)
		calls++
		if a.status != b.status {
			t.Fatalf("%s status %d != %d: %s %s", r.name, a.status, b.status, a.body, b.body)
		}
		for _, h := range []string{"Content-Type", "Cache-Control", "Content-Disposition"} {
			if a.header.Get(h) != b.header.Get(h) {
				t.Fatalf("%s header %s differs", r.name, h)
			}
		}
		if strings.Contains(a.header.Get("Content-Type"), "application/zip") {
			compareZIP(t, a.body, b.body)
			return
		}
		var av, bv any
		ae, be := json.Unmarshal(a.body, &av), json.Unmarshal(b.body, &bv)
		if ae != nil || be != nil {
			compareGeneratedYAML(t, a.body, b.body)
			return
		}
		if r.name == "generate" {
			for i, v := range []any{av, bv} {
				m := v.(map[string]any)
				id, _ := m["id"].(string)
				did, _ := m["deployment_id"].(string)
				if len(id) != 32 || len(did) != 32 {
					t.Fatal("bad generation identity")
				}
				ids[i]["preview"] = id
				ids[i]["deployment"] = did
				values[i][id] = "@preview"
				values[i][did] = "@deployment"
			}
		}
		if r.name == "stage" {
			for _, value := range []any{av, bv} {
				task, ok := value.(map[string]any)
				if !ok || task["ipv6_verification_requested"] != true {
					t.Fatal("A23 enrollment missing from staged publication")
				}
			}
		}

		normalizePublication(av, values[0])
		normalizePublication(bv, values[1])
		if !reflect.DeepEqual(av, bv) {
			t.Fatalf("%s: %s", r.name, publishDifference("", av, bv))
		}
	}
	for _, r := range cases {
		comparePair(r)
	}
	for _, kind := range []string{"download", "file?path=config.yaml", "file?path=missing"} {
		comparePair(request{name: "preview " + kind, method: "GET", admin: true}, base+"/generated-config/"+ids[0]["preview"]+"/"+kind, base+"/generated-config/"+ids[1]["preview"]+"/"+kind)
	}
	comparePair(request{name: "expired artifact", method: "GET", path: base + "/generated-config/invalid/download", admin: true})
	for _, suffix := range []string{"/push-config", "/generate-config", "/deployments/absent/activate", "/deployments/absent/rollback", "/deployments/absent/recover"} {
		comparePair(request{name: "auth", method: "POST", path: base + suffix, body: `{}`, contentType: J})
	}
	comparePair(request{name: "stage", method: "POST", path: base + "/push-config", body: `{"deployment_id":"http-publish","restart":false,"base_url":"https://panel.example.com"}`, contentType: J, admin: true})
	for _, action := range []string{"poll", "activate", "rollback", "poll", "recover"} {
		method, path := "POST", base+"/deployments/http-publish/"+action
		if action == "poll" {
			method, path = "GET", base+"/deployments/http-publish"
		}
		comparePair(request{name: action, method: method, path: path, admin: true})
	}
	for _, path := range []string{"/deployments/missing", "/deployments/missing/activate", "/deployments/missing/rollback", "/deployments/missing/recover"} {
		method := "POST"
		if path == "/deployments/missing" {
			method = "GET"
		}
		comparePair(request{name: "missing task", method: method, path: base + path, admin: true})
	}
	left, right := pf.Calls(), gf.Calls()
	if len(left) != len(right) {
		t.Fatal("Agent call counts", len(left), len(right))
	}
	for i, a := range left {
		b := right[i]
		if a.Method != b.Method || a.Path != b.Path {
			t.Fatal("Agent sequence")
		}
		for _, key := range []string{"Authorization", "Content-Type", "X-Deployment-ID", "X-Activate"} {
			if a.Headers.Get(key) != b.Headers.Get(key) {
				t.Fatal("Agent raw header", key)
			}
		}
		if a.Path == "/api/deployments" {
			if a.Headers.Get("X-Content-SHA256") != hashBytes(a.Body) || b.Headers.Get("X-Content-SHA256") != hashBytes(b.Body) {
				t.Fatal("archive digest not body digest")
			}
			compareUpload(t, a.Body, b.Body)
		} else if a.Path == "/api/config/final-preview" {
			var left, right struct {
				Content string `json:"content"`
			}
			if json.Unmarshal(a.Body, &left) != nil || json.Unmarshal(b.Body, &right) != nil || left != right {
				t.Fatal("final-preview JSON content differs")
			}
			var expected bytes.Buffer
			encoder := json.NewEncoder(&expected)
			encoder.SetEscapeHTML(false)
			if e := encoder.Encode(right); e != nil {
				t.Fatal(e)
			}
			if !bytes.Equal(b.Body, bytes.TrimSuffix(expected.Bytes(), []byte{'\n'})) {
				t.Fatal("native final-preview raw bytes differ")
			}
		} else if !bytes.Equal(a.Body, b.Body) {
			t.Fatal("Agent raw body differs", a.Path)
		}
	}
	t.Logf("%d Flask/Go HTTP requests, complete DB document, private files, raw Agent calls: zero differences", calls)
}
func hashBytes(v []byte) string { s := sha256.Sum256(v); return hex.EncodeToString(s[:]) }
func compareGeneratedYAML(t *testing.T, a, b []byte) {
	t.Helper()
	x, e := yamlx.Parse(a)
	if e != nil {
		t.Fatal(e)
	}
	y, e := yamlx.Parse(b)
	if e != nil {
		t.Fatal(e)
	}
	if !yamlx.Equal(x, y) {
		t.Fatal("generated YAML differs")
	}
}
func compareZIP(t *testing.T, a, b []byte) {
	t.Helper()
	decode := func(raw []byte) map[string][]byte {
		z, e := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
		if e != nil {
			t.Fatal(e)
		}
		out := map[string][]byte{}
		for _, f := range z.File {
			r, e := f.Open()
			if e != nil {
				t.Fatal(e)
			}
			out[f.Name], e = io.ReadAll(r)
			r.Close()
			if e != nil {
				t.Fatal(e)
			}
		}
		return out
	}
	x, y := decode(a), decode(b)
	if len(x) != len(y) {
		t.Fatal("ZIP file set")
	}
	for k, v := range x {
		w, ok := y[k]
		if !ok {
			t.Fatal("ZIP missing", k)
		}
		if strings.HasSuffix(k, ".yaml") {
			compareGeneratedYAML(t, v, w)
		} else if !bytes.Equal(v, w) {
			t.Fatal("ZIP copied bytes", k)
		}
	}
}
func compareUpload(t *testing.T, a, b []byte) {
	t.Helper()
	decode := func(raw []byte) map[string][]byte {
		gz, e := gzip.NewReader(bytes.NewReader(raw))
		if e != nil {
			t.Fatal(e)
		}
		defer gz.Close()
		tr := tar.NewReader(gz)
		out := map[string][]byte{}
		for {
			h, e := tr.Next()
			if e == io.EOF {
				break
			}
			if e != nil {
				t.Fatal(e)
			}
			v, e := io.ReadAll(tr)
			if e != nil {
				t.Fatal(e)
			}
			out[h.Name] = v
		}
		return out
	}
	x, y := decode(a), decode(b)
	if len(x) != len(y) {
		t.Fatal("archive file set")
	}
	for k, v := range x {
		w, ok := y[k]
		if !ok {
			t.Fatal("archive missing", k)
		}
		if k == "manifest.json" {
			var p, q deploy.Manifest
			if json.Unmarshal(v, &p) != nil || json.Unmarshal(w, &q) != nil {
				t.Fatal("manifest JSON")
			}
			if !reflect.DeepEqual(p, q) {
				t.Fatal("manifest mismatch")
			}
		} else if strings.HasSuffix(k, ".yaml") {
			compareGeneratedYAML(t, v, w)
		} else if !bytes.Equal(v, w) {
			t.Fatal("archive copied bytes", k)
		}
	}
}
func normalizePublication(v any, values map[string]string) {
	switch x := v.(type) {
	case []any:
		for _, e := range x {
			normalizePublication(e, values)
		}
	case map[string]any:
		for k, e := range x {
			switch k {
			case "created_at", "updated_at", "agent_updated_at", "pushed_at", "at", "expires_at":
				if e != nil {
					x[k] = "@time"
				}
			case "checked_at", "first_seen_at", "last_seen_at", "version":
				if k == "version" {
					if _, notification := x["first_seen_at"]; !notification {
						break
					}
				}
				if text, ok := e.(string); ok {
					_, naive := time.Parse("2006-01-02T15:04:05.999999", text)
					_, utc := time.Parse(time.RFC3339Nano, text)
					if naive == nil || utc == nil {
						x[k] = "@time"
					}
				}
			case "port":
				x[k] = "@port"
			case "sha256", "source_sha256":
				if _, file := x["path"]; !file {
					x[k] = "@archive-sha"
				}
			case "input_digest":
				x[k] = "@transient-input"
			case "message":
				if strings.HasPrefix(fmt.Sprint(e), "正在上传发布包（") {
					x[k] = "@upload-size"
				}
			}
			normalizePublication(x[k], values)
			if s, ok := x[k].(string); ok {
				if replacement, ok := values[s]; ok {
					x[k] = replacement
				}
			}
		}
	case string:
	}
}
func publishDifference(path string, a, b any) string {
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
					return path + "/" + k + " presence differs"
				}
				if d := publishDifference(path+"/"+k, av, bv); d != "" {
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
				if d := publishDifference(fmt.Sprintf("%s/%d", path, i), x[i], y[i]); d != "" {
					return d
				}
			}
		}
	}
	return fmt.Sprintf("%s: %v != %v", path, a, b)
}
func comparePublicationState(t *testing.T, a, b string, values []map[string]string) {
	t.Helper()
	var documents [2]any
	for i, dir := range []string{a, b} {
		entry, err := store.New(filepath.Join(dir, "configflow.sqlite3"), func() time.Duration { return 10 * time.Second }).Load()
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(entry.Document)
		if err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(raw, &documents[i]); err != nil {
			t.Fatal(err)
		}
		normalizePublication(documents[i], values[i])
	}
	if !reflect.DeepEqual(documents[0], documents[1]) {
		t.Fatal("complete DB document: ", publishDifference("", documents[0], documents[1]))
	}
	comparePublicationFiles(t, a, b, values)
}

func comparePublicationFiles(t *testing.T, a, b string, values []map[string]string) {
	t.Helper()
	collect := func(root string, replacements map[string]string) map[string]any {
		out := map[string]any{}
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			// DB contents are compared above. Runtime logs/framing and Python's old
			// process-lock artifacts are the only excluded paths, never user files.
			if entry.IsDir() {
				if rel == ".agent-deployment-locks" {
					return filepath.SkipDir
				}
				return nil
			}
			if rel == "task-status.json.lock" || rel == ".dns-follow.lock" || rel == ".task-scheduler.lock" || rel == ".oxidns-rankings.lock" || rel == "router-rule-sync/.scheduler.lock" || rel == "probes/.auto.lock" || rel == "notifications.lock" {
				raw, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				if len(raw) != 0 {
					return fmt.Errorf("nonempty process lock %s", rel)
				}
				return nil
			}
			if rel == "app.log" || rel == "configflow.sqlite3" || rel == "configflow.sqlite3-wal" || rel == "configflow.sqlite3-shm" || rel == "configflow.sqlite3.lock" {
				return nil
			}
			if entry.Name() == "lock" && (strings.HasPrefix(rel, ".agent-rule-sync/") || strings.HasPrefix(rel, ".generated-previews/")) {
				return nil
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for original, replacement := range replacements {
				rel = strings.ReplaceAll(rel, original, replacement)
			}
			if strings.HasSuffix(rel, ".tar.gz") {
				gz, err := gzip.NewReader(bytes.NewReader(raw))
				if err != nil {
					return err
				}
				defer gz.Close()
				tr := tar.NewReader(gz)
				members := map[string][]byte{}
				for {
					header, err := tr.Next()
					if err == io.EOF {
						break
					}
					if err != nil {
						return err
					}
					content, err := io.ReadAll(tr)
					if err != nil {
						return err
					}
					if _, exists := members[header.Name]; exists {
						return fmt.Errorf("duplicate archive member")
					}
					members[header.Name] = content
				}
				var manifest deploy.Manifest
				if err = json.Unmarshal(members["manifest.json"], &manifest); err != nil {
					return err
				}
				if _, err = deploy.Open(manifest, raw, hashBytes(raw), "", nil); err != nil {
					return err
				}
				normalized := map[string]any{}
				for name, content := range members {
					var value any
					if name == "manifest.json" {
						if err = json.Unmarshal(content, &value); err != nil {
							return err
						}
						normalizePublication(value, replacements)
					} else {
						value = string(content)
					}
					normalized[name] = value
				}
				if strings.HasPrefix(filepath.Base(rel), "bundle-") {
					rel = filepath.Join(filepath.Dir(rel), "bundle-@archive.tar.gz")
				}
				out[rel] = normalized
			} else if strings.HasSuffix(rel, ".json") {
				var value any
				if err = json.Unmarshal(raw, &value); err != nil {
					return fmt.Errorf("%s: %w", rel, err)
				}
				normalizePublication(value, replacements)
				out[rel] = value
			} else {
				out[rel] = string(raw)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	x, y := collect(a, values[0]), collect(b, values[1])
	if !reflect.DeepEqual(x, y) {
		onlyLeft, onlyRight := []string{}, []string{}
		for key := range x {
			if _, ok := y[key]; !ok {
				onlyLeft = append(onlyLeft, key)
			}
		}
		for key := range y {
			if _, ok := x[key]; !ok {
				onlyRight = append(onlyRight, key)
			}
		}
		sort.Strings(onlyLeft)
		sort.Strings(onlyRight)
		t.Fatalf("complete DATA_DIR files: %s; Python-only=%v; Go-only=%v", publishDifference("", x, y), onlyLeft, onlyRight)
	}
}
