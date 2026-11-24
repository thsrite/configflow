package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"

	"configflow/internal/agents/agentfake"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/logx"
	"configflow/internal/model"
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
	s, m, admin, fake := newDeployHTTPWithManager(t, agentfake.Options{})
	var logs lockedBuffer
	if _, err := logx.Setup(logx.Config{Level: logx.Info, Console: &logs}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = logx.Setup(logx.Config{Level: logx.Info, Console: io.Discard}) })
	// Until A18 registers the pair publisher an HA member fails closed (500).
	if err := s.Repo.UpdateSystem(s.Context(), func(_ context.Context, system *model.System) error {
		return model.SetStoredMember(&system.Agents[0], "oxidns_ha", json.RawMessage(`{"enabled":true}`))
	}); err != nil {
		t.Fatal(err)
	}
	response := s.Post("/api/agents/publish-agent/push-config", httpxtest.JSON(map[string]any{"deployment_id": "unexpected"}), admin)
	response.WantStatus(500)
	body := parseBodyMap(t, response)
	if mapStr(body, "failed_stage") != "preparing" || mapStr(body, "deployment_id") != "unexpected" ||
		!strings.Contains(mapStr(body, "error_details"), "DNS HA pair publication is not available") {
		t.Fatal("response", body)
	}
	agent, err := m.ReadAgent(s.Context(), "publish-agent", true)
	if err != nil {
		t.Fatal(err)
	}
	agentMap := parseJSONMap(t, mustRawJSON(t, agent))
	task := mapObj(mapObj(agentMap, "deployments"), "unexpected")
	if mapStr(task, "status") != "failed" || mapStr(task, "failed_stage") != "preparing" || mapStr(task, "message") != "Deployment preparation failed" {
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
