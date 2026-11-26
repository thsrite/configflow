package subs

import "testing"

// TestRegistrableCorpus compares registrable (golang.org/x/net/publicsuffix)
// with proxy_server_dns._registrable (tldextract's bundled snapshot, private
// suffixes included) on names built from every rule of that snapshot.
func TestRegistrableCorpus(t *testing.T) {
	rows := loadCorpus(t, "psl.jsonl.gz")
	diffs := 0
	for i, row := range rows {
		name := row.In.(string)
		want, _ := row.Out.(string)
		if got := registrable(name); got != want {
			diffs++
			if diffs <= 40 {
				t.Errorf("row %d registrable(%q) = %q, Python %q", i, name, got, want)
			}
		}
	}
	t.Logf("%d names, %d differences", len(rows), diffs)
}
