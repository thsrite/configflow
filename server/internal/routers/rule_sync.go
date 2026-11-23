package routers

import (
	"configflow/internal/routers/device"
	"configflow/internal/routers/spec"
	"context"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"configflow/internal/agents"
	"configflow/internal/agents/diag"
	"configflow/internal/lifecycle"
	"configflow/internal/lockx"
	"configflow/internal/logx"
	"configflow/internal/notifications"
	"configflow/internal/repo"
	"configflow/internal/taskstatus"
	"configflow/internal/weburl"
)

// RuleSyncService owns manual baselines, automatic destination deltas and jobs.
// Drivers remain real production clients; tests inject a loopback-backed factory.
type RuleSyncService struct {
	Repo      *repo.Repository
	Agents    AgentSource
	NewDriver func(*Router) *RouterOS
	Download  RuleSyncDownload
	// Spawner defaults to the production lifecycle; isolated runtimes may be injected.
	Spawner lockx.Spawner
}

var syncLogger = logx.Get("backend.routers.rule_sync")
var syncWorkers = lockx.NewKeyed[string](lockx.LevelJob, "routers:rule-sync")

func (s *RuleSyncService) store() RuleSyncStore { return RuleSyncStore{DataDir: s.Repo.DataDir()} }
func (s *RuleSyncService) context(ctx context.Context) context.Context {
	return repo.WithRepository(lifecycle.Detach(ctx), s.Repo)
}
func (s *RuleSyncService) driver(r *Router) *RouterOS {
	if s.NewDriver != nil {
		return s.NewDriver(r)
	}
	return NewRouterOS(r.Endpoint, r.Username, r.Password, r.VerifyTLS)
}
func (s *RuleSyncService) baseline(ctx context.Context, config json.RawMessage, r *Router, d *RouterOS, desired *Desired, records map[string]*RuleSyncRecord, control string) (*RuleSyncBaseline, error) {
	static, err := syncStaticResources(ctx, d, desired)
	if err != nil {
		return nil, err
	}
	nat, err := d.NATResources(ctx)
	if err != nil {
		return nil, err
	}
	return &RuleSyncBaseline{Control: control, Rules: records, Desired: desired, StaticResources: static, Policy: syncNull(slices.Clone(d.PolicyManifest())), NATList: nat}, nil
}

