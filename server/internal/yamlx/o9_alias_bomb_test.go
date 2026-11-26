package yamlx_test

import (
	"strings"
	"testing"
	"time"

	"configflow/internal/yamlx"
)

// O9 round 1: a subscription or rule source can return YAML. Alias
// amplification ("billion laughs") must be rejected by Parse, quickly.
func TestO9ParseRejectsAliasAmplification(t *testing.T) {
	var b strings.Builder
	b.WriteString("a: &a [\"lol\",\"lol\",\"lol\",\"lol\",\"lol\",\"lol\",\"lol\",\"lol\",\"lol\"]\n")
	prev := "a"
	for _, name := range []string{"b", "c", "d", "e", "f", "g", "h", "i"} {
		b.WriteString(name + ": &" + name + " [")
		for i := 0; i < 9; i++ {
			if i > 0 {
				b.WriteString(",")
			}
			b.WriteString("*" + prev)
		}
		b.WriteString("]\n")
		prev = name
	}
	start := time.Now()
	_, err := yamlx.Parse([]byte(b.String()))
	if err == nil {
		t.Fatal("alias amplification accepted")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("rejection took %s", elapsed)
	}
	var out any
	if err := yamlx.Unmarshal([]byte(b.String()), &out); err == nil {
		t.Fatal("Unmarshal accepted alias amplification")
	}
}
