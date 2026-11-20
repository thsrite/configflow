package agents

import (
	"errors"
	"testing"

	"configflow/internal/model"
)

func TestHistoricalHAPairReadDoesNotReconcile(t *testing.T) {
	for _, status := range []string{"succeeded", "failed", "ready", "applying", "rollback_failed"} {
		t.Run(status, func(t *testing.T) {
			e := newEnv(t)
			e.begin("history")
			e.record("history", map[string]any{"status": "succeeded", "submitted": true})
			e.finish("history")
			e.editSystem(func(system *model.System) {
				raw := mustJSON(t, map[string]any{
					"id": "pair", "status": status, "message": "synthetic pair status",
					"children": map[string]any{e.id(): "history"},
				})
				if err := e.firstAgent(system).SetJSON(HAField, raw); err != nil {
					t.Fatal(err)
				}
			})
			before := e.system()
			got, err := e.m.GetDeploymentModel(e.ctx, e.id(), "history")
			if status == "applying" || status == "rollback_failed" {
				if !errors.Is(err, ErrHAOwnerMissing) {
					t.Fatal(got, err)
				}
			} else {
				if err != nil || got.Status != status || derefStr(got.Message) != "synthetic pair status" ||
					deploymentFlagTrue(got, "success") != (status != "failed") ||
					memberPresent(got, "pending") || memberPresent(got, "http_status") {
					t.Fatal(got, err)
				}
			}
			if diff := diffJSONValues(t, before, e.system()); diff != "" || len(e.fake.urls()) != 0 {
				t.Fatalf("read reconciled or wrote state: %s", diff)
			}
		})
	}
}
