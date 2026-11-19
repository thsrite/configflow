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
