package rules

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"unicode"

	"configflow/internal/jsonx"
)

// IGNORECASE against CPython 3.11 (testdata/gen_case.py →
// testdata/icase_reference.json.gz): every cased literal, ASCII-mode
// literals and a list of character sets, on both engines (a leading
// look-ahead forces the backtracking engine).

func loadICaseReference(t *testing.T) *jsonx.Object {
	t.Helper()
	file, err := os.Open("testdata/icase_reference.json.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	value, err := jsonx.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return value.(*jsonx.Object)
}

func runeOf(v any) rune { n, _ := jsonx.AsInt(v); return rune(n) }

// matchedRunes runs a one-character pattern over haystack and returns the
// matched characters (like re.findall).
func matchedRunes(t *testing.T, re *PyRegex, haystack string) map[rune]bool {
	t.Helper()
	found := map[rune]bool{}
	if re.re != nil {
		for _, loc := range re.re.FindAllStringIndex(haystack, -1) {
			for _, r := range haystack[loc[0]:loc[1]] {
				found[r] = true
			}
		}
		return found
	}
	m, err := re.net.FindStringMatch(haystack)
	for m != nil && err == nil {
		for _, r := range m.String() {
			found[r] = true
		}
		m, err = re.net.FindNextMatch(m)
	}
	if err != nil {
		t.Fatal(err)
	}
	return found
}

func compileBoth(t *testing.T, pattern string) []*PyRegex {
	t.Helper()
	var out []*PyRegex
	for _, source := range []string{pattern, "(?=[\\s\\S])" + pattern} {
		if strings.HasPrefix(pattern, "(?") && !strings.HasPrefix(pattern, "(?i:") && source != pattern {
			// Global flags must stay first: put the look-ahead after them.
			end := strings.IndexByte(pattern, ')')
			source = pattern[:end+1] + "(?=[\\s\\S])" + pattern[end+1:]
		}
		re, err := CompilePyRegex(source, false)
		if err != nil {
			t.Fatalf("compile %q: %v", source, err)
		}
		out = append(out, re)
	}
	if out[1].net == nil {
		t.Fatalf("%q did not use the backtracking engine", pattern)
	}
	return out
}

func TestICaseLiteralsLikePython(t *testing.T) {
	ref := loadICaseReference(t)
	var hay strings.Builder
	for _, entry := range ref.GetList("literals") {
		hay.WriteRune(runeOf(entry.([]any)[0]))
	}
	haystack := hay.String()
	check := func(prefix string, entries []any, engines int) {
		for _, entry := range entries {
			row := entry.([]any)
			c := runeOf(row[0])
			want := map[rune]bool{}
			for _, v := range row[1:] {
				want[runeOf(v)] = true
			}
			pattern := prefix + string(c) // cased letters need no escaping
			for i, re := range compileBoth(t, pattern)[:engines] {
				got := matchedRunes(t, re, haystack)
				if fmt.Sprint(sortedRunes(got)) != fmt.Sprint(sortedRunes(want)) {
					t.Errorf("engine %d: %q matches %U, Python %U", i, pattern, sortedRunes(got), sortedRunes(want))
				}
			}
		}
	}
	check("(?i)", ref.GetList("literals"), 1)
	check("(?ai)", ref.GetList("ascii_literals"), 2)
	// The backtracking engine on a sample (all literals take long there).
	var sample []any
	for i, entry := range ref.GetList("literals") {
		if i%37 == 0 || strings.ContainsRune("iIİıkKsSſß", runeOf(entry.([]any)[0])) {
			sample = append(sample, entry)
		}
	}
	check("(?i)", sample, 2)
}

func TestICaseSetsLikePython(t *testing.T) {
	var hay strings.Builder
	for r := rune(0); r <= unicode.MaxRune; r++ {
		if r < 0xD800 || r > 0xDFFF {
			hay.WriteRune(r)
		}
	}
	haystack := hay.String()
	ref := loadICaseReference(t)
	// Characters assigned after Python's Unicode version (Go's tables are
	// newer; domain-level accepted difference) are not compared.
	unassigned := map[rune]bool{}
	for _, item := range ref.GetList("unassigned") {
		pair := item.([]any)
		for r := runeOf(pair[0]); r <= runeOf(pair[1]); r++ {
			unassigned[r] = true
		}
	}
	for _, entry := range ref.GetList("sets") {
		record := entry.(*jsonx.Object)
		pattern := record.GetString("pattern")
		want := map[rune]bool{}
		for _, item := range record.GetList("ranges") {
			pair := item.([]any)
			for r := runeOf(pair[0]); r <= runeOf(pair[1]); r++ {
				want[r] = true
			}
		}
		for i, re := range compileBoth(t, pattern) {
			got := matchedRunes(t, re, haystack)
			for r := range got {
				if unassigned[r] && !want[r] {
					delete(got, r)
				}
			}
			for r := range want {
				if unassigned[r] && !got[r] {
					delete(want, r)
				}
			}
			if len(got) != len(want) {
				t.Errorf("engine %d: %q matches %d characters, Python %d (%s)", i, pattern, len(got), len(want), diffRunes(got, want))
				continue
			}
			for r := range want {
				if !got[r] {
					t.Errorf("engine %d: %q misses %U", i, pattern, r)
					break
				}
			}
		}
	}
}

func sortedRunes(set map[rune]bool) []rune {
	var out []rune
	for r := rune(0); r <= unicode.MaxRune && len(out) < len(set); r++ {
		if set[r] {
			out = append(out, r)
		}
	}
	return out
}

func diffRunes(got, want map[rune]bool) string {
	var extra, missing []rune
	for r := range got {
		if !want[r] && len(extra) < 8 {
			extra = append(extra, r)
		}
	}
	for r := range want {
		if !got[r] && len(missing) < 8 {
			missing = append(missing, r)
		}
	}
	return fmt.Sprintf("extra %U, missing %U", extra, missing)
}

// Ignore-case sets are folded without testing every code point, so rule
// sets with many DOMAIN-REGEX lines (compiled with IGNORECASE) stay cheap.
func TestICaseSetFoldIsCheap(t *testing.T) {
	model := caseModel{t: loadCaseTables()}
	items := []setItem{{kind: setRange, lo: 'a', hi: 'z'}, {kind: setRange, lo: '0', hi: '9'}, {kind: setLiteral, lo: '-'}, {kind: setCategory, cat: catWord}}
	result := testing.Benchmark(func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			model.foldSet(items)
		}
	})
	if per := result.NsPerOp(); per > 2_000_000 {
		t.Fatalf("foldSet takes %d ns", per)
	}
}
