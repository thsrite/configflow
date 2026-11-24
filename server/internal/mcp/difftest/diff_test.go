package difftest

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"configflow/internal/auth"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/server"
)

type request struct {
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Body    string            `json:"body"`
	Headers map[string]string `json:"headers"`
	Login   bool              `json:"login"`
	Token   *string           `json:"token,omitempty"`
}
type answer struct {
	Status int             `json:"status"`
	Body   json.RawMessage `json:"body"`
	State  json.RawMessage `json:"state"`
	Error  string          `json:"oracle_error"`
}

func decode(raw json.RawMessage) interface{} {
	var out interface{}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	_ = d.Decode(&out)
	return normalize(out)
}
func normalize(v interface{}) interface{} {
	switch x := v.(type) {
	case map[string]interface{}:
		for k, item := range x {
			if k == "created_at" || k == "updated_at" {
				x[k] = "<time>"
				continue
			}
			x[k] = normalize(item)
		}
	case []interface{}:
		for j, item := range x {
			x[j] = normalize(item)
		}
	case string:
		if len(x) > 0 && (x[0] == '{' || x[0] == '[') {
			var inner interface{}
			d := json.NewDecoder(strings.NewReader(x))
			d.UseNumber()
			if d.Decode(&inner) == nil {
				return normalize(inner)
			}
		}
	}
	return v
}
func TestRealFlaskHTTP(t *testing.T) {
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("CONFIGFLOW_DIFF=1")
	}
	previous := auth.Configure(auth.Config{SecretKey: "synthetic-mcp-jwt-key-32-characters"})
	defer auth.Configure(previous)
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := r.UpdateSystem(ctx, func(_ context.Context, s *model.System) error {
		s.SystemConfig.ConfigToken = "synthetic-mcp-admin"
		s.SystemConfig.RuleProxyToken = "synthetic-internal"
		s.SystemConfig.RetiredRuleProxyTokens = []string{"synthetic-retired"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.UpdateShared(ctx, func(_ context.Context, s *model.Shared) error {
		return json.Unmarshal([]byte(`[{"id":"sub-a","name":"A","url":"https://example.test/a","health_check":{"enable":false,"interval":300}},{"id":"sub-b","name":"B","url":"https://example.test/b"}]`), &s.Subscriptions)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.UpdateProfile(ctx, "default", func(_ context.Context, p *model.Profile) error {
		return json.Unmarshal([]byte(`[{"id":"group-a","name":"A","type":"select","manual_nodes":["DIRECT"]},{"id":"group-b","name":"B","type":"select","manual_nodes":["DIRECT"]}]`), &p.ProxyGroups)
	}); err != nil {
		t.Fatal(err)
	}
	full, err := server.New(ctx, server.Options{Repo: r})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(full.Agents.Close)
	app := full.HTTP
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	processCtx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	cmd := exec.CommandContext(processCtx, filepath.Join(home, "cf-test/.venv/bin/python"), "-u", filepath.Join(root, "server/internal/mcp/difftest/oracle.py"), t.TempDir())
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "PYTHONPATH="+root, "JWT_SECRET_KEY=synthetic-mcp-jwt-key-32-characters")
	input, _ := cmd.StdinPipe()
	output, _ := cmd.StdoutPipe()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		input.Close()
		defer cancel()
		if err := cmd.Wait(); err != nil {
			t.Errorf("oracle: %v %s", err, stderr.String())
		}
	})
	scanner := bufio.NewScanner(output)
	scanner.Buffer(make([]byte, 4096), 8*1024*1024)
	headers := map[string]string{"Authorization": "Bearer synthetic-mcp-admin", "Content-Type": "application/json"}
	rpc := func(name, args string) string {
		return fmt.Sprintf(`{"jsonrpc":"2.0","id":"synthetic","method":"tools/call","params":{"name":%q,"arguments":%s}}`, name, args)
	}
	cases := []request{
		{Method: "POST", Path: "/mcp", Body: `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`, Headers: headers},
		{Method: "POST", Path: "/mcp/", Body: `{"jsonrpc":"2.0","id":2,"method":"ping"}`, Headers: headers},
		{Method: "POST", Path: "/mcp", Body: `{"jsonrpc":"2.0","id":3,"method":"tools/list"}`, Headers: headers},
		{Method: "POST", Path: "/mcp", Body: rpc("list_subscriptions", `{}`), Headers: headers},
		{Method: "POST", Path: "/mcp", Body: rpc("manage_subscription", `{"action":"update","id":"sub-a","data":{"health_check":{"enable":true}}}`), Headers: headers},
		{Method: "POST", Path: "/mcp", Body: rpc("reorder_items", `{"collection":"subscriptions","ids":["sub-b"]}`), Headers: headers},
		{Method: "POST", Path: "/mcp", Body: rpc("manage_proxy_group", `{"action":"update","id":"group-a","data":{"name":"Changed"}}`), Headers: headers},
		{Method: "POST", Path: "/mcp", Body: rpc("reorder_items", `{"collection":"proxy_groups","ids":["group-b"],"position":"bottom"}`), Headers: headers},
		{Method: "POST", Path: "/mcp", Body: rpc("manage_profile", `{"action":"create","data":{"id":"alpha","name":"Alpha"}}`), Headers: headers},
		{Method: "POST", Path: "/mcp", Body: rpc("manage_proxy_group", `{"action":"create","profile_id":"alpha","data":{"id":"alpha-group","name":"Alpha Group","type":"select","manual_nodes":["DIRECT"]}}`), Headers: headers},
		{Method: "POST", Path: "/mcp", Body: rpc("list_proxy_groups", `{}`), Headers: map[string]string{"Authorization": "Bearer synthetic-mcp-admin", "X-ConfigFlow-Profile": "alpha"}},
		{Method: "POST", Path: "/mcp", Body: rpc("list_proxy_groups", `{"profile_id":"alpha"}`), Headers: headers},
		{Method: "POST", Path: "/mcp", Body: rpc("get_settings", `{"section":"config_token"}`), Headers: headers},
		{Method: "POST", Path: "/mcp", Body: rpc("manage_config_backup", `{"action":"export","scope":"profile","profile_id":"alpha"}`), Headers: headers},
		{Method: "POST", Path: "/mcp", Body: rpc("missing_tool", `{}`), Headers: headers},
		{Method: "POST", Path: "/mcp", Body: rpc("list_nodes", `"wrong"`), Headers: headers},
		{Method: "POST", Path: "/mcp", Body: rpc("manage_node", `{"action":"update","data":{"name":"Missing"}}`), Headers: headers},
		{Method: "POST", Path: "/mcp", Body: `{"jsonrpc":"2.0","id":4,"method":"missing"}`, Headers: headers},
		{Method: "POST", Path: "/mcp", Body: `{"id":1,"method":"ping"}`, Headers: headers},
		{Method: "POST", Path: "/mcp", Body: `[1,{"jsonrpc":"2.0","method":"tools/call","params":{"name":"manage_config_backup","arguments":{"action":"reset"}}},{"jsonrpc":"2.0","id":5,"method":"ping"}]`, Headers: headers},
		{Method: "POST", Path: "/mcp", Body: `[{"jsonrpc":"2.0","method":"ping"},null]`, Headers: headers},
		{Method: "POST", Path: "/mcp", Body: `[]`, Headers: headers}, {Method: "POST", Path: "/mcp", Body: `1`, Headers: headers},
		{Method: "POST", Path: "/mcp", Body: `{"broken"`, Headers: headers},
		{Method: "POST", Path: "/mcp", Body: `{}`, Headers: map[string]string{"Authorization": "Bearer wrong"}},
		{Method: "POST", Path: "/mcp?token=synthetic-mcp-admin", Body: `{"jsonrpc":"2.0","id":6,"method":"ping"}`},
		{Method: "POST", Path: "/mcp", Body: `{}`, Headers: map[string]string{"Authorization": "Bearer synthetic-internal"}},
		{Method: "POST", Path: "/mcp?token=synthetic-retired", Body: `{}`, Headers: headers},
	}
	// Case aliases must not become protocol members. Compare with the real
	// Python transport, including notification detection and mixed-case input.
	cases = append(cases,
		request{Method: "POST", Path: "/mcp", Body: `{"jsonrpc":"2.0","id":9,"METHOD":"ping"}`, Headers: headers},
		request{Method: "POST", Path: "/mcp", Body: `{"jsonrpc":"2.0","ID":9,"method":"ping"}`, Headers: headers},
		request{Method: "POST", Path: "/mcp", Body: `{"JSONRPC":"2.0","id":9,"method":"ping"}`, Headers: headers},
		request{Method: "POST", Path: "/mcp", Body: `{"jsonrpc":"2.0","id":9,"method":"ping","METHOD":"missing"}`, Headers: headers},
	)
	for _, path := range []string{"/mcp", "/mcp/"} {
		for _, method := range []string{"GET", "DELETE"} {
			cases = append(cases, request{Method: method, Path: path})
		}
	}
	noToken := ""
	cases = append(cases, request{Method: "POST", Path: "/mcp", Token: &noToken, Body: `{"jsonrpc":"2.0","id":7,"method":"ping"}`}, request{Method: "POST", Path: "/mcp", Body: `{}`, Login: true})
	jwt, err := auth.GenerateToken("synthetic-admin")
	if err != nil {
		t.Fatal(err)
	}
	cases = append(cases, request{Method: "POST", Path: "/mcp", Login: true, Body: `{"jsonrpc":"2.0","id":8,"method":"ping"}`, Headers: map[string]string{"Authorization": "Bearer " + jwt}})
	for index, q := range cases {
		t.Run(fmt.Sprintf("%02d", index), func(t *testing.T) {
			auth.Configure(auth.Config{SecretKey: "synthetic-mcp-jwt-key-32-characters", AdminUsername: func() string {
				if q.Login {
					return "synthetic-admin"
				}
				return ""
			}(), AdminPassword: func() string {
				if q.Login {
					return "synthetic-password"
				}
				return ""
			}()})
			if q.Token != nil {
				if err := r.UpdateSystem(ctx, func(_ context.Context, s *model.System) error { s.SystemConfig.ConfigToken = *q.Token; return nil }); err != nil {
					t.Fatal(err)
				}
			}
			raw, _ := json.Marshal(q)
			if _, err := fmt.Fprintln(input, string(raw)); err != nil {
				t.Fatal(err)
			}
			var expected answer
			found := false
			for scanner.Scan() {
				line := scanner.Text()
				if strings.HasPrefix(line, "MCP_RESULT ") {
					if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "MCP_RESULT ")), &expected); err != nil {
						t.Fatal(err)
					}
					found = true
					break
				}
			}
			if !found || expected.Error != "" {
				t.Fatalf("oracle %s stderr=%s", expected.Error, stderr.String())
			}
			req := httptest.NewRequest(q.Method, q.Path, strings.NewReader(q.Body))
			req = req.WithContext(ctx)
			for k, v := range q.Headers {
				req.Header.Set(k, v)
			}
			recorder := httptest.NewRecorder()
			app.ServeHTTP(recorder, req)
			body := json.RawMessage(recorder.Body.Bytes())
			if !json.Valid(body) {
				body, _ = json.Marshal(string(body))
			}
			if recorder.Code != expected.Status || !reflect.DeepEqual(decode(body), decode(expected.Body)) {
				t.Errorf("HTTP %s %s got %d %s want %d %s", q.Method, q.Path, recorder.Code, body, expected.Status, expected.Body)
			}
			repo.ResetConfigContext(ctx)
			shared, err := r.Shared(ctx)
			if err != nil {
				t.Fatal(err)
			}
			profile, err := r.Profile(ctx, "default")
			if err != nil {
				t.Fatal(err)
			}
			state, _ := json.Marshal(struct {
				Subscriptions []model.Subscription `json:"subscriptions"`
				Groups        []model.ProxyGroup   `json:"groups"`
			}{shared.Subscriptions, profile.ProxyGroups})
			if !reflect.DeepEqual(decode(state), decode(expected.State)) {
				t.Errorf("stored state got %s want %s", state, expected.State)
			}
		})
	}
	t.Logf("%d real Flask HTTP requests and stored-state comparisons", len(cases))
}
