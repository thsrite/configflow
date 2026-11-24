package routers

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"configflow/internal/configexport"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/routers/spec"
)

func TestRepositoryOptionsWireRouterRedactionAndJobLock(t *testing.T) {
	previous := repo.SetHooks(repo.Hooks{})
	defer repo.SetHooks(previous)

	raw, err := spec.RedactRouters(json.RawMessage(`{"items":[{"id":"synthetic","name":"Synthetic","password":"synthetic-password","endpoint":"https://router.example.com"}]}`))
	if err != nil || strings.Contains(string(raw), "synthetic-password") || !strings.Contains(string(raw), "Synthetic") {
		t.Fatal("typed redaction was not called", err)
	}
	router := model.Router{Type: "routeros", Endpoint: "https://router.example.com"}
	release, ok := JobLockRecord(context.Background(), "profile", "router", router)
	if !ok {
		t.Fatal("typed router lease was not acquired")
	}
	defer release()
	if duplicate, ok := JobLockRecord(context.Background(), "alias", "alias", router); ok {
		duplicate()
		t.Fatal("typed callback bypassed shared lease")
	}

	// Verify repository dispatches instance Options without any global hooks.
	redactCalls := 0
	r, err := repo.Open(t.TempDir(), repo.Options{
		Export: configexport.Exporter{
			RedactRouters: func(in json.RawMessage) (json.RawMessage, error) {
				redactCalls++
				return spec.RedactRouters(in)
			},
		},
		RouterJobLock: JobLockRecord,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	ctx := context.Background()
	_, err = r.UpdateProfile(ctx, "default", func(_ context.Context, p *model.Profile) error {
		return p.SetJSON("routers", []byte(`{"items":[{"id":"r1","name":"Synthetic","type":"routeros","password":"synthetic-password","endpoint":"https://router.example.com"}]}`))
	})
	if err != nil {
		t.Fatal(err)
	}
	exported, err := r.ExportAllJSON(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if redactCalls != 1 || strings.Contains(string(exported), "synthetic-password") || !strings.Contains(string(exported), "Synthetic") {
		t.Fatalf("repository did not invoke instance RedactRouters once: calls=%d", redactCalls)
	}
}
