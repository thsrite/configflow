package agents

import (
	"configflow/internal/jsonx"
	"errors"
	"testing"
)

func TestHistoricalHAPairReadDoesNotReconcile(t *testing.T) {
	for _, status := range []string{"succeeded", "failed", "ready", "applying", "rollback_failed"} {
		t.Run(status, func(t *testing.T) {
			e := newEnv(t)
			e.begin("history")
			e.record("history", jsonx.Obj("status", "succeeded", "submitted", true))
			e.finish("history")
			e.editSystem(func(system *jsonx.Object) {
				e.firstAgent(system).Set(HAField, jsonx.Obj("id", "pair", "status", status, "message", "synthetic pair status", "children", jsonx.Obj(e.id(), "history")))
			})
			before := e.system().DeepCopy()
			got, err := e.m.GetDeployment(e.ctx, e.id(), "history")
			if status == "applying" || status == "rollback_failed" {
				if !errors.Is(err, ErrNotPorted) {
					t.Fatal(got, err)
				}
			} else {
				if err != nil || got.GetString("status") != status || got.GetString("message") != "synthetic pair status" || got.GetBool("success") != (status != "failed") || got.Has("pending") || got.Has("http_status") {
					t.Fatal(got, err)
				}
			}
			if !jsonx.Equal(before, e.system()) || len(e.fake.urls()) != 0 {
				t.Fatal("read reconciled or wrote state")
			}
		})
	}
}
