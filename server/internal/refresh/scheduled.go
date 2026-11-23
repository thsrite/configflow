package refresh

import (
	"configflow/internal/lifecycle"
	"configflow/internal/lockx"
	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/notifications"
	"configflow/internal/repo"
	"configflow/internal/rulefetch"
	"configflow/internal/rules"
	"configflow/internal/substore"
	"configflow/internal/taskstatus"
	"context"
	"errors"
	"math"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

const (
	DefaultRuleInterval = 86400
	RetrySeconds        = 300
	TickSeconds         = 30
	FirstDelaySeconds   = 30
	Workers             = 3
	RuleTimeout         = 30
)

var logger = logx.Get("backend.utils.scheduled_refresh")
var running = lockx.NewKeyed[string](lockx.LevelJob, "refresh")

// TaskBusy means a resource refresh already owns the keyed job lock.
type TaskBusy struct{ Message string }

func (e *TaskBusy) Error() string { return e.Message }

// ErrorClass returns the stable native logging category.
func (e *TaskBusy) ErrorClass() string { return "TaskBusy" }

// StaleRuleCache diagnoses failed refreshes while validated old bytes are served.
type StaleRuleCache struct{ Message string }

func (e *StaleRuleCache) Error() string { return e.Message }

// ErrorClass returns the stable native logging category.
func (e *StaleRuleCache) ErrorClass() string { return "StaleRuleCache" }

// NotFoundError means the resource referenced by a task no longer exists.
type NotFoundError struct{ Message string }

func (e *NotFoundError) Error() string { return e.Message }

// ErrorClass returns the stable native logging category.
func (e *NotFoundError) ErrorClass() string { return "NotFound" }

// SourceTaskKey is a subscription's stable task identity.
func SourceTaskKey(id string) string { return "subscription:" + id }

// RuleTaskKey is a library source's stable task identity.
func RuleTaskKey(id string) string { return "rule_source:" + id }

// IsRunning reports keyed refresh ownership without doing I/O.
func IsRunning(key string) bool       { return running.Held(key) }
func nowUTC() time.Time               { return refreshEnv().Clock.Now().UTC().Truncate(time.Microsecond) }
func nowISO() string                  { return refreshEnv().Clock.Now().UTC().Format("2006-01-02T15:04:05Z") }
func elapsedMs(start float64) float64 { return (refreshEnv().Clock.Monotonic() - start) * 1000 }
func mtimeSeconds(seconds, nanos int64) int64 {
	whole, fraction := math.Modf(float64(seconds) + float64(nanos)*1e-9)
	fraction = math.RoundToEven(fraction * 1e6)
	if fraction >= 1e6 {
		whole++
	} else if fraction < 0 {
		whole--
	}
	return int64(whole)
}

// SourceCacheAt returns the node cache's time only for its current URL.
func SourceCacheAt(ctx context.Context, sub model.Subscription) (*string, error) {
	return sourceCacheAtID(ctx, sub.ID, sub.URL)
}

func sourceCacheAtID(ctx context.Context, id, subURL string) (*string, error) {
	env := refreshEnv()
	if env.Sources.LoadCacheSummary != nil {
		summary, err := env.Sources.LoadCacheSummary(ctx, id)
		if err != nil || summary == nil || !summary.HasNodes {
			return nil, err
		}
		if summary.URLBad || summary.URL == nil || *summary.URL != subURL {
			return nil, nil
		}
		return summary.UpdatedAt, nil
	}
	cache, err := env.Sources.LoadNodes(ctx, id)
	if err != nil || cache == nil || len(cache.Nodes) == 0 {
		return nil, err
	}
	url, malformed := cacheURL(cache)
	if malformed || url == nil || *url != subURL {
		return nil, nil
	}
	if cache.UpdatedAtDamaged || cache.UpdatedAt == nil {
		return nil, nil
	}
	return cache.UpdatedAt, nil
}

// RuleCacheAt is the local file's refresh time without touching its mtime.
func RuleCacheAt(ctx context.Context, rule model.RuleLibraryItem) (*string, error) {
	return ruleCacheAtLibrary(ctx, rules.LibraryRule(rule))
}

func ruleCacheAtLibrary(ctx context.Context, rule model.RuleConfig) (*string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := rulefetch.CacheReadPath(ctx, rule, nil)
	if err != nil {
		if errors.Is(err, repo.ErrRejected) || isFileError(err) {
			return nil, nil
		}
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, nil
	}
	stamp := info.ModTime()
	value := time.Unix(mtimeSeconds(stamp.Unix(), int64(stamp.Nanosecond())), 0).UTC().Format("2006-01-02T15:04:05Z")
	return &value, nil
}
func urlRule(rule model.RuleLibraryItem) bool { return textOr(rule.SourceType, "url") == "url" }

// StoredRule reports whether a snapshot still declares this exact source.
func StoredRule(ctx context.Context, rule model.RuleLibraryItem) (bool, error) {
	if !urlRule(rule) || textOr(rule.URL, "") == "" || rule.ID == "" {
		return false, nil
	}
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return false, err
	}
	var found bool
	err = r.View(ctx, func(document *model.Document) error {
		for _, item := range document.Shared.RuleLibrary {
			if item.ID == rule.ID {
				found = sameRuleDeclaration(item, rule)
				break
			}
		}
		return nil
	})
	return found, err
}
func sameRuleDeclaration(a, b model.RuleLibraryItem) bool {
	return equalStoredMembers(a, b, []string{"url", "behavior", "format", "source_type"})
}

