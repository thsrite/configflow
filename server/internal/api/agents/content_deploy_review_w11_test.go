package agents

import (
	"context"
	"errors"
	"testing"

	core "configflow/internal/agents"
	"configflow/internal/deploy"
	"configflow/internal/repo"
)

func newReviewW11Manager(t *testing.T) *core.Manager {
	t.Helper()
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	m, err := core.New(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	return m
}

// Content comparison and gateway onboarding publication must use the deploy
// Service injected by server.New (its Sub-Store, aggregation-aware provider,
// HA pair publisher and IPv6 enroller), never a fresh deploy.New(Options{}).
func TestHandlerUsesInjectedDeployForContentAndInstallation(t *testing.T) {
	m := newReviewW11Manager(t)
	dp := deploy.New(deploy.Options{})
	h := NewHandler(Deps{Agents: m, Deploy: dp})
	if got := contentChecks.deployFor(m); got != dp {
		t.Fatalf("content deploy = %p, want injected %p", got, dp)
	}
	if got := h.content.deps.installationService(m.Repository(), m).Deploy; got != dp {
		t.Fatalf("installation deploy = %p, want injected %p", got, dp)
	}
	m.Close()
	if contentChecks.deployFor(m) != nil {
		t.Fatal("closed manager kept its deploy binding")
	}
}

// Negative control: a Manager without a bound deploy Service refuses content
// preparation instead of building one without the server's collaborators.
func TestPrepareContentRefusesUnboundManager(t *testing.T) {
	m := newReviewW11Manager(t)
	_, err := prepareContent(context.Background(), m, deploy.Target{}, nil, "https://panel.example.com", "synthetic")
	if !errors.Is(err, errContentDeployUnbound) {
		t.Fatalf("err = %v, want errContentDeployUnbound", err)
	}
}
