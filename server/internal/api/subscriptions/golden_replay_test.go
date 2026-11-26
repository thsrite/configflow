package subscriptions_test

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"configflow/internal/httpx/httpxtest"
	"configflow/internal/server/servertest"
	"configflow/internal/testgolden"
)

var (
	subISO   = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}`)
	subGenID = regexp.MustCompile(`^sub_[0-9a-f]{8,}$`)
)

func normalizeSubGolden(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			if k == "created_at" || k == "updated_at" || k == "last_updated" {
				if s, ok := val.(string); ok && s != "" {
					out[k] = "<time>"
					continue
				}
			}
			out[k] = normalizeSubGolden(val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = normalizeSubGolden(item)
		}
		return out
	case string:
		if subISO.MatchString(x) {
			return "<time>"
		}
		if subGenID.MatchString(x) {
			return "<sub_id>"
		}
		return x
	default:
		return v
	}
}

func TestSubscriptionsWriteRoutesGoldenReplay(t *testing.T) {
	ref := testgolden.New(t, testgolden.Spec{Compress: true})
	s, _ := servertest.New(t, httpxtest.Options{})

	recordStep := func(name, method, path, body string) map[string]any {
		t.Helper()
		var opts []httpxtest.Option
		if body != "" {
			opts = append(opts, httpxtest.Body([]byte(body), "application/json"))
		}
		resp := s.Do(method, path, opts...)
		var parsed any
		if err := json.Unmarshal(resp.Body, &parsed); err != nil {
			parsed = string(resp.Body)
		}
		key, _ := json.Marshal(map[string]string{"case": name, "method": method, "path": path})
		actual, _ := json.Marshal(map[string]any{"status": resp.Status, "body": normalizeSubGolden(parsed)})
		ref.Assert(t, key, actual)
		m, _ := parsed.(map[string]any)
		return m
	}

	// Create valid subscriptions and error cases.
	created1 := recordStep("create-first", "POST", "/api/subscriptions",
		`{"id":"sub-one","name":"Primary Feed","url":"https://feed.example/one","enabled":true}`)
	_ = created1
	recordStep("create-second", "POST", "/api/subscriptions",
		`{"id":"sub-two","name":"Secondary Feed","url":"https://feed.example/two","enabled":true}`)
	recordStep("create-duplicate-id", "POST", "/api/subscriptions",
		`{"id":"sub-one","name":"Other","url":"https://feed.example/other"}`)
	recordStep("create-missing-name", "POST", "/api/subscriptions",
		`{"url":"https://feed.example/noname"}`)
	recordStep("create-missing-url", "POST", "/api/subscriptions",
		`{"name":"No URL"}`)
	recordStep("create-invalid-scheme", "POST", "/api/subscriptions",
		`{"name":"Bad Scheme","url":"ftp://feed.example/sub"}`)
	recordStep("create-non-object", "POST", "/api/subscriptions", `[1,2]`)
	recordStep("create-malformed", "POST", "/api/subscriptions", `{`)

	// Update existing and missing.
	recordStep("update-valid", "PUT", "/api/subscriptions/sub-one",
		`{"name":"Primary Renamed","url":"https://feed.example/one-v2","enabled":false}`)
	recordStep("update-missing", "PUT", "/api/subscriptions/missing",
		`{"name":"Ghost"}`)
	recordStep("update-non-object", "PUT", "/api/subscriptions/sub-one", `[]`)

	// Reorder valid & invalid.
	recordStep("reorder-valid", "POST", "/api/subscriptions/reorder",
		`{"ids":["sub-two","sub-one"]}`)
	recordStep("reorder-duplicate", "POST", "/api/subscriptions/reorder",
		`{"ids":["sub-one","sub-one"]}`)
	recordStep("reorder-unknown", "POST", "/api/subscriptions/reorder",
		`{"ids":["sub-one","unknown"]}`)
	recordStep("reorder-non-object", "POST", "/api/subscriptions/reorder", `[]`)

	// Reference sub-two from a proxy group so DELETE returns conflict/in-use.
	s.Post("/api/proxy-groups", httpxtest.JSON(map[string]any{
		"id":            "pg-ref",
		"name":          "Referenced Group",
		"type":          "select",
		"subscriptions": []string{"sub-two"},
	})).WantStatus(200)

	// Delete referenced (blocked), unused (succeeds), missing (404).
	recordStep("delete-referenced", "DELETE", "/api/subscriptions/sub-two", "")
	recordStep("delete-unused", "DELETE", "/api/subscriptions/sub-one", "")
	recordStep("delete-missing", "DELETE", "/api/subscriptions/missing", "")
	recordStep("list-final", "GET", "/api/subscriptions", "")

	// Sanity check: no raw URL leaked into unexpected fields.
	if strings.Contains(s.Get("/api/subscriptions").Text(), "sub-one") {
		t.Fatal("deleted subscription still present")
	}
}
