package setup

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"configflow/internal/repo"
)

func TestNewWithOptionsNilOwnersFailClosed(t *testing.T) {
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	svc, err := NewWithOptions(r, Options{})
	if err != nil {
		t.Fatal(err)
	}
	const id = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if _, err := svc.Create(ctx, json.RawMessage(`{"id":"`+id+`","goal":"gateway","name":"Synthetic"}`)); err != nil {
		t.Fatal(err)
	}
	_, err = svc.Action(ctx, id, "prepare", json.RawMessage(`{"domain_rules":{"direct":[],"proxy":[]},"local_dns":"https://192.0.2.53/dns-query","controller_secret":"synthetic"}`), "http://127.0.0.1:5001")
	var setupErr *Error
	if !errors.As(err, &setupErr) || setupErr.Status != 503 {
		t.Fatalf("NewWithOptions(r, Options{}) prepare error = %v, want 503 *setup.Error", err)
	}
}