// roundNoticeBatch coalesces the notification refresh of one scheduled round:
// each resource's status receipt is still written immediately, but the
// notification snapshot runs once after every worker of the round finished
// (and released its running lock) instead of once per receipt. Direct and
// manual refreshes have no batch and refresh notifications per receipt.
type roundNoticeBatchKey struct{}

type roundNoticeBatch struct {
	recorded atomic.Bool
}

func recordStatus(ctx context.Context, key string, result taskstatus.Result) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return err
	}
	batch, _ := ctx.Value(roundNoticeBatchKey{}).(*roundNoticeBatch)
	store := notifications.TaskStatus(ctx, r.DataDir())
	if batch != nil {
		store = taskstatus.New(r.DataDir())
	}
	store.Now = refreshEnv().Clock.Now
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err = store.SaveResult(ctx, key, result); err != nil {
		return err
	}
	if batch != nil {
		batch.recorded.Store(true)
	}
	return nil
}
func taskStatusOf(ctx context.Context) (*taskstatus.Store, error) {
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	store := taskstatus.New(r.DataDir())
	store.Now = refreshEnv().Clock.Now
	return store, nil
}

// RefreshSource downloads nodes and records health/status under one generation.
func RefreshSource(ctx context.Context, sub model.Subscription, trigger string) ([]model.Node, *SourceResult, *NodeCache, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, nil, nil, err
	}
	if sub.ID == "" {
		return nil, nil, nil, repo.NewValidationError(msgSourceIdentity)
	}
	key := SourceTaskKey(sub.ID)
	started, clock := nowISO(), refreshEnv().Clock.Monotonic()
	nodes, result, cache, failure := DownloadNodes(ctx, sub)
	if err := ctx.Err(); err != nil {
		return nil, nil, nil, err
	}
	if failure != nil {
		previous, err := refreshEnv().Sources.LoadNodes(ctx, sub.ID)
		if err != nil {
			return nil, nil, nil, err
		}
		retained := previous != nil && len(previous.Nodes) > 0
		count := model.Int(0)
		if previous != nil {
			count = previous.Count
		}
		if _, err := RecordHealth(ctx, sub.ID, FetchEventInput{FromCache: retained, Count: count}); err != nil {
			return nil, nil, nil, err
		}
		reason := FailureReason(failure)
		status := taskstatus.Result{OK: false, StartedAt: started, Trigger: trigger, Error: reason, DurationMs: elapsedMs(clock), CacheRetained: &retained}
		if retained {
			status.CacheUpdatedAt = textOr(previous.UpdatedAt, "")
		}
		if err := recordStatus(ctx, key, status); err != nil {
			return nil, nil, nil, err
		}
		suffix := ""
		if retained {
			suffix = msgRetainedNodes
		}
		logger.Warningf(msgSubscriptionFailed, safeName(sub.Name, sub.ID), suffix, reason)
		return nil, nil, nil, failure
	}
	count := len(refreshEnv().Sources.FilterNodes(sub, nodes))
	if _, err := RecordHealth(ctx, sub.ID, FetchEventInput{OK: true, Count: model.Int(count), Traffic: result.Traffic}); err != nil {
		return nil, nil, nil, err
	}
	updated := ""
	if cache != nil {
		updated = textOr(cache.UpdatedAt, "")
	}
	if err := recordStatus(ctx, key, taskstatus.Result{OK: true, StartedAt: started, Trigger: trigger, DurationMs: elapsedMs(clock), CacheUpdatedAt: updated}); err != nil {
		return nil, nil, nil, err
	}
	return nodes, result, cache, nil
}

