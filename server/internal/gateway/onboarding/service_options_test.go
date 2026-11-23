package onboarding

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"configflow/internal/repo"
)

func TestNewNilAdopterFailsClosedDespiteGlobalHook(t *testing.T) {
	t.Cleanup(SetAdoptOwnedWrite(func(context.Context, *repo.Repository, string, json.RawMessage, json.RawMessage) error {
		return nil
	}))
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(r.DataDir(), "setup-sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	session := `{"id":"cccccccccccccccccccccccccccccccc","profile_id":"default","goal":"gateway","completed":false,"abandoned":false}`
	if err := os.WriteFile(filepath.Join(dir, "cccccccccccccccccccccccccccccccc.json"), []byte(session), 0o600); err != nil {
		t.Fatal(err)
	}

	svc := New(r, ServiceOptions{DNSLoop: CheckDNSLoop})
	_, err = svc.Execute(context.Background(), Operation{
		ProfileID: "default",
		BaseURL:   "http://127.0.0.1:5001",
		Write:     true,
		Body:      json.RawMessage(`{"rule_set_ids":[],"direct_rule_set_ids":[],"proxy_rule_set_ids":[]}`),
	})
	if !errors.Is(err, ErrSetupUnavailable) {
		t.Fatalf("New(r, nil adopter) Execute error = %v, want %v", err, ErrSetupUnavailable)
	}
}
