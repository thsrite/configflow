package dnsha

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"configflow/internal/agents"
	"configflow/internal/model"
)

// lostClock drives a fixture whose standby (the new VIP holder) is lost after
// the confirmed handoff, until the publication stops with msgMemberLost.
type lostClock struct {
	f   *fixture
	now time.Time
}

func newLostPair(t *testing.T) *lostClock {
	t.Helper()
	c := &lostClock{now: time.Now()}
	c.f = fixtureNew(t, Options{Now: func() time.Time { return c.now }}, true, nil)
	c.stop(t, "member-lost")
	return c
}

// stop publishes did (unless a publication is already running), loses the
// standby after the confirmed handoff and lets the lost-member stop settle.
func (c *lostClock) stop(t *testing.T, did string) {
	t.Helper()
	f := c.f
	if did != "" {
		f.publish(t, "primary", did, true)
	}
	confirmed := false
	for range 12 {
		if _, e := f.s.Advance(f.ctx, "primary"); e != nil {
			t.Fatal(e)
		}
		j := c.journal(t)
		if h := j.Handoff; h != nil && yes(h.Confirmed) {
			confirmed = true
			break
		}
	}
	if !confirmed {
		t.Fatal("handoff never confirmed")
	}
	c.offline("secondary")
	if last := f.drain(t, 3); last.Status != "unknown" {
		t.Fatalf("lost member: %s %s", last.Status, last.Message)
	}
	c.now = c.now.Add(MemberLostTimeout + time.Second)
	f.drain(t, 2)
	c.now = c.now.Add(HandoffTimeout + time.Second)
	last := f.drain(t, 4)
	if last.Status != "failed" || last.Message != msgMemberLost {
		t.Fatalf("stop: %s %s", last.Status, last.Message)
	}
	j := c.journal(t)
	if j.AutoComplete == nil || j.AutoComplete.Status != autoWaiting || j.AutoCompleteWatch == nil || strings.Join(j.AutoCompleteWatch.Lost, ",") != "secondary" {
		t.Fatalf("waiting state: %+v %+v", j.AutoComplete, j.AutoCompleteWatch)
	}
	if last.AutoComplete == nil || last.AutoComplete.Message != msgAutoWaiting {
		t.Fatalf("public waiting state: %+v", last.AutoComplete)
	}
}

// offline cuts every listed member off the loopback network (none: all back).
func (c *lostClock) offline(ids ...string) {
	hosts := map[string]string{"primary": primaryHost, "secondary": secondaryHost}
	if len(ids) == 0 {
		c.f.client.setBefore(nil)
		return
	}
	c.f.client.setBefore(func(_ context.Context, r agents.Request) error {
		for _, id := range ids {
			if strings.Contains(r.URL, hosts[id]) {
				return errors.New("synthetic member offline")
			}
		}
		return nil
	})
}
func (c *lostClock) journal(t *testing.T) *model.HAJournal {
	t.Helper()
	a, e := c.f.s.get(c.f.ctx, "primary")
	if e != nil {
		t.Fatal(e)
	}
	return a.OxiDNSHAPublication
}

// beat reports one healthy heartbeat of id, then runs one owner round.
func (c *lostClock) beat(t *testing.T, s *Service, id string) {
	t.Helper()
	if _, e := c.f.m.ObserveHeartbeat(c.f.ctx, id, agentModelForTest(t, map[string]any{"service_status": "running"})); e != nil {
		t.Fatal(e)
	}
	if e := s.AutoComplete(c.f.ctx, "primary"); e != nil {
		t.Fatal(e)
	}
}
func (c *lostClock) uploads() int { return len(c.f.uploads()) }
func (c *lostClock) fingerprint(t *testing.T, id string) string {
	t.Helper()
	a, e := c.f.s.get(c.f.ctx, id)
	if e != nil {
		t.Fatal(e)
	}
	if a.AppliedContent == nil {
		return ""
	}
	return a.AppliedContent.Fingerprint
}

