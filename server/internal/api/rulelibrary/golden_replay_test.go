package rulelibrary

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"configflow/internal/httpx/httpxtest"
	"configflow/internal/testgolden"
)

var (
	rlISO   = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}`)
	rlGenID = regexp.MustCompile(`^rule_[0-9a-f]{8,}$`)
)

func normalizeRLGolden(v any, mockURL string) any {
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
			out[k] = normalizeRLGolden(val, mockURL)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = normalizeRLGolden(item, mockURL)
		}
		return out
	case string:
		if mockURL != "" && strings.Contains(x, mockURL) {
			x = strings.ReplaceAll(x, mockURL, "http://rules.example")
		}
		if rlISO.MatchString(x) {
			return "<time>"
		}
		if rlGenID.MatchString(x) {
			return "<rule_id>"
		}
		return x
	default:
		return v
	}
}

func TestRuleLibraryWriteRoutesGoldenReplay(t *testing.T) {
	ref := testgolden.New(t, testgolden.Spec{Compress: true})
	mockRules := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/yaml")
		_, _ = w.Write([]byte("payload:\n  - DOMAIN-SUFFIX,remote.example\n"))
	}))
	defer mockRules.Close()

	s := fixture(t)
	auth := s.Login()

	recordStep := func(name, method, path, body string) {
		t.Helper()
		opts := []httpxtest.Option{auth}
		if body != "" {
			opts = append(opts, httpxtest.Body([]byte(body), "application/json"))
		}
		resp := s.Do(method, path, opts...)
		var parsed any
		if err := json.Unmarshal(resp.Body, &parsed); err != nil {
			parsed = string(resp.Body)
		}
		key, _ := json.Marshal(map[string]string{"case": name, "method": method, "path": path})
		actual, _ := json.Marshal(map[string]any{"status": resp.Status, "body": normalizeRLGolden(parsed, mockRules.URL)})
		ref.Assert(t, key, actual)
	}

	// Create content and URL rule-library items + validation errors.
	recordStep("create-content", "POST", "/api/rule-library",
		`{"id":"lib-content","name":"Local Domains","source_type":"content","behavior":"domain","format":"text","content":"+.example.com\n+.test.example\n","enabled":true}`)
	recordStep("create-url", "POST", "/api/rule-library",
		`{"id":"lib-url","name":"Remote List","source_type":"url","behavior":"classical","format":"yaml","url":"`+mockRules.URL+`/list.yaml","enabled":true}`)
	recordStep("create-duplicate-id", "POST", "/api/rule-library",
		`{"id":"lib-content","name":"Dup","source_type":"content","behavior":"domain","format":"text","content":"+.dup.example\n"}`)
	recordStep("create-missing-name", "POST", "/api/rule-library",
		`{"source_type":"content","behavior":"domain","format":"text","content":"+.noname.example\n"}`)
	recordStep("create-invalid-behavior", "POST", "/api/rule-library",
		`{"name":"Bad Behavior","source_type":"content","behavior":"bogus","format":"text","content":"+.x.example\n"}`)
	recordStep("create-non-object", "POST", "/api/rule-library", `[]`)
	recordStep("create-malformed", "POST", "/api/rule-library", `{`)

	// Update item & draft content.
	recordStep("update-content", "PUT", "/api/rule-library/lib-content",
		`{"name":"Local Domains Updated","source_type":"content","behavior":"domain","format":"text","content":"+.example.com\n+.updated.example\n","enabled":true}`)
	recordStep("update-missing", "PUT", "/api/rule-library/missing",
		`{"name":"Ghost","source_type":"content","behavior":"domain","format":"text","content":"+.x.example\n"}`)
	recordStep("draft-update-valid", "PUT", "/api/rule-library/lib-content/draft-content",
		`{"content":"+.draft.example\n"}`)
	recordStep("draft-update-missing", "PUT", "/api/rule-library/missing/draft-content",
		`{"content":"+.draft.example\n"}`)

	// Reorder & proxy domains.
	recordStep("reorder-valid", "POST", "/api/rule-library/reorder",
		`{"ids":["lib-url","lib-content"]}`)
	recordStep("reorder-unknown", "POST", "/api/rule-library/reorder",
		`{"ids":["lib-url","unknown"]}`)
	recordStep("proxy-domains-valid", "POST", "/api/rule-library/proxy-domains",
		`{"domains":["rules.example","cdn.example"]}`)
	recordStep("proxy-domains-non-object", "POST", "/api/rule-library/proxy-domains", `[]`)

	// Reference lib-url from a rule config so DELETE returns 409 conflict.
	s.Post("/api/rules", auth, httpxtest.JSON(map[string]any{
		"id":              "ref-lib-url",
		"itemType":        "ruleset",
		"library_rule_id": "lib-url",
		"policy":          "DIRECT",
		"enabled":         true,
	})).WantStatus(200)

	// Delete referenced (409), unused (200), missing (404), then list final.
	recordStep("delete-referenced", "DELETE", "/api/rule-library/lib-url", "")
	recordStep("delete-unused", "DELETE", "/api/rule-library/lib-content", "")
	recordStep("delete-missing", "DELETE", "/api/rule-library/missing", "")
	recordStep("list-final", "GET", "/api/rule-library", "")
}
