package gateway

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/yamlx"
)

func TestReviewN73DGatewayReaderCauses(t *testing.T) {
	var chain strings.Builder
	chain.WriteString("l0: &l0 [entry]\n")
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&chain, "l%d: &l%d [*l%d, *l%d]\n", i, i, i-1, i-1)
	}
	tests := []struct {
		name, source string
		kind         error
	}{
		{"syntax", "x: [", yamlx.ErrSyntax},
		{"duplicate", "x: 1\nx: 2", yamlx.ErrDuplicateKey},
		{"tag shape", "x: !!str []", yamlx.ErrPolicy},
		{"merge", "<<: 7", yamlx.ErrPolicy},
		{"scalar", "x: !!bool yes", yamlx.ErrPolicy},
		{"recursive", "x: &x [*x]", yamlx.ErrAliasRecursion},
		{"budget", chain.String(), yamlx.ErrAliasBudget},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := "payload: ['IP-CIDR,192.0.2.0/24']\n" + tt.source
			_, original := yamlx.Parse([]byte(source))
			config := NewFixtureConfig(jsonx.Obj("mihomo", jsonx.Obj("custom_config", source)))
			doc, baseErr := config.BaseDocument()
			prefixes, payloadErr := ExtractCIDRs(source, "classical")
			if doc != nil || prefixes != nil {
				t.Fatal("invalid graph produced an output")
			}
			for _, boundary := range []struct {
				err     error
				message string
			}{{baseErr, msgBaseYAMLInvalid}, {payloadErr, msgYAMLSyntax}} {
				if !errors.Is(boundary.err, tt.kind) {
					t.Fatalf("gateway lost reader cause: %v", boundary.err)
				}
				var position, want *yamlx.SyntaxError
				if !errors.As(original, &want) || !errors.As(boundary.err, &position) || position.Line != want.Line || position.Column != want.Column || position.Error() != want.Error() {
					t.Fatalf("native diagnostic changed: %v", boundary.err)
				}
				var gatewayErr *Error
				if !errors.As(boundary.err, &gatewayErr) || boundary.err.Error() != boundary.message {
					t.Fatalf("gateway visible contract changed: %v", boundary.err)
				}
			}
		})
	}
}