// Capture records a verified explicit publication. It is called under the
// publisher's L1/L2 and must never acquire either lock again.
func (s *RuleSyncService) Capture(ctx context.Context, profile string, config json.RawMessage, r *Router, driver Driver, desired *Desired) error {
	if r.Type != "routeros" {
		return nil
	}
	work := s.context(ctx)
	if err := work.Err(); err != nil {
		return err
	}
	d, ok := driver.(*RouterOS)
	if !ok {
		return device.ErrNotPorted
	}
	defer d.ReuseAddressReads()()
	timestamp := syncNow(work)
	err := func() error {
		_, records, err := loadSyncSources(work, config, r, nil)
		if err != nil {
			return err
		}
		signature, err := s.SyncControls(work, config, r)
		if err != nil {
			return err
		}
		latest, err := syncConfig(work, s.Repo, profile)
		if err != nil {
			return err
		}
		current := spec.FindSyncRouter(latest, r.ID)
		if current == nil {
			return device.Error(messageSyncCaptureChanged)
		}
		liveSignature, err := s.SyncControls(work, latest, current)
		if err != nil {
			return err
		}
		if liveSignature != signature {
			return device.Error(messageSyncCaptureChanged)
		}
		pinned, err := spec.ComputeDesired(work, config, desired.ProxyGateway, r.StaticRuleSets(), syncIPLoader(records))
		if err != nil {
			return err
		}
		if !syncCIDRsEqual(pinned.ProxyCIDRs, desired.ProxyCIDRs) {
			return device.Error(messageSyncCaptureCache)
		}
		if len(r.RosPolicyRuleSets) > 0 {
			expected, err := spec.ComputeDesired(work, config, desired.ProxyGateway, r.RosPolicyRuleSets, syncIPLoader(records))
			if err != nil {
				return err
			}
			manifest, err := policyManifest(d.PolicyManifest())
			if err != nil {
				return err
			}
			destinations := map[string]bool{}
			for _, item := range expected.ProxyCIDRs {
				if !slices.Equal(item.Sources, []string{"fake-ip"}) {
					destinations[item.CIDR] = true
				}
			}
			if manifest == nil || manifest.Spec == nil || !maps.Equal(destinations, device.StringSet(manifest.Spec.Destinations...)) {
				return device.Error(messageSyncCapturePolicy)
			}
			if err = VerifyROSPolicy(work, d, &ROSPolicyCheck{Spec: manifest.Spec, Previous: d.PolicyManifest()}); err != nil {
				return err
			}
		}
		baseline, err := s.baseline(work, config, r, d, pinned, records, signature)
		if err != nil {
			return err
		}
		return s.store().Write(work, profile, r.ID, RuleSyncReceipt{Status: "synced", Message: messageSyncCaptured, SyncedAt: &timestamp, UpdatedAt: &timestamp, Baseline: baseline, Rules: records, RuleOrder: syncSelectedOrder(config, r)})
	}()
	if err == nil {
		ruleSyncLoop.wake.Notify()
		return nil
	}
	if work.Err() != nil {
		return work.Err()
	}
	message := messageSyncCaptureIO
	var device *RouterError
	if errors.As(err, &device) {
		message = routeSafeError(err, r.Endpoint, r.Password).Error()
	}
	return s.store().Write(work, profile, r.ID, RuleSyncReceipt{Status: "waiting", Message: messageSyncCapturePrefix + message, CheckedAt: &timestamp})
}
func (s *RuleSyncService) saveManifest(ctx context.Context, profile string, r *Router, d *RouterOS) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	latest, err := syncConfig(ctx, s.Repo, profile)
	if err != nil {
		return err
	}
	current := spec.FindSyncRouter(latest, r.ID)
	publication := repo.RouterPublicationJSON{}
	if current == nil || !device.JSONEqual(syncNull(current.State("ros_policy_state")), syncNull(d.PolicyManifest())) {
		publication.Manifest = repo.JSONUpdate{Set: true, Value: d.PolicyManifest()}
	}
	if d.IPv6RoutesManifestSet() && (current == nil || !device.JSONEqual(syncNull(current.State("ipv6_routes_state")), syncNull(d.IPv6RoutesManifest()))) {
		publication.IPv6Routes = repo.JSONUpdate{Set: true, Value: d.IPv6RoutesManifest()}
	}
	if !publication.Manifest.Set && !publication.IPv6Routes.Set {
		return nil
	}
	expected, err := spec.ReleaseRouterRecord(r)
	if err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return s.Repo.SaveRouterPublicationJSON(ctx, profile, expected, publication)
}
func syncIPv6Delta(state *RouterState, plan *Plan) string {
	v := state.IPv6RoutesPlan
	if v == nil {
		return ""
	}
	reason := strings.Join(v.Skip, "；")
	if reason == "" && len(v.Ops) > 0 && v.Auto != "" {
		reason = v.Auto + messageSyncManualHint
	}
	if reason != "" {
		for i := range plan.Stages {
			if plan.Stages[i].Key == StageIPv6Routes {
				plan.Stages[i].Ops = []Op{}
			}
		}
		if plan.Checks != nil {
			plan.Checks.IPv6Routes = nil
		}
		return messageSyncIPv6Note + reason
	}
	return ""
}

