package rules

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// These are CPython 3.11 re boundary results for marks and connector
// punctuation. Python's \w is alphanumeric plus underscore, while the
// backtracking engine's built-in \b uses its own broader word categories.
func TestReviewUnicodeWordBoundaries(t *testing.T) {
	cases := []struct {
		pattern, text string
		want          bool
	}{
		{`a\b`, "a\u0301", true}, // combining acute is not Python \w
		{`a\B`, "a\u0301", false},
		{`\ba`, "\u0301a", true}, // the mark before a is not Python \w
		{`a\b`, "a\u203f", true}, // undertie (Pc) is not Python \w
		{`a\b`, "a_", false},     // underscore is Python \w
		{`(?a)a\b`, "a界", true},  // ASCII mode has ASCII word chars
		{`(?a)a\B`, "a界", false},
		{`\B`, "", false}, // CPython 3.11 does not match it on empty text
	}
	for _, tc := range cases {
		re, err := CompilePyRegex(tc.pattern, false)
		if err != nil {
			t.Fatalf("CompilePyRegex(%q): %v", tc.pattern, err)
		}
		if got := re.Search(tc.text); got != tc.want {
			t.Errorf("Search(%q, %q) = %v; CPython wants %v", tc.pattern, tc.text, got, tc.want)
		}
	}
}

func TestReviewSubNegativeCount(t *testing.T) {
	re, err := CompilePyRegex("a", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, count := range []int{-2, -1} {
		got, err := re.Sub("aba", func(*Match) (string, error) { return "X", nil }, count)
		if err != nil {
			t.Fatalf("Sub count %d: %v", count, err)
		}
		if got != "aba" {
			t.Errorf("Sub count %d = %q; CPython leaves it unchanged", count, got)
		}
	}
}

func TestReviewInt32RepeatLimitIsExplicit(t *testing.T) {
	// CPython 3.11 accepts this count, while both Go engines limit repeat
	// counts to int32; filter validation reports it as unsupported syntax.
	_, err := CompilePyRegex(`a{4294967294}`, false)
	if !errors.Is(err, ErrRepeatTooLarge) || !IsPyRegexError(err) {
		t.Fatalf("CompilePyRegex oversized repeat = %v; want the documented ErrRepeatTooLarge rejection", err)
	}
}

func TestReviewNamedGroupCaptures(t *testing.T) {
	re, err := CompilePyRegex(`(?P<region>香港|日本)\s*(?P<num>\d+)`, false)
	if err != nil {
		t.Fatal(err)
	}
	match, err := re.Match("日本 ０１")
	if err != nil || match == nil {
		t.Fatalf("Match = %v, %v", match, err)
	}
	for name, want := range map[string]string{"region": "日本", "num": "０１"} {
		got, ok := match.NamedGroup(name)
		if !ok || got != want {
			t.Errorf("NamedGroup(%q) = %q, %v; want %q", name, got, ok, want)
		}
	}
}

func TestReviewDollarMatchesBeforeOneFinalNewline(t *testing.T) {
	for _, pattern := range []string{`HK$`, `(?m)HK$`} {
		re, err := CompilePyRegex(pattern, false)
		if err != nil {
			t.Fatalf("CompilePyRegex(%q): %v", pattern, err)
		}
		for _, tc := range []struct {
			text string
			want bool
		}{
			{"HK", true}, {"HK\n", true}, {"HK\r\n", false}, {"HK\nnext", pattern == `(?m)HK$`},
		} {
			if got := re.Search(tc.text); got != tc.want {
				t.Errorf("Search(%q, %q) = %v; CPython wants %v", pattern, tc.text, got, tc.want)
			}
		}
	}
}

func TestReviewBacktrackingMatchTimesOut(t *testing.T) {
	previous := BacktrackTimeout
	BacktrackTimeout = 10 * time.Millisecond
	t.Cleanup(func() { BacktrackTimeout = previous })

	re, err := CompilePyRegex(`^(a+)+(?=b)$`, false)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	_, err = re.Match(strings.Repeat("a", 6000) + "c")
	if !errors.Is(err, ErrMatchTimeout) {
		t.Fatalf("Match error = %v; want ErrMatchTimeout", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("catastrophic match took %s despite %s timeout", elapsed, BacktrackTimeout)
	}
}
