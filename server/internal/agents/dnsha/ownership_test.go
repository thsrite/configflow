package dnsha

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"configflow/internal/agents"
	"configflow/internal/model"
)

func TestNoOrSplitVIPRefusesBeforeCoreMutation(t *testing.T) {
	for _, split := range []bool{false, true} {
		t.Run(map[bool]string{false: "no-holder", true: "split"}[split], func(t *testing.T) {
			f := fixtureNew(t, Options{}, true, nil)
			f.segment.SetHolder("")
			if split {
				f.client.setAfter(func(ctx context.Context, r agents.Request, reply *agents.Response) error {
					if r.Method == "GET" && strings.HasSuffix(r.URL, "/api/oxidns-ha") {
						o := parseObject(reply.Body)
						runtime := o.obj("runtime")
						runtime["vip_present"] = []byte("true")
						o["runtime"] = rawObject(runtime)
						reply.Body = rawObject(o)
					}
					return nil
				})
			}
			f.publish(t, "primary", "unsafe-owner", true)
			last := f.drain(t, 3)
			if last.Status != "failed" || last.Message != msgUniqueHolder || len(f.uploads()) != 0 {
				t.Fatalf("unsafe ownership: %s %s uploads=%d", last.Status, last.Message, len(f.uploads()))
			}
		})
	}
}
func TestHandoffTimeoutReleasesOwnYieldWithoutUpdatingHolder(t *testing.T) {
	now := time.Now()
	f := fixtureNew(t, Options{Now: func() time.Time { return now }}, true, nil)
	f.segment.SetHealth("primary", true, false)
	f.publish(t, "primary", "handoff-timeout", true)
	found := false
	for range 8 {
		if _, e := f.s.Advance(f.ctx, "primary"); e != nil {
			t.Fatal(e)
		}
		a, _ := f.s.get(f.ctx, "primary")
		if a.OxiDNSHAPublication.Handoff != nil {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("handoff timeout fault not armed")
	}
	f.segment.SetHealth("primary", true, true)
	now = now.Add(HandoffTimeout + time.Second)
	last := f.drain(t, 4)
	if last.Status != "failed" || last.Message != msgHandoffTimeout || len(f.uploads()) != 1 {
		t.Fatalf("timeout: %s %s uploads=%d", last.Status, last.Message, len(f.uploads()))
	}
	a, _ := f.s.get(f.ctx, "primary")
	if !yes(a.OxiDNSHAPublication.Handoff.Released) {
		t.Fatal("owned yield not released")
	}
}
func TestRollbackRecoverySettlesFailedPair(t *testing.T) {
	f := fixtureNew(t, Options{}, true, nil)
	f.client.setAfter(func(ctx context.Context, r agents.Request, reply *agents.Response) error {
		if r.Method == "GET" && strings.Contains(r.URL, secondaryHost) && strings.Contains(r.URL, "/api/deployments/") {
			o := parseObject(reply.Body)
			o["status"] = []byte(`"rollback_failed"`)
			o["success"] = []byte("false")
			reply.Body = rawObject(o)
		}
		return nil
	})
	f.publish(t, "primary", "recover-pair", true)
	last := f.drain(t, 6)
	if last.Status != "rollback_failed" {
		t.Fatalf("setup: %s %s", last.Status, last.Message)
	}
	f.client.setAfter(nil)
	last = f.drain(t, 4)
	if last.Status != "failed" || last.Message != msgRecovered {
		t.Fatalf("recovery: %s %s", last.Status, last.Message)
	}
}
func TestIdleReconciliationDoesNotCommit(t *testing.T) {
	f := fixtureNew(t, Options{}, true, nil)
	before := f.r.CommitCount()
	for range 3 {
		if e := f.s.Reconcile(f.ctx); e != nil {
			t.Fatal(e)
		}
	}
	if f.r.CommitCount() != before || len(f.client.calls()) != 0 {
		t.Fatal("empty HA loop wrote or contacted device")
	}
	f.publish(t, "primary", "idle-resume", true)
	for range 12 {
		if e := f.s.Reconcile(f.ctx); e != nil {
			t.Fatal(e)
		}
		a, _ := f.s.get(f.ctx, "primary")
		if a.OxiDNSHAPublication.Status == "succeeded" {
			break
		}
	}
	a, _ := f.s.get(f.ctx, "primary")
	if a.OxiDNSHAPublication.Status != "succeeded" {
		t.Fatal("commit did not wake HA loop")
	}
	before = f.r.CommitCount()
	calls := len(f.client.calls())
	for range 3 {
		if e := f.s.Reconcile(f.ctx); e != nil {
			t.Fatal(e)
		}
	}
	if f.r.CommitCount() != before || len(f.client.calls()) != calls {
		t.Fatal("settled HA loop did not rest")
	}
}
func TestMalformedHandoffFlagsCannotBypassVIPGate(t *testing.T) {
	// A handoff_required flag of another type keeps its truth value (O18 T1b,
	// as Python's job.get('handoff_required')), so only released is malformed.
	for _, field := range []string{"released"} {
		t.Run(field, func(t *testing.T) {
			f := fixtureNew(t, Options{}, true, nil)
			f.publish(t, "primary", "corrupt-proof", true)
			if field == "released" {
				for range 8 {
					if _, e := f.s.Advance(f.ctx, "primary"); e != nil {
						t.Fatal(e)
					}
					a, _ := f.s.get(f.ctx, "primary")
					if a.OxiDNSHAPublication.Handoff != nil {
						break
					}
				}
			}
			e := f.r.UpdateSystem(f.ctx, func(_ context.Context, sys *model.System) error {
				for i := range sys.Agents {
					raw, err := json.Marshal(sys.Agents[i].OxiDNSHAPublication)
					if err != nil {
						return err
					}
					var j map[string]any
					if err := json.Unmarshal(raw, &j); err != nil {
						return err
					}
					if field == "handoff_required" {
						j[field] = "yes"
					} else {
						j["handoff"].(map[string]any)[field] = "yes"
					}
					updated, err := json.Marshal(j)
					if err != nil {
						return err
					}
					if err := model.SetStoredMember(&sys.Agents[i], "oxidns_ha_publication", updated); err != nil {
						return err
					}
				}
				return nil
			})
			if e != nil {
				t.Fatal(e)
			}
			last := f.drain(t, 4)
			if last.Status == "succeeded" || len(f.uploads()) != 1 {
				t.Fatalf("malformed proof bypassed handoff: %s uploads=%d", last.Status, len(f.uploads()))
			}
		})
	}
}
