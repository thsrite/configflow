package agents_test

import (
	"context"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/agents/upgrades"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/jsondecode"
	"configflow/internal/jsonx"
	"configflow/internal/model"
)

func legacyUpgradeObject(value any) *jsonx.Object {
	raw, _ := model.Marshal(value)
	parsed, _ := jsonx.Parse(raw)
	out, _ := parsed.(*jsonx.Object)
	return out
}
func legacyUpgradeSetRequestForTest(fn func(*jsonx.Object, string, string, *jsonx.Object) (*agents.Response, error)) func() {
	return upgrades.SetRequestForTest(func(_ context.Context, a *model.Agent, method, path string, body []byte) (*agents.Response, error) {
		var payload *jsonx.Object
		if len(body) > 0 {
			v, _ := jsonx.Parse(body)
			payload, _ = v.(*jsonx.Object)
		}
		return fn(legacyUpgradeObject(a), method, path, payload)
	})
}
func legacyUpgradeSetReachesForTest(fn func(*jsonx.Object, string) bool) func() {
	return upgrades.SetReachesForTest(func(a *model.Agent, host string) bool { return fn(legacyUpgradeObject(a), host) })
}
func legacyUpgradeSetAskForTest(fn func(*jsonx.Object, string, string, *jsonx.Object) (int, *jsonx.Object)) func() {
	return upgrades.SetAskForTest(func(a *model.Agent, host, method string, body []byte) (int, model.Fields) {
		var payload *jsonx.Object
		if len(body) > 0 {
			v, _ := jsonx.Parse(body)
			payload, _ = v.(*jsonx.Object)
		}
		status, data := fn(legacyUpgradeObject(a), host, method, payload)
		raw, _ := jsonx.Compact(data)
		var fields model.Fields
		_ = jsondecode.Unmarshal(raw, &fields)
		return status, fields
	})
}
func legacyUpgradeAgentResponse(payload any, status int) *agents.Response {
	raw, _ := jsonx.Default(payload)
	return &agents.Response{StatusCode: status, Header: map[string][]string{"Content-Type": {"application/json"}}, Body: raw}
}

func TestN810TypedHeartbeatHooksTakePrecedence(t *testing.T) {
	h := newHeartbeat(t)
	followed, confirmed := 0, 0
	agents.RegisterHooks(agents.Hooks{
		AddressFollow: func(context.Context, *agents.Manager, *jsonx.Object, string) error {
			t.Fatal("legacy follow double-triggered")
			return nil
		},
		AddressConfirmed: func(*jsonx.Object, string) string { t.Fatal("legacy confirmation double-triggered"); return "" },
		AddressFollowTyped: func(_ context.Context, _ *agents.Manager, a *model.Agent, host string) error {
			followed++
			if a.ID != h.id || host != "192.0.2.50" {
				t.Fatal("typed identity lost")
			}
			return nil
		},
		AddressConfirmedTyped: func(_ *model.Agent, host string) string { confirmed++; return host },
	})
	h.s.Post(h.path(), httpxtest.Bearer(h.token), httpxtest.JSON(jsonx.Obj("host", "192.0.2.50"))).WantStatus(200)
	if followed != 1 || confirmed != 1 {
		t.Fatal("typed heartbeat hooks did not run exactly once", followed, confirmed)
	}
}

func TestN810CorruptUpgradeDoesNotBreakAgentList(t *testing.T) {
	for _, raw := range []string{`"abc"`, `5`, `[["status","succeeded"],["target_version","9.9.9-go"],"ok"]`} {
		t.Run(raw, func(t *testing.T) {
			a := newUpgradeApp(t)
			if err := a.m.EditAgents(a.s.Context(), "", func(_ context.Context, rows *[]model.Agent) error {
				return (*rows)[0].SetJSON("latest_upgrade", []byte(raw))
			}); err != nil {
				t.Fatal(err)
			}
			before, err := a.s.Repo.Store().Fingerprint()
			if err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{"/api/agents", "/api/agents?summary=1"} {
				items, ok := a.s.Get(path, a.admin).WantStatus(200).JSON().([]any)
				if !ok || len(items) != 2 {
					t.Fatal("bad Agent hid the list", items)
				}
				found := false
				for _, item := range items {
					record := item.(*jsonx.Object)
					if record.GetString("id") == a.other.GetString("id") {
						found = record.GetString("name") == "other-agent"
					}
				}
				if !found {
					t.Fatal("healthy Agent was lost")
				}
			}
			a.s.Get("/api/agents/"+a.id()+"/upgrade", a.admin).WantStatus(500)
			after, err := a.s.Repo.Store().Fingerprint()
			if err != nil || after != before {
				t.Fatal("corrupt upgrade list/poll wrote database", err)
			}
		})
	}
}

func TestN810TypedAddressSnapshotPreservesHeartbeatPreferences(t *testing.T) {
	h := newHeartbeat(t)
	manager, err := agents.Get(h.s.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.EditAgents(h.s.Context(), "", func(_ context.Context, rows *[]model.Agent) error {
		if err := (*rows)[0].SetJSON("domain_discovery_enabled", []byte(`"enabled"`)); err != nil {
			return err
		}
		return (*rows)[0].SetJSON("traffic_history_enabled", []byte(`1`))
	}); err != nil {
		t.Fatal(err)
	}
	result := h.s.Post(h.path(), httpxtest.Bearer(h.token), httpxtest.JSON(jsonx.Obj())).WantStatus(200).Object()
	if result.Value("domain_discovery_enabled") != true || result.Value("traffic_history_enabled") != true {
		t.Fatal("typed snapshot changed existing heartbeat preferences", result)
	}
}
