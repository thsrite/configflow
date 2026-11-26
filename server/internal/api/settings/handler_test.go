package settings

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"configflow/internal/httpx"
	"configflow/internal/repo"
	"configflow/internal/weblistener"
)

func TestHandlerSetWebListenerTwoPhase(t *testing.T) {
	h := NewHandler(nil, nil, nil)
	m1 := &weblistener.Manager{DataDir: t.TempDir()}
	m2 := &weblistener.Manager{DataDir: t.TempDir()}

	if err := h.SetWebListener(m1); err != nil {
		t.Fatalf("first SetWebListener: %v", err)
	}
	if err := h.SetWebListener(m2); !errors.Is(err, ErrWebListenerAlreadySet) {
		t.Fatalf("second SetWebListener = %v, want %v", err, ErrWebListenerAlreadySet)
	}
	if got := h.currentWebListener(); got != m1 {
		t.Fatalf("currentWebListener changed after rejected second set")
	}

	h2 := NewHandler(nil, nil, nil)
	h2.MarkStarted()
	if err := h2.SetWebListener(m1); !errors.Is(err, ErrHandlerStarted) {
		t.Fatalf("SetWebListener after MarkStarted = %v, want %v", err, ErrHandlerStarted)
	}
}

func TestHandlerExplicitNilWebListenerDoesNotFallbackToGlobal(t *testing.T) {
	r, err := repo.OpenDefault(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)

	h := NewHandler(r, nil, nil)
	if err := h.SetWebListener(nil); err != nil {
		t.Fatalf("SetWebListener(nil): %v", err)
	}
	h.MarkStarted()
	if got := h.currentWebListener(); got != nil {
		t.Fatalf("explicit nil web listener fell back to non-nil manager")
	}

	app := httpx.New()
	app.Repo = r
	h.Register(app.Router)
	res, err := app.Invoke(context.Background(), httpx.InvokeRequest{Method: http.MethodGet, Path: "/api/settings/web-port"})
	if err != nil {
		t.Fatalf("GET /api/settings/web-port: %v", err)
	}
	if res.Status != 200 {
		t.Fatalf("status = %d, want 200", res.Status)
	}
	var payload struct {
		Available bool `json:"available"`
	}
	if err := json.Unmarshal(res.Body, &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Available {
		t.Fatalf("expected available=false for explicit nil web listener, got %s", res.Body)
	}
}
