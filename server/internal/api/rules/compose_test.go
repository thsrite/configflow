package rules

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"configflow/internal/httpx/httpxtest"
	"configflow/internal/jsonx"
	shared "configflow/internal/rules"
)

func composeBody(source record, dns string) record {
	sourceJSON, _ := json.Marshal(source)
	return record{"source": sourceJSON, "rule": json.RawMessage(`{"policy":"PROXY"}`), "dns": stringJSON(dns)}
}
func TestComposeAtomicValuesAndLibraries(t *testing.T) {
	s := fixture(t)
	auth := s.Login()
	body := composeBody(record{"kind": stringJSON("values"), "rule_type": stringJSON("DOMAIN"), "text": stringJSON("example.com\n*.new.example\nfull:exact.example\nDOMAIN-REGEX,^a{1,3}\\.example$\n")}, "direct")
	result := s.Post("/api/rules/compose?profile=other", auth, httpxtest.JSON(body)).WantStatus(200).Object()
	if len(result.GetObject("data").GetList("rules")) != 3 || len(result.GetList("skipped")) != 1 {
		t.Fatal("unexpected composition result")
	}
	before, err := s.Repo.ExportAll(s.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	bad := composeBody(record{"kind": stringJSON("new_library"), "library": json.RawMessage(`{"id":"client-id","name":"New","source_type":"content","behavior":"domain","content":"+.new.example\n"}`)}, "default")
	bad["rule"] = json.RawMessage(`{"policy":"unknown"}`)
	s.Post("/api/rules/compose?profile=other", auth, httpxtest.JSON(bad)).WantStatus(400)
	after, err := s.Repo.ExportAll(s.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	if !jsonx.Equal(before, after) {
		t.Fatal("shared/profile changes were not rolled back")
	}
	bad["rule"] = json.RawMessage(`{"policy":"DIRECT"}`)
	added := s.Post("/api/rules/compose?profile=other", auth, httpxtest.JSON(bad)).WantStatus(200).Object().GetObject("data")
	lib := added.GetObject("library")
	if !strings.HasPrefix(lib.GetString("id"), "lib_") || lib.GetString("id") == "client-id" {
		t.Fatal("server did not own library identity")
	}
	shared, err := s.Repo.GetShared(s.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(shared.GetList("rule_library")) != 2 {
		t.Fatal("new library not committed")
	}
	s.Post("/api/rules/compose?profile=other", auth, httpxtest.JSON(composeBody(record{"kind": stringJSON("library"), "library_rule_ids": json.RawMessage(`["ads"]`)}, "default"))).WantStatus(409)
}
func TestComposeCacheFailureDoesNotUndoCommit(t *testing.T) {
	s := fixture(t)
	dir := filepath.Join(s.DataDir, "shared", "rules")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0755) })
	body := composeBody(record{"kind": stringJSON("new_library"), "library": json.RawMessage(`{"name":"No cache","source_type":"content","behavior":"domain","content":"+.new.example\n"}`)}, "default")
	result := s.Post("/api/rules/compose?profile=other", s.Login(), httpxtest.JSON(body)).WantStatus(200).Object()
	if !jsonx.Equal(result.Value("warnings"), []any{msgComposedCacheWarning}) {
		t.Fatalf("warnings %v", result.Value("warnings"))
	}
	shared, err := s.Repo.GetShared(s.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(shared.GetList("rule_library")) != 2 {
		t.Fatal("accepted commit was lost")
	}
}
func TestCancelledComposeAndConcurrentDuplicateChecks(t *testing.T) {
	s := fixture(t)
	ctx, cancel := context.WithCancel(s.Context())
	cancel()
	body := composeBody(record{"kind": stringJSON("values"), "rule_type": stringJSON("DOMAIN"), "text": stringJSON("cancelled.example")}, "default")
	if _, err := (&Service{}).Compose(ctx, "other", body, "http://localhost"); err != nil {
		t.Fatal(err)
	}
	body = composeBody(record{"kind": stringJSON("values"), "rule_type": stringJSON("DOMAIN"), "text": stringJSON("same.example")}, "default")
	var workers sync.WaitGroup
	results := make(chan composeResult, 2)
	failures := make(chan error, 2)
	start := make(chan struct{})
	for range 2 {
		workers.Go(func() {
			<-start
			out, err := (&Service{}).Compose(s.Context(), "other", body, "http://localhost")
			if err != nil {
				failures <- err
			} else {
				results <- out
			}
		})
	}
	close(start)
	workers.Wait()
	close(results)
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	added, skipped := 0, 0
	for out := range results {
		added += len(out.Data.Rules)
		skipped += len(out.Skipped)
	}
	if added != 1 || skipped != 1 {
		t.Fatalf("added=%d skipped=%d", added, skipped)
	}
}
func TestPasteKeepsCommaPayloadAndLineErrors(t *testing.T) {
	rows, _, err := pasteRows("# comment\nDOMAIN-REGEX,^node{1,3}\\.example$\nMATCH,DIRECT\n", "DOMAIN")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Value != `^node{1,3}\.example$` || rows[1].Value != "" {
		t.Fatal(rows)
	}
	_, _, err = pasteRows("DOMAIN,good.example\nIP-CIDR,bad.example\nDOMAIN,\n", "DOMAIN")
	var fields *pasteError
	if !errorsAsPaste(err, &fields) || !reflect.DeepEqual([]int{fields.Fields[0].Line, fields.Fields[1].Line}, []int{2, 3}) {
		t.Fatal(err)
	}
}
func errorsAsPaste(err error, out **pasteError) bool {
	value, ok := err.(*pasteError)
	*out = value
	return ok
}
func FuzzPastedRules(f *testing.F) {
	for _, seed := range []string{"DOMAIN,example.com", "DOMAIN-REGEX,^a{1,3}\\.example$", "AND,((DOMAIN,a.example),(DST-PORT,443))", "# comment\n*.example.com\n"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		rows, _, err := pasteRows(text, "DOMAIN")
		if err != nil {
			return
		}
		seen := map[[2]string]bool{}
		for _, row := range rows {
			key := [2]string{row.Kind, row.Value}
			if seen[key] {
				t.Fatal("duplicate row")
			}
			seen[key] = true
			if message := shared.RuleValueError(row.Kind, row.Value); message != "" {
				t.Fatal(message)
			}
		}
	})
}
