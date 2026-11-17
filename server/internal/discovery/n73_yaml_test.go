package discovery

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"configflow/internal/yamlx/yamltest"
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
		if err == nil || len(ports) != 0 {
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

// Plain nesting has no alias limit; test the native limit without extrapolating
// Python's recursion ceiling. Durations are logged, not compared to wall time.
func TestN73PlainDepthBoundary(t *testing.T) {
	for _, depth := range []int{1000, 4000, 10000, 10001} {
		t.Run(fmt.Sprint(depth), func(t *testing.T) {
			nested := strings.Repeat("[", depth) + "0" + strings.Repeat("]", depth)
			started := time.Now()
			ports, err := customPorts("mixed-port: 7890\nextra: " + nested)
			if depth <= 10000 && (err != nil || !ports[7890] || len(ports) != 1) {
				t.Fatalf("ports: %v %v", ports, err)
			}
			if depth > 10000 && err != nil {
				t.Fatalf("ordinary syntax must retain empty fallback: %v", err)
			}
			t.Logf("depth=%d bytes=%d elapsed=%s", depth, len(nested), time.Since(started))
		})
	}
}

// Port discovery has a string-key schema. Opaque keys are ignored while the
// known ports are retained, including inside listener metadata.
func TestN73CustomPortsIgnoreCompositeMetadata(t *testing.T) {
	source := "? [a,b]\n: ignored\nmixed-port: 7890\nlisteners: [{? [a,b]: ignored, port: 7891}]\n"
	ports, err := customPorts(source)
	if err != nil || len(ports) != 2 || !ports[7890] || !ports[7891] {
		t.Fatalf("known port lost: %v %v", ports, err)
	}
}

func TestN73CustomPortReaderMigrationGate(t *testing.T) {
	sources := []string{"mixed-port: 7890", "port: 7891\nlisteners: [{name: Synthetic, port: 7892}]", "base: &b {mixed-port: 0o17322}\n<<: *b\n", "? [a,b]\n: ignored\nmixed-port: 7890", "mixed-port: !custom bad", "mixed-port: 1\nmixed-port: 2", "["}
	yamltest.Check(t, "discovery/customPorts", sources)
}
