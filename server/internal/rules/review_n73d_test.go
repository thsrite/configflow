package rules

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"configflow/internal/yamlx"
)

func TestReviewN73DMatcherAliasBomb(t *testing.T) {
	if os.Getenv("CONFIGFLOW_REVIEW_N73D_MATCHER_CHILD") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestReviewN73DMatcherAliasBomb$")
		command.Env = append(os.Environ(), "CONFIGFLOW_REVIEW_N73D_MATCHER_CHILD=1")
		start := time.Now()
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("alias guard failed or exceeded 10s: %v\n%s", err, output)
		}
		t.Logf("isolated alias regressions completed in %s", time.Since(start))
		return
	}
	var chain strings.Builder
	chain.WriteString("l0: &l0 [entry]\n")
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&chain, "l%d: &l%d [*l%d, *l%d]\n", i, i, i-1, i-1)
	}
	chain.WriteString("payload: *l40\n")
	tests := []struct {
		name, source string
		kind         error
	}{
		{"bomb", chain.String(), yamlx.ErrAliasBudget},
		{"recursive", "payload: &p [*p]\n", yamlx.ErrAliasRecursion},
		{"policy", "payload: []\nx: !!bool yes", yamlx.ErrPolicy},
		{"syntax", "payload: [", yamlx.ErrSyntax},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entries, err := yamlPayloadLines(tt.source)
			if len(entries) != 0 || !errors.Is(err, tt.kind) {
				t.Fatalf("internal reader lost cause: %v", err)
			}
			var position *yamlx.SyntaxError
			if !errors.As(err, &position) || position.Line < 1 {
				t.Fatal("native diagnostic lost")
			}
			if matcherFor(tt.source, "classical", "format", "yaml").Match("example.com") != nil {
				t.Fatal("invalid graph matched")
			}
		})
	}
}
