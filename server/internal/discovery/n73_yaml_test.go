package discovery

import (
	"fmt"
	"strings"
	"testing"
)

func TestN73CustomPortYAMLGraphs(t *testing.T) {
	var chain strings.Builder
	chain.WriteString("a0: &a0 [x]\n")
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&chain, "a%d: &a%d [*a%d, *a%d]\n", i, i, i-1, i-1)
	}
	chain.WriteString("mixed-port: 7890\n")
	for _, text := range []string{chain.String(), "a: &a [*a]\nmixed-port: 7890\n", "mixed-port: 7890\nmixed-port: 7891\n", "mixed-port: !!str [x]\n"} {
		ports, err := customPorts(text)
		if err != nil || len(ports) != 0 {
			t.Fatalf("invalid document used for ports: %v %v", ports, err)
		}
	}
	if _, err := customPorts("mixed-port: !custom not-an-integer\n"); err == nil || !strings.Contains(err.Error(), "必须是整数端口") {
		t.Fatalf("tagged invalid port hidden: %v", err)
	}
	ports, err := customPorts("base: &b {mixed-port: 0o17322}\n<<: *b\nlisteners: [{name: Synthetic, port: 7891}]\n")
	if err != nil || !ports[7890] || !ports[7891] || len(ports) != 2 {
		t.Fatalf("merge/octal/listener ports: %v %v", ports, err)
	}
}

func FuzzCustomPortYAML(f *testing.F) {
	f.Add("mixed-port: 7890")
	f.Add("a: &a [*a]")
	f.Fuzz(func(t *testing.T, text string) { _, _ = customPorts(text) })
}
