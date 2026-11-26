package deploy

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"configflow/internal/agents"
	"configflow/internal/configgen"
	"configflow/internal/convert/mihomoenv"
	"configflow/internal/model"
	"configflow/internal/repo"
)

func artifactManager(t *testing.T) (context.Context, *agents.Manager) {
	t.Helper()
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := repo.WithScope(t.Context())
	m, err := agents.New(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	return ctx, m
}

func TestManualArtifactRuntime(t *testing.T) {
	mihomoenv.OverrideForTest(t, nil, nil, nil)
	for _, engine := range []configgen.Engine{configgen.Mihomo, configgen.Surge, configgen.Loon, configgen.MosDNS, configgen.OxiDNS} {
		for _, shutdown := range []bool{true, false} {
			t.Run(string(engine)+map[bool]string{true: "/shutdown", false: "/disconnect"}[shutdown], func(t *testing.T) {
				base, m := artifactManager(t)
				rt := startGlobalPublishRuntime(t, base)
				request, cancel := context.WithCancel(base)
				defer cancel()
				ctx := rt.BindRequest(request)
				if shutdown {
					rt.Stop(0)
				} else {
					cancel()
				}
				before, err := m.Repository().ExportAllJSON(base, false)
				if err != nil {
					t.Fatal(err)
				}
				artifact, err := New(Options{}).GenerateArtifact(ctx, m, "default", engine, "https://panel.example.com")
				if shutdown {
					if !errors.Is(err, context.Canceled) {
						t.Fatalf("shutdown result: %v", err)
					}
					for _, relative := range []string{".generated-previews", "profiles/default/generated"} {
						if _, err := os.Stat(filepath.Join(m.Repository().DataDir(), relative)); !errors.Is(err, os.ErrNotExist) {
							t.Fatalf("shutdown created %s: %v", relative, err)
						}
					}
				} else {
					if err != nil {
						t.Fatal(err)
					}
					pv, err := New(Options{}).LoadPreview(m, artifact.ID, "manual")
					if err != nil || pv.ConfigRevision == nil || pv.ProfileID != "default" {
						t.Fatalf("disconnect lost artifact: %v", err)
					}
					data, ok, err := pv.File(artifact.ConfigPath)
					if err != nil || !ok || string(data) != artifact.Content {
						t.Fatal("disconnect lost frozen bytes", err)
					}
				}
				after, err := m.Repository().ExportAllJSON(base, false)
				if err != nil || !reflect.DeepEqual(before, after) {
					t.Fatal("artifact generation changed configuration", err)
				}
			})
		}
	}
}

func TestManualArtifactUsesOwnerRepository(t *testing.T) {
	ctx, m := artifactManager(t)
	mihomoenv.OverrideForTest(t, m.Repository(), nil, nil)
	_, foreign := artifactManager(t)
	if _, err := foreign.Repository().UpdateProfile(ctx, "default", func(_ context.Context, p *model.Profile) error {
		p.Surge.CustomConfig = "[General]\nloglevel=warning\n# foreign-profile-secret"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	request := ctx
	artifact, err := New(Options{}).GenerateArtifact(request, m, "default", configgen.Surge, "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(artifact.Content, "foreign-profile-secret") {
		t.Fatal("foreign repository leaked")
	}
	pv, err := New(Options{}).LoadPreview(m, artifact.ID, "manual")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(pv.Source), "foreign-profile-secret") {
		t.Fatal("foreign source persisted")
	}
	var source struct {
		ProfileID string `json:"profile_id"`
	}
	if err := json.Unmarshal(pv.Source, &source); err != nil || source.ProfileID != artifact.ProfileID {
		t.Fatal("source/profile mismatch", err)
	}
	if _, err := New(Options{}).LoadPreview(foreign, artifact.ID, "manual"); err == nil {
		t.Fatal("preview leaked across repositories")
	}
}

func TestManualArtifactShutdownBeforePreviewCommit(t *testing.T) {
	mihomoenv.OverrideForTest(t, nil, nil, nil)
	base, m := artifactManager(t)
	rt := startGlobalPublishRuntime(t, base)
	ctx := rt.BindRequest(base)
	svc := New(Options{})
	owner := svc.PreviewsFor(m)
	owner.mu.Lock()
	locked := true
	defer func() {
		if locked {
			owner.mu.Unlock()
		}
	}()
	done := make(chan error, 1)
	go func() { _, err := svc.GenerateArtifact(ctx, m, "default", configgen.Surge, ""); done <- err }()
	// The existing generator's committed file proves the new request has
	// passed rendering. Holding the real leaf lock fences the next commit.
	path := filepath.Join(m.Repository().DataDir(), "profiles", "default", "generated", "config.conf")
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	waiting := true
	for waiting {
		select {
		case err := <-done:
			t.Fatalf("artifact passed held preview lock: %v", err)
		case <-deadline.C:
			t.Fatal("generator did not reach file commit")
		case <-ticker.C:
			if _, err := os.Stat(path); err == nil {
				waiting = false
			}
		}
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Use the production grace, rather than a shortened seam that could hide
	// incorrect Runtime cancellation or admission behavior.
	rt.Stop(8 * time.Second)
	owner.mu.Unlock()
	locked = false
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("late shutdown commit: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("artifact did not finish after shutdown")
	}
	if _, err := os.Stat(filepath.Join(m.Repository().DataDir(), ".generated-previews")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("shutdown wrote preview", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(before) != string(after) {
		t.Fatal("shutdown changed committed generated file", err)
	}
}
