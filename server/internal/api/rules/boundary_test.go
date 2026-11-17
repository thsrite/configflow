package rules

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"configflow/internal/httpx/httpxtest"
	"configflow/internal/jsonx"
)

func TestHEADCollectionsAndDNSStayReadOnly(t *testing.T) {
	s := fixture(t)
	auth := s.Login()
	before, err := s.Repo.ExportAll(s.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/rules", "/api/profiles/other/rules", "/api/rule-sets", "/api/rules/dns?profile=other"} {
		s.Do("HEAD", path, auth, httpxtest.JSON(map[string]any{"routes": map[string]string{"rule_1": "blackhole"}})).WantStatus(200)
	}
	after, err := s.Repo.ExportAll(s.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	if !jsonx.Equal(before, after) {
		t.Fatal("HEAD changed rules or routing")
	}
}
func TestComposeRejectsYAMLPolicyFailuresAtomically(t *testing.T) {
	for _, content := range []string{"payload: [a.example]\npayload: [b.example]\n", "payload: &a [*a]\n"} {
		t.Run(content, func(t *testing.T) {
			s := fixture(t)
			before, err := s.Repo.ExportAll(s.Context(), false)
			if err != nil {
				t.Fatal(err)
			}
			library := record{"name": stringJSON("Bad source"), "source_type": stringJSON("content"), "behavior": stringJSON("domain"), "format": stringJSON("yaml"), "content": stringJSON(content)}
			raw, _ := json.Marshal(library)
			request := composeBody(record{"kind": stringJSON("new_library"), "library": raw}, "default")
			s.Post("/api/rules/compose?profile=other", s.Login(), httpxtest.JSON(request)).WantStatus(400)
			after, err := s.Repo.ExportAll(s.Context(), false)
			if err != nil {
				t.Fatal(err)
			}
			if !jsonx.Equal(before, after) {
				t.Fatal("rejected YAML changed state")
			}
		})
	}
}
func TestPasteUsesSharedLineFraming(t *testing.T) {
	for _, separator := range []string{"\n", "\r\n", "\r", "\v", "\f", "\x1c", "\x1d", "\x1e", "\u0085", "\u2028", "\u2029"} {
		rows, _, err := pasteRows(strings.Join([]string{"# comment", "first.example", "second.example", ""}, separator), "DOMAIN")
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(rows, []pasteRow{{Line: 2, Kind: "DOMAIN", Value: "first.example"}, {Line: 3, Kind: "DOMAIN", Value: "second.example"}}) {
			t.Fatalf("separator%q rows=%+v", separator, rows)
		}
	}
}
