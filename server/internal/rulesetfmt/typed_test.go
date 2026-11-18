package rulesetfmt

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/model"
)

// The archived outputs were produced by backend/converters/common.py, not
// by either Go entry. Invalid stored members have separate assertions below.
func TestTypedEntryMatchesPythonFormatCorpus(t *testing.T) {
	file, err := os.Open("../convert/testdata/corpus.jsonl.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	r, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 4096), 16<<20)
	checked := 0
	for s.Scan() {
		var row struct {
			Function string            `json:"fn"`
			Args     []json.RawMessage `json:"args"`
			Output   string            `json:"out"`
			Error    json.RawMessage   `json:"err"`
		}
		// Most rows belong to unrelated functions and have non-string outputs.
		kind, err := jsonx.Parse(s.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		if kind.(*jsonx.Object).GetString("fn") != "ruleset_format" {
			continue
		}
		if err := json.Unmarshal(s.Bytes(), &row); err != nil {
			t.Fatal(err)
		}
		if len(row.Error) != 0 {
			continue
		}
		var rule model.RuleConfig
		var config struct {
			Library []model.RuleLibraryItem `json:"rule_library"`
		}
		var source string
		if err := json.Unmarshal(row.Args[0], &rule); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(row.Args[1], &config); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(row.Args[2], &source); err != nil {
			t.Fatal(err)
		}
		if len(model.Issues(rule)) != 0 {
			continue
		}
		valid := true
		for _, item := range config.Library {
			valid = valid && len(model.Issues(item)) == 0
		}
		if !valid {
			continue
		}
		oldRule, err := jsonx.Parse(row.Args[0])
		if err != nil {
			t.Fatal(err)
		}
		oldConfig, err := jsonx.Parse(row.Args[1])
		if err != nil {
			t.Fatal(err)
		}
		legacy, legacyErr := Format(oldRule.(*jsonx.Object), oldConfig.(*jsonx.Object).GetList("rule_library"), source)
		native, nativeErr := RuleFormat(rule, config.Library, source)
		var parseErr *URLParseError
		if nativeErr != nil && !errors.As(nativeErr, &parseErr) {
			t.Fatal(nativeErr)
		}
		if (legacyErr == nil) != (nativeErr == nil) || legacy != native || native != row.Output {
			t.Fatalf("row %d: legacy %q/%v native %q/%v Python %q", checked, legacy, legacyErr, native, nativeErr, row.Output)
		}
		checked++
	}
	if err := s.Err(); err != nil {
		t.Fatal(err)
	}
	if checked < 200 {
		t.Fatalf("only %d normal rows", checked)
	}
	t.Logf("normal Python/legacy/typed byte comparisons: %d", checked)
}

func TestTypedEntryMatchesLegacyBytes(t *testing.T) {
	tests := []struct{ rule, library, source, want string }{
		{`{}`, `[]`, "", "text"},
		{`{"url":"https://example.com/a.YML?download=1"}`, `[]`, "", "yaml"},
		{`{"url":"https://example.com/a.mrs#x"}`, `[]`, "", "mrs"},
		{`{"source_type":"content","content":"# comment\n---\npayload: [example.com]"}`, `[]`, "", "yaml"},
		{`{"source_type":"content","content":"DOMAIN,example.com"}`, `[{}]`, "", "text"},
		{`{"library_rule_id":"a","format":"text","url":"x.txt"}`, `[{"id":"a","format":"mrs"}]`, "", "mrs"},
		{`{"name":"set","format":"yaml"}`, `[{"name":"set","format":"invalid"}]`, "", "yaml"},
		{`{"name":"set","url":"a.txt"}`, `[{"name":"set","url":"a.mrs"}]`, "source.yaml", "mrs"},
		{`{"url":"a.txt"}`, `[]`, "source.yaml", "yaml"},
	}
	for _, tt := range tests {
		t.Run(tt.rule+tt.library, func(t *testing.T) {
			var rule model.RuleConfig
			var library []model.RuleLibraryItem
			if err := json.Unmarshal([]byte(tt.rule), &rule); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(tt.library), &library); err != nil {
				t.Fatal(err)
			}
			oldRule, err := jsonx.ParseString(tt.rule)
			if err != nil {
				t.Fatal(err)
			}
			oldLibrary, err := jsonx.ParseString(tt.library)
			if err != nil {
				t.Fatal(err)
			}
			legacy, legacyErr := Format(oldRule.(*jsonx.Object), oldLibrary.([]any), tt.source)
			native, nativeErr := RuleFormat(rule, library, tt.source)
			if legacyErr != nil || nativeErr != nil || legacy != native || native != tt.want {
				t.Fatalf("legacy=%q/%v native=%q/%v want=%q", legacy, legacyErr, native, nativeErr, tt.want)
			}
		})
	}
}

func TestMalformedMembersUseTypedValidation(t *testing.T) {
	for _, raw := range []string{`{"url":0}`, `{"url":false}`, `{"url":[]}`, `{"url":{}}`, `{"url":5}`} {
		var rule model.RuleConfig
		if err := json.Unmarshal([]byte(raw), &rule); err != nil {
			t.Fatal(err)
		}
		if _, err := RuleFormat(rule, nil, ""); !errors.Is(err, ErrURLType) {
			t.Fatalf("%s: %v", raw, err)
		}
		old, _ := jsonx.ParseString(raw)
		var urlType *URLTypeError
		if _, err := Format(old.(*jsonx.Object), nil, ""); !errors.As(err, &urlType) {
			t.Fatalf("legacy URL type category: %v", err)
		}
	}
	for _, raw := range []string{`{"library_rule_id":5}`, `{"name":false}`} {
		var rule model.RuleConfig
		if err := json.Unmarshal([]byte(raw), &rule); err != nil {
			t.Fatal(err)
		}
		if _, err := RuleFormat(rule, []model.RuleLibraryItem{{Name: "valid"}}, ""); err == nil {
			t.Fatal("invalid reference accepted")
		}
	}
	value, _ := jsonx.ParseString(`{}`)
	var entryType *LibraryEntryTypeError
	if _, err := Format(value.(*jsonx.Object), []any{jsonx.Int(1)}, ""); !errors.As(err, &entryType) {
		t.Fatalf("entry category: %v", err)
	}
}

func FuzzRuleFormat(f *testing.F) {
	f.Add(`{"url":"https://example.com/a.yaml"}`, `[{"name":"set","format":"mrs"}]`)
	f.Fuzz(func(t *testing.T, ruleText, libraryText string) {
		var rule model.RuleConfig
		var library []model.RuleLibraryItem
		if json.Unmarshal([]byte(ruleText), &rule) != nil || json.Unmarshal([]byte(libraryText), &library) != nil {
			return
		}
		_, _ = RuleFormat(rule, library, "")
	})
}