// Sync detaches client cancellation, takes the common L1, and runs a single job.
func (s *RuleSyncService) Sync(ctx context.Context, profile, id, trigger string) (RuleSyncStatus, error) {
	work := s.context(ctx)
	if err := work.Err(); err != nil {
		return RuleSyncStatus{}, err
	}
	config, err := syncConfig(work, s.Repo, profile)
	if err != nil {
		return RuleSyncStatus{}, err
	}
	connection := spec.FindSyncRouter(config, id)
	held, release, lockErr := RouterJobLock(work, profile, id, connection)
	if lockErr != nil {
		var conflict *RouterError
		if errors.As(lockErr, &conflict) && conflict.Kind == KindConflict {
			return RuleSyncStatus{}, device.Error(messageSyncBusy)
		}
		return RuleSyncStatus{}, lockErr
	}
	defer release()
	return s.sync(held, profile, id, trigger)
}
func (s *RuleSyncService) sync(ctx context.Context, profile, id, trigger string) (RuleSyncStatus, error) {
	store := s.store()
	config, err := syncConfig(ctx, s.Repo, profile)
	if err != nil {
		return RuleSyncStatus{}, err
	}
	r := spec.FindSyncRouter(config, id)
	if r == nil || r.Type != "routeros" || !r.AutoRuleSync {
		return RuleSyncStatus{}, device.Error(messageSyncEnable)
	}
	if r.StateRestoredAt() != "" {
		return RuleSyncStatus{}, device.Error(messageSyncRestored)
	}
	receipt, err := store.Read(ctx, profile, id)
	if err != nil {
		return RuleSyncStatus{}, err
	}
	base := receipt.Baseline
	if base == nil {
		return RuleSyncStatus{}, device.Error(messageSyncBaseline)
	}
	if receipt.Status == "running" {
		receipt.Status = "paused"
		receipt.Message = messageSyncInterrupted
		if err = store.Write(ctx, profile, id, receipt); err != nil {
			return RuleSyncStatus{}, err
		}
	}
	if receipt.Status == "paused" {
		return RuleSyncStatus{}, device.Error(messageSyncConfirm)
	}
	timestamp := syncNow(ctx)
	started := lifecycle.ClockFrom(ctx).Now()
	receipt.Status = "running"
	receipt.Message = messageSyncChecking
	receipt.CheckedAt = &timestamp
	if err = store.Write(ctx, profile, id, receipt); err != nil {
		return RuleSyncStatus{}, err
	}
	var d *RouterOS
	writesStarted := false
	err = func() error {
		signature, err := s.SyncControls(ctx, config, r)
		if err != nil {
			return err
		}
		if signature != base.Control {
			return device.Error(messageSyncControls)
		}
		if !device.JSONEqual(syncNull(r.State("ros_policy_state")), syncNull(base.Policy)) {
			return device.Error(messageSyncPolicyChanged)
		}
		ctx, err = withSyncEnvironment(ctx, config)
		if err != nil {
			return err
		}
		download := s.Download
		if download == nil {
			download = (&RuleSyncDownloads{}).Download
		}
		contents, records, err := loadSyncSources(ctx, config, r, download)
		if err != nil {
			return err
		}
		for id, record := range records {
			if old := base.Rules[id]; old != nil {
				record.Extra = maps.Clone(old.Extra)
			}
		}
		receipt.Rules = records
		receipt.RuleOrder = syncSelectedOrder(config, r)
		for key, v := range records {
			if base.Rules[key] == nil || v.Version != base.Rules[key].Version {
				receipt.UpdatedAt = &timestamp
				break
			}
		}
		if err = checkSyncUpdates(base.Rules, records); err != nil {
			return err
		}
		held, unlock, err := RouterOperationLock(ctx, *r, true)
		if err != nil {
			return err
		}
		defer unlock()
		ctx = held
		// Revision-gated checks are cheap between writes, forced before/after the
		// whole delta. A revision change always reloads the full control signature.
		var lastRevision json.RawMessage
		var lastCheck time.Time
		ensure := func(force bool) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			revision, err := s.Repo.ConfigRevision(ctx, profile)
			if err != nil {
				return err
			}
			rawRevision := device.JSON(revision)
			now := lifecycle.ClockFrom(ctx).Now()
			if !force && device.JSONEqual(rawRevision, lastRevision) && now.Sub(lastCheck) < time.Second {
				return nil
			}
			latest, err := syncConfig(ctx, s.Repo, profile)
			if err != nil {
				return err
			}
			item := spec.FindSyncRouter(latest, id)
			if item == nil || !item.AutoRuleSync {
				return device.Error(messageSyncCurrent)
			}
			current, err := s.SyncControls(ctx, latest, item)
			if err != nil {
				return err
			}
			if current != base.Control || !device.JSONEqual(syncNull(item.State("ros_policy_state")), syncNull(r.State("ros_policy_state"))) || !device.JSONEqual(syncNull(item.State("ipv6_routes_state")), syncNull(r.State("ipv6_routes_state"))) {
				return device.Error(messageSyncCurrent)
			}
			sources, err := selectedSyncSources(latest, item)
			if err != nil {
				return err
			}
			for _, source := range sources {
				if source.Source.SourceType == "content" && (records[source.Item.ID] == nil || syncVersion(source.Source.Content) != records[source.Item.ID].Version) {
					return device.Error(messageSyncLocalDuring)
				}
			}
			lastRevision, lastCheck = rawRevision, now
			return nil
		}
		if err = ensure(false); err != nil {
			return err
		}
		if err = r.ValidateForDevice(); err != nil {
			return err
		}
		d = s.driver(r)
		inspection, err := Inspect(ctx, InspectRequest{ProfileID: profile, Config: config, Router: r, Driver: d, Loader: syncIPLoader(records), Agents: s.Agents, Repo: s.Repo})
		if err != nil {
			return err
		}
		plan, err := BuildPlan(ctx, r, inspection, d)
		if err != nil {
			return err
		}
		if plan.Checks == nil {
			return device.Error(strings.Join(plan.Blockers, "；"))
		}
		checked := map[string]bool{}
		for _, item := range plan.Checks.Routes {
			checked[item.CIDR] = true
		}
		if !maps.Equal(checked, syncCIDRSet(inspection.Desired.ProxyCIDRs)) {
			return device.Error(messageSyncConflict)
		}
		if v := inspection.State.IPv6FakeIPPlan; v != nil && (len(v.Blockers) > 0 || len(v.Ops) > 0) {
			return device.Error(messageSyncFakeIP)
		}
		if v := inspection.State.IPv6DNSPlan; v != nil && len(v.Ops) > 0 && len(v.Blockers) == 0 {
			return device.Error(messageSyncDNS)
		}
		if v := inspection.State.IPv6PDGuardPlan; v != nil && len(v.Ops) > 0 && len(v.Blockers) == 0 {
			return device.Error(messageSyncPD)
		}
		note := syncIPv6Delta(inspection.State, plan)
		writesStarted = true
		outcome, err := ApplyRuleSyncDelta(ctx, d, inspection.Desired, plan, base, ensure)
		if err != nil {
			return err
		}
		if err = s.saveManifest(ctx, profile, r, d); err != nil {
			return err
		}
		updated, err := s.baseline(ctx, config, r, d, inspection.Desired, records, base.Control)
		if err != nil {
			return err
		}
		updated.Extra = maps.Clone(base.Extra)
		base = updated
		receipt.Baseline = base
		receipt.Rules = records
		receipt.RuleOrder = syncSelectedOrder(config, r)
		receipt.Status = "running"
		receipt.Message = messageSyncCaching
		receipt.Added = outcome.Added
		receipt.Removed = outcome.Removed
		receipt.Log = outcome.Log
		synced := syncNow(ctx)
		receipt.SyncedAt = &synced
		if err = store.Write(ctx, profile, id, receipt); err != nil {
			return err
		}
		sources, err := selectedSyncSources(config, r)
		if err != nil {
			return err
		}
		if err = writeSyncSources(ctx, sources, contents, config); err != nil {
			return err
		}
		receipt.Status = "synced"
		receipt.Message = messageSyncDone
		if note != "" {
			receipt.Message += "；" + note
		}
		return store.Write(ctx, profile, id, receipt)
	}()
	if d != nil {
		defer d.Close()
	}
	if err != nil {
		if ctx.Err() != nil {
			return RuleSyncStatus{}, ctx.Err()
		} // No local/device writes after Runtime cancellation.
		rollbackFailed := false
		var re *RouterError
		if errors.As(err, &re) {
			rollbackFailed = re.Kind == KindRollback
		}
		if d != nil && writesStarted {
			if e := s.saveManifest(ctx, profile, r, d); e != nil {
				rollbackFailed = true
			} else {
				static, e := syncStaticResources(ctx, d, base.Desired)
				if e != nil {
					rollbackFailed = true
				} else {
					next := *base
					next.StaticResources = static
					next.Policy = syncNull(slices.Clone(d.PolicyManifest()))
					base = &next
					receipt.Baseline = base
				}
			}
		}
		var transient *syncDownloadError
		isRouter := errors.As(err, &re)
		receipt.Status = "failed"
		if rollbackFailed || isRouter && !errors.As(err, &transient) {
			receipt.Status = "paused"
		}
		if isRouter {
			receipt.Message = routeSafeError(err, r.Endpoint, r.Password).Error()
		} else {
			receipt.Message = weburl.SafeErrorDetails(err)
		}
		receipt.CheckedAt = &timestamp
		if e := store.Write(ctx, profile, id, receipt); e != nil {
			return RuleSyncStatus{}, e
		}
	}
	if err = ctx.Err(); err != nil {
		return RuleSyncStatus{}, err
	}
	label := releaseLabel(r)
	message := diag.EventMessage(receipt.Message, []any{r.Password})
	switch receipt.Status {
	case "synced":
		syncLogger.Infof(messageSyncLogOK, label, receipt.Added, receipt.Removed)
	case "paused":
		syncLogger.Errorf(messageSyncLogPaused, label, message)
	default:
		syncLogger.Warningf(messageSyncLogFailed, label, message)
	}
	_, err = notifications.TaskStatus(ctx, s.Repo.DataDir()).SaveResult(ctx, syncTaskKey(profile, id), taskstatus.Result{OK: receipt.Status == "synced", StartedAt: timestamp, Error: receipt.Message, Trigger: trigger, DurationMs: float64(lifecycle.ClockFrom(ctx).Now().Sub(started).Milliseconds())})
	return receipt.Public(true), err
}
func syncTaskKey(profile, id string) string { return "router_rule_sync:" + profile + ":" + id }

