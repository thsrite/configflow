package yamlx

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestSyntaxDiagnosticsAreChinese(t *testing.T) {
	tests := []struct {
		name, source string
		depth        bool
	}{
		{"ordinary", "a: [", false},
		{"depth", strings.Repeat("[", 10001) + strings.Repeat("]", 10001), true},
		{"slash escape", `a: "\/"`, false},
		{"version directive", "%YAML 1.2\n---\na: 1\n", false},
		{"double BOM", "\ufeff\ufeffbom: 1\n🙂:\n- 0x1f\n", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.source))
			var syntax *SyntaxError
			if !errors.Is(err, ErrSyntax) || !errors.As(err, &syntax) || !strings.HasPrefix(err.Error(), msgSyntax) {
				t.Fatalf("missing syntax diagnostic: %v", err)
			}
			if syntax.Line > 0 && !strings.Contains(err.Error(), fmt.Sprintf("（第 %d 行）", syntax.Line)) {
				t.Fatalf("lost available line: %v", err)
			}
			if tc.depth && !strings.Contains(err.Error(), fmt.Sprintf(msgDepthLimit, "10000")) {
				t.Fatalf("untranslated depth limit: %v", err)
			}
		})
	}
	manual := &SyntaxError{Line: 3, Problem: "native detail"}
	if !strings.HasPrefix(manual.Error(), msgSyntax) || !strings.Contains(manual.Error(), "（第 3 行）") {
		t.Fatal(manual)
	}
}
