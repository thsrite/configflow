package routers

import (
	"fmt"
	"net/netip"
	"strings"
	"testing"

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
