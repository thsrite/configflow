package repo

import (
	"context"
	"errors"
	"testing"

	"configflow/internal/model"
)

func TestScopedRecordTransactions(t *testing.T) {
	for _, kind := range []string{"single", "shared", "single-failure", "shared-failure"} {
		t.Run(kind, func(t *testing.T) {
			r, ctx := scRepository(t)
			marker := func(ctx context.Context, id string) string {
				t.Helper()
				value, err := GetConfig(ctx, id)
				if err != nil {
					t.Fatal(err)
				}
				return value.GetObject("mihomo").GetString("marker")
			}
			for _, id := range []string{"default", "other"} {
				if got := marker(ctx, id); got != "" {
					t.Fatal(got)
				}
				if err := r.UpdateConfig(bg, id, func(_ context.Context, config *model.Config) error {
					return config.Profile.SetJSON("mihomo", []byte(`{"marker":"external"}`))
				}); err != nil {
					t.Fatal(err)
				}
			}
			scope := ScopeOf(ctx)
			cached, baseline := scope.cache["default"], scope.baselines["default"]
			stop := errors.New("reject edit")
			edit := func(_ context.Context, config *model.Config) error {
				if err := config.Profile.SetJSON("mihomo", []byte(`{"marker":"edited"}`)); err != nil {
					return err
				}
				if kind == "single-failure" || kind == "shared-failure" {
					return stop
				}
				return nil
			}
			var err error
			if kind == "single" || kind == "single-failure" {
				err = UpdateConfig(ctx, "default", edit)
			} else {
				err = UpdateSharedConfigs(ctx, func(_ context.Context, shared *model.Shared) error {
					return shared.SetJSON("rule_library", []byte(`[]`))
				}, func(ctx context.Context, _ string, _, after *model.Config) error {
					return edit(ctx, after)
				}, nil)
			}
			wantDefault, wantOther := "edited", ""
			if kind == "shared" {
				wantOther = "external"
			}
			if kind == "single-failure" || kind == "shared-failure" {
				if !errors.Is(err, stop) {
					t.Fatal(err)
				}
				wantDefault = ""
				afterBaseline, hasBaseline := scope.baselines["default"]
				if scope.cache["default"] != cached || !hasBaseline || afterBaseline.value != baseline.value {
					t.Fatal("failed transaction invalidated scope")
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if got := marker(ctx, "default"); got != wantDefault {
				t.Fatalf("default: %q want %q", got, wantDefault)
			}
			if got := marker(ctx, "other"); got != wantOther {
				t.Fatalf("other: %q want %q", got, wantOther)
			}
		})
	}
}
