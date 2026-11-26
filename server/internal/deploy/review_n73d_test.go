package deploy

import (
	"errors"
	"strings"
	"testing"

	"configflow/internal/yamlx"
)

func TestReviewN73DSourceErrorUnknownCoordinates(t *testing.T) {
	tests := []struct{ name, source string }{
		{"no position", "\x00"},
		{"line only", "payload: ["},
		{"line and column", "payload: !!str []"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, cause := yamlx.Parse([]byte(tt.source))
			var position *yamlx.SyntaxError
			if !errors.As(cause, &position) {
				t.Fatal("missing native control error")
			}
			err := SourceError("Synthetic YAML", tt.source, Position{}, cause, "Synthetic source")
			var got *yamlx.SyntaxError
			if !errors.As(err, &got) || got != position {
				t.Fatal("source error lost native cause")
			}
			message := err.Error()
			if strings.Contains(message, "第 0 行") || strings.Contains(message, "第 0 列") {
				t.Fatalf("invented zero coordinate: %s", message)
			}
			if position.Line == 0 && strings.Contains(message, "位置：") {
				t.Fatalf("invented location: %s", message)
			}
			if position.Column == 0 && strings.Contains(message, "列") {
				t.Fatalf("invented column: %s", message)
			}
		})
	}
}
