package rulesync

import (
	"context"
	"strings"
	"testing"

	"configflow/internal/convert/mihomoenv"
	"configflow/internal/deploy"
	"configflow/internal/model"
	"configflow/internal/refresh"
	"configflow/internal/repo"
)

func TestPrepareCandidateWithResolvesDefaultProfileAndFailsClosed(t *testing.T) {
	mihomoenv.OverrideForTest(t, nil, nil, nil)
	r, err := repo.OpenDefault(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)

	ctx := context.Background()
	const defaultID = "custom-default"
	if _, err := r.CreateProfileRecord(ctx, repo.ProfileCreation{ID: defaultID, Name: "Custom Default"}, ""); err != nil {
		t.Fatal(err)
	}
	if err := r.UpdateSystem(ctx, func(_ context.Context, s *model.System) error {
		s.SystemConfig.DefaultProfileID = new(defaultID)
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	src, err := deploy.SourceFromJSON([]byte(`{}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	target := deploy.Target{ID: "device-default", ServiceType: "mihomo"}
	baselineBundle, err := deploy.Build(ctx, deploy.BuildInput{
		Repo:          r,
		Target:        target,
		Source:        src,
		ConfigContent: "mode: rule\nrules: []\n",
		DeploymentID:  "baseline-1",
		BaseURL:       "https://panel.example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	baseline := Baseline{
		Receipt: &deploy.Receipt{
			Manifest: baselineBundle.Manifest(),
			Source:   []byte(`{}`),
			BaseURL:  "https://panel.example.com",
			Bundle:   baselineBundle,
		},
		Bundle: baselineBundle,
	}

	got, err := PrepareCandidateWith(ctx, refresh.New(refresh.Options{Repo: r}), target, src, baseline, Settings{Rules: true}, "cand-ok", false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Manifest().ProfileID != defaultID {
		t.Fatalf("Manifest.ProfileID = %q, want %q", got.Manifest().ProfileID, defaultID)
	}

	for _, tc := range []struct {
		name string
		rf   *refresh.Service
	}{
		{"nil refresh", nil},
		{"refresh without repo", refresh.New(refresh.Options{})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := PrepareCandidateWith(ctx, tc.rf, target, src, baseline, Settings{Rules: true}, "cand-fail", false)
			if err == nil || !strings.Contains(err.Error(), "deploy: repository is nil") {
				t.Fatalf("err = %v, want deploy: repository is nil", err)
			}
		})
	}
}
