package pushdiff

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"

	"configflow/internal/yamlx"
)

func TestReviewN73DListingAliasBomb(t *testing.T) {
	if os.Getenv("CONFIGFLOW_REVIEW_N73D_LISTING_CHILD") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestReviewN73DListingAliasBomb$")
		command.Env = append(os.Environ(), "CONFIGFLOW_REVIEW_N73D_LISTING_CHILD=1")
		start := time.Now()
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("alias guard failed or exceeded 10s: %v\n%s", err, output)
		}
		t.Logf("isolated alias regressions completed in %s", time.Since(start))
		return
	}
	tests := []struct {
		name, source string
		kind         error
	}{
		{"bomb", aliasChain(), yamlx.ErrAliasBudget},
		{"recursive", "a: &a [*a]\n", yamlx.ErrAliasRecursion},
		{"policy", "x: !!bool yes\n", yamlx.ErrPolicy},
		{"syntax", "x: [\n", yamlx.ErrSyntax},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			providers := []byte("proxies: []\n" + tt.source)
			_, err := nodeItems(providers)
			if !errors.Is(err, tt.kind) {
				t.Fatalf("internal reader lost cause: %v", err)
			}
			var position *yamlx.SyntaxError
			if !errors.As(err, &position) || position.Line < 1 {
				t.Fatal("native diagnostic lost")
			}
			summary, details := nodeChange(nil, providers, &detailBudget{left: MaxDetailBytes}, nil)
			if summary.Kind != "size" || summary.Before != nil || summary.After == nil || *summary.After != len(providers) || details != nil {
				t.Fatalf("invalid graph produced itemized contents: %+v", summary)
			}

			data := []byte("payload:\n  - *reference\n" + tt.source)
			entries, err := RuleEntries(data)
			if err != nil || len(entries) != 1 || entries[0] != "*reference" {
				t.Fatalf("lexical reader changed alias text: %v", err)
			}
			summary, details = ruleChange(nil, data, &detailBudget{left: MaxDetailBytes}, nil)
			if summary.Kind != "rules" || summary.After == nil || *summary.After != 1 || details == nil {
				t.Fatal("lexical listing lost literal alias")
			}
		})
	}
}
