package routers

import (
	"encoding/json"
	"fmt"
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
		if got, err := FakeIPNetwork(input); err == nil || got.IsValid() {
			t.Fatalf("unsafe document used for routing: %s %v", got, err)
		}
	}
	input := "dns: &dns {fake-ip-range: 198.19.0.0/16}\nother: *dns\n"
	if got, err := FakeIPNetwork(input); err != nil || got.String() != "198.19.0.0/16" {
		t.Fatalf("ordinary shared mapping: %s %v", got, err)
	}
}

func FuzzFakeIPYAML(f *testing.F) {
	f.Add("dns: {fake-ip-range: 198.19.0.0/16}")
	f.Add("x: &x [*x]")
	f.Fuzz(func(t *testing.T, text string) {
		network, err := FakeIPNetwork(text)
		if err == nil && !network.IsValid() {
			t.Fatal("invalid desired prefix")
		}
		profile, marshalErr := json.Marshal(struct {
			Mihomo struct {
				Custom string `json:"custom_config"`
			} `json:"mihomo"`
		}{Mihomo: struct {
			Custom string `json:"custom_config"`
		}{Custom: text}})
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		plan, planErr := ComputeDesired(profile, "192.0.2.1", nil, nil)
		if planErr != nil && plan != nil || planErr == nil && plan == nil {
			t.Fatalf("inconsistent plan/error: %#v %v", plan, planErr)
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
			network, err := FakeIPNetwork("dns: {fake-ip-range: 198.19.0.0/16}\nextra: " + nested)
			if err != nil {
				t.Fatal(err)
			}
			got := network.String()
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
