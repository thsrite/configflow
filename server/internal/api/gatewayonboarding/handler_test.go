package gatewayonboarding

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"configflow/internal/convert"
	"configflow/internal/convert/mihomoenv"
	"configflow/internal/gateway"
	"configflow/internal/gateway/onboarding"
	"configflow/internal/httpx"
	"configflow/internal/model"
	"configflow/internal/repo"
)

// TestNewHandlerNilAdopterFailsClosed verifies that NewHandler with a nil
// AdoptOwnedWrite fails closed with 503 when a pending setup session exists.
func TestNewHandlerNilAdopterFailsClosed(t *testing.T) {
	_ = convert.SetMihomoIntegrations(gateway.Hooks())
	mihomoenv.OverrideForTest(t, nil, nil, nil)
	dir := t.TempDir()
	r, err := repo.OpenDefault(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	if err := r.UpdateShared(context.Background(), func(_ context.Context, c *model.Shared) error {
		return c.SetJSON("nodes", json.RawMessage(`[{"id":"n","name":"Example","type":"ss","server":"192.0.2.2","port":443,"cipher":"aes-128-gcm","password":"synthetic-node-secret"}]`))
	}); err != nil {
		t.Fatal(err)
	}

	// Create a pending setup session for the default profile.
	sessionsDir := filepath.Join(dir, "setup-sessions")
	if err := os.MkdirAll(sessionsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	session := `{"profile_id":"default","goal":"gateway","completed":false}`
	if err := os.WriteFile(filepath.Join(sessionsDir, strings.Repeat("a", 32)+".json"), []byte(session), 0o600); err != nil {
		t.Fatal(err)
	}

	app := httpx.New()
	app.Repo = r
	NewHandler(r, onboarding.CheckDNSLoop, nil).Register(app.Router)

	res, err := app.Invoke(context.Background(), httpx.InvokeRequest{
		Method: http.MethodPost,
		Path:   "/api/profiles/default/gateway-onboarding",
		JSON:   json.RawMessage(validForm),
	})
	if err != nil {
		t.Fatalf("POST gateway-onboarding: %v", err)
	}
	if res.Status != 503 {
		t.Fatalf("status = %d, want 503 (body=%s)", res.Status, res.Body)
	}
}
