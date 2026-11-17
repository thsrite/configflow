package difftest

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"configflow/internal/agents/agentfake"
	"configflow/internal/deploy"
	"configflow/internal/jsonx"
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
	_, e = r.UpdateSystemTransaction(ctx, func(_ context.Context, s *jsonx.Object) (*jsonx.Object, error) {
		s.Set("system_config", jsonx.Obj("server_domain", "https://panel.example.com"))
		s.Set("agents", []any{jsonx.Obj("id", "a14-agent", "name", "Synthetic", "token", "a14-synthetic-token", "host", "127.0.0.1", "port", port, "service_type", "mihomo", "profile_id", "default", "deployment_method", "docker", "version", "1.8.60-go", "enabled", true)})
		return s, nil
	})
	if e != nil {
		t.Fatal(e)
	}
	_, _, e = r.UpdateProfileFields(ctx, "default", jsonx.Obj("mihomo", jsonx.Obj("custom_config", "mode: rule\n"), "proxy_groups", []any{}, "rule_configs", []any{}), nil)
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
	_, e = gr.UpdateSystemTransaction(context.Background(), func(_ context.Context, s *jsonx.Object) (*jsonx.Object, error) {
		s.GetList("agents")[0].(*jsonx.Object).Set("port", port(gs))
		return s, nil
	})
	if e != nil {
		t.Fatal(e)
	}
	// Keep initial write counts identical; only target port differs.
	pr, e := repo.Open(pyDir, repo.Options{})
	if e != nil {
		t.Fatal(e)
	}
	_, e = pr.UpdateSystemTransaction(context.Background(), func(_ context.Context, s *jsonx.Object) (*jsonx.Object, error) { return s, nil })
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
	// Every stored member is compared, including private task proofs and full source snapshots.
	docs := []any{}
	for _, dir := range []string{pyDir, goDir} {
		entry, e := store.New(filepath.Join(dir, "configflow.sqlite3"), func() time.Duration { return 10 * time.Second }).Load()
		if e != nil {
			t.Fatal(e)
		}
		raw, e := json.Marshal(entry.Document)
		if e != nil {
			t.Fatal(e)
		}
		var v any
		_ = json.Unmarshal(raw, &v)
		docs = append(docs, v)
	}
	for i, v := range docs {
		normalizePublication(v, values[i])
	}
	if !reflect.DeepEqual(docs[0], docs[1]) {
		t.Fatal("complete DB document: ", publishDifference("", docs[0], docs[1]))
	}
	comparePublicationFiles(t, pyDir, goDir, values)
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
func comparePublicationFiles(t *testing.T, a, b string, values []map[string]string) {
	t.Helper()
	collect := func(root string, replacements map[string]string) map[string]any {
		out := map[string]any{}
		for _, sub := range []string{".agent-rule-sync", ".generated-previews"} {
			e := filepath.WalkDir(filepath.Join(root, sub), func(path string, entry os.DirEntry, e error) error {
				if os.IsNotExist(e) {
					return nil
				}
				if e != nil {
					return e
				}
				if entry.IsDir() {
					return nil
				}
				if entry.Name() == "lock" {
					return nil
				} // Python flock artifacts are intentionally absent under O3.
				rel, _ := filepath.Rel(root, path)
				for original, replacement := range replacements {
					rel = strings.ReplaceAll(rel, original, replacement)
				}
				raw, e := os.ReadFile(path)
				if e != nil {
					return e
				}
				if strings.HasSuffix(rel, ".tar.gz") {
					rel = filepath.Join(filepath.Dir(rel), "bundle-@archive.tar.gz")
					out[rel] = "archive validated by raw Agent transcript"
				} else {
					var v any
					if strings.HasSuffix(rel, ".json") {
						if e = json.Unmarshal(raw, &v); e != nil {
							return fmt.Errorf("%s: %w", rel, e)
						}
						normalizePublication(v, replacements)
						out[rel] = v
					} else {
						out[rel] = string(raw)
					}
				}
				return nil
			})
			if e != nil {
				t.Fatal(e)
			}
		}
		return out
	}
	x, y := collect(a, values[0]), collect(b, values[1])
	if !reflect.DeepEqual(x, y) {
		t.Fatal("private files: ", publishDifference("", x, y))
	}
}
