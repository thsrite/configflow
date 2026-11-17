package routersplit

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"configflow/internal/jsonx"
	"configflow/internal/yamlx"
)

func TestN73BaseYAMLGraphs(t *testing.T) {
	var chain strings.Builder
	chain.WriteString("a0: &a0 [x]\n")
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&chain, "a%d: &a%d [*a%d, *a%d]\n", i, i, i-1, i-1)
	}
	for _, source := range []string{chain.String(), "a: &a [*a]\n"} {
		if _, err := FullTunBaseYAML(source); err == nil {
			t.Fatal("unsafe graph generated")
		}
		config := jsonx.Obj("mihomo", jsonx.Obj("custom_config", source))
		full, err := BaseIsFullTun(config)
		if err != nil || full {
			t.Fatalf("unsafe graph used for TUN shape: %v %v", full, err)
		}
	}
}

func TestN73DisplacedTunAnchorsKeepTheirValues(t *testing.T) {
	for _, source := range []string{"tun: &n null\nkeep: *n\n", "tun: {dns-hijack: &h [udp://any:53]}\nkeep: *h\n"} {
		out, err := FullTunBaseYAML(source)
		if err != nil {
			t.Fatal(err)
		}
		original, err := yamlx.Parse([]byte(source))
		if err != nil {
			t.Fatal(err)
		}
		generated, err := yamlx.Parse([]byte(out))
		if err != nil {
			t.Fatal(err)
		}
		if !yamlx.Equal(yamlx.Get(original, "keep"), yamlx.Get(generated, "keep")) {
			t.Fatal("shared value changed")
		}

	}
}

func FuzzRouterSplitYAML(f *testing.F) {
	f.Add("tun: {enable: true}")
	f.Add("tun: &t {include-interface: *t}")
	f.Fuzz(func(t *testing.T, text string) {
		out, err := FullTunBaseYAML(text)
		if err == nil && !sameYAML(out, out) {
			t.Fatal("invalid generated YAML")
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
			source := "extra: " + nested
			out, err := FullTunBaseYAML(source)
			if depth <= 10000 {
				if err != nil {
					t.Fatal(err)
				}
				if _, err = yamlx.Parse([]byte(out)); err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("over-depth generated")
			}
			config := jsonx.Obj("mihomo", jsonx.Obj("custom_config", source))
			_, _ = BaseIsFullTun(config)
			t.Logf("depth=%d bytes=%d elapsed=%s", depth, len(nested), time.Since(started))
		})
	}
}
