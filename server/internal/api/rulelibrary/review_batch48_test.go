package rulelibrary

import (
	"context"
	"encoding/json"
	"testing"

	"configflow/internal/httpx/httpxtest"
	"configflow/internal/model"
	"configflow/internal/testjson"
)

// TestReviewBatch48CreateLenientReplaceStrict pins the two decodings the
// typed request kept: a creation reads a declared member of another type with
// the stored-data rules (a number as text, a text flag as true), a replace
// refuses it with the repository's message and leaves the catalog unchanged.
func TestReviewBatch48CreateLenientReplaceStrict(t *testing.T) {
	const content = `"source_type":"content","content":"DOMAIN,b.example\n","behavior":"classical"`
	tests := []struct {
		name, method, path, body string
		status                   int
		message                  string
		stored                   string // the stored item "new", when status is 200
	}{
		{"create number name", "POST", "/api/rule-library", `{"id":"new","name":5,` + content + `}`, 200, "",
			`{"id":"new","name":"5","source_type":"content","behavior":"classical","content":"DOMAIN,b.example\n"}`},
		{"create text flag and number remark", "POST", "/api/rule-library", `{"id":"new","name":"N","enabled":"x","remark":5,"update_interval":"x",` + content + `}`, 200, "",
			`{"id":"new","name":"N","enabled":true,"source_type":"content","behavior":"classical","content":"DOMAIN,b.example\n","remark":"5"}`},
		{"replace text flag", "PUT", "/api/rule-library/r2", `{"name":"N","enabled":"x",` + content + `}`, 400, "rule_library 类型不正确", ""},
		{"replace number name", "PUT", "/api/rule-library/r2", `{"name":5,` + content + `}`, 400, "共享规则仓库名称必须非空且唯一", ""},
		{"create policy", "POST", "/api/rule-library", `{"id":"new","name":"N","policy":"DIRECT",` + content + `}`, 400, "规则仓库不包含目标策略，请在规则配置中设置", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(Register)})
			err := srv.Repo.UpdateShared(srv.Context(), func(_ context.Context, s *model.Shared) error {
				return s.SetJSON("rule_library", json.RawMessage(`[{"id":"r2","name":"R2","source_type":"content","content":"DOMAIN,a.example\n","behavior":"classical","format":"text"}]`))
			})
			if err != nil {
				t.Fatal(err)
			}
			before, err := srv.Repo.ExportAllJSON(srv.Context(), false)
			if err != nil {
				t.Fatal(err)
			}
			res := srv.Do(tc.method, tc.path, httpxtest.Body([]byte(tc.body), "application/json"))
			if res.Status != tc.status {
				t.Fatalf("status %d, want %d: %s", res.Status, tc.status, res.Body)
			}
			if tc.status != 200 {
				if got := res.Object().GetString("message"); got != tc.message {
					t.Fatalf("message %q, want %q", got, tc.message)
				}
				after, err := srv.Repo.ExportAllJSON(srv.Context(), false)
				if err != nil {
					t.Fatal(err)
				}
				if !testjson.Equal(before, after) {
					t.Fatal("refused request changed the repository")
				}
				return
			}
			shared, err := srv.Repo.Shared(srv.Context())
			if err != nil {
				t.Fatal(err)
			}
			i := find(shared.RuleLibrary, "new")
			if i < 0 {
				t.Fatal("item not stored")
			}
			got, err := json.Marshal(shared.RuleLibrary[i])
			if err != nil {
				t.Fatal(err)
			}
			if !testjson.Equal(got, []byte(tc.stored)) {
				t.Fatalf("stored %s, want %s", got, tc.stored)
			}
		})
	}
}
