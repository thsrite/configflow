package agents

import (
	"configflow/internal/httpx/httpxtest"
	"testing"
	"time"
)

// TestPublishedContentUsesRealAsyncPreparation exercises the production A14
// callback through A13's worker, rather than injecting an already-built bundle.
func TestPublishedContentUsesRealAsyncPreparation(t *testing.T) {
	s, admin, _ := newDeployHTTP(t)
	WireContentSync()
	base := "/api/agents/publish-agent"
	s.Post(base+"/push-config", httpxtest.JSON(map[string]any{"deployment_id": "real-content-worker"}), admin).WantStatus(202)
	for i := 0; i < 4; i++ {
		s.Get(base+"/deployments/real-content-worker", admin).WantStatus(200)
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		response := s.Get(base+"/push-diff", admin)
		response.WantStatus(200)
		status := response.Object().GetString("status")
		if status != "checking" {
			if status != "synced" {
				t.Fatal(response.Object())
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("real content preparation did not finish")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
