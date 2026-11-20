package routers

import (
	"configflow/internal/model"
	"configflow/internal/repo"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestWireRegistersOnlyNativeRepositoryCallbacks(t *testing.T) {
	previous := repo.SetHooks(repo.Hooks{})
	defer repo.SetHooks(previous)
	Wire()
	installed := repo.SetHooks(repo.Hooks{})
	if installed.RedactRoutersJSON == nil || installed.JobLockRecord == nil {
		t.Fatal("missing repository callbacks")
	}
	raw, err := installed.RedactRoutersJSON(json.RawMessage(`{"items":[{"id":"synthetic","name":"Synthetic","password":"synthetic-password","endpoint":"https://router.example.com"}]}`))
	if err != nil || strings.Contains(string(raw), "synthetic-password") || !strings.Contains(string(raw), "Synthetic") {
		t.Fatal("typed redaction was not called", err)
	}
	router := model.Router{Type: "routeros", Endpoint: "https://router.example.com"}
	release, ok := installed.JobLockRecord(context.Background(), "profile", "router", router)
	if !ok {
		t.Fatal("typed router lease was not acquired")
	}
	defer release()
	if duplicate, ok := installed.JobLockRecord(context.Background(), "alias", "alias", router); ok {
		duplicate()
		t.Fatal("typed callback bypassed shared lease")
	}

	// Verify repository itself dispatches the typed hooks registered by Wire().
	Wire()
	redactCalls := 0
	realHooks := repo.SetHooks(repo.Hooks{})
	wrapped := realHooks
	wrapped.RedactRoutersJSON = func(in json.RawMessage) (json.RawMessage, error) {
		redactCalls++
		return realHooks.RedactRoutersJSON(in)
	}
	wrapped.SanitizeExternalDocument = func(_ context.Context, payload *model.Document, _ model.SystemConfig) (*model.Document, error) {
		return payload, nil
	}
	wrapped.RedactOxidnsJSON = func(in json.RawMessage) (json.RawMessage, error) { return in, nil }
	wrapped.RedactEasytierJSON = func(in json.RawMessage) (json.RawMessage, error) { return in, nil }
	repo.SetHooks(wrapped)

	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
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
		t.Fatalf("repository did not invoke typed RedactRoutersJSON once: calls=%d", redactCalls)
	}
}
