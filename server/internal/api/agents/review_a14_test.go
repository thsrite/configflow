package agents

import (
	"bytes"
	"context"
	"io"
	"strings"
	"sync"
	"testing"

	core "configflow/internal/agents"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/jsonx"
	"configflow/internal/logx"
)

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// A14 review L2: an unexpected publication failure answers the generic 500,
// records the failed preparation without contacting the device, and logs the
// error class (Python's route log) without the error text.
func TestReviewA14UnexpectedPushFailureIsLoggedAndRecorded(t *testing.T) {
	s, admin, fake := newDeployHTTP(t)
	var logs lockedBuffer
	if _, err := logx.Setup(logx.Config{Level: logx.Info, Console: &logs}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = logx.Setup(logx.Config{Level: logx.Info, Console: io.Discard}) })
	// Until A18 registers the pair publisher an HA member fails closed (500).
	_, err := s.Repo.UpdateSystemTransaction(s.Context(), func(_ context.Context, system *jsonx.Object) (*jsonx.Object, error) {
		system.GetList("agents")[0].(*jsonx.Object).Set("oxidns_ha", jsonx.Obj("enabled", true))
		return system, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	response := s.Post("/api/agents/publish-agent/push-config", httpxtest.JSON(map[string]any{"deployment_id": "unexpected"}), admin)
	response.WantStatus(500)
	body := response.Object()
	if body.GetString("failed_stage") != "preparing" || body.GetString("deployment_id") != "unexpected" ||
		!strings.Contains(body.GetString("error_details"), "DNS HA pair publication is not available") {
		t.Fatal("response", body)
	}
	m, err := core.Get(s.Context())
	if err != nil {
		t.Fatal(err)
	}
	agent, err := m.GetAgentByID(s.Context(), "publish-agent", true)
	if err != nil {
		t.Fatal(err)
	}
	task := agent.GetObject("deployments").GetObject("unexpected")
	if task.GetString("status") != "failed" || task.GetString("failed_stage") != "preparing" || task.GetString("message") != "Deployment preparation failed" {
		t.Fatal("record", task)
	}
	for _, call := range fake.Calls() {
		if call.Path == "/api/deployments" {
			t.Fatal("device received an upload")
		}
	}
	text := logs.String()
	if !strings.Contains(text, "Agent deployment preparation failed (*errors.errorString)") || strings.Contains(text, "pair publication is not available") {
		t.Fatal("route log", text)
	}
}