// RuleSyncPause is the pause of router-rule-sync after a failed round, and
// RuleSyncMaxPause its longest pause between due times.
const (
	RuleSyncPause    = 30 * time.Second
	RuleSyncMaxPause = 10 * time.Minute
)

// ruleSyncPacer paces router-rule-sync by the next due router (design
// o17-runtime-state.md 2.6): the loop sleeps until the earliest enrolled
// router is due (at least a second, at most RuleSyncMaxPause) and is woken by
// configuration commits (auto sync switched on, intervals, enrolment).
type ruleSyncPacer struct {
	wake    lifecycle.Signal
	pause   atomic.Int64 // time.Duration
	commits repo.CommitWake
}

var ruleSyncLoop ruleSyncPacer

func ruleSyncPause(context.Context) time.Duration {
	if pause := time.Duration(ruleSyncLoop.pause.Load()); pause > 0 {
		return max(pause, time.Second)
	}
	return RuleSyncPause
}

// RunRound skips disabled and unenrolled routers without loading shared rules.
func (s *RuleSyncService) RunRound(ctx context.Context) error {
	work := s.context(ctx)
	if err := work.Err(); err != nil {
		return err
	}
	held, release, ok := syncWorkers.TryLock(work, s.Repo.DataDir())
	if !ok {
		return nil
	}
	defer release()
	ruleSyncLoop.commits.Follow(s.Repo, repo.ChangeConfig, ruleSyncLoop.wake.Notify)
	next := RuleSyncMaxPause
	defer func() { ruleSyncLoop.pause.Store(int64(next)) }()
	profiles, err := spec.RouterProfiles(held, s.Repo)
	if err != nil {
		next = RuleSyncPause
		return err
	}
	copyService := *s
	if copyService.Download == nil {
		copyService.Download = (&RuleSyncDownloads{}).Download
	}
	for _, profile := range profiles {
		for _, r := range profile.Routers {
			if r.Type != "routeros" || !r.AutoRuleSync || r.StateRestoredAt() != "" {
				continue
			}
			receipt, err := s.store().Read(held, profile.ID, r.ID)
			if err != nil {
				return err
			}
			if receipt.Baseline == nil || receipt.Status == "paused" {
				continue
			}
			checked, valid := time.Time{}, false
			if receipt.CheckedAt != nil {
				checked, valid = taskstatus.ParseTimestamp(*receipt.CheckedAt)
			}
			interval := time.Duration(r.RuleSyncInterval) * time.Minute
			if elapsed := lifecycle.ClockFrom(held).Now().Sub(checked); receipt.Status != "running" && receipt.Status != "queued" && valid && elapsed < interval {
				next = min(next, interval-elapsed)
				continue
			}
			if _, err = copyService.Sync(held, profile.ID, r.ID, "schedule"); err != nil {
				if held.Err() != nil {
					return held.Err()
				}
				next = min(next, RuleSyncPause)
				continue
			}
			next = min(next, interval)
		}
	}
	return nil
}