// completed asserts that the publication after recovery is the automatic
// completion and that it succeeded with the interrupted content.
func (c *lostClock) completed(t *testing.T, stopped string, content string) {
	t.Helper()
	last := c.f.drain(t, 16)
	j := c.journal(t)
	if last.Status != "succeeded" || j.ID == stopped || j.AutoComplete == nil || !j.AutoComplete.Attempt {
		t.Fatalf("auto completion: %s %s %+v", last.Status, last.Message, j.AutoComplete)
	}
	if last.AutoComplete == nil || !last.AutoComplete.Attempt {
		t.Fatalf("history mark: %+v", last.AutoComplete)
	}
	for _, id := range []string{"primary", "secondary"} {
		if got := c.fingerprint(t, id); got != content {
			t.Fatalf("%s applied %s, want interrupted content %s", id, got, content)
		}
	}
}

func TestAutoCompleteAfterRecovery(t *testing.T) {
	c := newLostPair(t)
	stopped := c.journal(t).ID
	content := c.fingerprint(t, "secondary")
	if content == "" || content == c.fingerprint(t, "primary") {
		t.Fatalf("setup: standby %q primary %q", content, c.fingerprint(t, "primary"))
	}
	uploads := c.uploads()
	c.offline()
	// Negative control: one healthy heartbeat is not enough.
	c.beat(t, c.f.s, "secondary")
	if j := c.journal(t); j.ID != stopped || j.AutoComplete.Status != autoWaiting || c.uploads() != uploads {
		t.Fatalf("published after one heartbeat: %+v uploads %d", j.AutoComplete, c.uploads())
	}
	// A restarted owner keeps the evidence: the second heartbeat completes.
	restarted := New(c.f.m, Options{Now: func() time.Time { return c.now }})
	c.beat(t, restarted, "secondary")
	if c.uploads() == uploads {
		t.Fatal("no publication after two healthy heartbeats")
	}
	c.completed(t, stopped, content)
	// Only once: further rounds do not publish again.
	uploads = c.uploads()
	for range 3 {
		c.beat(t, c.f.s, "secondary")
		if e := c.f.s.Reconcile(c.f.ctx); e != nil {
			t.Fatal(e)
		}
	}
	if c.uploads() != uploads {
		t.Fatal("completed pair published again")
	}
}

func TestAutoCompleteDoesNotStart(t *testing.T) {
	tests := []struct {
		name string
		// before runs after the stop and before the recovery heartbeats.
		before func(t *testing.T, c *lostClock)
		// online lists the members reachable during the heartbeats.
		online []string
	}{
		{"disabled", func(t *testing.T, c *lostClock) {
			if _, e := c.f.s.Disable(c.f.ctx, "primary", true); e != nil {
				t.Fatal(e)
			}
		}, []string{"primary", "secondary"}},
		{"manual-publication", func(t *testing.T, c *lostClock) {
			c.offline()
			c.f.publish(t, "primary", "manual-after-stop", true)
			if last := c.f.drain(t, 16); last.Status != "succeeded" {
				t.Fatalf("manual publication: %s %s", last.Status, last.Message)
			}
		}, []string{"primary", "secondary"}},
		{"both-offline", nil, nil},
		{"only-holder-back", nil, []string{"primary"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newLostPair(t)
			if tt.before != nil {
				tt.before(t, c)
			}
			journal := c.journal(t)
			uploads := c.uploads()
			switch len(tt.online) {
			case 2:
				c.offline()
			case 1:
				c.offline("secondary")
			default:
				c.offline("primary", "secondary")
			}
			for range 3 {
				c.beat(t, c.f.s, "secondary")
				c.beat(t, c.f.s, "primary")
			}
			if c.uploads() != uploads {
				t.Fatalf("auto completion published (%d -> %d uploads)", uploads, c.uploads())
			}
			after := c.journal(t)
			if journal == nil {
				if after != nil {
					t.Fatalf("journal reappeared: %+v", after)
				}
				return
			}
			if after.ID != journal.ID {
				t.Fatal("journal replaced")
			}
			if len(tt.online) < 2 && after.AutoComplete.Status != autoWaiting {
				t.Fatalf("still waiting expected: %+v", after.AutoComplete)
			}
			// Negative control: the same pair completes once both are back.
			if len(tt.online) < 2 {
				c.offline()
				c.beat(t, c.f.s, "secondary")
				c.beat(t, c.f.s, "secondary")
				if c.uploads() == uploads {
					t.Fatal("control: recovery did not complete")
				}
			}
		})
	}
}

