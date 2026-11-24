package routers_test

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"

	api "configflow/internal/api/routers"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/model"
	"configflow/internal/repo"
	core "configflow/internal/routers"
)

// Native request differences are separate from the frozen R15 workflows. Each
// rejects a whole original Flask response class and proves zero Go DB writes;
// request integers retain their strict JSON type contract.
func TestN89NativeRequestBoundary(t *testing.T) {
	tests := []struct {
		name, key string
		value     any
		goStatus  int
		message   string
	}{
		{"numeric-user", "username", 12, 400, "路由器身份和凭据字段必须是文本"},
		{"numeric-agent", "proxy_gateway_agent_id", 12, 400, "路由器身份和凭据字段必须是文本"},
		{"container-password", "password", []string{"x"}, 400, "路由器身份和凭据字段必须是文本"},
		{"null-tls", "verify_tls", nil, 400, "路由器身份和凭据字段必须是文本"},
		{"bool-name", "name", true, 400, "请填写名称"},
		{"false-selection", "ip_rule_sets", false, 400, "IP 路由规则集必须是规则集 ID 列表"},
		{"ascii-interval", "rule_sync_interval", "60", 400, "请选择有效的规则检查间隔"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := routerBody("http://127.0.0.1:9")
			body[tt.key] = tt.value
			dir := t.TempDir()
			s := httpxtest.New(t, httpxtest.Options{DataDir: filepath.Join(dir, "go"), RepoOptions: repo.Options{RouterJobLock: core.JobLockRecord}})
			api.New(api.Deps{Repo: s.Repo}).Register(s.App.Router)
			before, err := s.Repo.Profile(s.Context(), "default")
			if err != nil {
				t.Fatal(err)
			}
			beforeBytes, err := model.Marshal(before)
			if err != nil {
				t.Fatal(err)
			}
			response := s.Post("/api/routers", httpxtest.JSON(body))
			response.WantStatus(tt.goStatus)
			after, err := s.Repo.Profile(s.Context(), "default")
			if err != nil {
				t.Fatal(err)
			}
			if tt.goStatus == 400 {
				want, _ := json.Marshal(map[string]any{"success": false, "message": tt.message})
				if !sameBoundaryJSON(response.Body, want) {
					t.Fatalf("whole native rejection differs: %s", response.Body)
				}
				afterBytes, err := model.Marshal(after)
				if err != nil || !bytes.Equal(beforeBytes, afterBytes) {
					t.Fatal("rejected request changed DB profile/revision", err)
				}

			}
		})
	}
}
func sameBoundaryJSON(left, right []byte) bool {
	var a, b any
	if json.Unmarshal(left, &a) != nil || json.Unmarshal(right, &b) != nil {
		return false
	}
	aa, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return bytes.Equal(aa, bb)
}

// Corrupt storage keeps settings repair and deletion available.
func TestN89CorruptIdentityRemainsRepairable(t *testing.T) {
	for _, tc := range []struct {
		name   string
		bad    json.RawMessage
		repair any
	}{
		{"username", json.RawMessage(`12345`), "repaired-user"},
		{"ip_rule_sets", json.RawMessage(`["r1",2]`), []string{}},
		{"ros_policy_rule_sets", json.RawMessage(`["r1",2]`), []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := fixture(t)
			login := s.Login()
			created := s.Post("/api/routers", login, httpxtest.JSON(routerBody("http://127.0.0.1:9")))
			created.WantStatus(200)
			id := created.Object().GetObject("router").GetString("id")
			path := "/api/routers/" + id
			// Writers refuse the malformed value (O18 T1c), so it is written
			// to the store as a damaged database holds it. Plain records read
			// it leniently (a number as its text, a list element as its
			// text); repair and deletion stay available.
			corrupt := func() {
				p, err := s.Repo.Profile(s.Context(), "default")
				if err != nil {
					t.Fatal(err)
				}
				var routers map[string]any
				raw, _ := model.Marshal(p.Routers)
				_ = json.Unmarshal(raw, &routers)
				var bad any
				_ = json.Unmarshal(tc.bad, &bad)
				routers["items"].([]any)[0].(map[string]any)[tc.name] = bad
				raw, _ = json.Marshal(routers)
				storeProfileRow(t, s, "routers", raw)
			}
			corrupt()
			s.Get("/api/routers", login).WantStatus(200)
			s.Put(path, login, httpxtest.JSON(map[string]any{tc.name: tc.repair})).WantStatus(200)
			stored, err := s.Repo.Profile(s.Context(), "default")
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := model.Marshal(&stored.Routers.Items[0])
			var fields map[string]json.RawMessage
			_ = json.Unmarshal(raw, &fields)
			want, _ := json.Marshal(tc.repair)
			if !sameBoundaryJSON(fields[tc.name], want) {
				t.Fatalf("repair not committed: %s", raw)
			}
			corrupt()
			s.Delete(path, login).WantStatus(200)
		})
	}
}
