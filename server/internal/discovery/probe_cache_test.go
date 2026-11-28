package discovery

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"configflow/internal/health"
	"configflow/internal/health/check"
	"configflow/internal/repo"
)

func BenchmarkLegacyProbes3000(b *testing.B) {
	r, err := repo.Open(b.TempDir(), repo.Options{})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(r.Close)
	svc := New(Options{Repo: r})
	store, err := svc.probeStore()
	if err != nil {
		b.Fatal(err)
	}
	path := store.Path("agent")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		b.Fatal(err)
	}
	var file strings.Builder
	file.WriteString(`{"hosts":{`)
	stamp := time.Now().Format("2006-01-02T15:04:05")
	for i := range 3000 {
		if i != 0 {
			file.WriteByte(',')
		}
		host := fmt.Sprintf("host-%04d.example.com", i)
		fmt.Fprintf(&file, `%q:{"host":%q,"url":"https://%s/","checked_at":%q,"verdict":"both_ok","target":"direct","confidence":85,"reasons":["synthetic control passed","synthetic host reachable"],"direct":{"ok":2,"total":2,"delays":[10,12],"errors":{}},"proxy":{"ok":2,"total":2,"delays":[20,22],"errors":{}},"proxy_path":"synthetic","future":"preserved on disk"}`, host, host, host, stamp)
	}
	file.WriteString("}}")
	if err := os.WriteFile(path, []byte(file.String()), 0600); err != nil {
		b.Fatal(err)
	}
	as := []Agent{{ID: "agent"}}
	if ps, err := svc.legacyProbes(b.Context(), as); err != nil || len(ps) != 3000 {
		b.Fatal(len(ps), err)
	}
	b.ReportAllocs()
	b.SetBytes(int64(file.Len()))
	for b.Loop() {
		if _, err := svc.legacyProbes(b.Context(), as); err != nil {
			b.Fatal(err)
		}
	}
}

func TestCachedProbeProjection(t *testing.T) {
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	svc := New(Options{Repo: r})
	store, err := svc.probeStore()
	if err != nil {
		t.Fatal(err)
	}
	path := store.Path("a")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().Add(-time.Hour).Format("2006-01-02T15:04:05")
	write := func(id, raw string) {
		t.Helper()
		if err := os.WriteFile(store.Path(id), []byte(strings.ReplaceAll(raw, "@stamp", stamp)), 0600); err != nil {
			t.Fatal(err)
		}
	}
	as := []Agent{{ID: "a"}, {ID: "b"}}
	referenceStore := health.ProbeStore(r.DataDir())
	compare := func(wantErr bool) map[string]Probe {
		t.Helper()
		entries, err := check.LatestProbeEntries([]string{"a", "b"}, referenceStore)
		if err != nil {
			t.Fatal(err)
		}
		var want map[string]Probe
		refErr := project(entries, &want)
		got, err := svc.legacyProbes(t.Context(), as)
		if (refErr != nil) != wantErr || (err != nil) != wantErr || err != nil && err.Error() != refErr.Error() || err == nil && !reflect.DeepEqual(got, want) {
			t.Fatalf("got %+v, %v; reference %+v, %v", got, err, want, refErr)
		}
		return got
	}
	write("a", `{"hosts":{"one.example.com":{"checked_at":"@stamp","host":"one.example.com","target":"proxy","url":"https://example.com/","reasons":[],"confidence":90,"proxy_path":"chosen","direct":{"ok":1,"total":2,"delays":[],"errors":{"reset":1},"unknown":true},"proxy":null,"HOST":"ignored","future":12},"expired.example.com":{"checked_at":"2000","target":false},"missing.example.com":{},"two.example.com":{"checked_at":"@stamp","reasons":null}}}`)
	write("b", `{"hosts":{"one.example.com":{"checked_at":"@stamp","target":123}}}`)
	compare(false)
	compare(false)
	write("b", `{"hosts":{"one.example.com":{"checked_at":"9999","target":123}}}`)
	compare(true)
	for _, fields := range []string{`"direct":1`, `"direct":{"ok":false}`, `"reasons":[false]`, `"proxy":{"errors":{"reset":false}}`, `"confidence":1e60`} {
		write("b", `{"hosts":{"one.example.com":{"checked_at":"9999",`+fields+`}}}`)
		compare(true)
	}
	if err := svc.legacySaveProbes(t.Context(), "b", map[string]Probe{"one.example.com": {CheckedAt: "9999", Verdict: "new"}}); err != nil {
		t.Fatal(err)
	}
	if compare(false)["one.example.com"].Verdict != "new" {
		t.Fatal("save did not replace snapshot")
	}
	if err := store.Delete(t.Context(), "b"); err != nil {
		t.Fatal(err)
	}
	if compare(false)["one.example.com"].Target == nil {
		t.Fatal("delete did not invalidate snapshot")
	}
}
