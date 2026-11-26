package mosdns

import (
	"testing"

	"configflow/internal/model"
	"configflow/internal/repo"
)

func TestRuleProxyCredentialPolicy(t *testing.T) {
	cfg := snapshot{SystemConfig: model.SystemConfig{RuleProxyToken: "synthetic-current", ConfigToken: "synthetic-config"}}
	tokens := repo.TokenSet{"synthetic-current": {}, "synthetic-retired": {}}
	for _, tt := range []struct {
		token string
		want  bool
	}{
		{"synthetic-current", true}, {"synthetic-config", true},
		{"synthetic-retired", false}, {"synthetic-currenx", false},
		{"synthetic-current-extra", false}, {"", false},
	} {
		t.Run(tt.token, func(t *testing.T) {
			got, err := authorized(cfg, AuthRequest{Token: tt.token}, tokens)
			if err != nil || got != tt.want {
				t.Fatalf("credential accepted=%v error=%v; want %v", got, err, tt.want)
			}
		})
	}
}
