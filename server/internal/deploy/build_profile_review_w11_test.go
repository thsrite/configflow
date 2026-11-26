package deploy

import (
	"context"
	"strings"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/model"
	"configflow/internal/refresh"
	"configflow/internal/repo"
)

func openRepoWithCustomDefaultProfile(t *testing.T, defaultID string) *repo.Repository {
	t.Helper()
	r, err := repo.OpenDefault(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	ctx := context.Background()
	if _, err := r.CreateProfileRecord(ctx, repo.ProfileCreation{ID: defaultID, Name: "Custom Default"}, ""); err != nil {
		t.Fatal(err)
	}
	if err := r.UpdateSystem(ctx, func(_ context.Context, s *model.System) error {
		s.SystemConfig.DefaultProfileID = &defaultID
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	got, err := r.DefaultProfileID(ctx)
	if err != nil || got != defaultID {
		t.Fatalf("DefaultProfileID = %q, %v; want %q", got, err, defaultID)
	}
	return r
}

// W11 regression: Build resolves the default profile of a target without a
// profile_id from a repository. The panel and rule-sync callers pass one
// explicitly or through their refresh Service; neither may be dropped.
func TestBuildInputResolvesDefaultProfileRepository(t *testing.T) {
	r := openRepoWithCustomDefaultProfile(t, "custom-default")
	tests := []struct {
		name string
		in   BuildInput
		want *repo.Repository
	}{
		{"explicit", BuildInput{Repo: r}, r},
		{"refresh", BuildInput{Refresh: refresh.New(refresh.Options{Repo: r})}, r},
		{"refresh-nil-repo", BuildInput{Refresh: refresh.New(refresh.Options{})}, nil},
		{"none", BuildInput{}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.in.repository(); got != tt.want {
				t.Fatalf("repository = %p, want %p", got, tt.want)
			}
		})
	}
	want := "custom-default"
	got, err := resolvedBuildProfile(context.Background(), BuildInput{Refresh: refresh.New(refresh.Options{Repo: r})}.repository(), nil, Target{})
	if err != nil || got != want {
		t.Fatalf("profile = %q, %v; want %q", got, err, want)
	}
	if _, err := resolvedBuildProfile(context.Background(), nil, nil, Target{}); err == nil {
		t.Fatal("missing repository must fail closed")
	}
}

func TestBuildResolvesDefaultProfileEndToEndAndFailsClosed(t *testing.T) {
	r := openRepoWithCustomDefaultProfile(t, "custom-default")
	ctx := context.Background()
	src := testSource(t, map[string]any{})
	target := Target{ID: "agent-default-profile", ServiceType: "mihomo"}

	t.Run("explicit repo", func(t *testing.T) {
		b, err := Build(ctx, BuildInput{
			Repo:          r,
			Target:        target,
			Source:        src,
			ConfigContent: "mode: rule\n",
			DeploymentID:  "explicit-repo",
		})
		if err != nil {
			t.Fatal(err)
		}
		if got := b.Manifest().ProfileID; got != "custom-default" {
			t.Fatalf("Manifest.ProfileID = %q, want %q", got, "custom-default")
		}
	})

	t.Run("refresh repo fallback", func(t *testing.T) {
		b, err := Build(ctx, BuildInput{
			Refresh:       refresh.New(refresh.Options{Repo: r}),
			Target:        target,
			Source:        src,
			ConfigContent: "mode: rule\n",
			DeploymentID:  "refresh-repo",
		})
		if err != nil {
			t.Fatal(err)
		}
		if got := b.Manifest().ProfileID; got != "custom-default" {
			t.Fatalf("Manifest.ProfileID = %q, want %q", got, "custom-default")
		}
	})

	for _, tc := range []struct {
		name string
		rf   *refresh.Service
	}{
		{"nil repo and nil refresh", nil},
		{"nil repo and refresh without repo", refresh.New(refresh.Options{})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Build(ctx, BuildInput{
				Target:        target,
				Source:        src,
				ConfigContent: "mode: rule\n",
				DeploymentID:  "fail-closed",
				Refresh:       tc.rf,
			})
			if err == nil || !strings.Contains(err.Error(), "deploy: repository is nil") {
				t.Fatalf("err = %v, want deploy: repository is nil", err)
			}
		})
	}
}

func TestPrepareContentBundleResolvesDefaultProfileFromManagerRepository(t *testing.T) {
	r := openRepoWithCustomDefaultProfile(t, "custom-default")
	ctx := context.Background()
	m, err := agents.New(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)

	src := testSource(t, map[string]any{})
	target := Target{ID: "agent-mosdns-default", ServiceType: "mosdns"}
	dp := New(Options{Refresh: refresh.New(refresh.Options{})})
	b, err := dp.PrepareContentBundle(ctx, m, target, src, "https://panel.example.com", "prepare-default-profile")
	if err != nil {
		t.Fatal(err)
	}
	if got := b.Manifest().ProfileID; got != "custom-default" {
		t.Fatalf("Manifest.ProfileID = %q, want %q", got, "custom-default")
	}
}
