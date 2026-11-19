package rules_test

import (
	"compress/gzip"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/rulefetch"
	"configflow/internal/rules"
	"configflow/internal/yamlx"
)

// C09 review: connection_rules.apply_ip reference results (Python with the
// repository and domain discovery stubbed, see testdata/review_c09_gen.py)
// against ParseDestination + ApplyIPRule with the real ruleset_content port.

func TestReviewC09ApplyIPRule(t *testing.T) {
	file, err := os.Open("testdata/review_c09.json.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	value, err := jsonx.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	encode := func(v any) string {
		out, err := jsonx.Compact(v)
		if err != nil {
			t.Fatal(err)
		}
		return string(out)
	}
	cases := value.(*jsonx.Object).GetList("apply")
	if len(cases) == 0 {
		t.Fatal("no cases")
	}
	nativeLayouts, nativeDiagnostics := 0, 0
	for _, entry := range cases {
		record := entry.(*jsonx.Object)
		policy := record.Value("policy")
		references := []any{}
		if policy != nil {
			references = append(references, jsonx.Obj("enabled", true, "after_match", false, "policy", policy))
		}
		deps := rules.ConnectionDeps{
			RulesetInfo: func(config *jsonx.Object, id string) *jsonx.Object {
				for _, item := range config.GetList("rule_library") {
					library := item.(*jsonx.Object)
					if library.Value("id") == id {
						return jsonx.Obj("id", id, "name", library.Value("name"), "behavior", library.GetOr("behavior", "classical"), "references", references)
					}
				}
				return nil
			},
			ReadRulePayload: func(content string) ([]string, any, error) {
				lines, payload, err := rulefetch.ReadRulePayload(content)
				if payload == nil {
					return lines, nil, err
				}
				return lines, payload, err
			},
			AppendRulePayload: func(content, line string, payload any) (string, error) {
				node, _ := payload.(*yamlx.Node)
				return rulefetch.AppendRulePayload(content, line, node)
			},
		}
		if strings.Contains(record.GetString("content"), "%") {
			continue // scopedNetwork: zones inside networks are refused by net/netip
		}
		shared := jsonx.Obj("rule_library", []any{jsonx.Obj("id", "lib", "name", "规则 lib", "source_type", "content",
			"behavior", record.Value("behavior"), "content", record.Value("content"))})
		config := jsonx.Obj("rule_configs", []any{})
		ruleType, target := record.GetString("rule_type"), record.GetString("target")
		got := jsonx.NewObject()
		destination, err := rules.ParseDestination(record.GetString("value"), ruleType)
		var result *rules.IPRuleResult
		if err == nil {
			result, err = rules.ApplyIPRule(deps, config, shared, "lib", target, destination, ruleType)
		}
		if err != nil {
			got.Set("message", err.Error())
		} else {
			got.Set("result", jsonx.Obj("added", result.Added, "skipped", result.Skipped, "warnings", result.Warnings))
			got.Set("content", shared.GetList("rule_library")[0].(*jsonx.Object).Value("content"))
		}
		want := record.GetObject("result").DeepCopy()
		want.Delete("exception") // compared by message
		// N7-3b/D3: actual rule edits may use the shared Node editor's rewrite
		// path. Compare only the returned YAML text structurally; every result
		// field remains exact. Rulefetch separately asserts untouched splice bytes,
		// comments, CRLF, no-op text and sequence order.
		if g, w := got.GetString("content"), want.GetString("content"); g != w {
			gn, ge := yamlx.Parse([]byte(g))
			wn, we := yamlx.Parse([]byte(w))
			if ge == nil && we == nil && yamlx.Get(gn, "payload") != nil && yamlx.Get(wn, "payload") != nil && yamlx.Equal(gn, wn) {
				want.Set("content", g)
				nativeLayouts++
			}
		}
		var syntax *yamlx.SyntaxError
		if errors.As(err, &syntax) && strings.HasSuffix(want.GetString("message"), "。") && got.GetString("message") == strings.TrimSuffix(want.GetString("message"), "。")+"："+syntax.Error() {
			want.Set("message", got.Value("message"))
			nativeDiagnostics++
		}
		if encode(got) != encode(want) {
			t.Errorf("apply_ip(%s, %s, %q, %v, policy=%v):\n got  %s\n want %s", record.GetString("behavior"),
				encode(record.Value("content")), record.GetString("value"), ruleType, policy, encode(got), encode(want))
		}
	}
	t.Logf("N7-3b: %d D3 layout and %d native diagnostic changes in %d C09 apply rows", nativeLayouts, nativeDiagnostics, len(cases))
}