// RuleReadOptions supplies a native declaration snapshot and download owner.
type RuleReadOptions struct {
	Config     *rulefetch.SourceConfig
	Fetch      func(context.Context, string) (rulefetch.Content, error)
	Timeout    time.Duration
	AllowStale bool
}

func (o RuleReadOptions) cacheOptions(timeout time.Duration) rulefetch.CacheOptions {
	if o.Timeout != 0 {
		timeout = o.Timeout
	}
	client := refreshEnv().RuleClient
	if client == nil {
		client = rulefetch.DefaultClient
	}
	return rulefetch.CacheOptions{Timeout: timeout, Fetch: o.Fetch, AllowStale: o.AllowStale, Client: client}
}

// RefreshRemote refreshes a declared source and records its task outcome.
func RefreshRemote(ctx context.Context, rule model.RuleLibraryItem, trigger string, opt RuleReadOptions) ([]byte, error, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if rule.ID == "" {
		return nil, nil, repo.NewValidationError(msgRuleIdentity)
	}
	key := RuleTaskKey(rule.ID)
	started, clock := nowISO(), refreshEnv().Clock.Monotonic()
	content, refreshErr, failure := rulefetch.RefreshRule(ctx, rules.LibraryRule(rule), opt.Config, opt.cacheOptions(RuleTimeout*time.Second))
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if failure != nil {
		cached, err := rulefetch.ReadCache(ctx, rules.LibraryRule(rule), opt.Config)
		if err != nil {
			return nil, nil, err
		}
		retained := cached != nil
		status := taskstatus.Result{OK: false, StartedAt: started, Trigger: trigger, Error: FailureReason(failure), DurationMs: elapsedMs(clock), CacheRetained: &retained}
		if retained {
			at, err := RuleCacheAt(ctx, rule)
			if err != nil {
				return nil, nil, err
			}
			status.CacheUpdatedAt = textOr(at, "")
		}
		if err := recordStatus(ctx, key, status); err != nil {
			return nil, nil, err
		}
		if trigger == "schedule" {
			suffix := ""
			if retained {
				suffix = msgRetainedRule
			}
			logger.Warningf(msgRuleFailed, safeName(rule.Name, rule.ID), suffix, FailureReason(failure))
		}
		return nil, nil, failure
	}
	status := taskstatus.Result{OK: true, StartedAt: started, Trigger: trigger, DurationMs: elapsedMs(clock), CacheUpdatedAt: nowISO()}
	if refreshErr != nil {
		retained := true
		at, err := RuleCacheAt(ctx, rule)
		if err != nil {
			return nil, nil, err
		}
		status.OK = false
		status.Error = FailureReason(refreshErr)
		status.CacheRetained = &retained
		status.CacheUpdatedAt = textOr(at, "")
	}
	if err := recordStatus(ctx, key, status); err != nil {
		return nil, nil, err
	}
	return content, refreshErr, nil
}

