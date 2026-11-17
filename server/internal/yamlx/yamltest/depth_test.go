package yamltest

import (
	"errors"
	"strings"
	"testing"
	"time"

	"configflow/internal/yamlx"
)

func TestN73NativeDepthBoundary(t *testing.T) {
	for _, depth := range []int{1000, 4000, 10000, 10001} {
		source := strings.Repeat("[", depth) + "0" + strings.Repeat("]", depth)
		started := time.Now()
		node, err := yamlx.Parse([]byte(source))
		if depth > 10000 {
			var position *yamlx.SyntaxError
			if err == nil || !errors.As(err, &position) || !strings.Contains(err.Error(), "YAML") || !(errors.Is(err, yamlx.ErrSyntax) || errors.Is(err, yamlx.ErrPolicy)) {
				t.Errorf("depth %d: missing Chinese positioned refusal: %v", depth, err)
			}
		} else {
			if err != nil {
				t.Fatalf("depth %d parse: %v", depth, err)
			}
			if _, err = yamlx.ToJSON(node); err != nil {
				t.Fatalf("depth %d JSON: %v", depth, err)
			}
		}
		t.Logf("depth=%d source_bytes=%d elapsed=%s", depth, len(source), time.Since(started))
	}
}
