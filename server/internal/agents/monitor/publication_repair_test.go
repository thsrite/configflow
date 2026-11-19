package monitor

import (
	"bytes"
	"testing"

	"configflow/internal/jsonx"
)

func TestDamagedPublicationCheckpointCanBeResetAndMonitoringResumes(t *testing.T) {
	for _, kind := range []string{"deployments", "latest_deployment", "entry", "status"} {
		t.Run(kind, func(t *testing.T) {
			e := newPubEnv(t)
			e.checkpoint(t, "publish-one", revB)
			saved := readFile(t, e.store.Path("one"))
			a := e.m.agent("one")
			switch kind {
			case "deployments":
				a.Set("deployments", []any{})
			case "latest_deployment":
				a.Set("latest_deployment", "")
			case "entry":
				a.GetObject("deployments").Set("publish-one", jsonx.Int(0))
			case "status":
				a.GetObject("deployments").GetObject("publish-one").Set("status", []any{"succeeded"})
			}
			_, err := Overview(ctx, e.m, "one", nil)
			wantMonitorError(t, err, "DASHBOARD_STATE_ERROR", 500)
			if !bytes.Equal(saved, readFile(t, e.store.Path("one"))) {
				t.Fatal("damaged guard discarded checkpoint")
			}
			result, err := ResetDashboard(ctx, e.m, "one", jsonx.Obj())
			if err != nil || result.GetObject("statistics").GetString("reason") != "manual" {
				t.Fatalf("manual statistics reset failed: %v", err)
			}
			if _, present := e.read(t).Get("pending"); present {
				t.Fatal("manual reset retained pending")
			}
			if result, err = Overview(ctx, e.m, "one", nil); err != nil || result.Value("success") != true {
				t.Fatalf("monitoring did not recover: %v", err)
			}
		})
	}
}

func TestPendingCheckpointDoesNotTrustProjectedStoredProofs(t *testing.T) {
	for _, field := range []string{"agent-profile", "task-profile", "task-service", "latest-id", "manual-rollback"} {
		t.Run(field, func(t *testing.T) {
			e := newPubEnv(t)
			e.checkpoint(t, "publish-one", revB)
			saved := readFile(t, e.store.Path("one"))
			change(e.f, 3000, 6, 60, revB, revE)
			a := e.m.agent("one")
			task := a.GetObject("deployments").GetObject("publish-one")
			task.Set("status", "succeeded")
			a.GetObject("latest_deployment").Set("status", "succeeded")
			switch field {
			case "agent-profile":
				a.Set("profile_id", jsonx.Int(7))
				task.Set("profile_id", jsonx.Int(7))
				a.Set("Profile_ID", "default")
			case "task-profile":
				task.Set("profile_id", jsonx.Int(7))
				task.Set("Profile_ID", "default")
			case "task-service":
				task.Set("service_type", jsonx.Int(7))
				task.Set("Service_Type", "oxidns")
			case "latest-id":
				a.GetObject("latest_deployment").Set("deployment_id", jsonx.Int(7))
				a.GetObject("latest_deployment").Set("Deployment_ID", "publish-one")
			case "manual-rollback":
				task.Set("manual_rollback", jsonx.Int(1))
			}
			_, err := Overview(ctx, e.m, "one", nil)
			wantMonitorError(t, err, "DASHBOARD_STATE_ERROR", 500)
			if !bytes.Equal(saved, readFile(t, e.store.Path("one"))) {
				t.Fatal("bad original proof discarded pending state")
			}
			if _, err := ResetDashboard(ctx, e.m, "one", jsonx.Obj()); err != nil {
				t.Fatal("explicit statistics reset could not recover", err)
			}
		})
	}
}
