package trafficstore

import (
	"bytes"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// A nonempty 400-item report updates a subset of a full day's existing hosts.
func BenchmarkReport400(b *testing.B) { benchmarkReport400(b, false) }

func BenchmarkColdReport400(b *testing.B) { benchmarkReport400(b, true) }

func benchmarkReport400(b *testing.B, cold bool) {
	var file, body strings.Builder
	at := time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC)
	store := NewNative(b.TempDir())
	file.WriteString(`{"status":null,"days":{"2026-11-28":{"ip_only_conns":0,"dropped":0,"hosts":{`)
	body.WriteString(`{"schema":1,"status":"running","items":[`)
	for i := range 3600 {
		host := fmt.Sprintf("h-%04d.example.com", i)
		if i > 0 {
			file.WriteByte(',')
		}
		fmt.Fprintf(&file, `%q:{"first_seen":"2026-11-28T00:00:00","last_seen":"2026-11-28T00:00:00","ports":{"443":5},"by_route":{"Match\t\tdirect\tDIRECT":{"conns":5,"fails":0,"zero_dl":0,"up":10,"down":50,"fail_kinds":{}}}}`, host)
		if i < 400 {
			if i > 0 {
				body.WriteByte(',')
			}
			fmt.Fprintf(&body, `{"host":%q,"port":443,"network":"tcp","policy":"DIRECT","outlet":"direct","rule":"Match","conns":1,"fails":0,"up":2,"down":10}`, host)
		}
	}
	file.WriteString("}}}}")
	body.WriteString("]}")
	if err := os.WriteFile(store.Path("agent"), []byte(file.String()), 0600); err != nil {
		b.Fatal(err)
	}
	raw := []byte(body.String())
	run := func() {
		report, err := DecodeReport(raw)
		if err != nil || report == nil || len(report.Items) != 400 {
			b.Fatal(err)
		}
		if _, err := store.Add(b.Context(), "agent", report, &at); err != nil {
			b.Fatal(err)
		}
	}
	run()
	b.ReportAllocs()
	b.SetBytes(int64(file.Len()))
	for b.Loop() {
		if cold {
			ResetCache()
		}
		run()
	}
}

func TestReportPublishesLegacyBytesAndCachesOnlyCommittedDocument(t *testing.T) {
	isolated(t)
	store := NewNative(t.TempDir())
	at := time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC)
	path := store.Path("a")
	const original = `{ "ext<": { "keep":"<&>" }, "days":{"2026-11-27": { "hosts":{}, "x":"\u0041" }, "2026-11-28":{"extra":1,"ip_only_conns":2.5,"dropped":true,"hosts":{}}}, "status":{"mihomo_version":"v<"} }`
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	r, err := DecodeReport([]byte(`{"schema":true,"status":"running","dropped":1,"ip_only_conns":2,"items":[{"host":"a.example.com","rule":"Match","policy":"<&>","outlet":"direct","port":443,"conns":2}]}`))
	if err != nil || r == nil {
		t.Fatal(err)
	}
	status, err := store.Add(t.Context(), "a", r, &at)
	if err != nil || string(status.MihomoVersion) != `"v<"` {
		t.Fatalf("inherited version: %+v %v", status, err)
	}
	const want = `{"ext\u003c":{"keep":"\u003c\u0026\u003e"},"days":{"2026-11-27":{"hosts":{},"x":"\u0041"},"2026-11-28":{"extra":1,"ip_only_conns":4.5,"dropped":2,"hosts":{"a.example.com":{"first_seen":"2026-11-28T12:00:00","last_seen":"2026-11-28T12:00:00","ports":{"443":2},"by_route":{"Match\t\tdirect\t\u003c\u0026\u003e":{"conns":2,"fails":0,"zero_dl":0,"up":0,"down":0,"fail_kinds":{}}}}}}},"status":{"value":"running","at":"2026-11-28T12:00:00","mihomo_version":"v\u003c"}}`
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != want {
		t.Fatalf("file %s (%v), want %s", raw, err, want)
	}
	calls := trackReads(store)
	doc := store.LoadDocument("a", &at)
	for _, v := range doc.DayRecords() {
		v[0] = '!'
	}
	current := store.LoadDocument("a", &at)
	encoded, err := current.MarshalJSON()
	if err != nil || string(encoded) != want || len(*calls) != 0 {
		t.Fatalf("snapshot %s, %v, reads %v", encoded, err, *calls)
	}
	for _, bad := range []string{`{"days":{},"future":"\ud800"}`, `{"days":{"day":{"a":true,"a":false}}}`} {
		if err := os.WriteFile(path, []byte(bad), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Add(t.Context(), "a", r, &at); !errors.Is(err, ErrInvalid) {
			t.Fatalf("damaged file %q: %v", bad, err)
		}
	}
}

func TestReportRestoresEvictedHostInFileOrder(t *testing.T) {
	isolated(t)
	old := MaxHostsPerDay
	MaxHostsPerDay = 2
	t.Cleanup(func() { MaxHostsPerDay = old })
	store := NewNative(t.TempDir())
	at := time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC)
	for i, name := range []string{"a", "b", "c", "a"} {
		raw := fmt.Sprintf(`{"schema":1,"status":"running","items":[{"host":"%s.example.com","rule":"Match","policy":"DIRECT","outlet":"direct","conns":%d}]}`, name, i+1)
		report, err := DecodeReport([]byte(raw))
		if err != nil || report == nil {
			t.Fatal(err)
		}
		if _, err := store.Add(t.Context(), "agent", report, &at); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(store.Path("agent"))
	if err != nil {
		t.Fatal(err)
	}
	const first, last = `"c.example.com"`, `"a.example.com"`
	if !jsontext.Value(raw).IsValid() || bytes.Contains(raw, []byte(`"b.example.com"`)) || bytes.Index(raw, []byte(first)) < 0 || bytes.Index(raw, []byte(first)) >= bytes.Index(raw, []byte(last)) {
		t.Fatalf("restored host or file order differs: %s", raw)
	}
}
