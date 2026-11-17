package pushdiff

import (
	"compress/gzip"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"configflow/internal/agents/diag"
	"configflow/internal/yamlx"
)

func TestReviewPythonBoundary(t *testing.T) {
	f, err := os.Open("testdata/review.json.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	z, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	var rows []struct {
		Kind, Name, Old, New, Text string
		Expected                   json.RawMessage
		Pages                      []struct {
			Change, Query string
			Offset        int
			Expected      json.RawMessage
		}
	}
	if err := json.NewDecoder(z).Decode(&rows); err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		t.Run(row.Kind+"/"+row.Name, func(t *testing.T) {
			var got any
			switch row.Kind {
			case "config":
				got = Config([]byte(row.Old), []byte(row.New), "", "")
			case "nodes":
				s, d := nodeChange([]byte(row.Old), []byte(row.New), &detailBudget{left: MaxDetailBytes}, nil)
				if row.Name == "typed-map-key" {
					// Python's JSON identity conflates integer and string object
					// keys. Preserve the actual YAML change instead (Python bug).
					if s.Modified == nil || *s.Modified != 1 || d == nil || len(d.Groups) != 1 || len(*d.Groups[0].Entries[0].Fields) != 2 {
						t.Fatal("typed mapping-key change lost")
					}
					return
				}
				got = []any{s, d}
			case "node-budget":
				keep := &keeper{left: MaxKeptBytes}
				_, d := nodeChange([]byte(row.Old), []byte(row.New), &detailBudget{left: MaxDetailBytes}, keep)
				if keep.last == nil || d == nil {
					t.Fatal("missing node details")
				}
				page := keep.last.Page("modified", 0, 50, "", nil)
				got = struct {
					Shown      int  `json:"shown"`
					Available  int  `json:"available"`
					PageCount  int  `json:"page_count"`
					NextOffset *int `json:"next_offset"`
				}{len(d.Groups[0].Entries), *d.Groups[0].Available, len(page.Entries), page.NextOffset}
			case "paging":
				keep := &keeper{left: MaxKeptBytes}
				s, d := ruleChange([]byte(row.Old), []byte(row.New), &detailBudget{left: MaxDetailBytes}, keep)
				got = struct {
					Summary *Summary `json:"summary"`
					Details *Details `json:"details"`
				}{s, d}
				if keep.last == nil {
					t.Fatal("no retained entries")
				}
				for _, page := range row.Pages {
					compareJSON(t, keep.last.Page(page.Change, page.Offset, 50, SearchQuery(page.Query), nil), page.Expected)
				}
			case "spans":
				parts := []string{}
				for _, span := range diag.CredentialSpans(row.Text) {
					parts = append(parts, row.Text[span[0]:span[1]])
				}
				// Malformed userinfo is intentionally hidden in full by net/url.
				if strings.Contains(row.Text, "synthetic\\tail@") {
					if len(parts) != 1 || parts[0] != row.Text {
						t.Fatalf("malformed URL partially exposed: %q", parts)
					}
					return
				}
				got = parts
			default:
				t.Fatal(row.Kind)
			}
			compareJSON(t, withoutRaw(decoded(t, got)), decoded(t, row.Expected))
		})
	}
}

func TestReviewRawMarkingFailsClosed(t *testing.T) {
	result := rawDiff([]byte("mode: rule\n"), []byte("url: *missing\npassword: synthetic-private\n"), "", "")
	hidden, _ := json.Marshal(WithoutSecrets(result))
	if strings.Contains(string(hidden), "synthetic-private") {
		t.Fatalf("invalid rendered YAML exposed secrets: %s", hidden)
	}
	var shown strings.Builder
	for _, line := range result.Lines {
		s, _ := line.Unpack()
		shown.WriteString(s)
	}
	if !strings.Contains(shown.String(), "synthetic-private") {
		t.Fatal("reveal lost original diff content")
	}
}

func TestReviewCredentialViewsAndSearch(t *testing.T) {
	for _, value := range []string{
		`https://rules.example.com/?token=synthetic\private-tail`,
		`https://user:synthetic\private-tail@rules.example.com/path`,
		`https://rules.example.com/path\part?key=synthetic-private-tail`,
		"https://rules.example.com/?key=synthetic-private-tail\ncontinued public text",
		"https://rules.example.com/?api_key=synthetic-private-tail",
	} {
		t.Run(value, func(t *testing.T) {
			quoted, _ := json.Marshal(value)
			for _, source := range []string{
				"k: " + string(quoted),
				"k: {" + string(quoted) + ": visible}",
				"hosts: {" + string(quoted) + ": visible}",
			} {
				result := Config([]byte("mode: rule"), []byte(source), "", "")
				hidden, _ := json.Marshal(WithoutSecrets(result))
				if strings.Contains(string(hidden), "private-tail") {
					t.Fatalf("hidden view exposes credentials: %s", hidden)
				}
			}
			provider := "proxies: [{name: " + string(quoted) + ", type: http, server: relay.example.com, port: 80}]"
			keep := &keeper{left: MaxKeptBytes}
			_, _ = nodeChange([]byte("proxies: []"), []byte(provider), &detailBudget{left: MaxDetailBytes}, keep)
			if keep.last == nil {
				t.Fatal("no retained provider")
			}
			if p := keep.last.Page("added", 0, 50, SearchQuery("private-tail"), nil); p.Total != 0 {
				t.Fatal("search exposed credentials")
			}
			if p := keep.last.Page("added", 0, 50, SearchQuery("relay.example.com:80"), nil); p.Total != 1 {
				t.Fatal("public node info lost")
			}
		})
	}
}

func TestReviewProviderAliasesKeepRawValidAndSecret(t *testing.T) {
	for _, section := range []string{"proxy-providers", "rule-providers"} {
		for _, path := range []string{"rules/a.yaml", "./rules/a.yaml"} {
			t.Run(section+"/"+path, func(t *testing.T) {
				source := section + ": &providers\n  alpha: {path: " + path + ", url: 'https://rules.example.com/?token=synthetic-private'}\ncopy: *providers\npassword: synthetic-password\n"
				result := Config([]byte("mode: rule\n"), []byte(source), "", "")
				var candidate strings.Builder
				for i, line := range result.Raw.Lines {
					text, _ := line.Unpack()
					if i > 2 && strings.HasPrefix(text, "+") {
						candidate.WriteString(text[1:] + "\n")
					}
				}
				doc, err := yamlx.Parse([]byte(candidate.String()))
				if err != nil {
					t.Errorf("raw candidate is invalid YAML: %v", err)
				} else {
					if scalar(yamlx.Get(yamlx.Get(yamlx.Get(doc, "copy"), "alpha"), "path")) != path {
						t.Error("changed an alias outside the provider path")
					}
				}
				hidden, _ := json.Marshal(WithoutSecrets(result))
				if strings.Contains(string(hidden), "synthetic-private") || strings.Contains(string(hidden), "synthetic-password") {
					t.Errorf("raw secrets exposed: %s", hidden)
				}
			})
		}
	}
}
