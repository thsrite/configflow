package settings

import (
	"strings"
	"testing"
)

func TestRandomToken(t *testing.T) {
	seen := map[string]bool{}
	for range 50 {
		token, err := randomToken(32)
		if err != nil || len(token) != 32 || strings.Trim(token, tokenAlphabet) != "" {
			t.Fatalf("bad token %q: %v", token, err)
		}
		seen[token] = true
	}
	if len(seen) != 50 {
		t.Error("tokens repeat")
	}
	for _, n := range []int{0, 5} {
		if token, err := randomToken(n); err != nil || len(token) != n {
			t.Errorf("length %d: %q %v", n, token, err)
		}
	}
}
