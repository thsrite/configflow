package repo

import (
	"configflow/internal/model"
	"context"
	"errors"
	"testing"
)

func TestCommitConfigReturnsCommittedSnapshotAndRollsBack(t *testing.T) {
	r := newRepo(t)
	err := r.UpdateShared(bg, func(_ context.Context, s *model.Shared) error {
		s.RuleLibrary = append(s.RuleLibrary, model.RuleLibraryItem{ID: "native-rule", Name: "Synthetic"})
		return nil
	})
	noError(t, err)
	saved, revision, err := r.CommitConfig(bg, "default", func(ctx context.Context, c *model.Config) error {
		if len(c.RuleLibrary) == 0 || c.RuleLibrary[0].ID != "native-rule" {
			t.Fatal("shared resources missing inside transaction")
		}
		return c.Profile.SetJSON("mihomo", []byte(`{"custom_config":"mode: rule"}`))
	})
	noError(t, err)
	profile, err := r.Profile(bg, "default")
	noError(t, err)
	current, err := r.ConfigRevision(bg, "default")
	noError(t, err)
	if saved.Revision != profile.Revision || saved.Mihomo.CustomConfig != profile.Mihomo.CustomConfig || current != revision {
		t.Fatal("returned another generation")
	}
	stop := errors.New("synthetic rollback")
	_, _, err = r.CommitConfig(bg, "default", func(ctx context.Context, c *model.Config) error {
		c.Profile.Mihomo.CustomConfig = "mode: global"
		return stop
	})
	if !errors.Is(err, stop) {
		t.Fatal(err)
	}
	after, err := r.Profile(bg, "default")
	noError(t, err)
	if after.Revision != saved.Revision || after.Mihomo.CustomConfig != saved.Mihomo.CustomConfig {
		t.Fatal("failed edit committed")
	}

	// Verify CommitConfig works safely inside a request Scope without breaking rollback.
	scoped := WithScope(WithRepository(bg, r))
	if _, err := GetConfig(scoped, "default"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.CommitConfig(scoped, "default", func(_ context.Context, c *model.Config) error {
		c.Profile.Mihomo.CustomConfig = "mode: direct"
		return stop
	}); !errors.Is(err, stop) {
		t.Fatal(err)
	}
	still, err := r.Profile(bg, "default")
	noError(t, err)
	if still.Mihomo.CustomConfig != saved.Mihomo.CustomConfig {
		t.Fatal("scoped rollback mutated stored profile")
	}
}
