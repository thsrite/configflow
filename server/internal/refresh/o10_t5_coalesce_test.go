package refresh

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/notifications"
	"configflow/internal/substore"
	"configflow/internal/tasks"
	"configflow/internal/taskstatus"
	"configflow/internal/testjson"
)

// TestO10T5NotificationSemanticsLockedBeforeCoalescing pins the user-visible
// notification semantics of RunRound before and after coalescing per-round
// notification refreshes:
//
//  1. Multiple failing sources in one round are all persisted as pending
//     incidents without a browser, with checked_at/last_seen_at taken from
//     each task's own last_run_at receipt.
//  2. Repeated failure in a later round preserves first_seen_at, increments
//     occurrences and failure_count, rotates version, and ignores stale reads.
//  3. Dismissed incidents stay dismissed across repeated snapshots until a
//     new failure changes evidence/fingerprint; a later success clears both
//     pending and dismissed receipts without a browser.
//  4. Mid-round browser observation or dismissal is never reverted or
//     double-counted by the round's notification refresh.
//  5. Direct single-source refreshes (manual/direct RefreshSource/RefreshRemote)
//     still refresh notifications immediately.
func TestO10T5NotificationSemanticsLockedBeforeCoalescing(t *testing.T) {
	f := setup(t, 60)
	var hexCounter atomic.Int64
	f.env.RandomHex = func() string {
		n := hexCounter.Add(1)
		return fmt.Sprintf("%08x", n)
	}
	f.updateShared(func(shared *model.Shared) {
		shared.Subscriptions = []model.Subscription{
			testjson.Decode[model.Subscription](t, map[string]any{
				"id": "s1", "name": "Sub One", "url": "https://sub1.example.com/feed", "enabled": true, "interval": 60,
			}),
			testjson.Decode[model.Subscription](t, map[string]any{
				"id": "s2", "name": "Sub Two", "url": "https://sub2.example.com/feed", "enabled": true, "interval": 60,
			}),
		}
		shared.RuleLibrary = []model.RuleLibraryItem{
			testjson.Decode[model.RuleLibraryItem](t, map[string]any{
				"id": "r1", "name": "Rule One", "source_type": "url", "url": "https://rules1.example.com/list.txt",
				"behavior": "domain", "format": "text", "enabled": true, "interval": 60,
			}),
			testjson.Decode[model.RuleLibraryItem](t, map[string]any{
				"id": "r2", "name": "Rule Two", "source_type": "url", "url": "https://rules2.example.com/list.txt",
				"behavior": "domain", "format": "text", "enabled": true, "interval": 60,
			}),
		}
	})

	// Wire task slots and notification inputs cleanly for this test.
	reg := tasks.NewRegistry(f.repo, nil)
	none := func(context.Context, taskstatus.Snapshot) ([]*tasks.Task, error) { return nil, nil }
	for _, slot := range []string{tasks.SlotRouters, tasks.SlotAgents, tasks.SlotDomains, tasks.SlotMetrics} {
		reg.SetRowBuilder(slot, none)
	}
	reg.SetRowBuilder(tasks.SlotSubscriptions, f.svc.SubscriptionTasks)
	reg.SetRowBuilder(tasks.SlotRuleSources, f.svc.RuleTasks)

	var snapshotRowsCalls atomic.Int32
	nt := notifications.New(notifications.Options{
		Repo:  f.repo,
		Tasks: reg,
		Rows: func(ctx context.Context) ([]*tasks.Task, error) {
			snapshotRowsCalls.Add(1)
			return reg.Available(ctx)
		},
		MissingTasks: reg.Missing,
		Agents:       func(context.Context) ([]model.Agent, error) { return nil, nil },
	})
	f.svc.notifications = nt

	prevClock := notifications.Clock
	notifications.Clock = f.clock.Now
	t.Cleanup(func() { notifications.Clock = prevClock })

	readSaved := func() map[string]map[string]any {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join(f.repo.DataDir(), "notifications.json"))
		if err != nil {
			t.Fatalf("read notifications.json: %v", err)
		}
		var doc struct {
			Items map[string]map[string]any `json:"items"`
		}
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("decode notifications.json: %v", err)
		}
		if doc.Items == nil {
			return map[string]map[string]any{}
		}
		return doc.Items
	}

	failAll := func() {
		f.download("", "direct", errors.New("synthetic sub timeout"))
		f.transport.reply = func(string) (int, string, error) {
			return 0, "", errors.New("synthetic rule timeout")
		}
	}
	succeedAll := func() {
		f.download(proxiesYAML(t), "direct", nil)
		f.transport.reply = func(string) (int, string, error) {
			return 200, "example.com\n", nil
		}
	}

	keys := []string{
		"task:subscription:s1",
		"task:subscription:s2",
		"task:rule_source:r1",
		"task:rule_source:r2",
	}

	// 1. Round 1: all 4 sources fail -> all 4 persisted as pending without browser,
	// with exactly ONE coalesced notification snapshot for the whole round.
	failAll()
	beforeRound1 := snapshotRowsCalls.Load()
	n, err := f.svc.RunRound(f.ctx, nil)
	if err != nil || n != 4 {
		t.Fatalf("round 1: count=%d err=%v", n, err)
	}
	if got := snapshotRowsCalls.Load() - beforeRound1; got != 1 {
		t.Fatalf("round 1 triggered %d notification snapshots for 4 failures, want 1", got)
	}
	// Empty round (0 due sources) must trigger 0 notification snapshots.
	beforeEmpty := snapshotRowsCalls.Load()
	if emptyCount, err := f.svc.RunRound(f.ctx, nil); err != nil || emptyCount != 0 {
		t.Fatalf("empty round: count=%d err=%v", emptyCount, err)
	}
	if got := snapshotRowsCalls.Load() - beforeEmpty; got != 0 {
		t.Fatalf("empty round triggered %d notification snapshots, want 0", got)
	}
	round1Time := f.clock.Now().UTC().Format("2006-01-02T15:04:05Z")
	saved1 := readSaved()
	firstSeen := map[string]string{}
	versions1 := map[string]string{}
	for _, k := range keys {
		item, ok := saved1[k]
		if !ok {
			t.Fatalf("round 1 missing persisted notification %s", k)
		}
		if item["state"] != "pending" {
			t.Fatalf("round 1 %s state=%v, want pending", k, item["state"])
		}
		if item["checked_at"] != round1Time || item["last_seen_at"] != round1Time {
			t.Fatalf("round 1 %s checked_at=%v last_seen_at=%v, want %s", k, item["checked_at"], item["last_seen_at"], round1Time)
		}
		if fmt.Sprint(item["occurrences"]) != "1" || fmt.Sprint(item["failure_count"]) != "1" {
			t.Fatalf("round 1 %s occurrences=%v failure_count=%v", k, item["occurrences"], item["failure_count"])
		}
		fs, _ := item["first_seen_at"].(string)
		ver, _ := item["version"].(string)
		if fs == "" || ver == "" {
			t.Fatalf("round 1 %s missing first_seen_at/version: %+v", k, item)
		}
		firstSeen[k] = fs
		versions1[k] = ver
	}

	// Dismiss s2 and r1; keep s1 and r2 pending.
	if err := notifications.Dismiss(f.ctx, f.repo.DataDir(), map[string]string{
		"task:subscription:s2": versions1["task:subscription:s2"],
		"task:rule_source:r1":  versions1["task:rule_source:r1"],
	}); err != nil {
		t.Fatalf("dismiss: %v", err)
	}
	// A snapshot without a new failure keeps s2 and r1 dismissed.
	items, err := nt.SnapshotItems(f.ctx, "")
	if err != nil {
		t.Fatalf("snapshot after dismiss: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 pending items after dismissing 2, got %d", len(items))
	}
	savedAfterDismiss := readSaved()
	for _, k := range []string{"task:subscription:s2", "task:rule_source:r1"} {
		if savedAfterDismiss[k]["state"] != "dismissed" {
			t.Fatalf("%s state=%v, want dismissed", k, savedAfterDismiss[k]["state"])
		}
	}

	// 2. Round 2 (after RetrySeconds): all 4 fail again.
	// Pending items (s1, r2) must keep their original first_seen_at, increment
	// occurrences -> 2 and failure_count -> 2, and rotate version.
	// Dismissed items (s2, r1) must re-open as pending with occurrences=1 and failure_count=2.
	f.clock.advance(RetrySeconds*time.Second + time.Second)
	round2Time := f.clock.Now().UTC().Format("2006-01-02T15:04:05Z")
	beforeRound2 := snapshotRowsCalls.Load()
	n, err = f.svc.RunRound(f.ctx, nil)
	if err != nil || n != 4 {
		t.Fatalf("round 2: count=%d err=%v", n, err)
	}
	if got := snapshotRowsCalls.Load() - beforeRound2; got != 1 {
		t.Fatalf("round 2 triggered %d notification snapshots for 4 failures, want 1", got)
	}
	saved2 := readSaved()
	for _, k := range []string{"task:subscription:s1", "task:rule_source:r2"} {
		item := saved2[k]
		if item["state"] != "pending" {
			t.Fatalf("round 2 pending %s state=%v", k, item["state"])
		}
		if item["first_seen_at"] != firstSeen[k] {
			t.Fatalf("round 2 %s first_seen_at changed: got %v want %s", k, item["first_seen_at"], firstSeen[k])
		}
		if fmt.Sprint(item["occurrences"]) != "2" || fmt.Sprint(item["failure_count"]) != "2" {
			t.Fatalf("round 2 %s occurrences=%v failure_count=%v, want 2/2", k, item["occurrences"], item["failure_count"])
		}
		if item["last_seen_at"] != round2Time {
			t.Fatalf("round 2 %s last_seen_at=%v, want %s", k, item["last_seen_at"], round2Time)
		}
		if item["version"] == versions1[k] {
			t.Fatalf("round 2 %s version did not rotate", k)
		}
	}
	for _, k := range []string{"task:subscription:s2", "task:rule_source:r1"} {
		item := saved2[k]
		if item["state"] != "pending" {
			t.Fatalf("round 2 dismissed %s did not reopen: %v", k, item["state"])
		}
		if fmt.Sprint(item["occurrences"]) != "1" || fmt.Sprint(item["failure_count"]) != "2" {
			t.Fatalf("round 2 reopened %s occurrences=%v failure_count=%v, want 1/2", k, item["occurrences"], item["failure_count"])
		}
	}
	// Stale read with Round 1's version must not dismiss Round 2's item.
	if err := notifications.Dismiss(f.ctx, f.repo.DataDir(), map[string]string{
		"task:subscription:s1": versions1["task:subscription:s1"],
	}); err != nil {
		t.Fatalf("stale dismiss: %v", err)
	}
	if readSaved()["task:subscription:s1"]["state"] != "pending" {
		t.Fatal("stale version dismissed newer incident")
	}

	// Dismiss s1 with its Round 2 version, then Round 3 succeeds for all 4:
	// both pending and dismissed markers must be removed without a browser.
	verS1R2, _ := saved2["task:subscription:s1"]["version"].(string)
	if err := notifications.Dismiss(f.ctx, f.repo.DataDir(), map[string]string{"task:subscription:s1": verS1R2}); err != nil {
		t.Fatalf("dismiss s1: %v", err)
	}
	f.clock.advance(2*RetrySeconds*time.Second + time.Second)
	succeedAll()
	beforeRound3 := snapshotRowsCalls.Load()
	n, err = f.svc.RunRound(f.ctx, nil)
	if err != nil || n != 4 {
		t.Fatalf("round 3: count=%d err=%v", n, err)
	}
	if got := snapshotRowsCalls.Load() - beforeRound3; got != 1 {
		t.Fatalf("round 3 triggered %d notification snapshots for 4 recoveries, want 1", got)
	}
	if saved3 := readSaved(); len(saved3) != 0 {
		t.Fatalf("round 3 recovery left items: %+v", saved3)
	}

	// 4. Direct single refresh (RefreshSource) still refreshes notifications immediately.
	failAll()
	f.clock.advance(time.Second)
	sub1 := f.sub()
	beforeDirect := snapshotRowsCalls.Load()
	_, _, _, _ = f.svc.RefreshSource(f.ctx, sub1, "manual")
	if snapshotRowsCalls.Load() <= beforeDirect {
		t.Fatal("direct RefreshSource did not trigger notification refresh")
	}
	if readSaved()["task:subscription:s1"]["state"] != "pending" {
		t.Fatal("direct RefreshSource did not persist pending incident")
	}
	// Recover s1 directly.
	succeedAll()
	f.clock.advance(time.Second)
	_, _, _, err = f.svc.RefreshSource(f.ctx, sub1, "manual")
	if err != nil {
		t.Fatalf("direct recovery: %v", err)
	}
	if _, exists := readSaved()["task:subscription:s1"]; exists {
		t.Fatal("direct recovery did not clear incident")
	}

	// 5. Shutdown cancellation prevents post-shutdown notification persistence.
	rt := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry(), Grace: time.Millisecond})
	rt.Start(f.ctx)
	bound := rt.BindRequest(f.ctx)
	rt.Shutdown(context.Background())
	if _, err := f.svc.RunRound(bound, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("post-shutdown RunRound err=%v, want context.Canceled", err)
	}
}