// ReadRemote serves validated bytes and never extends a cache's age on a poll.
func ReadRemote(ctx context.Context, rule model.RuleLibraryItem, opt RuleReadOptions) ([]byte, error, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	opt.AllowStale = true
	stored, err := StoredRule(ctx, rule)
	if err != nil {
		return nil, nil, err
	}
	if !stored {
		return rulefetch.RefreshRule(ctx, rules.LibraryRule(rule), opt.Config, opt.cacheOptions(10*time.Second))
	}
	cached, err := rulefetch.ReadCache(ctx, rules.LibraryRule(rule), opt.Config)
	if err != nil {
		return nil, nil, err
	}
	if cached != nil {
		store, err := taskStatusOf(ctx)
		if err != nil {
			return nil, nil, err
		}
		statuses, err := store.Read()
		if err != nil {
			return nil, nil, err
		}
		entry := statuses.Entry(RuleTaskKey(rule.ID))
		if textOr(entry.LastStatus, "") == "failed" {
			message := textOr(entry.LastError, msgRuleRefreshFailed)
			if message == "" {
				message = msgRuleRefreshFailed
			}
			return cached, &StaleRuleCache{Message: message}, nil
		}
		return cached, nil, nil
	}
	if opt.Timeout == 0 {
		opt.Timeout = 10 * time.Second
	}
	return RefreshRemote(ctx, rule, "pull", opt)
}

// Resource is a typed subscription or rule source snapshot, never a generic map.
type Resource struct {
	Kind         string
	Subscription *model.Subscription
	Rule         *model.RuleLibraryItem
}

func (r Resource) id() string {
	if r.Subscription != nil {
		return r.Subscription.ID
	}
	if r.Rule != nil {
		return r.Rule.ID
	}
	return ""
}
func (r Resource) key() string {
	if r.Kind == "subscription" {
		return SourceTaskKey(r.id())
	}
	return RuleTaskKey(r.id())
}
func due(ctx context.Context, now *time.Time) ([]Resource, error) {
	if now == nil {
		current := nowUTC()
		now = &current
	}
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	shared, err := r.Shared(ctx)
	if err != nil {
		return nil, err
	}
	store, err := taskStatusOf(ctx)
	if err != nil {
		return nil, err
	}
	statuses, err := store.Read()
	if err != nil {
		return nil, err
	}
	var jobs []Resource
	for _, sub := range shared.Subscriptions {
		// A placeholder address (the new data directory's template) is not
		// scheduled at all: no failure every few minutes, no Sub-Store wake-up.
		if !enabledResource(sub.Enabled) || !substore.SourceURLConfigured(sub.URL) {
			continue
		}
		baseline, err := SourceCacheAt(ctx, sub)
		if err != nil {
			return nil, err
		}
		at, err := NextDue(statuses.Entry(SourceTaskKey(sub.ID)), SourceInterval(sub), baseline, now)
		if err != nil {
			return nil, err
		}
		if at != nil && !at.After(*now) {
			item := sub
			jobs = append(jobs, Resource{Kind: "subscription", Subscription: &item})
		}
	}
	for _, rule := range shared.RuleLibrary {
		if !urlRule(rule) || textOr(rule.URL, "") == "" || !enabledResource(rule.Enabled) {
			continue
		}
		baseline, err := RuleCacheAt(ctx, rule)
		if err != nil {
			return nil, err
		}
		at, err := NextDue(statuses.Entry(RuleTaskKey(rule.ID)), RemoteInterval(rule), baseline, now)
		if err != nil {
			return nil, err
		}
		if at != nil && !at.After(*now) {
			item := rule
			jobs = append(jobs, Resource{Kind: "rule_source", Rule: &item})
		}
	}
	return jobs, nil
}
func runResource(ctx context.Context, resource Resource, trigger string) {
	scope := repo.WithScope(ctx)
	defer repo.ResetConfigContext(scope)
	defer func() {
		if recover() != nil {
			logger.Errorf(msgRefreshPanic, resource.Kind)
		}
	}()
	if scope.Err() != nil {
		return
	}
	if resource.Subscription != nil {
		_, _, _, _ = RefreshSource(scope, *resource.Subscription, trigger)
	} else if resource.Rule != nil {
		_, _, _ = RefreshRemote(scope, *resource.Rule, trigger, RuleReadOptions{})
	}
}

