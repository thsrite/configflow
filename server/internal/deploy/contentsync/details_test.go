package contentsync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"configflow/internal/deploy"
	"configflow/internal/deploy/pushdiff"
	"configflow/internal/jsonx"
)

func answerFields(t *testing.T, a Answer) map[string]json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]json.RawMessage
	if err = json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	return result
}
func versionOf(t *testing.T, a Answer) string {
	t.Helper()
	fields := answerFields(t, a)
	var version string
	if err := json.Unmarshal(fields["version"], &version); err != nil {
		t.Fatal(err)
	}
	return version
}
func TestSharedDetailsExpiryPagingAndSearch(t *testing.T) {
	f := newFixture(t, nil)
	var rules strings.Builder
	for i := 0; i < 80; i++ {
		fmt.Fprintf(&rules, "DOMAIN,new-%03d.example.test\n", i)
	}
	f.cfg.Set("test_rule", rules.String())
	f.status()
	f.spawn.run(t)
	a := f.diff()
	requireStatus(t, a.Result, "changed", "")
	version := versionOf(t, a)
	if f.calls != 1 || f.spawn.count() != 0 {
		t.Fatal("detail caused another preparation")
	}
	query := EntriesQuery{Version: version, Path: "rules/example.list", Change: "added", Offset: 20, Limit: 50}
	page, err := f.c.Entries(f.ctx, f.target(), f.source(), "https://config.example.test", query)
	if err != nil || page.Status != "ok" || page.Total != 80 || len(page.Entries) != 50 {
		t.Fatalf("%+v %v", page, err)
	}
	query.Query = pushdiff.SearchQuery("new-001")
	query.Offset = 0
	page, err = f.c.Entries(f.ctx, f.target(), f.source(), "https://config.example.test", query)
	if err != nil || page.Total != 1 {
		t.Fatalf("search %+v %v", page, err)
	}
	query.Change = "modified"
	if _, err = f.c.Entries(f.ctx, f.target(), f.source(), "https://config.example.test", query); !errors.Is(err, ErrNoEntries) {
		t.Fatal(err)
	}
	query.Change = "added"
	query.Version = "old"
	page, _ = f.c.Entries(f.ctx, f.target(), f.source(), "https://config.example.test", query)
	if page.Reason != "replaced" {
		t.Fatal(page)
	}
	// Input mismatch wins over version mismatch, and expiration wins over both.
	f.cfg.Set("profile_name", "renamed")
	page, _ = f.c.Entries(f.ctx, f.target(), f.source(), "https://config.example.test", query)
	if page.Reason != "changed" {
		t.Fatal(page)
	}
	f.clock.Advance(SuccessTTL)
	page, _ = f.c.Entries(f.ctx, f.target(), f.source(), "https://config.example.test", query)
	if page.Reason != "expired" {
		t.Fatal(page)
	}
}
func TestDetailFailuresRetryWithoutDiscardingStatus(t *testing.T) {
	fail := true
	f := newFixture(t, func(o *Options) {
		o.LoadReceipt = func(context.Context, deploy.Target) (*deploy.Receipt, error) {
			if fail {
				return nil, errors.New("synthetic secret")
			}
			return nil, nil
		}
	})
	f.cfg.Set("test_rule", "changed")
	f.diff()
	f.spawn.run(t)
	a := f.diff()
	fields := answerFields(t, a)
	if string(fields["reason"]) != `"diff_failed"` || f.calls != 1 {
		t.Fatal(string(a.raw))
	}
	f.clock.Advance(ErrorTTL - time.Nanosecond)
	f.diff()
	if f.spawn.count() != 0 {
		t.Fatal("early retry")
	}
	f.clock.Advance(time.Nanosecond)
	requireStatus(t, f.status(), "changed", "")
	requireStatus(t, f.diff().Result, "checking", "")
	fail = false
	f.spawn.run(t)
	if f.calls != 2 || versionOf(t, f.diff()) == versionOf(t, a) {
		t.Fatal("not retried/versioned")
	}
}
func TestDetailCountByteQuotasAndSyncedCleanup(t *testing.T) {
	f := newFixture(t, func(o *Options) { o.MaxDetails = 2; o.MaxListBytes = 1 })
	f.cfg.Set("test_rule", "DOMAIN,changed.example.test\n")
	for i := 0; i < 3; i++ {
		f.agent.Set("id", fmt.Sprintf("agent-%d", i))
		f.diff()
		f.spawn.run(t)
		if len(f.c.details) != 1 {
			t.Fatal("byte eviction or newest-alone rule")
		}
	}
	// Removing the byte constraint exposes the count constraint independently.
	f.c.opts.MaxListBytes = MaxListBytes
	for i := 3; i < 6; i++ {
		f.agent.Set("id", fmt.Sprintf("agent-%d", i))
		f.diff()
		f.spawn.run(t)
	}
	if len(f.c.details) != 2 {
		t.Fatal("detail count")
	}
	f.agent.Set("id", "agent-3")
	requireStatus(t, f.diff().Result, "checking", "")
	f.spawn.run(t)
	f.cfg.Set("test_rule", "DOMAIN,example.test\n")
	f.status()
	f.spawn.run(t)
	if f.c.details["agent-3"] != nil {
		t.Fatal("synced answer retained old details")
	}
}
func TestStateLRUSkipsRunningAndDetailExpiration(t *testing.T) {
	f := newFixture(t, func(o *Options) { o.MaxEntries = 2; o.Workers = 1 })
	f.status()
	for _, id := range []string{"b", "c", "d"} {
		f.agent.Set("id", id)
		f.status()
	}
	if len(f.c.entries) != 2 || f.c.entries["agent-test"] == nil {
		t.Fatal("trimmed running state or unbounded entries")
	}
	f.spawn.run(t)
	// Expired details of other identities are pruned when a new list is stored.
	f.cfg.Set("test_rule", "changed")
	f.agent.Set("id", "x")
	f.diff()
	f.spawn.run(t)
	f.clock.Advance(SuccessTTL)
	f.agent.Set("id", "y")
	f.diff()
	f.spawn.run(t)
	if f.c.details["x"] != nil {
		t.Fatal("expired detail retained")
	}
}
func TestRequestSnapshotsAndRetarget(t *testing.T) {
	f := newFixture(t, nil)
	target := f.target()
	source := f.source()
	target.Record.Name = "captured"
	key, _ := source.InputKey()
	f.agent.GetObject("applied_content").Set("config_path", "active.yaml")
	// Retarget is exercised using the updated baseline, without changing identity.
	target = f.target()
	target.Record.Name = "captured"
	base := f.prepare
	f.prepare = func(ctx context.Context, tgt deploy.Target, s *deploy.Source, url, id string) (*deploy.Bundle, error) {
		if tgt.Record.Name != "captured" {
			t.Error("target not copied")
		}
		got, _ := s.InputKey()
		if got != key {
			t.Error("source not captured")
		}
		tgt.Record.Name = "worker changed"
		return base(ctx, tgt, s, url, id)
	}
	f.c.Status(f.ctx, target, source, "")
	target.Record.Name = "caller changed"
	f.cfg.Set("test_main", "mode: global\n")
	f.spawn.run(t)
	if target.Record.Name != "caller changed" {
		t.Fatal("mutated caller")
	}
	requireStatus(t, f.c.Status(f.ctx, f.target(), source, ""), "synced", "")
}
func TestDispatcherBroadcastAndUnregister(t *testing.T) {
	a, b := newFixture(t, nil), newFixture(t, nil)
	for _, f := range []*fixture{a, b} {
		f.status()
		f.spawn.run(t)
	}
	var dispatcher Dispatcher
	unregisterA := dispatcher.Register(a.c)
	unregisterB := dispatcher.Register(b.c)
	dispatcher.Invalidate()
	for _, f := range []*fixture{a, b} {
		requireStatus(t, f.status(), "checking", "")
		f.spawn.run(t)
	}
	unregisterA()
	unregisterA()
	dispatcher.Invalidate()
	requireStatus(t, a.status(), "synced", "")
	requireStatus(t, b.status(), "checking", "")
	unregisterB()
}
func TestInvalidInputsAndFingerprintStrictness(t *testing.T) {
	f := newFixture(t, nil)
	requireStatus(t, f.c.Status(f.ctx, deploy.Target{}, nil, ""), "unknown", "invalid_inputs")
	for _, fp := range []string{"v1:" + strings.Repeat("a", 64) + "\n", "v1:" + strings.Repeat("A", 64), "", "v2:" + strings.Repeat("a", 64)} {
		f.agent.GetObject("applied_content").Set("fingerprint", fp)
		requireStatus(t, f.status(), "unknown", "baseline_invalid")
	}
	// The key includes unknown fields in the complete applied baseline.
	f = newFixture(t, nil)
	f.status()
	f.spawn.run(t)
	f.agent.GetObject("applied_content").Set("extra", jsonx.Obj("nested", 1))
	requireStatus(t, f.status(), "checking", "")
}
