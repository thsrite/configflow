package linuxgateway_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"configflow/internal/auth"
	"configflow/internal/convert"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/server/servertest"
	"configflow/internal/testgolden"
	"configflow/internal/yamlx"
)

type lgWireRequest struct {
	Method      string `json:"method"`
	Path        string `json:"path"`
	Auth        string `json:"auth"`
	Accept      string `json:"accept"`
	ContentType string `json:"content_type"`
	Body        string `json:"body"`
}

type lgFakeAgent struct {
	mu    sync.Mutex
	phase string
	calls []lgWireRequest
}

func (f *lgFakeAgent) setPhase(phase string) {
	f.mu.Lock()
	f.phase = phase
	f.mu.Unlock()
}

func (f *lgFakeAgent) take() []lgWireRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.calls
	f.calls = nil
	return out
}

func (f *lgFakeAgent) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	phase := f.phase
	f.calls = append(f.calls, lgWireRequest{
		Method:      r.Method,
		Path:        r.URL.Path,
		Auth:        r.Header.Get("Authorization"),
		Accept:      r.Header.Get("Accept"),
		ContentType: r.Header.Get("Content-Type"),
		Body:        string(raw),
	})
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	status := 200
	body := `{"success":true,"supported":true,"managed":{"active":false},"ipv4_forwarding":{"supported":true,"current":"1","persisted":true,"layout":"native_aio"}}`
	switch phase {
	case "record":
		body = `{"success":true,"supported":true,"managed":{"active":true,"preserves_forwarding":false},"ipv4_forwarding":{"supported":true,"current":"1"}}`
	case "kept":
		body = `{"success":true,"managed":{"active":true,"preserves_forwarding":true}}`
	case "notjson":
		body = "not json"
	case "array":
		body = "[]"
	case "big":
		body = strings.Repeat("x", 512*1024+1)
	case "401":
		status = 401
	case "404":
		status = 404
	case "409":
		status = 409
		body = `{"message":"合成设备拒绝"}`
	case "500":
		status = 500
		body = `{"error":"合成设备错误"}`
	case "filter":
		body = `{"success":true,"forward_filter":{"protocol":1,"state":"may_drop","device":"Meta","flows":["forward","dns"],"forward_protocols":["udp","tcp","udp"],"remedies":[{"source":"iptables","device":"Meta","commands":["iptables -I FORWARD -o Meta -j ACCEPT","iptables -F; reboot"]}]}}`
	}
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

type lgWorld struct {
	t              *testing.T
	srv            *httpxtest.Server
	token          string
	fake           *lgFakeAgent
	profileHeaders []string
}