func TestAutoCompleteBlockedContent(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, c *lostClock)
		want  string
	}{
		{"payload-corrupt", func(t *testing.T, c *lostClock) {
			path := filepath.Join(c.f.s.folder(c.journal(t)), "bundle.tar.gz")
			if e := os.WriteFile(path, []byte("synthetic damage"), 0o600); e != nil {
				t.Fatal(e)
			}
		}, msgAutoPayload},
		{"expired", func(t *testing.T, c *lostClock) { c.now = c.now.Add(AutoCompleteMaxAge) }, msgAutoExpired},
		{"agent-unsupported", func(t *testing.T, c *lostClock) {
			c.f.client.setAfter(func(_ context.Context, r agents.Request, reply *agents.Response) error {
				if strings.Contains(r.URL, secondaryHost) && strings.HasSuffix(r.URL, "/api/capabilities") {
					o := parseObject(reply.Body)
					delete(o, "oxidns_ha_protocols")
					reply.Body = rawObject(o)
				}
				return nil
			})
		}, "DNS secondary 的 Agent 不支持主备发布，请先升级"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newLostPair(t)
			uploads := c.uploads()
			c.offline()
			tt.setup(t, c)
			c.beat(t, c.f.s, "secondary")
			c.beat(t, c.f.s, "secondary")
			j := c.journal(t)
			want := strings.Replace(msgAutoBlocked, "%s", tt.want, 1)
			if c.uploads() != uploads || j.AutoComplete.Status != autoBlocked || j.AutoComplete.Message != want || j.Status != "failed" {
				t.Fatalf("blocked: %+v uploads %d->%d", j.AutoComplete, uploads, c.uploads())
			}
			// Blocked is final: later healthy heartbeats do not publish.
			c.beat(t, c.f.s, "secondary")
			c.beat(t, c.f.s, "secondary")
			if c.uploads() != uploads {
				t.Fatal("blocked pair published")
			}
		})
	}
}

func TestAutoCompleteFailureIsNotRetried(t *testing.T) {
	c := newLostPair(t)
	stopped := c.journal(t).ID
	c.offline()
	// The completing publication fails on the standby; the stopped one did not.
	c.f.client.setAfter(func(_ context.Context, r agents.Request, reply *agents.Response) error {
		if r.Method == "GET" && strings.Contains(r.URL, secondaryHost) && strings.Contains(r.URL, "/api/deployments/") && !strings.Contains(r.URL, childID("primary", "member-lost", "secondary")) {
			o := parseObject(reply.Body)
			o["status"] = []byte(`"failed"`)
			o["success"] = []byte("false")
			reply.Body = rawObject(o)
		}
		return nil
	})
	c.beat(t, c.f.s, "secondary")
	c.beat(t, c.f.s, "secondary")
	last := c.f.drain(t, 8)
	j := c.journal(t)
	if j.ID == stopped || last.Status != "failed" || j.AutoComplete == nil || !j.AutoComplete.Attempt || awaitingCompletion(j) {
		t.Fatalf("failed completion: %s %s %+v", last.Status, last.Message, j.AutoComplete)
	}
	uploads := c.uploads()
	for range 3 {
		c.beat(t, c.f.s, "secondary")
		if e := c.f.s.Reconcile(c.f.ctx); e != nil {
			t.Fatal(e)
		}
	}
	if c.uploads() != uploads || c.journal(t).ID != j.ID {
		t.Fatal("failed completion was retried")
	}
}

