package agents_test

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"configflow/internal/agents/upgrades"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/logx"
	"configflow/internal/model"
)

// TestReviewN810CorruptHAFollowKeepsHeartbeat: a damaged HA field refuses the
// address follow (no write, no device contact) but the heartbeat still answers
// 200 with its feature flags; the warning is throttled; explicit address
// dialog operations still report the damage as 500.
func TestReviewN810CorruptHAFollowKeepsHeartbeat(t *testing.T) {
	var logs bytes.Buffer
	if _, err := logx.Setup(logx.Config{Level: logx.Info, Console: &logs}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = logx.Setup(logx.Config{Level: logx.LevelFromEnv(), Console: os.Stderr}) })
	a := newUpgradeApp(t)
	upgrades.ResetAddressState()
	t.Cleanup(upgrades.ResetAddressState)
	t.Cleanup(upgrades.RunInlineForTest())
	t.Cleanup(upgrades.SetReachesForTest(func(*model.Agent, string) bool { t.Fatal("damaged HA guard probed device"); return false }))
	t.Cleanup(upgrades.SetAskForTest(func(*model.Agent, string, string, []byte) (int, model.Fields) {
		t.Fatal("damaged HA guard contacted device")
		return 0, model.Fields{}
	}))
	if err := a.m.EditAgents(a.s.Context(), "", func(_ context.Context, rows *[]model.Agent) error {
		if err := (*rows)[0].SetJSON("domain_discovery_enabled", []byte(`true`)); err != nil {
			return err
		}
		return (*rows)[0].SetJSON("oxidns_ha", []byte(`{"enabled":1}`))
	}); err != nil {
		t.Fatal(err)
	}
	before, err := a.s.Repo.Store().Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/agents/" + a.id() + "/heartbeat"
	for i := 0; i < 3; i++ {
		reply := parseBodyMap(t, a.s.Post(path, a.agentAuth(), httpxtest.JSON(map[string]any{"host": "192.0.2.77"})).WantStatus(200))
		if reply["success"] != true || reply["domain_discovery_enabled"] != true || mapHas(reply, "confirmed_host") {
			t.Fatal("heartbeat lost its reply", reply)
		}
	}
	if host := mapStr(a.stored(a.id()), "host"); host != "127.0.0.1" {
		t.Fatal("damaged HA guard followed address", host)
	}
	after, err := a.s.Repo.Store().Fingerprint()
	if err != nil || after != before {
		t.Fatal("refused follow wrote database", err)
	}
	if n := strings.Count(logs.String(), "未跟随新地址"); n != 1 || !strings.Contains(logs.String(), "oxidns_ha.enabled），请修复存储记录后重试；未跟随新地址") {
		t.Fatalf("warning not throttled to one line (%d):\n%s", n, logs.String())
	}
	address := "/api/agents/" + a.id() + "/address"
	for _, r := range []*httpxtest.Result{
		a.s.Get(address, a.admin),
		a.s.Put(address, a.admin, httpxtest.JSON(map[string]any{"host": "192.0.2.77"})),
	} {
		r.WantStatus(500)
		if !strings.Contains(mapStr(parseBodyMap(t, r), "message"), "oxidns_ha.enabled") {
			t.Fatal("dialog hid damaged field", r.Text())
		}
	}
}
