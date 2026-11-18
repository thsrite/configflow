package pushdiff

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"

	"configflow/internal/deploy"
)

type snapshot struct {
	Manifest   deploy.Manifest   `json:"manifest"`
	Files      map[string][]byte `json:"files"`
	Comparison *deploy.File      `json:"comparison"`
}

func (s *snapshot) bundle(t *testing.T) *deploy.Bundle {
	t.Helper()
	if s == nil {
		return nil
	}
	files := map[string]deploy.Content{}
	for _, f := range s.Manifest.Files {
		files[f.Path] = deploy.Content{Data: s.Files[f.Path], Role: f.Role}
	}
	b, err := deploy.Pack(s.Manifest, files, "", s.Comparison)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

type corpusRow struct {
	Kind, Name string
	Old, New   []byte
	OldName    string `json:"old_name"`
	NewName    string `json:"new_name"`
	Expected   json.RawMessage
	Agent      struct {
		ServiceType string `json:"service_type"`
		Applied     struct {
			Files map[string]string `json:"files"`
		} `json:"applied_content"`
	}
	Before, After *snapshot
	Comparison    *deploy.File
	Pages         map[string]map[string][]json.RawMessage
	DetailBudget  int `json:"detail_budget"`
}

func decoded(t *testing.T, v any) any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err = json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}
func compareJSON(t *testing.T, got, want any) {
	t.Helper()
	g, w := decoded(t, got), decoded(t, want)
	if !reflect.DeepEqual(g, w) {
		t.Fatalf("%s", difference(g, w, "$"))
	}
}
func difference(a, b any, p string) string {
	if reflect.DeepEqual(a, b) {
		return ""
	}
	switch x := a.(type) {
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok {
			break
		}
		for k, v := range y {
			if _, ok := x[k]; !ok {
				return p + " missing " + k
			}
			if d := difference(x[k], v, p+"."+k); d != "" {
				return d
			}
		}
		for k := range x {
			if _, ok := y[k]; !ok {
				return p + " extra " + k
			}
		}
	case []any:
		y, ok := b.([]any)
		if !ok {
			break
		}
		if len(x) != len(y) {
			return fmt.Sprintf("%s length got %d want %d", p, len(x), len(y))
		}
		for i, v := range x {
			if d := difference(v, y[i], fmt.Sprintf("%s[%d]", p, i)); d != "" {
				return d
			}
		}
	}
	return fmt.Sprintf("%s got %#v want %#v", p, a, b)
}

// Raw YAML layout is deliberately native (PORTING). All other JSON fields are
// strict. Raw patches are independently checked by TestUnifiedPatchAndMarkers.
func withoutRaw(v any) any {
	switch x := v.(type) {
	case map[string]any:
		delete(x, "raw")
		for _, item := range x {
			withoutRaw(item)
		}
	case []any:
		for _, item := range x {
			withoutRaw(item)
		}
	}
	return v
}
func TestPythonCorpus(t *testing.T) {
	t.Parallel()
	f, err := os.Open("testdata/python.json.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	z, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	var corpus struct {
		Rows     []corpusRow
		Executed []string
	}
	if err = json.NewDecoder(z).Decode(&corpus); err != nil {
		t.Fatal(err)
	}
	if len(corpus.Executed) != 32 || len(corpus.Rows) < 67 {
		t.Fatalf("corpus shrank: %d tests, %d rows", len(corpus.Executed), len(corpus.Rows))
	}
	for i, row := range corpus.Rows {
		t.Run(fmt.Sprintf("%02d-%s-%s", i, row.Kind, row.Name), func(t *testing.T) {
			t.Parallel()
			var got any
			switch row.Kind {
			case "config":
				got = Config(row.Old, row.New, row.OldName, row.NewName)
			case "nodes":
				s, d := nodeChange(row.Old, row.New, &detailBudget{left: MaxDetailBytes}, nil)
				got = []any{s, d}
			case "rules":
				s, d := ruleChange(row.Old, row.New, &detailBudget{left: MaxDetailBytes}, nil)
				got = []any{s, d}
			case "build":
				var receipt *deploy.Receipt
				if row.Before != nil {
					receipt = &deploy.Receipt{Manifest: row.Before.Manifest, Bundle: row.Before.bundle(t), ComparisonConfig: row.Comparison}
				}
				lists := map[string]*FileEntries{}
				var err error
				got, err = build(AppliedView{ServiceType: row.Agent.ServiceType, Files: row.Agent.Applied.Files}, row.After.bundle(t), receipt, lists, row.DetailBudget)
				if err != nil {
					t.Fatal(err)
				}
				queries := []struct {
					offset int
					query  string
				}{{0, ""}, {20, ""}, {0, "example"}, {0, "香港"}, {0, "password"}}
				for path, groups := range row.Pages {
					entries := lists[path]
					if entries == nil {
						t.Fatalf("missing entries %s", path)
					}
					for change, pages := range groups {
						for i, page := range pages {
							q := queries[i]
							compareJSON(t, entries.Page(change, q.offset, 50, SearchQuery(q.query), nil), page)
						}
					}
				}
			default:
				t.Fatal(row.Kind)
			}
			compareJSON(t, withoutRaw(decoded(t, got)), withoutRaw(decoded(t, row.Expected)))
		})
	}
}
