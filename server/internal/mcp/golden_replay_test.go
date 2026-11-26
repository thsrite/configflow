package mcp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"configflow/internal/auth"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/server"
	"configflow/internal/testgolden"
)

func normalizeMCPGolden(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, item := range x {
			if k == "created_at" || k == "updated_at" || k == "export_time" {
				if s, ok := item.(string); ok && s != "" {
					out[k] = "<time>"
					continue
				}
			}
			out[k] = normalizeMCPGolden(item)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = normalizeMCPGolden(item)
		}
		return out
	case string:
		if len(x) > 0 && (x[0] == '{' || x[0] == '[') {
			var inner any
			if json.Unmarshal([]byte(x), &inner) == nil {
				return normalizeMCPGolden(inner)
			}
		}
	}
	return v
}

func TestMCPGoldenReplay(t *testing.T) {
	ref := testgolden.New(t, testgolden.Spec{Compress: true})

	previous := auth.Configure(auth.Config{SecretKey: "synthetic-mcp-jwt-key-32-characters"})
	t.Cleanup(func() { auth.Configure(previous) })

	r, err := repo.Open(t.TempDir(), server.RepoOptions())
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
	headers := map[string]string{"Authorization": "Bearer synthetic-mcp-admin", "Content-Type": "application/json"}
	rpc := func(name, args string) string {
		return fmt.Sprintf(`{"jsonrpc":"2.0","id":"synthetic","method":"tools/call","params":{"name":%q,"arguments":%s}}`, name, args)
	}

	type req struct {
		name, method, path, body string
		headers                  map[string]string
	}
	cases := []req{
		{"initialize", "POST", "/mcp", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`, headers},
		{"ping-slash", "POST", "/mcp/", `{"jsonrpc":"2.0","id":2,"method":"ping"}`, headers},
		{"tools-list", "POST", "/mcp", `{"jsonrpc":"2.0","id":3,"method":"tools/list"}`, headers},
		{"list-subs", "POST", "/mcp", rpc("list_subscriptions", `{}`), headers},
		{"update-sub", "POST", "/mcp", rpc("manage_subscription", `{"action":"update","id":"sub-a","data":{"health_check":{"enable":true}}}`), headers},
		{"reorder-subs", "POST", "/mcp", rpc("reorder_items", `{"collection":"subscriptions","ids":["sub-b"]}`), headers},
		{"update-group", "POST", "/mcp", rpc("manage_proxy_group", `{"action":"update","id":"group-a","data":{"name":"Changed"}}`), headers},
		{"reorder-groups", "POST", "/mcp", rpc("reorder_items", `{"collection":"proxy_groups","ids":["group-b"],"position":"bottom"}`), headers},
		{"create-profile", "POST", "/mcp", rpc("manage_profile", `{"action":"create","data":{"id":"alpha","name":"Alpha"}}`), headers},
		{"create-alpha-group", "POST", "/mcp", rpc("manage_proxy_group", `{"action":"create","profile_id":"alpha","data":{"id":"alpha-group","name":"Alpha Group","type":"select","manual_nodes":["DIRECT"]}}`), headers},
		{"list-alpha-header", "POST", "/mcp", rpc("list_proxy_groups", `{}`), map[string]string{"Authorization": "Bearer synthetic-mcp-admin", "X-ConfigFlow-Profile": "alpha"}},
		{"list-alpha-arg", "POST", "/mcp", rpc("list_proxy_groups", `{"profile_id":"alpha"}`), headers},
		{"get-settings", "POST", "/mcp", rpc("get_settings", `{"section":"config_token"}`), headers},
		{"export-alpha", "POST", "/mcp", rpc("manage_config_backup", `{"action":"export","scope":"profile","profile_id":"alpha"}`), headers},
		{"missing-tool", "POST", "/mcp", rpc("missing_tool", `{}`), headers},
		{"bad-args", "POST", "/mcp", rpc("list_nodes", `"wrong"`), headers},
		{"update-missing-node", "POST", "/mcp", rpc("manage_node", `{"action":"update","data":{"name":"Missing"}}`), headers},
		{"missing-method", "POST", "/mcp", `{"jsonrpc":"2.0","id":4,"method":"missing"}`, headers},
		{"missing-jsonrpc", "POST", "/mcp", `{"id":1,"method":"ping"}`, headers},
		{"batch-mixed", "POST", "/mcp", `[1,{"jsonrpc":"2.0","method":"tools/call","params":{"name":"manage_config_backup","arguments":{"action":"reset"}}},{"jsonrpc":"2.0","id":5,"method":"ping"}]`, headers},
		{"batch-empty", "POST", "/mcp", `[]`, headers},
		{"scalar-body", "POST", "/mcp", `1`, headers},
		{"broken-json", "POST", "/mcp", `{"broken"`, headers},
		{"wrong-bearer", "POST", "/mcp", `{}`, map[string]string{"Authorization": "Bearer wrong"}},
		{"query-token", "POST", "/mcp?token=synthetic-mcp-admin", `{"jsonrpc":"2.0","id":6,"method":"ping"}`, nil},
		{"internal-token-rejected", "POST", "/mcp", `{}`, map[string]string{"Authorization": "Bearer synthetic-internal"}},
		{"retired-token-rejected", "POST", "/mcp?token=synthetic-retired", `{}`, headers},
		{"case-METHOD", "POST", "/mcp", `{"jsonrpc":"2.0","id":9,"METHOD":"ping"}`, headers},
		{"case-ID", "POST", "/mcp", `{"jsonrpc":"2.0","ID":9,"method":"ping"}`, headers},
		{"case-JSONRPC", "POST", "/mcp", `{"JSONRPC":"2.0","id":9,"method":"ping"}`, headers},
		{"get-mcp", "GET", "/mcp", "", nil},
		{"delete-mcp", "DELETE", "/mcp/", "", nil},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			httpReq := httptest.NewRequest(c.method, c.path, strings.NewReader(c.body)).WithContext(ctx)
			for k, v := range c.headers {
				httpReq.Header.Set(k, v)
			}
			rec := httptest.NewRecorder()
			app.ServeHTTP(rec, httpReq)
			var parsed any
			if err := json.Unmarshal(rec.Body.Bytes(), &parsed); err != nil {
				parsed = rec.Body.String()
			}
			key, _ := json.Marshal(map[string]string{"case": c.name})
			val, _ := json.Marshal(normalizeMCPGolden(map[string]any{
				"status": rec.Code,
				"body":   parsed,
			}))
			ref.Assert(t, key, val)
		})
	}
}
