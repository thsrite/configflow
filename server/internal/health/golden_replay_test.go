package health_test

import (
	"context"
	"encoding/json"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"configflow/internal/auth"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/server"
	"configflow/internal/testgolden"
)

const healthProbeScript = `#!/bin/sh
cat > "$FAKE_PROBE_DIR/input.json"
cat "$FAKE_PROBE_DIR/stdout"
echo "worker stderr with https://user:secret@dns.example" >&2
exit "$(cat "$FAKE_PROBE_DIR/code")"
`

func normalizeHealthGolden(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			if k == "tested_at" && val != nil {
				out[k] = "<set>"
				continue
			}
			out[k] = normalizeHealthGolden(val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = normalizeHealthGolden(item)
		}
		return out
	}
	return v
}

func TestHealthLatencyGoldenReplay(t *testing.T) {
	ref := testgolden.New(t, testgolden.Spec{Compress: true})

	fake := t.TempDir()
	script := filepath.Join(fake, "configflow-node-probe")
	if err := os.WriteFile(script, []byte(healthProbeScript), 0o755); err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closed := "http://127.0.0.1:" + strconv.Itoa(l.Addr().(*net.TCPAddr).Port)
	_ = l.Close()

	t.Setenv("SUB_STORE_BUILTIN_DIR", "/nonexistent")
	t.Setenv("SUB_STORE_URL", closed)
	t.Setenv("CONFIGFLOW_NODE_PROBE_BIN", script)
	t.Setenv("FAKE_PROBE_DIR", fake)
	t.Setenv("CONFIGFLOW_NODE_TEST_TIMEOUT_MS", "500")
	t.Setenv("NO_PROXY", "127.0.0.1,localhost")

	dataDir := t.TempDir()
	t.Setenv("DATA_DIR", dataDir)
	prev := auth.Configure(auth.Config{AdminUsername: "diff-admin", AdminPassword: "diff-password", SecretKey: "health-secret"})
	t.Cleanup(func() { auth.Configure(prev) })

	r, err := repo.OpenDefaultWithOptions(dataDir, server.RepoOptions())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)

	ctx := context.Background()
	if err := r.UpdateShared(ctx, func(_ context.Context, shared *model.Shared) error {
		nodes := []any{
			map[string]any{"id": "a", "name": "A", "type": "trojan", "server": "example.invalid", "port": 443, "password": "test-only", "tls": true},
			map[string]any{"id": "j", "name": "香港 J", "proxy_string": `{"type":"http","server":"local.invalid","port":8080}`},
			map[string]any{"id": "u", "name": "URI", "proxy_string": "ss://YWVzLTEyOC1nY206cA@uri.invalid:8388#x"},
			map[string]any{"id": "off", "name": "Off", "type": "http", "server": "o.invalid", "port": 1, "enabled": false},
		}
		raw, _ := json.Marshal(nodes)
		return shared.SetJSON("nodes", raw)
	}); err != nil {
		t.Fatal(err)
	}

	full, err := server.New(ctx, server.Options{Repo: r})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(full.Agents.Close)
	app := full.HTTP
	loginReq := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{"username":"diff-admin","password":"diff-password"}`))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	app.ServeHTTP(loginRec, loginReq)
	var loginResp struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(loginRec.Body.Bytes(), &loginResp)
	authHeader := "Bearer " + loginResp.Token

	a := `"[\"node\",\"a\"]"`
	j := `"[\"node\",\"j\"]"`
	results := func(entries ...string) string { return `{"version":4,"results":[` + strings.Join(entries, ",") + `]}` }
	all := results(`{"name":`+a+`,"latency":120,"first_latency":300}`, `{"name":`+j+`,"latency":null,"error_code":"timeout","error":"连接或请求超时"}`)
	onlyA := results(`{"name":` + a + `,"latency":99}`)
	check := func(res string) string { return `{"version":4,"dns_check":` + res + `}` }

	type workerCfg struct {
		stdout string
		code   int
	}
	type req struct {
		name, method, path, body string
		w                        workerCfg
	}
	const L, S, C = "/api/nodes/latency", "/api/nodes/latency/settings", "/api/nodes/latency/dns/check"
	reqs := []req{
		{"results-initial", "GET", L, "", workerCfg{}},
		{"settings-initial", "GET", S, "", workerCfg{}},
		{"all-candidates", "POST", L, `{}`, workerCfg{stdout: all}},
		{"names-A", "POST", L, `{"names":["A","A","missing"]}`, workerCfg{stdout: onlyA}},
		{"keys-with-off", "POST", L, `{"keys":["[\"node\",\"a\"]","[\"node\",\"off\"]"]}`, workerCfg{stdout: onlyA}},
		{"uri-conversion-fail", "POST", L, `{"keys":["[\"node\",\"u\"]"]}`, workerCfg{stdout: onlyA}},
		{"old-worker", "POST", L, `{"names":["A"]}`, workerCfg{stdout: `{"version":3,"results":[]}`}},
		{"worker-dns-exit-3", "POST", L, `{"names":["A"]}`, workerCfg{code: 3}},
		{"worker-exit-1", "POST", L, `{"names":["A"]}`, workerCfg{stdout: onlyA, code: 1}},
		{"worker-garbage", "POST", L, `{"names":["A"]}`, workerCfg{stdout: "not json"}},
		{"worker-wrong-names", "POST", L, `{"names":["A"]}`, workerCfg{stdout: results(`{"name":"A","latency":1}`)}},
		{"worker-bool-latency", "POST", L, `{"names":["A"]}`, workerCfg{stdout: results(`{"name":` + a + `,"latency":true}`)}},
		{"names-not-list", "POST", L, `{"names":"A"}`, workerCfg{}},
		{"keys-not-strings", "POST", L, `{"keys":[1]}`, workerCfg{}},
		{"names-and-keys", "POST", L, `{"names":[],"keys":[]}`, workerCfg{}},
		{"list-body", "POST", L, `[]`, workerCfg{}},
		{"results-after", "GET", L, "", workerCfg{}},
		{"settings-dns-servers", "POST", S, `{"dns_servers":[" 192.0.2.53 ","192.0.2.53:53","127.0.0.1:5353"]}`, workerCfg{}},
		{"settings-invalid", "POST", S, `{"dns_servers":["https://user:secret@dns.example/q"]}`, workerCfg{}},
		{"settings-managed", "POST", S, `{"dns":{"mode":"managed"}}`, workerCfg{}},
		{"settings-none", "POST", S, `{}`, workerCfg{}},
		{"settings-not-object", "POST", S, `"x"`, workerCfg{}},
		{"settings-custom", "POST", S, `{"dns":{"mode":"custom","servers":["https://DNS.EXAMPLE","tls://192.0.2.9:853"],"bootstrap":["192.0.2.53:53"]}}`, workerCfg{}},
		{"settings-after", "GET", S, "", workerCfg{}},
		{"check-success", "POST", C, `{"dns":{"mode":"custom","servers":["tls://dns.example"],"bootstrap":["192.0.2.53"]}}`,
			workerCfg{stdout: check(`{"success":true,"addresses":["192.0.2.1","2001:DB8::1","192.0.2.1"],"latency_ms":12}`)}},
		{"check-saved-null", "POST", C, `{"dns":null}`,
			workerCfg{stdout: check(`{"success":false,"addresses":[],"error_code":"tls","error":"raw secret"}`)}},
		{"check-fake-ip", "POST", C, `{"dns":{"mode":"system"}}`,
			workerCfg{stdout: check(`{"success":true,"addresses":["198.18.0.5"]}`)}},
		{"check-old-worker", "POST", C, `{"dns":{"mode":"system"}}`, workerCfg{stdout: `{"version":3}`}},
		{"check-invalid-dns", "POST", C, `{"dns":{"mode":"custom","servers":[]}}`, workerCfg{}},
	}

	for _, tc := range reqs {
		t.Run(tc.name, func(t *testing.T) {
			_ = os.WriteFile(filepath.Join(fake, "stdout"), []byte(tc.w.stdout), 0o600)
			_ = os.WriteFile(filepath.Join(fake, "code"), []byte(strconv.Itoa(tc.w.code)), 0o600)
			httpReq := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			if tc.body != "" {
				httpReq.Header.Set("Content-Type", "application/json")
			}
			httpReq.Header.Set("Authorization", authHeader)
			rec := httptest.NewRecorder()
			app.ServeHTTP(rec, httpReq)
			var parsed any
			if err := json.Unmarshal(rec.Body.Bytes(), &parsed); err != nil {
				parsed = rec.Body.String()
			}
			key, _ := json.Marshal(map[string]string{"case": tc.name})
			val, _ := json.Marshal(normalizeHealthGolden(map[string]any{
				"status": rec.Code,
				"body":   parsed,
			}))
			ref.Assert(t, key, val)
		})
	}
}
