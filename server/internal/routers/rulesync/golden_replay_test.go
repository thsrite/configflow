package rulesync

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"configflow/internal/routers"
	"configflow/internal/routers/device"
	"configflow/internal/routers/ros"
	"configflow/internal/routers/routerfake"
	"configflow/internal/routers/spec"
	"configflow/internal/testgolden"
)

func goldenMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func comparableValue(t *testing.T, v any) any {
	t.Helper()
	var out any
	if err := json.Unmarshal(goldenMarshal(t, v), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestRuleSyncGoldenReplay records and replays deterministic Go-native boundary
// outputs for R14 (router rule-sync delta across static, policy, nat, unchanged).
func TestRuleSyncGoldenReplay(t *testing.T) {
	ref := testgolden.New(t, testgolden.Spec{Compress: true})

	for _, mode := range []string{"static", "policy", "nat", "unchanged"} {
		t.Run("R14/"+mode, func(t *testing.T) {
			state := r08State()
			if mode == "nat" {
				delete(state.Tables[ros.PathNAT][0], "out-interface")
			}
			f := routerfake.New(nil)
			f.Reset(state)
			srv := httptest.NewServer(f)
			defer srv.Close()
			d := routers.NewRouterOS(srv.URL, "synthetic-user", "synthetic-pass", false)
			defer d.Close()

			r := r10Router()
			r.ID = "r-sync"
			r.ManageReturnRouting = false
			policy := mode == "policy"

			old := r08Desired(policy)
			old.IPv6CIDRs = []spec.ProxyCIDR{}
			old.BlockingWarnings = []string{}
			if mode != "unchanged" {
				if policy {
					old.ROSPolicy.CIDRs = []spec.ProxyCIDR{}
				} else {
					old.ProxyCIDRs = old.ProxyCIDRs[:1]
				}
			}
			if policy || mode == "unchanged" {
				initPlan := r14Plan(t, t.Context(), d, r, old)
				for _, stage := range initPlan.Stages {
					for _, op := range stage.Ops {
						if _, err := d.Apply(t.Context(), device.JSON(op)); err != nil {
							t.Fatal(err)
						}
					}
				}
			}
			static, err := syncStaticResources(t.Context(), d, old)
			if err != nil {
				t.Fatal(err)
			}

			next := r08Desired(policy)
			next.IPv6CIDRs = []spec.ProxyCIDR{}
			next.BlockingWarnings = []string{}
			plan := r14Plan(t, t.Context(), d, r, next)
			res, err := ApplyRuleSyncDelta(t.Context(), d, next, plan, &RuleSyncBaseline{Desired: old, StaticResources: static}, nil)
			errText := ""
			if err != nil {
				errText = err.Error()
			}
			ref.Assert(t, goldenMarshal(t, map[string]string{"module": "R14", "mode": mode}), goldenMarshal(t, map[string]any{
				"result": comparableValue(t, res),
				"error":  errText,
				"tables": comparableValue(t, f.Snapshot().Tables),
			}))
		})
	}
}
