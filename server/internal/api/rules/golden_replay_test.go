package rules

import (
	"encoding/json"
	"regexp"
	"testing"

	"configflow/internal/httpx/httpxtest"
	"configflow/internal/testgolden"
)

var (
	ruleISO   = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}`)
	ruleGenID = regexp.MustCompile(`^rule_[0-9a-f]{8,}$`)
)

func normalizeRuleGolden(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			if k == "created_at" || k == "updated_at" {
				if s, ok := val.(string); ok && s != "" {
					out[k] = "<time>"
					continue
				}
			}
			out[k] = normalizeRuleGolden(val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = normalizeRuleGolden(item)
		}
		return out
	case string:
		if ruleISO.MatchString(x) {
			return "<time>"
		}
		if ruleGenID.MatchString(x) {
			return "<rule_id>"
		}
		return x
	default:
		return v
	}
}

func TestRulesWriteRoutesGoldenReplay(t *testing.T) {
	ref := testgolden.New(t, testgolden.Spec{Compress: true})
	s := httpxtest.New(t, httpxtest.Options{})
	New(Deps{Repo: s.Repo}).Register(s.App.Router)

	recordStep := func(name, method, path, body string) {
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
		actual, _ := json.Marshal(map[string]any{"status": resp.Status, "body": normalizeRuleGolden(parsed)})
		ref.Assert(t, key, actual)
	}

	// Single rule create & validation errors.
	recordStep("add-domain-rule", "POST", "/api/rules",
		`{"id":"r-domain","itemType":"rule","rule_type":"DOMAIN-SUFFIX","value":"example.com","policy":"DIRECT","group_name":"Custom","enabled":true}`)
	recordStep("add-ip-rule", "POST", "/api/rules",
		`{"id":"r-ip","itemType":"rule","rule_type":"IP-CIDR","value":"192.0.2.0/24","policy":"REJECT","group_name":"Custom","enabled":true}`)
	recordStep("add-duplicate-id", "POST", "/api/rules",
		`{"id":"r-domain","itemType":"rule","rule_type":"DOMAIN","value":"dup.example","policy":"DIRECT"}`)
	recordStep("add-missing-policy", "POST", "/api/rules",
		`{"itemType":"rule","rule_type":"DOMAIN","value":"nopolicy.example"}`)
	recordStep("add-non-object", "POST", "/api/rules", `[1]`)
	recordStep("add-malformed", "POST", "/api/rules", `{`)

	// Update existing & missing.
	recordStep("update-rule", "PUT", "/api/rules/r-domain",
		`{"itemType":"rule","rule_type":"DOMAIN-SUFFIX","value":"updated.example","policy":"DIRECT","group_name":"Custom","enabled":false}`)
	recordStep("update-missing", "PUT", "/api/rules/missing",
		`{"itemType":"rule","rule_type":"DOMAIN","value":"x.example","policy":"DIRECT"}`)

	// Batch add.
	recordStep("batch-add-valid", "POST", "/api/rules/batch",
		`{"rules":[{"id":"r-b1","itemType":"rule","rule_type":"DOMAIN","value":"a.example","policy":"DIRECT"},{"id":"r-b2","itemType":"rule","rule_type":"DOMAIN","value":"b.example","policy":"DIRECT"}]}`)
	recordStep("batch-add-empty", "POST", "/api/rules/batch", `{"rules":[]}`)
	recordStep("batch-add-non-object", "POST", "/api/rules/batch", `[]`)

	// Group rename, policy update, reorder, DNS update.
	recordStep("rename-group-valid", "POST", "/api/rules/group-name",
		`{"old_name":"Custom","new_name":"Renamed"}`)
	recordStep("rename-group-empty", "POST", "/api/rules/group-name",
		`{"old_name":"","new_name":"X"}`)
	recordStep("set-policy-valid", "PUT", "/api/rules/policy",
		`{"ids":["r-domain","r-ip"],"policy":"DIRECT"}`)
	recordStep("set-policy-missing-policy", "PUT", "/api/rules/policy",
		`{"ids":["r-domain"]}`)
	recordStep("reorder-invalid", "POST", "/api/rules/reorder",
		`{"ids":["nonexistent"]}`)
	recordStep("dns-update-valid", "PUT", "/api/rules/dns",
		`{"rules":[{"id":"r-domain","dns_mode":"direct"}]}`)
	recordStep("dns-update-non-object", "PUT", "/api/rules/dns", `[]`)

	// Delete existing & missing, then list final state.
	recordStep("delete-existing", "DELETE", "/api/rules/r-b2", "")
	recordStep("delete-missing", "DELETE", "/api/rules/missing", "")
	recordStep("list-final", "GET", "/api/rules", "")
}
