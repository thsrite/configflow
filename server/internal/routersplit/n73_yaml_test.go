package routersplit

import (
	"fmt"
	"strings"
	"testing"

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
