package repo_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"configflow/internal/model"
	"configflow/internal/repo"
)

func TestConfigFromMatchesSnapshot(t *testing.T) {
	r := recordRepository(t)
	hydrateFixture(t, r)
	ctx := context.Background()
	if err := r.UpdateSystem(ctx, func(_ context.Context, system *model.System) error {
		system.SystemConfig.ConfigToken = "synthetic-config-token"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	expected, err := r.Config(ctx, "default", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.View(ctx, func(document *model.Document) error {
		actual, err := repo.ConfigFrom(document, "default", nil)
		if err != nil {
			return err
		}
		left, err := model.Marshal(expected)
		if err != nil {
			return err
		}
		right, err := model.Marshal(actual)
		if err != nil {
			return err
		}
		if !bytes.Equal(left, right) || actual.HasAgents || len(actual.Agents) != 0 || len(actual.RuleLibrary) != 1 || actual.SystemConfig.ConfigToken != "synthetic-config-token" || len(actual.Profile.RuleConfigs) != 1 || actual.Profile.RuleConfigs[0].Content == nil || *actual.Profile.RuleConfigs[0].Content == "" {
			t.Fatalf("snapshot resolution differs or skipped hydration: %s", right)
		}
		if _, err := repo.ConfigFrom(document, "missing", nil); !errors.Is(err, repo.ErrNotFound) {
			t.Fatalf("unknown profile: %v", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
