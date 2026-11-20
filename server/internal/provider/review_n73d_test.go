package provider_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"configflow/internal/convert"
	"configflow/internal/deploy"
	"configflow/internal/provider"
	"configflow/internal/yamlx"
)

func TestReviewN73DProviderReaderCauses(t *testing.T) {
	t.Cleanup(provider.SetLocations(deploy.Locations{}))
	var chain strings.Builder
	chain.WriteString("l0: &l0 [entry]\n")
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&chain, "l%d: &l%d [*l%d, *l%d]\n", i, i, i-1, i-1)
	}
	tests := []struct {
		name, source string
		kind         error
	}{
		{"syntax", "proxies: [", yamlx.ErrSyntax},
		{"duplicate", "proxies: []\nproxies: []", yamlx.ErrDuplicateKey},
		{"tag shape", "proxies: !!str []", yamlx.ErrPolicy},
		{"merge", "proxies: []\n<<: 7", yamlx.ErrPolicy},
		{"scalar", "proxies: []\nx: !!bool yes", yamlx.ErrPolicy},
		{"recursive", "proxies: []\nx: &x [*x]", yamlx.ErrAliasRecursion},
		{"budget", "proxies: []\n" + chain.String(), yamlx.ErrAliasBudget},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, original := yamlx.Parse([]byte(tt.source))
			proxies, err := provider.ParseNodes(tt.source, "Synthetic", "./providers/example.yaml")
			if proxies != nil || !errors.Is(err, tt.kind) {
				t.Fatalf("provider lost reader cause: %v", err)
			}
			var position, want *yamlx.SyntaxError
			if !errors.As(original, &want) || !errors.As(err, &position) || position != want && (position.Line != want.Line || position.Column != want.Column || position.Error() != want.Error()) {
				t.Fatalf("native diagnostic changed: %v", err)
			}
			var dialer *convert.DialerReferenceError
			if !errors.As(err, &dialer) {
				t.Fatal("provider validation class changed")
			}
			if !strings.Contains(err.Error(), "Provider YAML 内容无效") || !strings.Contains(err.Error(), "Synthetic") {
				t.Fatal("provider context lost")
			}
		})
	}
}
