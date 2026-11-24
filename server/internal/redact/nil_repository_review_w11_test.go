package redact

import (
	"context"
	"errors"
	"testing"
)

// W11 removed the repository locator: every repository-backed secret source
// must refuse a nil repository rather than return an empty secret set (which
// would let a response through unredacted).
func TestRepositorySecretSourcesRefuseNilRepository(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name string
		call func() error
	}{
		{"AgentRecordCredentials", func() error { _, err := AgentRecordCredentials(ctx, nil, nil); return err }},
		{"ExternalPayloadSecrets", func() error { _, err := ExternalPayloadSecrets(ctx, nil, nil); return err }},
		{"RuleProxyTokens", func() error { _, err := RuleProxyTokens(ctx, nil); return err }},
		{"SanitizeExternalPayload", func() error {
			_, err := SanitizeExternalPayload(ctx, nil, map[string]any{"token": "synthetic"}, nil)
			return err
		}},
		{"ContainsInternalRuleProxyToken", func() error { _, err := ContainsInternalRuleProxyToken(ctx, nil, "synthetic"); return err }},
		{"RedactSecretsInText", func() error { _, err := RedactSecretsInText(ctx, nil, "synthetic"); return err }},
		{"StoredSecretFinder", func() error { _, err := StoredSecretFinder(ctx, nil); return err }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.call(); !errors.Is(err, errNilRepository) {
				t.Fatalf("err = %v, want errNilRepository", err)
			}
		})
	}
}
