package rules_test

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"configflow/internal/jsondecode"
	"configflow/internal/model"
	"configflow/internal/rulefetch"
	"configflow/internal/rules"
	"configflow/internal/testjson"
	"configflow/internal/yamlx"
)

// C09 review: connection_rules.apply_ip reference results (Python with the
// repository and domain discovery stubbed, see testdata/review_c09_gen.py)
// against ParseDestination + AppendIP with the real ruleset_content port.

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
	var root struct {
		Apply []struct {
			Behavior json.RawMessage `json:"behavior"`
			Content  json.RawMessage `json:"content"`
			Policy   *string         `json:"policy"`
			RuleType string          `json:"rule_type"`
			Target   string          `json:"target"`
			Value    string          `json:"value"`
			Result   map[string]any  `json:"result"`
		} `json:"apply"`
	}
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	if len(root.Apply) == 0 {
		t.Fatal("no cases")
	}
	nativeLayouts, nativeDiagnostics := 0, 0
	for _, record := range root.Apply {
		var contentStr string
		_ = json.Unmarshal(record.Content, &contentStr)
		if strings.Contains(contentStr, "%") {
			continue // scopedNetwork: zones inside networks are refused by net/netip
		}
		var references []rules.RuleReference
		if record.Policy != nil {
			references = append(references, rules.RuleReference{Enabled: true, AfterMatch: false, Policy: *record.Policy})
		}
		rawLib, _ := json.Marshal(map[string]any{
			"id":          "lib",
			"name":        "规则 lib",
			"source_type": "content",
			"behavior":    record.Behavior,
			"content":     record.Content,
		})
		var libItem model.RuleLibraryItem
		_ = jsondecode.Unmarshal(rawLib, &libItem)
		library := []model.RuleLibraryItem{libItem}
		deps := rules.IPDeps{
			Describe: func(id string) (rules.RulesetInfo, error) {
				for _, lib := range library {
					if lib.ID == id {
						beh := "classical"
						if lib.Behavior != nil {
							beh = *lib.Behavior
						}
						return rules.RulesetInfo{ID: id, Name: lib.Name, Behavior: beh, References: references}, nil
					}
				}
				return rules.RulesetInfo{}, nil
			},
			ReadPayload: func(content string) ([]string, *yamlx.Node, error) {
				return rulefetch.ReadRulePayload(content)
			},
			AppendPayload: func(content, line string, node *yamlx.Node) (string, error) {
				return rulefetch.AppendRulePayload(content, line, node)
			},
		}
		got := map[string]any{}
		destination, err := rules.ParseDestination(record.Value, record.RuleType)
		var result *rules.IPResult
		if err == nil {
			result, err = rules.AppendIP(deps, library, "lib", record.Target, destination, record.RuleType)
		}
		if err != nil {
			got["message"] = err.Error()
		} else {
			added := result.Added
			if added == nil {
				added = []rules.IPAdded{}
			}
			skipped := result.Skipped
			if skipped == nil {
				skipped = []rules.IPSkipped{}
			}
			warnings := result.Warnings
			if warnings == nil {
				warnings = []string{}
			}
			got["result"] = map[string]any{"added": added, "skipped": skipped, "warnings": warnings}
			updatedContent := ""
			if len(result.Updated) > 0 && result.Updated[0].Content != nil {
				updatedContent = *result.Updated[0].Content
			} else if library[0].Content != nil {
				updatedContent = *library[0].Content
			}
			got["content"] = updatedContent
		}
		want := make(map[string]any, len(record.Result))
		for k, v := range record.Result {
			if k == "exception" {
				continue // compared by message
			}
			want[k] = v
		}
		// N7-3b/D3: actual rule edits may use the shared Node editor's rewrite
		// path. Compare only the returned YAML text structurally; every result
		// field remains exact.
		gContent, _ := got["content"].(string)
		wContent, _ := want["content"].(string)
		if gContent != wContent {
			gn, ge := yamlx.Parse([]byte(gContent))
			wn, we := yamlx.Parse([]byte(wContent))
			if ge == nil && we == nil && yamlx.Get(gn, "payload") != nil && yamlx.Get(wn, "payload") != nil && yamlx.Equal(gn, wn) {
				want["content"] = gContent
				nativeLayouts++
			}
		}
		var syntax *yamlx.SyntaxError
		wMsg, _ := want["message"].(string)
		gMsg, _ := got["message"].(string)
		if errors.As(err, &syntax) && strings.HasSuffix(wMsg, "。") && gMsg == strings.TrimSuffix(wMsg, "。")+"："+syntax.Error() {
			want["message"] = gMsg
			nativeDiagnostics++
		}
		if !testjson.Equal(want, got) {
			t.Errorf("apply_ip(%s, %s, %q, %v, policy=%v):\n%s", string(record.Behavior),
				string(record.Content), record.Value, record.RuleType, record.Policy, testjson.Diff(want, got))
		}
	}
	t.Logf("N7-3b: %d D3 layout and %d native diagnostic changes in %d C09 apply rows", nativeLayouts, nativeDiagnostics, len(root.Apply))
}
