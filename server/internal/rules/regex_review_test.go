package rules

import (
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/dlclark/regexp2"
)

// Engine-native anchors and boundaries are intentional: the backend uses the
// same Go regex libraries as Mihomo rather than reproducing CPython's edges.
func TestNativeRegexAnchorsAndBoundaries(t *testing.T) {
	tests := []struct {
		pattern   string
		backtrack bool
	}{
		{`a\b`, false}, {`a\B`, false}, {`\ba`, false}, {`\B`, false},
		{`(?i)\b(?:hk|jp|sg)\b`, false}, {`\bUS\b`, false},
		{`HK$`, false}, {`(?m)HK$`, false},
		{`(?=a)a\b`, true}, {`(?=a)a\B`, true}, {`(?=)\B`, true},
		{`(?=HK)HK$`, true}, {`(?m)(?=HK)HK$`, true},
	}
	texts := []string{"", "a", "a\u0301", "\u0301a", "a\u203f", "a_", "a界", "HK", "HK\n", "HK\r\n", "HK\nnext", "香港HK", "🇭🇰 HK 01", "美国 US 2x"}
	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			re, err := CompilePyRegex(tt.pattern, false)
			if err != nil {
				t.Fatal(err)
			}
			if (re.re2 == nil) != tt.backtrack {
				t.Fatalf("backtracking = %v, want %v", re.re2 == nil, tt.backtrack)
			}
			nativeBT := regexp2.MustCompile(tt.pattern, regexp2.None)
			var nativeRE2 *regexp.Regexp
			if !tt.backtrack {
				nativeRE2 = regexp.MustCompile(tt.pattern)
			}
			for _, text := range texts {
				want, err := nativeBT.MatchString(text)
				if err != nil {
					t.Fatal(err)
				}
				if nativeRE2 != nil {
					want = nativeRE2.MatchString(text)
				}
				if got := re.Search(text); got != want {
					t.Errorf("Search(%q) = %v, native engine = %v", text, got, want)
				}
				nativeMatch, err := nativeBT.FindStringMatch(text)
				if err != nil {
					t.Fatal(err)
				}
				match, err := re.Match(text)
				if err != nil || (match != nil) != (nativeMatch != nil) {
					t.Errorf("Match(%q) = %v, %v; native match = %v", text, match, err, nativeMatch)
				}
			}
		})
	}
}

func TestSubRejectsNegativeCount(t *testing.T) {
	re, err := CompilePyRegex("a", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, count := range []int{-2, -1} {
		_, err := re.Sub("aba", func(*Match) (string, error) {
			t.Fatal("callback called for invalid count")
			return "", nil
		}, count)
		if !errors.Is(err, ErrNegativeSubCount) {
			t.Errorf("Sub count %d error = %v, want ErrNegativeSubCount", count, err)
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