// StartJob queues a tracked owner, retaining worker admission until it finishes.
func (s *RuleSyncService) StartJob(ctx context.Context, profile, id string) error {
	work := s.context(ctx)
	if err := work.Err(); err != nil {
		return err
	}
	held, release, ok := syncWorkers.TryLock(work, s.Repo.DataDir())
	if !ok {
		return device.Error(messageSyncWorkerBusy)
	}
	transferred := false
	defer func() {
		if !transferred {
			release()
		}
	}()
	config, err := syncConfig(held, s.Repo, profile)
	if err != nil {
		return err
	}
	r := spec.FindSyncRouter(config, id)
	job, unlock, lockErr := RouterJobLock(held, profile, id, r)
	if lockErr != nil {
		return lockErr
	}
	receipt, err := s.store().Read(job, profile, id)
	if err != nil {
		unlock()
		return err
	}
	if r == nil || !r.AutoRuleSync || receipt.Baseline == nil {
		unlock()
		return device.Error(messageSyncStart)
	}
	if r.StateRestoredAt() != "" {
		unlock()
		return device.Error(messageSyncRestored)
	}
	if receipt.Status == "paused" {
		unlock()
		return device.Error(PausedMessage(receipt))
	}
	if receipt.Status == "running" || receipt.Status == "queued" {
		unlock()
		return device.Error(messageSyncUnfinished)
	}
	previous := receipt
	timestamp := syncNow(job)
	receipt.Status = "queued"
	receipt.Message = messageSyncQueued
	receipt.CheckedAt = &timestamp
	if err = s.store().Write(job, profile, id, receipt); err != nil {
		unlock()
		return err
	}
	unlock()
	spawner := s.Spawner
	if spawner == nil {
		spawner = lifecycle.DefaultSpawner{}
	}
	err = spawner.Spawn(held, "router-rule-sync-now", func(run context.Context) {
		defer release()
		defer func() {
			if recover() != nil {
				syncLogger.Errorf(messagePublishPanic, id)
			}
		}()
		_, err := s.Sync(run, profile, id, "manual")
		if err == nil || run.Err() != nil {
			return
		}
		value, e := s.store().Read(run, profile, id)
		if e == nil && value.Status == "queued" {
			value.Status = "failed"
			value.Message = diag.EventMessage(err.Error(), []any{r.Password})
			value.CheckedAt = &timestamp
			_ = s.store().Write(run, profile, id, value)
		}
	})
	if err != nil {
		if held.Err() == nil {
			_ = s.store().Write(held, profile, id, previous)
		}
		return err
	}
	transferred = true
	return nil
}

func newSyncService(ctx context.Context) (*RuleSyncService, error) {
	db, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	manager, err := agents.New(ctx, db)
	if err != nil {
		return nil, err
	}
	return &RuleSyncService{Repo: db, Agents: ManagerAgentSource{Manager: manager}}, nil
}

func syncSelectedOrder(config json.RawMessage, r *Router) []string {
	sources, err := selectedSyncSources(config, r)
	if err != nil {
		return nil
	}
	ids := []string{}
	for _, source := range sources {
		ids = append(ids, source.Item.ID)
	}
	return ids
}
