package agents

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	core "configflow/internal/agents"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
)

// TestReviewN810ServiceOperationShutdownAndDisconnect drives the native
// restart/uninstall owner with a real Runtime: after shutdown no device request
// is sent; after a request disconnect the operation still reaches the device.
func TestReviewN810ServiceOperationShutdownAndDisconnect(t *testing.T) {
	operations := []struct {
		name      string
		operation core.DirectOperation
		path      string
	}{
		{"restart", core.RestartService, "/api/restart"},
		{"uninstall", core.UninstallService, "/api/uninstall"},
	}
	for _, op := range operations {
		for _, shutdown := range []bool{true, false} {
			name := op.name + "/disconnect"
			if shutdown {
				name = op.name + "/shutdown"
			}
			t.Run(name, func(t *testing.T) {
				s, _, _ := newDeployHTTP(t)
				paths := make(chan string, 4)
				device := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					paths <- r.Method + " " + r.URL.RequestURI()
					w.Header().Set("Content-Type", "application/json")
					_, _ = io.WriteString(w, `{"success": true}`)
				}))
				defer device.Close()
				parsed, err := url.Parse(device.URL)
				if err != nil {
					t.Fatal(err)
				}
				host, port, err := net.SplitHostPort(parsed.Host)
				if err != nil {
					t.Fatal(err)
				}
				number, err := strconv.Atoi(port)
				if err != nil {
					t.Fatal(err)
				}
				if err := s.Repo.UpdateSystem(s.Context(), func(_ context.Context, system *model.System) error {
					system.Agents[0].Host = host
					system.Agents[0].Port = model.Int(number)
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				manager, err := core.Get(s.Context())
				if err != nil {
					t.Fatal(err)
				}
				runtime := lifecycle.NewRuntime(lifecycle.Options{Grace: 2 * time.Second})
				runtime.Start(s.Context())
				t.Cleanup(func() { runtime.Shutdown(context.Background()) })
				parent, cancel := context.WithCancel(s.Context())
				defer cancel()
				ctx := runtime.BindRequest(parent)
				if shutdown {
					runtime.Shutdown(context.Background())
				} else {
					cancel()
				}
				result, err := manager.RunServiceOperation(ctx, "publish-agent", op.operation, nil, nil)
				if shutdown {
					if !errors.Is(err, context.Canceled) {
						t.Fatalf("shutdown result: %s %v", result, err)
					}
					select {
					case got := <-paths:
						t.Fatalf("device request after shutdown: %s", got)
					default:
					}
					return
				}
				if err != nil {
					t.Fatalf("disconnected operation failed: %v", err)
				}
				var body struct {
					Success bool `json:"success"`
				}
				if err := json.Unmarshal(result, &body); err != nil || !body.Success {
					t.Fatalf("disconnected operation result: %s %v", result, err)
				}
				select {
				case got := <-paths:
					if got != http.MethodPost+" "+op.path {
						t.Fatalf("device request: %s", got)
					}
				default:
					t.Fatal("disconnected operation did not reach the device")
				}
			})
		}
	}
}
