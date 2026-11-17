package routers

import (
	"fmt"
	"net/netip"
	"strings"
	"testing"
	"time"

	"configflow/internal/yamlx"
)

func TestN73FakeIPYAMLGraphs(t *testing.T) {
	var chain strings.Builder
	chain.WriteString("a0: &a0 [x]\n")
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&chain, "a%d: &a%d [*a%d, *a%d]\n", i, i, i-1, i-1)
	}
	chain.WriteString("dns: {fake-ip-range: 198.19.0.0/16}\n")
	for _, input := range []string{chain.String(), "a: &a [*a]\ndns: {fake-ip-range: 198.19.0.0/16}\n"} {
		if _, err := yamlx.Parse([]byte(input)); err == nil {
			t.Fatal("unsafe graph accepted by facade")
		}
		if got := FakeIPNetwork(input).String(); got != netip.MustParsePrefix(DefaultFakeIPRange).Masked().String() {
			t.Fatalf("invalid document used for routing: %s", got)
		}
	}
	input := "dns: &dns {fake-ip-range: 198.19.0.0/16}\nother: *dns\n"
	if got := FakeIPNetwork(input).String(); got != "198.19.0.0/16" {
		t.Fatalf("ordinary shared mapping: %s", got)
	}
}

func FuzzFakeIPYAML(f *testing.F) {
	f.Add("dns: {fake-ip-range: 198.19.0.0/16}")
	f.Add("x: &x [*x]")
	f.Fuzz(func(t *testing.T, text string) {
		if !FakeIPNetwork(text).IsValid() {
			t.Fatal("invalid desired prefix")
		}
	})
}

// Plain nesting has no alias limit; test the native limit without extrapolating
// Python's recursion ceiling. Durations are logged, not compared to wall time.
func TestN73PlainDepthBoundary(t *testing.T) {
	for _, depth := range []int{1000, 4000, 10000, 10001} {
		t.Run(fmt.Sprint(depth), func(t *testing.T) {
			nested := strings.Repeat("[", depth) + "0" + strings.Repeat("]", depth)
			started := time.Now()
			got := FakeIPNetwork("dns: {fake-ip-range: 198.19.0.0/16}\nextra: " + nested).String()
			want := "198.19.0.0/16"
			if depth > 10000 {
				want = "198.18.0.0/16"
			}
			if got != want {
				t.Fatalf("prefix %s want %s", got, want)
			}
			t.Logf("depth=%d bytes=%d elapsed=%s", depth, len(nested), time.Since(started))
		})
	}
}