func newLGWorld(t *testing.T) *lgWorld {
	t.Helper()
	w := &lgWorld{t: t, fake: &lgFakeAgent{}}
	fakeSrv := httptest.NewServer(w.fake)
	t.Cleanup(fakeSrv.Close)
	_, portText, _ := net.SplitHostPort(strings.TrimPrefix(fakeSrv.URL, "http://"))
	port, _ := strconv.Atoi(portText)
	srv, _ := servertest.New(t, httpxtest.Options{
		Auth:      true,
		Username:  "diff-admin",
		Password:  "diff-password",
		SecretKey: "a20-secret",
	})
	w.srv = srv
	ctx := w.srv.Context()
	r := w.srv.Repo
	if _, err := r.CreateProfileRecord(ctx, repo.ProfileCreation{ID: "home", Name: "Home"}, ""); err != nil {
		t.Fatal(err)
	}
	if err := r.UpdateSystem(ctx, func(_ context.Context, c *model.System) error {
		c.SystemConfig.ConfigToken = "a20-subscription"
		c.SystemConfig.ServerDomain = "http://localhost"
		raw, err := json.Marshal([]any{map[string]any{
			"id":                "gateway",
			"name":              "Gateway",
			"host":              "127.0.0.1",
			"port":              port,
			"token":             "a20-agent-token",
			"service_type":      "mihomo",
			"deployment_method": "shell",
			"profile_id":        "home",
			"version":           "1.8.43-go",
			"status":            "online",
			"enabled":           true,
			"last_heartbeat":    time.Now().UTC().Format(time.RFC3339Nano),
		}})
		if err != nil {
			return err
		}
		return c.SetJSON("agents", raw)
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.UpdateShared(ctx, func(_ context.Context, c *model.Shared) error {
		return c.SetJSON("rule_library", json.RawMessage(`[{"id":"lib","name":"IP","source_type":"content","content":"192.0.2.0/24\n198.51.100.0/24","behavior":"ipcidr","format":"text","enabled":true}]`))
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.UpdateProfile(ctx, "home", func(_ context.Context, c *model.Profile) error {
		if err := c.SetJSON("mihomo", json.RawMessage(`{"custom_config":"mixed-port: 7890\ndns: {enable: true, enhanced-mode: fake-ip, fake-ip-range: 198.18.0.1/16}\n","linux_gateway":{"enabled":true,"rule_set_ids":["r"]},"future":{"unchanged":[null,true,1.5]}}`)); err != nil {
			return err
		}
		if err := c.SetJSON("oxidns", json.RawMessage(`{"native_config":"plugins: []\n","future":"keep"}`)); err != nil {
			return err
		}
		return c.SetJSON("rule_configs", json.RawMessage(`[{"id":"r","itemType":"ruleset","library_rule_id":"lib","policy":"DIRECT","enabled":true}]`))
	}); err != nil {
		t.Fatal(err)
	}
	var err error
	w.token, err = auth.EncodeHS256(map[string]any{"username": "diff-admin", "exp": time.Now().Add(time.Hour).Unix()}, "a20-secret")
	if err != nil {
		t.Fatal(err)
	}
	return w
}

var (
	lgPreviewToken = regexp.MustCompile(`^[0-9a-f]{64}$`)
	lgRestoreID    = regexp.MustCompile(`^[0-9a-f]{32}$`)
	lgISOTime      = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}`)
)

func normalizeLGGolden(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, item := range x {
			switch k {
			case "created_at", "updated_at", "last_heartbeat":
				if s, ok := item.(string); ok && lgISOTime.MatchString(s) {
					out[k] = "<time>"
					continue
				}
			case "preview_token":
				if s, ok := item.(string); ok && lgPreviewToken.MatchString(s) {
					out[k] = "<preview-token>"
					continue
				}
			case "id":
				if s, ok := item.(string); ok && lgRestoreID.MatchString(s) {
					out[k] = "<restore-id>"
					continue
				}
			case "port":
				if _, hasHost := x["host"]; hasHost {
					out[k] = "<port>"
					continue
				}
			}
			out[k] = normalizeLGGolden(item)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = normalizeLGGolden(item)
		}
		return out
	case string:
		if lgISOTime.MatchString(x) {
			return "<time>"
		}
		return x
	}
	return v
}

func (w *lgWorld) call(ref *testgolden.Reference, label, method, path, body string, authHeader bool) (int, any) {
	w.t.Helper()
	request := httptest.NewRequest(method, "http://localhost"+path, strings.NewReader(body))
	if authHeader {
		request.Header.Set("Authorization", "Bearer "+w.token)
	}
	request.Header.Set("Content-Type", "application/json")
	for _, value := range w.profileHeaders {
		request.Header.Add("X-ConfigFlow-Profile", value)
	}
	rec := httptest.NewRecorder()
	w.srv.Handler().ServeHTTP(rec, request)
	var parsed any
	if method == "HEAD" {
		parsed = ""
	} else if strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
		if err := json.Unmarshal(rec.Body.Bytes(), &parsed); err != nil {
			w.t.Fatal(err)
		}
	} else if strings.HasPrefix(rec.Header().Get("Content-Type"), "text/yaml") {
		node, err := yamlx.Parse(rec.Body.Bytes())
		if err != nil {
			w.t.Fatal(err)
		}
		raw, err := yamlx.ToJSON(node)
		if err != nil {
			w.t.Fatal(err)
		}
		_ = json.Unmarshal(raw, &parsed)
	} else {
		parsed = rec.Body.String()
	}
	calls := w.fake.take()
	key, _ := json.Marshal(map[string]string{"case": label, "method": method, "path": path})
	actual, _ := json.Marshal(normalizeLGGolden(map[string]any{
		"status":        rec.Code,
		"content_type":  rec.Header().Get("Content-Type"),
		"cache_control": rec.Header().Get("Cache-Control"),
		"body":          parsed,
		"wire_calls":    calls,
	}))
	ref.Assert(w.t, key, actual)
	return rec.Code, parsed
}

func (w *lgWorld) editProfile(profileID, rawJSON string) {
	w.t.Helper()
	var updates map[string]json.RawMessage
	if err := json.Unmarshal([]byte(rawJSON), &updates); err != nil {
		w.t.Fatal(err)
	}
	if _, err := w.srv.Repo.UpdateProfile(w.srv.Context(), profileID, func(_ context.Context, c *model.Profile) error {
		for k, v := range updates {
			if err := c.SetJSON(k, v); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		w.t.Fatal(err)
	}
}

func TestLinuxGatewayGoldenReplay(t *testing.T) {
	ref := testgolden.New(t, testgolden.Spec{Compress: true})

	t.Run("editor-and-rules", func(t *testing.T) {
		w := newLGWorld(t)
		path := "/api/profiles/home/custom-config/linux-gateway"
		w.call(ref, "editor-get-auth", "GET", path, "", true)
		w.call(ref, "editor-get-unauth", "GET", path, "", false)
		w.call(ref, "editor-post-nan", "POST", path, `{"enabled":NaN}`, true)
		for i, body := range []string{`[]`, `null`, `{`, `{"enabled":1}`, `{"enabled":true,"rule_set_ids":["missing"]}`} {
			w.call(ref, fmt.Sprintf("editor-post-invalid-%d", i), "POST", path, body, true)
		}
		rev, _ := w.srv.Repo.ConfigRevision(w.srv.Context(), "home")
		body := fmt.Sprintf(`{"settings":{"enabled":false,"rule_set_ids":[]},"expected_config_revision":%d}`, rev)
		_, preview := w.call(ref, "editor-preview-disable", "POST", path+"/preview", body, true)
		token := preview.(map[string]any)["preview_token"].(string)
		w.call(ref, "editor-save-bad-token", "POST", path, `{"settings":{"enabled":false},"preview_token":"bad","expected_config_revision":0}`, true)
		w.call(ref, "editor-save-disable", "POST", path, strings.TrimSuffix(body, "}")+`,"preview_token":"`+token+`"}`, true)

		cfg, err := w.srv.Repo.Config(w.srv.Context(), "home", repo.ResourceRoots{})
		if err != nil {
			t.Fatal(err)
		}
		text, err := convert.MihomoConfig(w.srv.Context(), cfg, convert.MihomoOptions{BaseURL: "http://localhost", SkipProviderPreflight: true})
		if err != nil {
			t.Fatal(err)
		}
		n, err := yamlx.Parse([]byte(text))
		if err != nil {
			t.Fatal(err)
		}
		b, err := yamlx.ToJSON(n)
		if err != nil {
			t.Fatal(err)
		}
		var gen any
		_ = json.Unmarshal(b, &gen)
		gKey, _ := json.Marshal(map[string]string{"case": "editor-generate-disabled"})
		gVal, _ := json.Marshal(gen)
		ref.Assert(t, gKey, gVal)

		w.editProfile("home", `{"oxidns":{"native_config":"plugins: [{tag: cache, type: cache}]\n","future":"later"}}`)
		rev, _ = w.srv.Repo.ConfigRevision(w.srv.Context(), "home")
		_, rPrev := w.call(ref, "editor-restore-preview", "POST", path+"/restore/preview", fmt.Sprintf(`{"expected_config_revision":%d}`, rev), true)
		rToken := rPrev.(map[string]any)["preview_token"].(string)
		w.call(ref, "editor-restore-apply", "POST", path+"/restore", fmt.Sprintf(`{"expected_config_revision":%d,"preview_token":%q}`, rev, rToken), true)

		for _, kind := range []string{"rules", "source"} {
			for _, id := range []string{"r", "fake-ip", "deleted"} {
				base := "/api/profiles/home/linux-gateway/" + kind + "/" + id
				w.call(ref, "lg-"+kind+"-"+id+"-ok", "GET", base+"?token=a20-subscription&profile_id=home", "", false)
				w.call(ref, "lg-"+kind+"-"+id+"-badtoken", "GET", base+"?token=wrong&profile_id=home", "", false)
			}
		}
	})

	t.Run("host-and-forwarding", func(t *testing.T) {
		w := newLGWorld(t)
		const hostSpec = `{"interface":"eth0","address":"192.0.2.2","lan_cidr":"192.0.2.0/24","upstream_gateway":"192.0.2.1"}`
		path := "/api/agents/gateway/linux-gateway/"
		ops := []struct{ name, method, op, body string }{
			{"status", "GET", "status", ""},
			{"preview", "POST", "preview", hostSpec},
			{"apply", "POST", "apply", hostSpec},
			{"remove", "POST", "remove", `{}`},
			{"forwarding", "POST", "forwarding", `{}`},
			{"get-remove", "GET", "remove", ""},
			{"post-status", "POST", "status", `{}`},
			{"unknown", "GET", "unknown", ""},
		}
		for _, op := range ops {
			w.call(ref, "host-"+op.name, op.method, path+op.op+"?profile_id=home", op.body, true)
		}
		w.call(ref, "host-status-unauth", "GET", path+"status?profile_id=home", "", false)
		w.call(ref, "host-status-missing-agent", "GET", "/api/agents/missing/linux-gateway/status?profile_id=home", "", true)
		for i, raw := range []string{`{`, `[]`, `null`, `{"interface":"lo","address":"192.0.2.2","lan_cidr":"192.0.2.0/24","upstream_gateway":"192.0.2.1"}`} {
			w.call(ref, fmt.Sprintf("host-preview-invalid-%d", i), "POST", path+"preview?profile_id=home", raw, true)
		}
		for _, phase := range []string{"filter", "record", "kept", "notjson", "array", "big", "401", "404", "409", "500"} {
			w.fake.setPhase(phase)
			w.call(ref, "host-phase-"+phase, "GET", path+"status?profile_id=home", "", true)
		}
		w.fake.setPhase("")
		w.call(ref, "gw-fwd-get-unlinked", "GET", "/api/agents/gateway-forwarding", "", true)
		w.call(ref, "gw-fwd-post-empty", "POST", "/api/agents/gateway-forwarding", `{}`, true)
		w.call(ref, "gw-fwd-post-unlinked", "POST", "/api/agents/gateway-forwarding", `{"agent_id":"gateway"}`, true)
		w.editProfile("default", `{"routers":{"items":[{"id":"router","name":"Main","proxy_gateway_agent_id":"gateway"}]}}`)
		w.call(ref, "gw-fwd-get-linked", "GET", "/api/agents/gateway-forwarding", "", true)
		w.call(ref, "gw-fwd-head-linked", "HEAD", "/api/agents/gateway-forwarding", `{"agent_id":"gateway"}`, true)
		w.call(ref, "gw-fwd-post-linked", "POST", "/api/agents/gateway-forwarding", `{"agent_id":"gateway"}`, true)
	})

	t.Run("profile-headers-and-privacy", func(t *testing.T) {
		w := newLGWorld(t)
		statusPath := "/api/agents/gateway/linux-gateway/status?profile_id=default"
		for i, headers := range [][]string{{"home"}, {"home", "default"}, {"home", ""}} {
			w.profileHeaders = headers
			w.call(ref, fmt.Sprintf("profile-header-%d", i), "GET", statusPath, "", true)
		}
		w.profileHeaders = nil
		root := "/api/profiles/home/custom-config/linux-gateway"
		w.editProfile("home", `{"oxidns":{"native_config":"plugins: []\n","password":"synthetic-old-password","note":"original"}}`)
		w.call(ref, "privacy-disable", "POST", root, `{"enabled":false,"rule_set_ids":[]}`, true)
		w.editProfile("home", `{"oxidns":{"native_config":"plugins: []\n","password":"synthetic-new-password","note":"previous synthetic-old-password; current synthetic-new-password; encoded synthetic%2Dnew%2Dpassword","future":{"password":"synthetic-nested-password","note":"reference synthetic-nested-password","synthetic-old-password":"keyed metadata"},"synthetic-new-password":"root metadata"}}`)
		rev, _ := w.srv.Repo.ConfigRevision(w.srv.Context(), "home")
		w.call(ref, "privacy-restore-preview", "POST", root+"/restore/preview", fmt.Sprintf(`{"expected_config_revision":%d}`, rev), true)
	})
}
