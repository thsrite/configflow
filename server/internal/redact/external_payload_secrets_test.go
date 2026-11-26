package redact

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/store"
)

func TestExternalPayloadSecretsUsesWorkingGeneration(t *testing.T) {
	ctx := t.Context()
	r, err := repo.OpenDefault(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	if err := r.UpdateSystem(ctx, func(_ context.Context, system *model.System) error {
		system.SystemConfig.RuleProxyToken = "synthetic-rule-original"
		system.SystemConfig.RetiredRuleProxyTokens = []string{"synthetic-rule-retired"}
		system.Agents = []model.Agent{{ID: "a", Name: "A", ProfileID: "default", Token: "synthetic-agent-original"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	settings := map[string]any{
		"rule_proxy_token":  "synthetic-rule-retired",
		"rule_fetch_proxy":  "synthetic-settings-secret",
		"ordinary_metadata": "synthetic-public-value",
	}
	check := func(ctx context.Context, rule, agent string) {
		t.Helper()
		got, err := ExternalPayloadSecrets(ctx, r, settings)
		if err != nil {
			t.Fatal(err)
		}
		want := NewSecrets([]string{rule, agent, "synthetic-rule-retired", "synthetic-settings-secret"})
		if !got.Equal(want) {
			t.Fatal("external payload secrets differ from the expected generation")
		}
	}

	check(ctx, "synthetic-rule-original", "synthetic-agent-original")
	abort := errors.New("abort test transaction")
	if err := r.Update(ctx, func(ctx context.Context, document *model.Document) error {
		document.System.SystemConfig.RuleProxyToken = "synthetic-rule-working"
		document.System.Agents[0].Token = "synthetic-agent-working"
		check(ctx, "synthetic-rule-working", "synthetic-agent-working")
		return abort
	}); !errors.Is(err, abort) {
		t.Fatalf("transaction error = %v, want %v", err, abort)
	}
	check(ctx, "synthetic-rule-original", "synthetic-agent-original")
}

func TestExternalPayloadSecretsPreservesRepositoryError(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	r, err := repo.OpenDefault(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	r.Close()
	if err := os.Remove(filepath.Join(dir, repo.DatabaseFilename)); err != nil {
		t.Fatal(err)
	}

	_, want := r.RuleProxyTokensForSanitization(ctx)
	got, err := ExternalPayloadSecrets(ctx, r, nil)
	if want == nil || !errors.Is(err, store.ErrStorage) || err.Error() != want.Error() || got != nil {
		t.Fatalf("external payload secrets = %v, %v; want nil, %v", got, err, want)
	}
}
