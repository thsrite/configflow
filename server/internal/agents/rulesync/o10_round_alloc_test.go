package rulesync

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"configflow/internal/agents"
	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/repo"
)

func o10RuleSyncFixture(t *testing.T, deployments int) (context.Context, *Service) {
	t.Helper()
	r, err := repo.OpenDefault(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	history := make([]string, 0, deployments)
	payload := strings.Repeat("x", 1024)
	for i := range deployments {
		history = append(history, fmt.Sprintf(`"d%d":{"id":"d%d","status":"success","config":%q}`, i, i, payload))
	}
	agentsJSON := `[{"id":"o10-agent","name":"O10 Agent","host":"192.0.2.20","port":8080,"token":"synthetic-agent-token",` +
		`"service_type":"oxidns","profile_id":"default","enabled":true,"status":"offline","future":{"password":"synthetic-nested-pass"},"last_heartbeat":"2026-01-01T00:00:00Z",` +
		`"rule_sync":{"settings":{"library":false,"rules":false}},` +
		`"deployments":{` + strings.Join(history, ",") + `},` +
		`"ipv6_verifications":{"v1":{"id":"v1","status":"ok","summary":` + fmt.Sprintf("%q", payload) + `}},` +
		`"latest_deployment":{"id":"d0","status":"success","config":` + fmt.Sprintf("%q", payload) + `}}]`
	ctx := context.Background()
	if err := r.UpdateSystem(ctx, func(_ context.Context, s *model.System) error {
		return s.SetJSON("agents", json.RawMessage(agentsJSON))
	}); err != nil {
		t.Fatal(err)
	}
	m, err := agents.New(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	return ctx, New(m, nil, Options{})
}

func o10AvgRoundBytes(t *testing.T, run func() error) uint64 {
	t.Helper()
	if err := run(); err != nil {
		t.Fatal(err)
	}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for range 20 {
		if err := run(); err != nil {
			t.Fatal(err)
		}
	}
	runtime.ReadMemStats(&after)
	return (after.TotalAlloc - before.TotalAlloc) / 20
}

func TestO10RoundSkipsAgentHistoryAndTracksLivePresence(t *testing.T) {
	smallCtx, smallSvc := o10RuleSyncFixture(t, 1)
	largeCtx, largeSvc := o10RuleSyncFixture(t, 500)
	// // Allocation measurements exclude the test-only view guard's encodings.
	smallSvc.m.Repository().SetViewGuard(nil)
	largeSvc.m.Repository().SetViewGuard(nil)

	t.Run("RuleSyncAgents/history", func(t *testing.T) {
		smallBytes := o10AvgRoundBytes(t, func() error {
			_, err := smallSvc.m.RuleSyncAgents(smallCtx)
			return err
		})
		largeBytes := o10AvgRoundBytes(t, func() error {
			_, err := largeSvc.m.RuleSyncAgents(largeCtx)
			return err
		})
		if largeBytes > smallBytes+64*1024 {
			t.Fatalf("RuleSyncAgents allocates %d bytes with 500 deployments (%d with 1)", largeBytes, smallBytes)
		}
	})

	t.Run("Round/history", func(t *testing.T) {
		smallBytes := o10AvgRoundBytes(t, func() error { return smallSvc.Round(smallCtx) })
		largeBytes := o10AvgRoundBytes(t, func() error { return largeSvc.Round(largeCtx) })
		if largeBytes > smallBytes+64*1024 {
			t.Fatalf("Round allocates %d bytes with 500 deployments (%d with 1)", largeBytes, smallBytes)
		}
	})

	// Negative control: deep-copying the listing with histories (the pre-R1
	// AgentSnapshots(ctx, true)) scales with them. Borrowed reads do not.
	fullCopy := func(ctx context.Context, svc *Service) func() error {
		return func() error {
			rows, err := svc.m.AgentSnapshots(ctx, true)
			for _, row := range rows {
				if err == nil {
					_, err = agents.CloneAgent(row)
				}
			}
			return err
		}
	}
	snapSmall := o10AvgRoundBytes(t, fullCopy(smallCtx, smallSvc))
	snapLarge := o10AvgRoundBytes(t, fullCopy(largeCtx, largeSvc))
	if snapLarge < snapSmall+512*1024 {
		t.Fatalf("control: deep copy allocates %d bytes with 500 deployments (%d with 1); bound would not detect a full copy", snapLarge, snapSmall)
	}
	borrowSmall := o10AvgRoundBytes(t, func() error { _, err := smallSvc.m.AgentSnapshots(smallCtx, true); return err })
	borrowLarge := o10AvgRoundBytes(t, func() error { _, err := largeSvc.m.AgentSnapshots(largeCtx, true); return err })
	if borrowLarge > borrowSmall+64*1024 {
		t.Fatalf("AgentSnapshots(true) allocates %d bytes with 500 deployments (%d with 1); borrowed reads must not copy histories", borrowLarge, borrowSmall)
	}

	// Live heartbeat transition is still observed and logged through Round without leaking tokens.
	var logs bytes.Buffer
	root, err := logx.Setup(logx.Config{Level: logx.Info, Console: &logs})
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := smallSvc.m.Repository().UpdateSystem(smallCtx, func(_ context.Context, s *model.System) error {
		s.Agents[0].Name = "o10-live-agent synthetic-agent-token synthetic-nested-pass"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	ok, err := smallSvc.m.ObserveHeartbeat(smallCtx, "o10-agent", &model.Agent{LastHeartbeat: time.Now().UTC().Format(time.RFC3339)})
	if err != nil || !ok {
		t.Fatalf("ObserveHeartbeat failed: ok=%v err=%v", ok, err)
	}
	if err := smallSvc.Round(smallCtx); err != nil {
		t.Fatal(err)
	}
	// Reset in-memory runtime state so the next Round observes the stale stored heartbeat.
	agents.ResetRuntime()
	staleMgr, err := agents.New(smallCtx, smallSvc.m.Repository())
	if err != nil {
		t.Fatal(err)
	}
	if err := New(staleMgr, nil, Options{}).Round(smallCtx); err != nil {
		t.Fatal(err)
	}
	out := logs.String()
	if !strings.Contains(out, "已离线") {
		t.Fatalf("expected offline presence transition log, got: %s", out)
	}
	// The undeclared "future" member is dropped on load (O18 T1b), so its
	// password is no credential any more.
	for _, secret := range []string{"synthetic-agent-token"} {
		if strings.Contains(out, secret) {
			t.Fatalf("presence log leaked %q: %s", secret, out)
		}
	}
}