// RunRound refreshes due sources with bounded joined workers. A stopping
// generation admits no more work and retains its original cancellation parent.
func RunRound(ctx context.Context, now *time.Time) (int, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	candidates, err := due(ctx, now)
	if err != nil {
		return 0, err
	}
	batch := &roundNoticeBatch{}
	roundCtx := context.WithValue(ctx, roundNoticeBatchKey{}, batch)
	flushNotices := func() {
		// Like the per-receipt hook, this runs detached even while stopping.
		if batch.recorded.Load() {
			notifications.Safely(func() error { return notifications.RefreshTasks(ctx, "") })
		}
	}
	type claimed struct {
		resource Resource
		ctx      context.Context
		release  func()
	}
	var jobs []claimed
	for _, resource := range candidates {
		if err := ctx.Err(); err != nil {
			for _, pending := range jobs {
				pending.release()
			}
			return 0, err
		}
		held, release, ok := running.TryLock(roundCtx, resource.key())
		if ok {
			jobs = append(jobs, claimed{resource, held, release})
		}
	}
	slots := make(chan struct{}, Workers)
	var wg sync.WaitGroup
	count := 0
	stop := func(index int) (int, error) {
		for _, pending := range jobs[index:] {
			pending.release()
		}
		wg.Wait()
		flushNotices()
		return count, ctx.Err()
	}
	for i, item := range jobs {
		select {
		case slots <- struct{}{}:
		case <-lifecycle.Stopping(ctx):
			return stop(i)
		}
		select {
		case <-lifecycle.Stopping(ctx):
			<-slots
			return stop(i)
		default:
		}
		wg.Add(1)
		count++
		go func(item claimed) {
			defer wg.Done()
			defer func() { <-slots }()
			defer item.release()
			runResource(item.ctx, item.resource, "schedule")
		}(item)
	}
	wg.Wait()
	flushNotices()
	return count, ctx.Err()
}

// LookupResource resolves a task against the current repository snapshot.
func LookupResource(ctx context.Context, kind, id string) (Resource, error) {
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return Resource{}, err
	}
	shared, err := r.Shared(ctx)
	if err != nil {
		return Resource{}, err
	}
	if kind == "subscription" {
		for _, item := range shared.Subscriptions {
			if item.ID == id {
				if item.URL == "" {
					return Resource{}, repo.NewValidationError(msgNoSourceAddress)
				}
				if !substore.SourceURLConfigured(item.URL) {
					return Resource{}, repo.NewValidationError(msgInvalidSourceURL)
				}
				return Resource{Kind: kind, Subscription: &item}, nil
			}
		}
	} else {
		for _, item := range shared.RuleLibrary {
			if item.ID == id {
				if !urlRule(item) || textOr(item.URL, "") == "" {
					return Resource{}, repo.NewValidationError(msgNoSourceAddress)
				}
				return Resource{Kind: "rule_source", Rule: &item}, nil
			}
		}
	}
	return Resource{}, &NotFoundError{Message: msgSourceGone}
}

// RunResourceNow admits one manual refresh and retains the keyed job claim.
func RunResourceNow(ctx context.Context, kind, id string) (string, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return "", err
	}
	resource, err := LookupResource(ctx, kind, id)
	if err != nil {
		return "", err
	}
	key := resource.key()
	held, release, ok := running.TryLock(ctx, key)
	if !ok {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		return "", &TaskBusy{Message: msgTaskBusy}
	}
	if err := lifecycle.Spawn(held, "task-"+key, func(work context.Context) { defer release(); runResource(work, resource, "manual") }); err != nil {
		release()
		return "", err
	}
	return key, nil
}
func enabledResource(value *bool) bool {
	return model.Flag(value, true)
}
