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
		// The archive contains non-JSON NaN in unrelated parser rows.
		// Read only its discriminator through the archive's legacy reader.
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
		native, nativeErr := RuleFormat(rule, config.Library, source)
		var parseErr *URLParseError
		if nativeErr != nil && !errors.As(nativeErr, &parseErr) {
			t.Fatal(nativeErr)
		}
		if native != row.Output {
			t.Fatalf("row %d: native %q/%v Python %q", checked, native, nativeErr, row.Output)
		}
		checked++
	}
	if err := s.Err(); err != nil {
		t.Fatal(err)
	}
	if checked < 200 {
		t.Fatalf("only %d normal rows", checked)
	}
	t.Logf("normal Python/typed byte comparisons: %d", checked)
}

func TestTypedEntryExpectedBytes(t *testing.T) {
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
			native, nativeErr := RuleFormat(rule, library, tt.source)
			if nativeErr != nil || native != tt.want {
				t.Fatalf("native=%q/%v want=%q", native, nativeErr, tt.want)
			}
		})
	}
}

func TestMalformedMembersUseTypedValidation(t *testing.T) {
	for _, raw := range []string{`{"url":false}`, `{"url":[]}`, `{"url":{}}`} {
		var rule model.RuleConfig
		if err := json.Unmarshal([]byte(raw), &rule); err != nil {
			t.Fatal(err)
		}
		if _, err := RuleFormat(rule, nil, ""); !errors.Is(err, ErrURLType) {
			t.Fatalf("%s: %v", raw, err)
		}

	}
	for _, raw := range []string{`{"library_rule_id":false}`, `{"name":false}`} {
		var rule model.RuleConfig
		if err := json.Unmarshal([]byte(raw), &rule); err != nil {
			t.Fatal(err)
		}
		if _, err := RuleFormat(rule, []model.RuleLibraryItem{{Name: "valid"}}, ""); err == nil {
			t.Fatal("invalid reference accepted")
		}
	}

}

func TestLibraryIndexStopsAfterFirstMatch(t *testing.T) {
	var invalid model.RuleLibraryItem
	if err := json.Unmarshal([]byte(`{"id":false}`), &invalid); err != nil {
		t.Fatal(err)
	}
	valid := model.RuleLibraryItem{ID: "a"}
	if index, err := LibraryIndex(model.RuleConfig{LibraryRuleID: "a"}, []model.RuleLibraryItem{valid, invalid}); index != 0 || err != nil {
		t.Fatalf("unused tail inspected: %d / %v", index, err)
	}
	if _, err := LibraryIndex(model.RuleConfig{LibraryRuleID: "a"}, []model.RuleLibraryItem{invalid, valid}); !errors.Is(err, ErrReferenceIDType) {
		t.Fatalf("invalid prefix ignored: %v", err)
	}
	if index, err := LibraryIndex(model.RuleConfig{}, []model.RuleLibraryItem{{}, invalid}); index != 0 || err != nil {
		t.Fatalf("empty first match lost: %d / %v", index, err)
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
