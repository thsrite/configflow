package version_test

import (
	"encoding/json"
	"math/big"
	"strings"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/agents/monitor"
	"configflow/internal/agents/rulesync"
	"configflow/internal/gateway"
	"configflow/internal/taskstatus"
	"configflow/internal/version"
)

// legacyCompareAgent is the arbitrary-precision comparison CompareAgent
// replaced; it stays here only to prove both agree on real versions.
func legacyCompareAgent(a, b string) int {
	number := func(component string) (*big.Int, bool) {
		component = strings.TrimSpace(component)
		if component == "" {
			return nil, false
		}
		raw, _ := json.Marshal(component)
		n, err := taskstatus.Count(raw)
		return n, err == nil
	}
	v1 := strings.Split(strings.SplitN(a, "-", 2)[0], ".")
	v2 := strings.Split(strings.SplitN(b, "-", 2)[0], ".")
	for len(v1) < len(v2) {
		v1 = append(v1, "0")
	}
	for len(v2) < len(v1) {
		v2 = append(v2, "0")
	}
	for i := range v1 {
		n1, ok1 := number(v1[i])
		n2, ok2 := number(v2[i])
		if ok1 && ok2 {
			if c := n1.Cmp(n2); c != 0 {
				return c
			}
			continue
		}
		if c := strings.Compare(v1[i], v2[i]); c != 0 {
			return c
		}
	}
	return 0
}

// legacyParseRelease is the arbitrary-precision parser ParseRelease replaced.
func legacyParseRelease(text string) []*big.Int {
	text = strings.TrimLeft(strings.TrimSpace(text), "vV")
	var parts []*big.Int
	for _, part := range strings.Split(text, ".") {
		n, ok := new(big.Int).SetString(part, 10)
		if !ok {
			return nil
		}
		parts = append(parts, n)
	}
	return parts
}

func legacyCompareRelease(a, b []*big.Int) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if c := a[i].Cmp(b[i]); c != 0 {
			return c
		}
	}
	return len(a) - len(b)
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	}
	return 0
}

// realVersions are the versions ConfigFlow compares: every Agent release in
// the changelog, the minimum versions features require, the latest Agent and
// release versions of the shapes Sub-Store and Mihomo report, plus empty and
// non-numeric text.
func realVersions() []string {
	out := []string{
		agents.LatestAgentVersion, rulesync.MinAgentVersion, monitor.RuleUpdatePathsMinAgentVersion,
		gateway.StandaloneForwardingMinAgentVersion, "1.1.0-go", "1.0.0", "1.2", "1.2-go", "1.10.0", "1.9.9",
		"2.0.0", "2.39.0", "2.39.9", "2.40.0", "2.40.5", "2.41.0", "2.42.0", "2.42.2", "2.42.3", "2.42.10",
		"2.43.0", "2.43.1", "2.99.0", "3.0.0", "9.9.9", "9.9.9-go", "V2.43.1", " v2.43.1\n", "v1.19.1",
		"v1.19.32", "v1.2.3", "v26.0.0", "1.0.0-rc.1", "2.0.0-beta.2", "", "dev", "unknown", "1..2", "1.",
		"1.x", "01.02", "0.0", "0",
	}
	for _, entry := range agents.Changelog() {
		out = append(out, entry.Version)
	}
	return out
}

func TestCompareAgentMatchesLegacyOnRealVersions(t *testing.T) {
	versions := realVersions()
	for _, a := range versions {
		for _, b := range versions {
			if got, want := version.CompareAgent(a, b), sign(legacyCompareAgent(a, b)); got != want {
				t.Errorf("CompareAgent(%q, %q) = %d, legacy %d", a, b, got, want)
			}
		}
	}
}

func TestReleaseMatchesLegacyOnRealVersions(t *testing.T) {
	versions := realVersions()
	for _, a := range versions {
		for _, b := range versions {
			x, y := legacyParseRelease(a), legacyParseRelease(b)
			newer := x != nil && (y == nil || legacyCompareRelease(x, y) > 0)
			same := (x == nil) == (y == nil) && legacyCompareRelease(x, y) == 0
			if got := version.NewerRelease(a, b); got != newer {
				t.Errorf("NewerRelease(%q, %q) = %t, legacy %t", a, b, got, newer)
			}
			if got := version.SameRelease(a, b); got != same {
				t.Errorf("SameRelease(%q, %q) = %t, legacy %t", a, b, got, same)
			}
		}
	}
}

// Components that only Python's int() accepted (signs, underscores, inner
// spaces) are no longer numbers: Agent versions compare them as text and
// release versions are invalid. Agent components of more than 64 bits still
// compare as numbers; release components that overflow are invalid.
func TestNonDecimalComponents(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want int
	}{
		{"padded numbers", "1.2", "1.2.0.0", 0},
		{"numeric beats text order", "1.10", "1.9", 1},
		{"text component", "1.alpha", "1.beta", -1},
		{"empty component", "1.", "1.0", -1},
		{"space is text", "1. 02 ", "1.2", -1},
		{"underscore is text", "1.2_0", "1.20", 1},
		{"sign is text", "1.+2", "1.2", -1},
		{"long number beats short", strings.Repeat("1", 30), "2", 1},
		{"long numbers compare as numbers", "9" + strings.Repeat("0", 25), "1" + strings.Repeat("0", 26), -1},
		{"long number with leading zeros", "000" + strings.Repeat("7", 25), strings.Repeat("7", 25), 0},
		{"update check on a huge current version", "1" + strings.Repeat("0", 25) + ".0", "2.0", 1},
		{"suffix ignored", "1.8.61-go", "1.8.61", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := version.CompareAgent(tt.a, tt.b); got != tt.want {
				t.Fatalf("CompareAgent(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
	for _, text := range []string{"+1.2", "1.-2", "1_0.2", strings.Repeat("9", 21) + ".1", "1. 2", ""} {
		if version.ParseRelease(text) != nil {
			t.Errorf("ParseRelease(%q) accepted", text)
		}
	}
	if got := version.ParseRelease(" V18446744073709551615.0 "); len(got) != 2 || got[0] != 1<<64-1 {
		t.Errorf("ParseRelease max uint64 = %v", got)
	}
	if !version.NewerRelease("1.0.0", "1.0") || version.SameRelease("1.0", "1.0.0") || !version.SameRelease("v2.43.1", "2.43.1") {
		t.Error("release length rule changed")
	}
}

func FuzzCompareAgent(f *testing.F) {
	for _, s := range []string{"1.2.3", "1.2-go", "", "1.٣", "1..2", "1.0_alpha"} {
		f.Add(s, "1.0")
	}
	f.Fuzz(func(t *testing.T, a, b string) {
		ab, ba := version.CompareAgent(a, b), version.CompareAgent(b, a)
		if ab < -1 || ab > 1 || ab != -ba || version.CompareAgent(a, a) != 0 {
			t.Fatalf("invalid comparison %q %q: %d/%d", a, b, ab, ba)
		}
	})
}