func TestAutoCompleteOncePerLoss(t *testing.T) {
	c := newLostPair(t)
	first := c.journal(t).ID
	c.offline()
	c.beat(t, c.f.s, "secondary")
	c.beat(t, c.f.s, "secondary")
	second := c.journal(t)
	if second.ID == first || second.AutoComplete == nil || !second.AutoComplete.Attempt {
		t.Fatalf("first completion did not start: %+v", second.AutoComplete)
	}
	// The completing publication loses the standby again: one more stop,
	// one more completion.
	c.stop(t, "")
	if c.journal(t).ID != second.ID {
		t.Fatal("second stop is another journal")
	}
	c.offline()
	c.beat(t, c.f.s, "secondary")
	c.beat(t, c.f.s, "secondary")
	third := c.journal(t)
	if third.ID == second.ID || !third.AutoComplete.Attempt {
		t.Fatalf("second completion did not start: %+v", third.AutoComplete)
	}
	if last := c.f.drain(t, 16); last.Status != "succeeded" {
		t.Fatalf("second completion: %s %s", last.Status, last.Message)
	}
}

func TestAutoCompletePublishesInterruptedContent(t *testing.T) {
	c := newLostPair(t)
	stopped := c.journal(t).ID
	content := c.fingerprint(t, "secondary")
	// The configuration changes while the member is away.
	_, e := c.f.r.UpdateProfile(c.f.ctx, "default", func(_ context.Context, doc *model.Profile) error {
		raw, err := json.Marshal(map[string]any{"config_mode": "native", "native_config": strings.Replace(config, "reject 3", "reject 5", 1)})
		if err != nil {
			return err
		}
		return doc.SetJSON("oxidns", raw)
	})
	if e != nil {
		t.Fatal(e)
	}
	current := c.f.bundle(t, "primary", "current-config", false)
	c.offline()
	c.beat(t, c.f.s, "secondary")
	c.beat(t, c.f.s, "secondary")
	c.completed(t, stopped, content)
	// Negative control: the newer configuration is a different, unpublished content.
	if c.fingerprint(t, "primary") == "" || current.ConfigVersion() == "" {
		t.Fatal("setup: missing content identities")
	}
	a, e := c.f.s.get(c.f.ctx, "primary")
	if e != nil {
		t.Fatal(e)
	}
	if a.AppliedContent.ConfigVersion == current.ConfigVersion() {
		t.Fatal("auto completion published the newer configuration")
	}
}

// The completion evaluates under one lease and reserves under another; a
// manual publication that lands in between replaces the interrupted intent
// and the completion must not publish the old content over it (HR batch-31).
func TestAutoCompleteYieldsToPublicationBetweenLeases(t *testing.T) {
	c := newLostPair(t)
	c.offline()
	c.beat(t, c.f.s, "secondary")
	if _, e := c.f.m.ObserveHeartbeat(c.f.ctx, "secondary", agentModelForTest(t, map[string]any{"service_status": "running"})); e != nil {
		t.Fatal(e)
	}
	next, e := c.f.s.evaluate(c.f.ctx, "primary")
	if e != nil || next == nil {
		t.Fatalf("evaluate: %v %v", next, e)
	}
	if j := c.journal(t); j.AutoComplete.Status != autoRunning {
		t.Fatalf("running state: %+v", j.AutoComplete)
	}
	c.f.publish(t, "primary", "manual-between-leases", true)
	if last := c.f.drain(t, 16); last.Status != "succeeded" {
		t.Fatalf("manual publication: %s %s", last.Status, last.Message)
	}
	manual := c.journal(t).ID
	uploads := c.uploads()
	_, e = next.run(c.f.ctx)
	if !IsUserError(e) || !strings.Contains(e.Error(), msgJournalChanged) {
		t.Fatalf("stale completion: %v", e)
	}
	if j := c.journal(t); j.ID != manual || c.uploads() != uploads || j.AutoComplete != nil {
		t.Fatalf("stale completion published: journal %s uploads %d->%d %+v", j.ID, uploads, c.uploads(), j.AutoComplete)
	}
}