// TestO10T5MidRoundObservationAndDismissalStayConsistent ensures that if a
// browser reads or dismisses a failed source mid-round while another source is
// still running, the end-of-round notification refresh preserves first_seen_at,
// occurrences, version and dismissed state.
func TestO10T5MidRoundObservationAndDismissalStayConsistent(t *testing.T) {
	f := setup(t, 60)
	var hexCounter atomic.Int64
	f.env.RandomHex = func() string {
		n := hexCounter.Add(1)
		return fmt.Sprintf("%08x", n)
	}
	f.updateShared(func(shared *model.Shared) {
		shared.Subscriptions = []model.Subscription{
			testjson.Decode[model.Subscription](t, map[string]any{
				"id": "fast", "name": "Fast", "url": "https://fast.example.com/feed", "enabled": true, "interval": 60,
			}),
			testjson.Decode[model.Subscription](t, map[string]any{
				"id": "slow", "name": "Slow", "url": "https://slow.example.com/feed", "enabled": true, "interval": 60,
			}),
		}
		shared.RuleLibrary = []model.RuleLibraryItem{}
	})
	reg := tasks.NewRegistry(f.repo, nil)
	none := func(context.Context, taskstatus.Snapshot) ([]*tasks.Task, error) { return nil, nil }
	for _, slot := range []string{tasks.SlotRuleSources, tasks.SlotRouters, tasks.SlotAgents, tasks.SlotDomains, tasks.SlotMetrics} {
		reg.SetRowBuilder(slot, none)
	}
	reg.SetRowBuilder(tasks.SlotSubscriptions, f.svc.SubscriptionTasks)
	nt := notifications.New(notifications.Options{
		Repo:         f.repo,
		Tasks:        reg,
		Rows:         reg.Available,
		MissingTasks: reg.Missing,
		Agents:       func(context.Context) ([]model.Agent, error) { return nil, nil },
	})
	f.svc.notifications = nt
	prevClock := notifications.Clock
	notifications.Clock = f.clock.Now
	t.Cleanup(func() { notifications.Clock = prevClock })

	slowStarted := make(chan struct{})
	releaseSlow := make(chan struct{})
	f.env.Sources.Fetch = func(_ context.Context, subID, _ string, _ substore.FetchOptions) (*SourceResult, error) {
		if subID == "slow" {
			close(slowStarted)
			<-releaseSlow
		}
		return nil, errors.New("synthetic fetch error")
	}

	done := make(chan error, 1)
	go func() {
		_, err := f.svc.RunRound(f.ctx, nil)
		done <- err
	}()
	<-slowStarted
	// Wait until fast's receipt is written and its running lock is released.
	deadline := time.Now().Add(5 * time.Second)
	for {
		if !IsRunning("subscription:fast") && f.status("subscription:fast")["last_status"] == "failed" {
			break
		}
		if time.Now().After(deadline) {
			close(releaseSlow)
			t.Fatal("fast source did not finish mid-round")
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Browser observes notifications mid-round and sees fast immediately.
	f.clock.advance(2 * time.Second)
	midItems, err := nt.SnapshotItems(f.ctx, "")
	if err != nil {
		close(releaseSlow)
		t.Fatalf("mid-round snapshot: %v", err)
	}
	var fastItem *notifications.Item
	for _, it := range midItems {
		if it.ID == "task:subscription:fast" {
			fastItem = it
		}
	}
	if fastItem == nil || fastItem.Version == nil || fastItem.FirstSeenAt == nil {
		close(releaseSlow)
		t.Fatalf("mid-round snapshot missing fast item: %+v", midItems)
	}
	fastFirstSeen := *fastItem.FirstSeenAt
	fastVersion := *fastItem.Version

	// Advance clock and let slow finish and round complete.
	f.clock.advance(5 * time.Second)
	close(releaseSlow)
	if err := <-done; err != nil {
		t.Fatalf("RunRound: %v", err)
	}

	// Verify fast kept its exact mid-round first_seen_at, version, and occurrences=1,
	// while slow is now also pending.
	afterItems, err := nt.SnapshotItems(f.ctx, "")
	if err != nil {
		t.Fatalf("post-round snapshot: %v", err)
	}
	byID := map[string]*notifications.Item{}
	for _, it := range afterItems {
		byID[it.ID] = it
	}
	if len(byID) != 2 {
		t.Fatalf("expected 2 pending items after round, got %d", len(byID))
	}
	gotFast := byID["task:subscription:fast"]
	if gotFast == nil || gotFast.FirstSeenAt == nil || *gotFast.FirstSeenAt != fastFirstSeen {
		t.Fatalf("fast first_seen_at changed: got %v want %s", gotFast, fastFirstSeen)
	}
	if gotFast.Version == nil || *gotFast.Version != fastVersion {
		t.Fatalf("fast version changed: got %v want %s", gotFast.Version, fastVersion)
	}
	if gotFast.Occurrences == nil {
		t.Fatal("fast missing occurrences")
	}
	occBytes, _ := json.Marshal(gotFast.Occurrences)
	if string(occBytes) != "1" {
		t.Fatalf("fast occurrences=%s, want 1", occBytes)
	}
	if byID["task:subscription:slow"] == nil {
		t.Fatal("slow item missing after round")
	}
}
