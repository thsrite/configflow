package monitor

import (
	"bytes"
	"testing"
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
				setAgentField(a, "deployments", []any{})
			case "latest_deployment":
				setAgentField(a, "latest_deployment", "")
			case "entry":
				setAgentField(a, "deployments", map[string]any{"publish-one": 0})
			case "status":
				d, _ := a.Deployments.Get("publish-one")
				_ = d.SetJSON("status", []byte(`["succeeded"]`))
				a.Deployments.Set("publish-one", d)
			}
			_, err := ReadOverview(ctx, e.m, "one", nil)
			wantMonitorError(t, err, "DASHBOARD_STATE_ERROR", 500)
			if !bytes.Equal(saved, readFile(t, e.store.Path("one"))) {
				t.Fatal("damaged guard discarded checkpoint")
			}
			result, err := ResetStatistics(ctx, e.m, "one", []byte(`{}`))
			if err != nil || asMap(toJSONMap(t, result)["statistics"])["reason"] != "manual" {
				t.Fatalf("manual statistics reset failed: %v", err)
			}
			if _, present := e.read(t)["pending"]; present {
				t.Fatal("manual reset retained pending")
			}
			if result, err = ReadOverview(ctx, e.m, "one", nil); err != nil || !result.Success {
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
			d, _ := a.Deployments.Get("publish-one")
			d.Status = "succeeded"
			a.LatestDeployment.Status = "succeeded"
			switch field {
			case "agent-profile":
				setAgentField(a, "profile_id", 7)
				_ = d.SetJSON("profile_id", []byte(`7`))
				setAgentField(a, "Profile_ID", "default")
			case "task-profile":
				_ = d.SetJSON("profile_id", []byte(`7`))
				_ = d.Fields.Set("Profile_ID", []byte(`"default"`))
			case "task-service":
				_ = d.SetJSON("service_type", []byte(`7`))
				_ = d.Fields.Set("Service_Type", []byte(`"oxidns"`))
			case "latest-id":
				_ = a.LatestDeployment.SetJSON("deployment_id", []byte(`7`))
				_ = a.LatestDeployment.Fields.Set("Deployment_ID", []byte(`"publish-one"`))
			case "manual-rollback":
				_ = d.SetJSON("manual_rollback", []byte(`1`))
			}
			a.Deployments.Set("publish-one", d)
			_, err := ReadOverview(ctx, e.m, "one", nil)
			wantMonitorError(t, err, "DASHBOARD_STATE_ERROR", 500)
			if !bytes.Equal(saved, readFile(t, e.store.Path("one"))) {
				t.Fatal("bad original proof discarded pending state")
			}
			if _, err := ResetStatistics(ctx, e.m, "one", []byte(`{}`)); err != nil {
				t.Fatal("explicit statistics reset could not recover", err)
			}
		})
	}
}

// A damaged status of the pending deployment or of a newer latest record must
// not decide that the checkpoint was superseded (review N8-10 monitor M1).
func TestReviewN810SupersededCheckDoesNotTrustDamagedStatus(t *testing.T) {
	for _, kind := range []string{"task-status", "latest-status"} {
		t.Run(kind, func(t *testing.T) {
			e := newPubEnv(t)
			e.checkpoint(t, "publish-one", revB)
			saved := readFile(t, e.store.Path("one"))
			task(e.m, "publish-two", "preparing", revC)
			a := e.m.agent("one")
			one, _ := a.Deployments.Get("publish-one")
			switch kind {
			case "task-status":
				_ = one.SetJSON("status", []byte(`["succeeded"]`))
			case "latest-status":
				one.Status = "succeeded"
				_ = a.LatestDeployment.SetJSON("status", []byte(`{"value":"preparing"}`))
			}
			a.Deployments.Set("publish-one", one)
			_, err := ReadOverview(ctx, e.m, "one", nil)
			wantMonitorError(t, err, "DASHBOARD_STATE_ERROR", 500)
			if !bytes.Equal(saved, readFile(t, e.store.Path("one"))) {
				t.Fatal("damaged status discarded the pending checkpoint")
			}
			if _, err := ResetStatistics(ctx, e.m, "one", []byte(`{}`)); err != nil {
				t.Fatal("explicit statistics reset could not recover", err)
			}
		})
	}
}
