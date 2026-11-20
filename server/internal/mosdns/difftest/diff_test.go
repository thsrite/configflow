// Package difftest is the Flask↔Go differential of C28: the 15 MosDNS
// routes (editor settings and the rule proxy) on copies of one data
// directory, with a loopback fake rule server. Status, JSON (structurally),
// text bodies, the cache headers, the stored MosDNS settings, the rule
// caches and task-status.json (times masked) must match.
//
//	rt -- bash -lc 'cd server && CONFIGFLOW_DIFF=1 go test -count=1 -v ./internal/mosdns/difftest/'
package difftest

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"configflow/internal/auth"
	"configflow/internal/localhttp"
	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/server"
	"configflow/internal/substore"
)

const (
	username = "diff-admin"
	password = "diff-password"
	secret   = "diff-shared-jwt-secret"
	domain   = "http://127.0.0.1:5001"
	cfgToken = "diff-config-token"
	retired  = "diff-retired-rule-token"
)

func freePort(t *testing.T) int {
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func upstream(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/list.txt":
			io.WriteString(w, "+.new.test\nexample.test\n")
		case "/classical.txt":
			io.WriteString(w, "DOMAIN,exact.test\nDOMAIN-SUFFIX,suffix.test\nIP-CIDR,192.0.2.0/24,no-resolve\n")
		case "/bad.txt":
			io.WriteString(w, "DOMAIN,bad.test\n")
		case "/down.txt":
			http.Error(w, "down", http.StatusServiceUnavailable)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func seed(t *testing.T, dir, base string) {
	r, err := repo.OpenDefault(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := r.UpdateSystem(ctx, func(_ context.Context, system *model.System) error {
		system.SystemConfig.ServerDomain = domain
		system.SystemConfig.ConfigToken = cfgToken
		system.SystemConfig.RetiredRuleProxyTokens = []string{retired}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	library := []any{
		map[string]any{"id": "inline", "name": "Inline", "source_type": "content", "content": "+.inline.test\nDOMAIN,exact.inline.test", "behavior": "classical", "format": "text"},
		map[string]any{"id": "remote", "name": "Remote", "source_type": "url", "url": base + "/list.txt", "behavior": "domain", "format": "text"},
		map[string]any{"id": "classical", "name": "Classical", "source_type": "url", "url": base + "/classical.txt", "behavior": "classical", "format": "text"},
		map[string]any{"id": "bad", "name": "Bad", "source_type": "url", "url": base + "/bad.txt", "behavior": "domain", "format": "text"},
		map[string]any{"id": "binary", "name": "Binary", "source_type": "url", "url": base + "/list.mrs", "behavior": "domain", "format": "mrs"},
		map[string]any{"id": "inline-off", "name": "InlineOff", "source_type": "content", "content": "+.off.test", "behavior": "domain", "format": "text"},
	}
	if err := r.UpdateShared(ctx, func(_ context.Context, shared *model.Shared) error {
		raw, err := json.Marshal(shared.RuleLibrary)
		if err != nil {
			return err
		}
		var items []any
		if err := json.Unmarshal(raw, &items); err != nil {
			return err
		}
		items = append(items, library...)
		merged, err := json.Marshal(items)
		if err != nil {
			return err
		}
		return shared.SetJSON("rule_library", merged)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.UpdateProfile(ctx, "default", func(_ context.Context, profile *model.Profile) error {
		raw, err := json.Marshal(profile.RuleConfigs)
		if err != nil {
			return err
		}
		var configs []any
		if err := json.Unmarshal(raw, &configs); err != nil {
			return err
		}
		for _, id := range []string{"inline", "remote", "classical", "bad", "binary"} {
			configs = append(configs, map[string]any{"id": "sel-" + id, "itemType": "ruleset", "library_rule_id": id,
				"policy": "DIRECT", "enabled": true})
		}
		// Hand-edited values: enabled null (Python: not selected) and a
		// numeric library_rule_id on another rule.
		configs = append(configs, map[string]any{"id": "sel-inline-off", "itemType": "ruleset", "library_rule_id": "inline-off",
			"policy": "DIRECT", "enabled": nil},
			map[string]any{"id": "odd", "itemType": "rule", "rule_type": "DOMAIN", "value": "odd.test", "policy": "DIRECT",
				"library_rule_id": 7, "enabled": "yes"})
		merged, err := json.Marshal(configs)
		if err != nil {
			return err
		}
		return profile.SetJSON("rule_configs", merged)
	}); err != nil {
		t.Fatal(err)
	}
}

func startPython(t *testing.T, dataDir string) string {
	python := os.Getenv("CONFIGFLOW_DIFF_PYTHON")
	if python == "" {
		home, _ := os.UserHomeDir()
		python = filepath.Join(home, "cf-test/.venv/bin/python")
	}
	root, _ := filepath.Abs("../../../..")
	port := freePort(t)
	cmd := exec.Command(python, "-m", "backend.app")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "DATA_DIR="+dataDir, "LOG_FILE="+filepath.Join(dataDir, "app.log"),
		"CONFIGFLOW_BACKEND_HOST=127.0.0.1", "CONFIGFLOW_BACKEND_PORT="+strconv.Itoa(port),
		"ADMIN_USERNAME="+username, "ADMIN_PASSWORD="+password, "JWT_SECRET_KEY="+secret, "LOG_LEVEL=INFO",
		"PYTHONDONTWRITEBYTECODE=1", "SUB_STORE_BUILTIN_DIR=/nonexistent", "SUB_STORE_URL=http://127.0.0.1:1",
		"HTTPS_PROXY=", "HTTP_PROXY=", "https_proxy=", "http_proxy=", "NO_PROXY=127.0.0.1,localhost")
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Wait()
		if t.Failed() {
			t.Logf("python output:\n%s", output.String())
		}
	})
	base := "http://127.0.0.1:" + strconv.Itoa(port)
	for deadline := time.Now().Add(90 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		if resp, err := http.Get(base + "/api/auth/status"); err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return base
			}
		}
	}
	t.Fatalf("Flask did not start:\n%s", output.String())
	return ""
}

func startGo(t *testing.T, dataDir string) string {
	t.Setenv("DATA_DIR", dataDir)
	for _, key := range []string{"HTTPS_PROXY", "HTTP_PROXY", "https_proxy", "http_proxy"} {
		t.Setenv(key, "")
	}
	root, err := logx.Setup(logx.Config{Level: logx.Info, File: filepath.Join(dataDir, "app.log")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	previous := auth.Configure(auth.Config{AdminUsername: username, AdminPassword: password, SecretKey: secret})
	t.Cleanup(func() { auth.Configure(previous) })
	r, err := repo.OpenDefault(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	repo.SetRepository(r)
	t.Cleanup(func() { repo.SetRepository(nil) })
	t.Cleanup(substore.SetDefault(substore.NewService(substore.NewEnv())))
	app := server.NewApp()
	// main registers the in-process dispatch of the server's own API
	// (callback URLs of the rule proxy).
	app.RegisterLocal()
	t.Cleanup(func() { localhttp.Register(nil, nil) })
	if _, err := repo.LoadConfig(repo.WithScope(context.Background())); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(app)
	t.Cleanup(srv.Close)
	return srv.URL
}

func login(t *testing.T, base string) string {
	resp, err := http.Post(base+"/api/auth/login", "application/json",
		strings.NewReader(`{"username": "`+username+`", "password": "`+password+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct{ Token string }
	json.NewDecoder(resp.Body).Decode(&body)
	return "Bearer " + body.Token
}

type request struct {
	name, method, path, body, contentType string
	anonymous                             bool
	header                                string // Authorization of an anonymous request
	deferred                              string // module whose missing route the Go answer depends on
}

type answer struct {
	Status  int
	JSON    any
	Text    string
	Headers map[string]string
}

func send(t *testing.T, base, token string, r request) answer {
	var body io.Reader
	if r.body != "" {
		body = strings.NewReader(r.body)
	}
	req, _ := http.NewRequest(r.method, base+r.path, body)
	if r.body != "" {
		contentType := r.contentType
		if contentType == "" {
			contentType = "application/json"
		}
		req.Header.Set("Content-Type", contentType)
	}
	if !r.anonymous {
		req.Header.Set("Authorization", token)
	} else if r.header != "" {
		req.Header.Set("Authorization", r.header)
	}
	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("%s: %v", r.name, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	out := answer{Status: resp.StatusCode, Headers: map[string]string{}}
	for _, key := range []string{"Cache-Control", "X-ConfigFlow-Rule-Cache", "Content-Type"} {
		out.Headers[key] = resp.Header.Get(key)
	}
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
		json.Unmarshal(data, &out.JSON)
	} else if resp.StatusCode < 500 {
		out.Text = string(data)
	}
	return out
}

func maskTimes(value any) any {
	switch v := value.(type) {
	case map[string]any:
		for key, item := range v {
			if strings.HasSuffix(key, "_at") || key == "last_duration_ms" {
				if item != nil {
					v[key] = "<set>"
				}
				continue
			}
			v[key] = maskTimes(item)
		}
	case []any:
		for i := range v {
			v[i] = maskTimes(v[i])
		}
	}
	return value
}

// written is the data the module writes: the default profile's MosDNS
// settings, the rule caches and task-status.json.
func written(t *testing.T, dir string) map[string]any {
	out := map[string]any{}
	r, err := repo.Open(dir, repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := r.Profile(context.Background(), "default")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(profile.MosDNS)
	var settings any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber() // integers of any size compare exactly
	decoder.Decode(&settings)
	out["profile.mosdns"] = settings
	var status any
	if data, err := os.ReadFile(filepath.Join(dir, "task-status.json")); err == nil {
		json.Unmarshal(data, &status)
	}
	out["task-status.json"] = maskTimes(status)
	filepath.WalkDir(filepath.Join(dir, "shared", "rules"), func(path string, entry fs.DirEntry, err error) error {
		if err == nil && entry.Type().IsRegular() && !strings.HasSuffix(path, ".lock") {
			rel, _ := filepath.Rel(dir, path)
			content, _ := os.ReadFile(path)
			out[rel] = string(content)
		}
		return nil
	})
	return out
}

func ruleToken(t *testing.T, dir string) string {
	r, err := repo.Open(dir, repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	system, err := r.System(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return system.SystemConfig.RuleProxyToken
}

func TestDifferentialMosDNSRoutes(t *testing.T) {
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("set CONFIGFLOW_DIFF=1 on the test server")
	}
	up := upstream(t)
	base := t.TempDir()
	seedDir, pyDir, goDir := filepath.Join(base, "seed"), filepath.Join(base, "python"), filepath.Join(base, "go")
	for _, dir := range []string{seedDir, pyDir, goDir} {
		os.MkdirAll(dir, 0o755)
	}
	seed(t, seedDir, up.URL)
	for _, dir := range []string{pyDir, goDir} {
		if out, err := exec.Command("cp", "-a", seedDir+"/.", dir).CombinedOutput(); err != nil {
			t.Fatalf("cp: %v %s", err, out)
		}
	}
	token := ruleToken(t, seedDir)
	pyBase := startPython(t, pyDir)
	goBase := startGo(t, goDir)
	pyToken, goToken := login(t, pyBase), login(t, goBase)

	proxy := func(name, path string, query url.Values, anonymous bool) request {
		return request{name: name, method: "GET", path: path + "?" + query.Encode(), anonymous: anonymous}
	}
	q := func(pairs ...string) url.Values {
		v := url.Values{}
		for i := 0; i < len(pairs); i += 2 {
			v.Set(pairs[i], pairs[i+1])
		}
		return v
	}
	const rp = "/api/mosdns/rule-proxy"
	const prp = "/api/profiles/default/mosdns/rule-proxy"
	inlineURL := domain + "/api/profiles/default/rule-library/content/inline"
	requests := []request{}
	for _, route := range []string{"rulesets", "custom-matches", "dns-servers", "log-settings", "api-settings", "cache-settings"} {
		requests = append(requests, request{name: "get " + route, method: "GET", path: "/api/mosdns/" + route})
	}
	requests = append(requests,
		request{name: "post rulesets", method: "POST", path: "/api/mosdns/rulesets", body: `{"direct_rulesets": ["sel-inline"], "proxy_rulesets": ["sel-remote"]}`},
		request{name: "post custom-matches", method: "POST", path: "/api/mosdns/custom-matches", body: `{"custom_matches": ["qname domain:a.test"], "position": "head"}`},
		request{name: "get custom-matches stored string", method: "GET", path: "/api/mosdns/custom-matches"},
		request{name: "post custom-matches empty", method: "POST", path: "/api/mosdns/custom-matches", body: `{}`},
		request{name: "post dns-servers", method: "POST", path: "/api/mosdns/dns-servers", body: `{"local_dns": "223.5.5.5", "remote_dns": "tls://1.1.1.1", "custom_hosts": "a.test 192.0.2.1"}`},
		request{name: "post log-settings", method: "POST", path: "/api/mosdns/log-settings", body: `{"log_enabled": false, "log_level": "debug", "log_file": "m.log"}`},
		request{name: "post api-settings", method: "POST", path: "/api/mosdns/api-settings", body: `{"api_enabled": false, "api_address": ":9090"}`},
		request{name: "post cache-settings ASCII integers", method: "POST", path: "/api/mosdns/cache-settings", body: `{"cache_size":"128","cache_lazy_ttl":"60","cache_dump_interval":"20","cache_enabled":true,"cache_dump_file":"cache.dump"}`},
		request{name: "post cache-settings null", method: "POST", path: "/api/mosdns/cache-settings", body: `null`},
		request{name: "post settings", method: "POST", path: "/api/mosdns/settings", body: `{"custom_config": "plugins: []", "direct_rulesets": ["sel-classical"], "local_dns": "119.29.29.29", "cache_size": 256, "api_address": ":8338", "unknown": 1}`},
		request{name: "post settings profile", method: "POST", path: "/api/mosdns/settings?profile=default", body: `{"log_level": "warn"}`},
		request{name: "post settings not object", method: "POST", path: "/api/mosdns/settings", body: `[1]`},
		request{name: "post settings missing rule", method: "POST", path: "/api/mosdns/settings", body: `{"proxy_rules": ["missing"]}`},
		request{name: "post settings unknown profile", method: "POST", path: "/api/mosdns/settings?profile=nope", body: `{}`},
	)
	for _, route := range []string{"rulesets", "custom-matches", "dns-servers", "log-settings", "api-settings", "cache-settings"} {
		requests = append(requests, request{name: "get after " + route, method: "GET", path: "/api/mosdns/" + route})
	}
	requests = append(requests,
		proxy("proxy inline", rp, q("url", inlineURL, "token", token, "part", "domain"), true),
		proxy("proxy inline config token", rp, q("url", inlineURL, "token", cfgToken), true),
		proxy("proxy inline jwt", prp, q("url", inlineURL, "part", "ip"), false),
		proxy("proxy remote declared", rp, q("url", up.URL+"/list.txt", "source_id", "remote", "token", token), true),
		proxy("proxy remote cached", prp, q("url", up.URL+"/list.txt", "source_id", "remote", "token", token, "part", "domain"), true),
		proxy("proxy classical ip", rp, q("url", up.URL+"/classical.txt", "token", token, "part", "ip"), true),
		proxy("proxy invalid content", rp, q("url", up.URL+"/bad.txt", "source_id", "bad", "token", token), true),
		proxy("proxy mrs", rp, q("url", up.URL+"/list.mrs", "source_id", "binary", "token", token), true),
		proxy("proxy undeclared down", rp, q("url", up.URL+"/down.txt", "token", token), true),
		proxy("proxy undeclared ok", rp, q("url", up.URL+"/other.txt?x=1", "token", token), true),
		proxy("proxy changed source", rp, q("url", up.URL+"/moved.txt", "source_id", "remote", "token", token), true),
		proxy("proxy no token", rp, q("url", inlineURL), true),
		proxy("proxy wrong token", prp, q("url", inlineURL, "token", "wrong"), true),
		proxy("proxy no url", rp, q("token", token), true),
		proxy("proxy bad part", rp, q("url", inlineURL, "token", token, "part", "both"), true),
		proxy("proxy ftp", rp, q("url", "ftp://127.0.0.1/rules", "token", token), true),
		withHeader(proxy("proxy retired bearer", rp, q("url", inlineURL, "token", token), true), "Bearer "+retired),
		proxy("proxy retired url token", rp, q("url", inlineURL, "token", retired), true),
		withHeader(proxy("proxy bad bearer + token", rp, q("url", inlineURL, "token", token), true), "Bearer not-a-jwt"),
		withHeader(proxy("proxy basic header", rp, q("url", inlineURL), true), "Basic YTpi"),
		withHeader(proxy("proxy rule token as bearer", rp, q("url", inlineURL), true), "Bearer "+token),
		withHeader(proxy("proxy empty bearer", rp, q("url", inlineURL, "token", cfgToken), true), "Bearer "),
		proxy("proxy long token", rp, q("url", inlineURL, "token", strings.Repeat("a", 5000)), true),
		proxy("proxy malformed url", rp, q("url", "http://a b/x", "token", token), true),
		proxy("proxy bad escape url", rp, q("url", "http://127.0.0.1:1/a%zz", "token", token), true),
		proxy("proxy relative url", rp, q("url", "/api/profiles/default/rule-library/content/inline", "token", token), true),
		// Python serves these through an in-process call of
		// C33 serves both callbacks through the complete local dispatch path.
		proxy("proxy userinfo inline", rp, q("url", "http://u@127.0.0.1:5001/api/profiles/default/rule-library/content/inline", "token", token), true),
		proxy("proxy inline disabled by null", rp, q("url", domain+"/api/profiles/default/rule-library/content/inline-off", "token", token), true),
		proxy("proxy unknown profile", "/api/profiles/nope/mosdns/rule-proxy", q("url", inlineURL, "token", token), true),
	)
	failed := 0
	for _, r := range requests {
		p := send(t, pyBase, pyToken, r)
		g := send(t, goBase, goToken, r)
		// Python-internal exception texts (AttributeError on a JSON body
		// that is not an object) are not chased: same status, own text.
		if pm, ok := p.JSON.(map[string]any); ok && p.Status == 500 &&
			strings.Contains(jsonText(pm["message"]), "object has no attribute") {
			if gm, ok := g.JSON.(map[string]any); ok {
				pm["message"], gm["message"] = "<python exception>", "<python exception>"
			}
		}
		if !reflect.DeepEqual(p, g) && r.deferred != "" && g.Status == 502 {
			t.Logf("deferred %-28s → python %d, go %d (route of %s missing)", r.name, p.Status, g.Status, r.deferred)
		} else if !reflect.DeepEqual(p, g) {
			failed++
			t.Errorf("DIFF %s %s %s:\n  python %+v\n  go     %+v", r.name, r.method, r.path, p, g)
		} else {
			t.Logf("same %-32s → %d", r.name, p.Status)
		}
	}
	pyFiles, goFiles := written(t, pyDir), written(t, goDir)
	applyNativeCachePaths(t, up.URL, pyFiles)
	for name := range mergeKeys(pyFiles, goFiles) {
		if !reflect.DeepEqual(pyFiles[name], goFiles[name]) {
			failed++
			t.Errorf("DIFF data %s:\n  python %v\n  go     %v", name, pyFiles[name], goFiles[name])
		}
	}
	if failed > 0 {
		goLog, _ := os.ReadFile(filepath.Join(goDir, "app.log"))
		t.Logf("go log:\n%s", goLog)
	}
	t.Logf("%d requests, %d data items, %d differences", len(requests), len(mergeKeys(pyFiles, goFiles)), failed)
}

func deferTo(module string, r request) request { r.deferred = module; return r }

func withHeader(r request, header string) request { r.header = header; return r }

func jsonText(v any) string { s, _ := v.(string); return s }

func mergeKeys(a, b map[string]any) map[string]bool {
	out := map[string]bool{}
	for k := range a {
		out[k] = true
	}
	for k := range b {
		out[k] = true
	}
	return out
}

// applyNativeCachePaths binds only the two sources this test downloads. Real
// Python supplies its own address; independent canonical JSON supplies D2's
// native address. Cache bytes and every other stored path remain strict.
func applyNativeCachePaths(t *testing.T, base string, files map[string]any) {
	t.Helper()
	python := os.Getenv("CONFIGFLOW_DIFF_PYTHON")
	if python == "" {
		home, _ := os.UserHomeDir()
		python = filepath.Join(home, "cf-test/.venv/bin/python")
	}
	const script = `import hashlib,json,sys
from backend.utils.rule_utils import rule_cache_filename
result={}
for ident,endpoint,behavior in [('remote','/list.txt','domain'),('classical','/classical.txt','classical')]:
 rule={'id':ident,'url':sys.argv[1]+endpoint,'behavior':behavior,'format':'text','source_type':'url'}
 old=rule_cache_filename(rule)
 native=json.dumps([ident,rule['url'],behavior,'text'],ensure_ascii=False,separators=(',',':'))
 result['shared/rules/'+old]='shared/rules/by-source/'+hashlib.sha256(native.encode()).hexdigest()+'.list'
print(json.dumps(result))`
	root, _ := filepath.Abs("../../../..")
	cmd := exec.Command(python, "-c", script, base)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "PYTHONPATH=.", "PYTHONDONTWRITEBYTECODE=1")
	output, err := cmd.Output()
	if err != nil {
		t.Fatal("independent D2 cache reference", err)
	}
	var paths map[string]string
	if err := json.Unmarshal(output, &paths); err != nil || len(paths) != 2 {
		t.Fatal("invalid exact D2 cache addresses", err)
	}
	for old, native := range paths {
		content, ok := files[old]
		if !ok || files[native] != nil || old == native {
			t.Fatal("D2 reference does not bind a unique original cache")
		}
		delete(files, old)
		files[native] = content
	}
}
