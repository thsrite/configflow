package dnsha

import (
	"configflow/internal/jsonresp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/agents/agentfake"
	"configflow/internal/agents/monitor"
	"configflow/internal/deploy"
	"configflow/internal/model"
)

func TestStandbyFailuresNeverTouchHolder(t *testing.T) {
	for _, status := range []string{"failed", "rolled_back", "rollback_failed"} {
		t.Run(status, func(t *testing.T) {
			f := fixtureNew(t, Options{}, true, map[string]agentfake.Options{"secondary": {ActivationOutcome: status}})
			f.publish(t, "primary", "standby-failure", true)
			last := f.drain(t, 8)
			want := "failed"
			if status == "rollback_failed" {
				want = "rollback_failed"
			}
			if last.Status != want {
				t.Fatalf("journal: %s %s", last.Status, last.Message)
			}
			if uploads := f.uploads(); len(uploads) != 1 || uploads[0].AgentID != "secondary" {
				t.Fatal(uploads)
			}
		})
	}
}
func TestUnhealthyAndUnknownStandbyGateHolder(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		t.Run(map[bool]string{false: "unhealthy", true: "unknown"}[unknown], func(t *testing.T) {
			f := fixtureNew(t, Options{}, true, nil)
			if unknown {
				f.client.setBefore(func(ctx context.Context, r agents.Request) error {
					if strings.Contains(r.URL, secondaryHost) && strings.HasSuffix(r.URL, "/api/oxidns-ha") && r.Method == "GET" {
						return errors.New("synthetic lost health")
					}
					return nil
				})
			} else {
				f.segment.SetHealth("secondary", false, true)
			}
			f.publish(t, "primary", "health-gate", true)
			last := f.drain(t, 6)
			if unknown {
				if last.Status != "unknown" {
					t.Fatalf("journal: %s %s", last.Status, last.Message)
				}
			} else if last.Status != "failed" {
				t.Fatalf("journal: %s %s", last.Status, last.Message)
			}
			if len(f.uploads()) > 1 {
				t.Fatal("holder updated")
			}
			if unknown {
				f.client.setBefore(nil)
				last = f.drain(t, 12)
				if last.Status != "succeeded" {
					t.Fatalf("journal: %s %s", last.Status, last.Message)
				}
				if len(f.uploads()) != 2 {
					t.Fatal("lost health caused duplicate upload")
				}
			}
		})
	}
}
func TestLostUploadAndHandoffRepliesResumeWithoutReposting(t *testing.T) {
	for _, point := range []string{"upload", "yield", "release"} {
		t.Run(point, func(t *testing.T) {
			f := fixtureNew(t, Options{}, true, nil)
			lost := false
			f.client.setAfter(func(ctx context.Context, r agents.Request, _ *agents.Response) error {
				u, _ := url.Parse(r.URL)
				hit := point == "upload" && r.Method == "POST" && u.Path == "/api/deployments" || point == "yield" && r.Method == "POST" && u.Path == "/api/oxidns-ha/handoff" || point == "release" && r.Method == "DELETE" && u.Path == "/api/oxidns-ha/handoff"
				if hit && !lost {
					lost = true
					return errors.New("synthetic accepted response lost")
				}
				return nil
			})
			f.publish(t, "primary", "lost-response", true)
			for range 10 {
				if lost {
					break
				}
				if _, e := f.s.Advance(f.ctx, "primary"); e != nil {
					t.Fatal(e)
				}
			}
			if !lost {
				t.Fatal("fault never fired")
			}
			m, e := agents.New(f.ctx, f.r)
			if e != nil {
				t.Fatal(e)
			}
			t.Cleanup(m.Close)
			m.SetClient(f.client)
			f.m = m
			f.s = New(m, Options{})
			services.Lock()
			services.byManager[m] = f.s
			services.Unlock()
			last := f.drain(t, 14)
			if last.Status != "succeeded" || len(f.uploads()) != 2 {
				t.Fatal(last, len(f.uploads()))
			}
			posts := 0
			deletes := 0
			for _, c := range f.segment.Calls() {
				if c.Path == "/api/oxidns-ha/handoff" {
					if c.Method == "POST" {
						posts++
					}
					if c.Method == "DELETE" {
						deletes++
					}
				}
			}
			if posts != 1 || deletes != 1 {
				t.Fatal("non-idempotent handoff", posts, deletes)
			}
		})
	}
}
func TestStagedActivationRechecksActualHolder(t *testing.T) {
	f := fixtureNew(t, Options{}, true, nil)
	f.publish(t, "primary", "staged", false)
	last := f.drain(t, 10)
	if last.Status != "ready" {
		t.Fatalf("journal: %s %s", last.Status, last.Message)
	}
	f.segment.SetHolder("secondary")
	r, e := f.s.ActivatePair(f.ctx, "secondary", childID("primary", "staged", "secondary"))
	if e != nil {
		t.Fatal(e)
	}
	if r.Status == "failed" {
		t.Fatal(r)
	}
	last = f.drain(t, 12)
	if last.Status != "succeeded" {
		t.Fatalf("journal: %s %s", last.Status, last.Message)
	}
	activations := []string{}
	for _, c := range f.segment.Calls() {
		if c.Method == "POST" && strings.HasSuffix(c.Path, "/activate") {
			activations = append(activations, c.AgentID)
		}
	}
	if !reflect.DeepEqual(activations, []string{"primary", "secondary"}) {
		t.Fatal(activations)
	}
	if len(f.uploads()) != 2 {
		t.Fatal("activation reuploaded")
	}
}
func TestHandoffKeepsReservationUntilCleanup(t *testing.T) {
	f := fixtureNew(t, Options{}, true, nil)
	f.client.setBefore(func(ctx context.Context, r agents.Request) error {
		if r.Method == "DELETE" && strings.HasSuffix(r.URL, "/handoff") {
			return errors.New("synthetic cleanup unavailable")
		}
		return nil
	})
	f.publish(t, "primary", "cleanup", true)
	last := f.drain(t, 12)
	if last.Status != "unknown" {
		t.Fatalf("journal: %s %s", last.Status, last.Message)
	}
	called := false
	rawReply, e := f.s.ServiceOperationJSON(f.ctx, "secondary", func() (json.RawMessage, error) { called = true; return nil, nil }, false)
	var statusReply struct {
		HTTPStatus int `json:"http_status"`
	}
	_ = json.Unmarshal(rawReply, &statusReply)
	if e != nil || called || statusReply.HTTPStatus != 409 {
		t.Fatal(string(rawReply), e, called)
	}
	_, e = f.s.Disable(f.ctx, "primary", true)
	if !IsUserError(e) {
		t.Fatal("force cleared active reservation", e)
	}
	f.client.setBefore(nil)
	last = f.drain(t, 8)
	if last.Status != "succeeded" {
		t.Fatalf("journal: %s %s", last.Status, last.Message)
	}
}
func TestHandoffRequiresTwoFreshHealthyConfirmations(t *testing.T) {
	f := fixtureNew(t, Options{}, true, nil)
	f.segment.SetHealth("primary", true, false)
	f.publish(t, "primary", "two-proofs", true)
	last := f.drain(t, 5)
	if last.Status != "checking" || len(f.uploads()) != 1 {
		t.Fatal(last, len(f.uploads()))
	}
	a, _ := f.s.get(f.ctx, "primary")
	if a.OxiDNSHAPublication.Handoff.Confirmations != 0 {
		t.Fatal("unsafe VIP DNS was confirmed")
	}
	f.segment.SetHealth("primary", true, true)
	if _, e := f.s.Advance(f.ctx, "primary"); e != nil {
		t.Fatal(e)
	}
	a, _ = f.s.get(f.ctx, "primary")
	if a.OxiDNSHAPublication.Handoff.Confirmations != 1 || len(f.uploads()) != 1 {
		t.Fatal("single confirmation touched holder")
	}
	last = f.drain(t, 12)
	if last.Status != "succeeded" {
		t.Fatalf("journal: %s %s", last.Status, last.Message)
	}
}
func TestMissingHandoffProtocolAndListenerRefuseBeforeWrites(t *testing.T) {
	f := fixtureNew(t, Options{}, true, map[string]agentfake.Options{"secondary": {Capabilities: json.RawMessage(`{"deployment_protocols":[1],"oxidns_ha_protocols":[1],"service_type":"oxidns","config_path":"config.yaml"}`)}})
	f.publish(t, "primary", "unsupported", true)
	last := f.drain(t, 6)
	if last.Status != "failed" || len(f.uploads()) != 0 {
		t.Fatalf("journal: %s %s", last.Status, last.Message)
	}
	if !strings.Contains(last.Message, fmt.Sprintf(msgHandoffUpgrade, "DNS secondary")) {
		t.Fatal(last.Message)
	}
}
func TestDisableForceAndUndoKeepRecordsAtomic(t *testing.T) {
	for _, undoFails := range []bool{false, true} {
		t.Run(map[bool]string{false: "undo-complete", true: "leftover"}[undoFails], func(t *testing.T) {
			f := fixtureNew(t, Options{}, false, nil)
			f.client.setBefore(func(ctx context.Context, r agents.Request) error {
				if strings.Contains(r.URL, primaryHost) && r.Method == "PUT" && strings.HasSuffix(r.URL, "/api/oxidns-ha") {
					return errors.New("synthetic apply refused")
				}
				if undoFails && r.Method == "DELETE" && strings.Contains(r.URL, secondaryHost) {
					return errors.New("synthetic undo unavailable")
				}
				return nil
			})
			_, e := f.s.Configure(f.ctx, "primary", []byte(`{"peer_id":"secondary","vip":"192.0.2.53"}`), "https://panel.example")
			if e != nil {
				t.Fatal(e)
			}
			last := f.drain(t, 12)
			if last.Status != "failed" {
				t.Fatalf("journal: %s %s", last.Status, last.Message)
			}
			a, _ := f.s.get(f.ctx, "primary")
			b, _ := f.s.get(f.ctx, "secondary")
			if haEnabled(a) || haEnabled(b) {
				t.Fatal("half-enabled pair")
			}
			if undoFails {
				if a.OxiDNSHAState == nil || b.OxiDNSHAState == nil {
					t.Fatal("missing leftover marker")
				}
				_, e = f.s.Disable(f.ctx, "primary", false)
				var he *Error
				if !errors.As(e, &he) || !he.CanForce {
					t.Fatal(e)
				}
				if _, e = f.s.Disable(f.ctx, "primary", true); e != nil {
					t.Fatal(e)
				}
			} else if a.OxiDNSHAState != nil || b.OxiDNSHAState != nil {
				t.Fatal("undone first enable left HA record")
			}
		})
	}
}
func TestKeepalivedBusyRetriesNextRound(t *testing.T) {
	f := fixtureNew(t, Options{}, false, nil)
	_, e := f.s.Configure(f.ctx, "primary", []byte(`{"peer_id":"secondary","vip":"192.0.2.53"}`), "https://panel.example")
	if e != nil {
		t.Fatal(e)
	}
	var unlock func()
	f.client.setAfter(func(ctx context.Context, r agents.Request, _ *agents.Response) error {
		if unlock == nil && strings.Contains(r.URL, primaryHost) && r.Method == "GET" && strings.Contains(r.URL, "/api/deployments/") {
			_, release, ok := haLocks.TryLock(context.Background(), f.r.DataDir())
			if ok {
				unlock = release
			}
		}
		return nil
	})
	last := f.drain(t, 8)
	if unlock == nil {
		t.Fatal("busy fault never fired")
	}
	defer func() {
		if unlock != nil {
			unlock()
		}
	}()
	if last.Status != "applying" || last.Phase != "keepalived" {
		t.Fatalf("journal: %s %s", last.Status, last.Message)
	}
	for _, c := range f.segment.Calls() {
		if c.Method == "PUT" {
			t.Fatal("busy HA lock wrote a device")
		}
	}
	unlock()
	unlock = nil
	f.client.setAfter(nil)
	last = f.drain(t, 5)
	if last.Status != "succeeded" {
		t.Fatalf("journal: %s %s", last.Status, last.Message)
	}
	if len(f.uploads()) != 2 {
		t.Fatal("retry reuploaded cores")
	}
}
func TestRulesHookUsesPairReceiptAndForcedReload(t *testing.T) {
	f := fixtureNew(t, Options{}, true, nil)
	mon := monitor.New(f.m.Repository().DataDir())
	if err := mon.SetHARules(f.s.PairRulesJSON); err != nil {
		t.Fatal(err)
	}
	r, code, e := mon.CallRuleUpdate(f.ctx, monitor.TypedFromAgents(f.m), "primary", "GET", nil)
	if e != nil || code != 200 || ruleUpdateStatus(t, r) != "idle" {
		t.Fatal(r, code, e)
	}
	f.publish(t, "primary", "baseline", true)
	if f.drain(t, 12).Status != "succeeded" {
		t.Fatal("baseline")
	}
	before := len(f.uploads())
	r, code, e = mon.CallRuleUpdate(f.ctx, monitor.TypedFromAgents(f.m), "secondary", "POST", nil)
	if e != nil {
		t.Fatal(r, code, e)
	}
	last := f.drain(t, 12)
	if last.Status != "succeeded" {
		t.Fatalf("journal: %s %s", last.Status, last.Message)
	}
	uploads := f.uploads()
	if len(uploads) != before+2 {
		t.Fatal("rules did not publish pair", len(uploads))
	}
	for _, c := range uploads[before:] {
		var m struct {
			Reload bool `json:"reload_only"`
		}
		_ = m
		fake := f.devices[c.AgentID]
		found := false
		for _, call := range fake.Calls() {
			if call.Method == "POST" && call.Path == "/api/deployments" && call.Manifest != nil && call.Manifest.ReloadOnly {
				found = true
			}
		}
		if !found {
			t.Fatal("rules update did not force reload-only")
		}
	}
	a, _ := f.s.get(f.ctx, "primary")
	if yes(a.OxiDNSHAPublication.HandoffRequired) {
		t.Fatal("rule update restarted core")
	}
	r, code, e = mon.CallRuleUpdate(f.ctx, monitor.TypedFromAgents(f.m), "primary", "GET", nil)
	if e != nil || code != 200 || ruleUpdateStatus(t, r) != "succeeded" {
		t.Fatal(r, code, e)
	}
}
func TestIdentityChangeAndPayloadTamperFailClosed(t *testing.T) {
	for _, change := range []string{"identity", "payload"} {
		t.Run(change, func(t *testing.T) {
			f := fixtureNew(t, Options{}, false, nil)
			_, e := f.s.Configure(f.ctx, "primary", []byte(`{"peer_id":"secondary","vip":"192.0.2.53"}`), "")
			if e != nil {
				t.Fatal(e)
			}
			a, _ := f.s.get(f.ctx, "primary")
			if change == "identity" {
				e = f.r.UpdateSystem(f.ctx, func(_ context.Context, sys *model.System) error {
					for i := range sys.Agents {
						if sys.Agents[i].ID == "secondary" {
							model.ZeroMember(&sys.Agents[i], "host")
						}
					}
					return nil
				})
			} else {
				p, _, err := f.s.readPayload(f.ctx, a.OxiDNSHAPublication)
				if err != nil {
					t.Fatal(err)
				}
				p.ID = "another-job"
				e = deploy.WriteFileAtomic(f.ctx, f.s.folder(a.OxiDNSHAPublication)+"/payload.json", jsonresp.Value(p))
			}
			if e != nil {
				t.Fatal(e)
			}
			last := f.drain(t, 3)
			if last.Status != "failed" || len(f.uploads()) != 0 {
				t.Fatalf("journal: %s %s", last.Status, last.Message)
			}
		})
	}
}

func ruleUpdateStatus(t *testing.T, result *monitor.RuleUpdateResult) string {
	t.Helper()
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		Task struct {
			Status string `json:"status"`
		} `json:"task"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	return body.Task.Status
}
