package diag

import (
	"strings"
	"testing"
)

// TestHideBlockSecretsAfterUnicodeSeparator checks that a YAML block key that
// follows U+0085/U+2028/U+2029 inside an editor line is still hidden: lines
// break like the web editor since O17, and redaction must not get narrower
// than with Python's splitlines.
func TestHideBlockSecretsAfterUnicodeSeparator(t *testing.T) {
	tests := []struct {
		name, text, want string
	}{
		{"plain", "password: |\n  s3cr3t-value\nnext: 1\n", "password: [REDACTED]\nnext: 1\n"},
		{"after U+2028", "log: x\u2028password: |\n  s3cr3t-value\nnext: 1\n", "log: x\u2028password: [REDACTED]\nnext: 1\n"},
		{"after U+0085", "a\u0085  token: >-\n    s3cr3t-value\nb\n", "a\u0085  token: [REDACTED]\nb\n"},
		{"after U+2029", "a: b\u2029secret: |\n s3cr3t-value\n", "a: b\u2029secret: [REDACTED]\n"},
		{"separator inside the comment", "token: | # comment\u0085 x\nnext", "token: [REDACTED]\nnext"},
		{"second of two separators", "a\u2028b\u2029password: |\n  s3cr3t-value\n", "a\u2028b\u2029password: [REDACTED]\n"},
		{"separator after key", "password: |\u2028  s3cr3t-value\n", "password: |\u2028  s3cr3t-value\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hideBlockSecrets(tt.text)
			if got != tt.want {
				t.Fatalf("hideBlockSecrets(%q) = %q, want %q", tt.text, got, tt.want)
			}
		})
	}
	got := DiagnosticText("log: x\u2028password: |\n  s3cr3t-value\n", nil, 0)
	if strings.Contains(got, "s3cr3t-value") {
		t.Fatalf("DiagnosticText leaked the block value: %q", got)
	}
}
