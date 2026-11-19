package redact

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestDiagnosticTextSmallByteBudgetsDoNotPanic(t *testing.T) {
	for limit := 1; limit < 128; limit++ {
		got := DiagnosticText(strings.Repeat("原始诊断", 100), nil, limit)
		if len(got) > limit || !utf8.ValidString(got) {
			t.Fatalf("limit=%d bytes=%d valid=%v", limit, len(got), utf8.ValidString(got))
		}
	}
}
