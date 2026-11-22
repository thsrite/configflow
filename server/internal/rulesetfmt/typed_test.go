package rulesetfmt

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"os"
	"testing"

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
		// Stop after the function discriminator: unrelated archived rows contain
		// non-JSON non-finite values in arguments later in the object.
		decoder := json.NewDecoder(bytes.NewReader(s.Bytes()))
		if _, err := decoder.Token(); err != nil {
			t.Fatal(err)
		}
		function := ""
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				t.Fatal(err)
			}
			if key == "fn" {
				if err := decoder.Decode(&function); err != nil {
					t.Fatal(err)
				}
				break
			}
			var ignored json.RawMessage
			if err := decoder.Decode(&ignored); err != nil {
				t.Fatal(err)
			}
		}
		if function != "ruleset_format" {
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

// TestMalformedMembersReadAsAbsent (O18 T1c; was
// TestMalformedMembersUseTypedValidation, which failed on the R5 original
// value): a stored url/reference of another JSON type reads as absent, so the
// format is the one of the same rule without that member. ErrURLType and
// ErrReferenceIDType are no longer reachable from stored records.
func TestMalformedMembersReadAsAbsent(t *testing.T) {
	tests := []struct{ raw, absent string }{
		{`{"url":false}`, `{}`},
		{`{"url":[]}`, `{}`},
		{`{"url":{}}`, `{}`},
		{`{"library_rule_id":false}`, `{}`},
		{`{"name":false}`, `{}`},
	}
	library := []model.RuleLibraryItem{{Name: "valid"}}
	for _, tt := range tests {
		var rule, plain model.RuleConfig
		if err := json.Unmarshal([]byte(tt.raw), &rule); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(tt.absent), &plain); err != nil {
			t.Fatal(err)
		}
		got, err := RuleFormat(rule, library, "")
		want, wantErr := RuleFormat(plain, library, "")
		if got != want || (err == nil) != (wantErr == nil) {
			t.Fatalf("%s: %q/%v, absent member %q/%v", tt.raw, got, err, want, wantErr)
		}
	}
}

// TestLibraryIndexStopsAfterFirstMatch: the first matching library item wins;
// a stored id of another type reads as empty (O18 T1c), so it matches no
// reference instead of failing the lookup with ErrReferenceIDType.
func TestLibraryIndexStopsAfterFirstMatch(t *testing.T) {
	var invalid model.RuleLibraryItem
	if err := json.Unmarshal([]byte(`{"id":false}`), &invalid); err != nil {
		t.Fatal(err)
	}
	valid := model.RuleLibraryItem{ID: "a"}
	if index, err := LibraryIndex(model.RuleConfig{LibraryRuleID: "a"}, []model.RuleLibraryItem{valid, invalid}); index != 0 || err != nil {
		t.Fatalf("unused tail inspected: %d / %v", index, err)
	}
	if index, err := LibraryIndex(model.RuleConfig{LibraryRuleID: "a"}, []model.RuleLibraryItem{invalid, valid}); index != 1 || err != nil {
		t.Fatalf("lenient empty id matched: %d / %v", index, err)
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
