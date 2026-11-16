package refresh

// Port of backend/utils/scheduled_refresh.py: server-side scheduled refresh
// of subscriptions and remote rule sources. Devices, OxiDNS and
// publications read the validated caches; the loop downloads each source
// when its interval is due, keeps the last valid cache when a download or
// validation fails, and records every run for 系统设置 → 定时任务. A source
// without a valid cache yet is downloaded on first use instead of waiting
// for the next round.

import (
	"context"
	"fmt"
	"math"
	"math/big"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"configflow/internal/jsonx"
	"configflow/internal/logx"
	"configflow/internal/notifications"
	"configflow/internal/repo"
	"configflow/internal/rulefetch"
	"configflow/internal/subs"
	"configflow/internal/substore"
	"configflow/internal/taskstatus"
)

// Constants of scheduled_refresh.py. A failed source retries sooner than
// its interval: 5, 10, 20 … minutes, never later than the interval itself.
const (
	DefaultRuleInterval = 86400
	RetrySeconds        = 300
	TickSeconds         = 30
	FirstDelaySeconds   = 30
	Workers             = 3
	RuleTimeout         = 30
)

var logger = logx.Get("backend.utils.scheduled_refresh")

var (
	runningMu sync.Mutex
	running   = map[string]bool{}
)

// TaskBusy is scheduled_refresh.TaskBusy (a RuntimeError).
type TaskBusy struct{ Message string }

func (e *TaskBusy) Error() string       { return e.Message }
func (e *TaskBusy) PythonClass() string { return "TaskBusy" }

// StaleRuleCache is a valid cache served while the source's last refresh
// failed.
type StaleRuleCache struct{ Message string }

func (e *StaleRuleCache) Error() string       { return e.Message }
func (e *StaleRuleCache) PythonClass() string { return "StaleRuleCache" }

// LookupError is find_resource's LookupError (404 in the task routes).
type LookupError struct{ Message string }

func (e *LookupError) Error() string       { return e.Message }
func (e *LookupError) PythonClass() string { return "LookupError" }

// SubscriptionKey is subscription_key(sub_id).
func SubscriptionKey(id any) string { return "subscription:" + jsonx.Str(id) }

// RuleSourceKey is rule_source_key(rule_id).
func RuleSourceKey(id any) string { return "rule_source:" + jsonx.Str(id) }

// IsRunning is is_running(key).
func IsRunning(key string) bool {
	runningMu.Lock()
	defer runningMu.Unlock()
	return running[key]
}

func claim(key string) bool {
	runningMu.Lock()
	defer runningMu.Unlock()
	if running[key] {
		return false
	}
	running[key] = true
	return true
}

func release(key string) {
	runningMu.Lock()
	delete(running, key)
	runningMu.Unlock()
}

// DescribeFailure is describe_failure(error): a readable reason that never
// renders a URL, token or response body.
func DescribeFailure(err error) string {
	if fetch, ok := err.(*substore.SubscriptionFetchError); ok {
		switch {
		case fetch.DirectError != nil:
			err = fetch.DirectError
		case fetch.SubStoreError != nil:
			err = fetch.SubStoreError
		}
	}
	switch err.(type) {
	case *rulefetch.RemoteRuleValidationError, *StaleRuleCache:
		return err.Error()
	}
	if status, ok := responseStatus(err); ok {
		return fmt.Sprintf("上游返回 HTTP %d", status)
	}
	switch {
	case isInstance(err, "MissingSchema"), isInstance(err, "InvalidSchema"), isInstance(err, "InvalidURL"):
		return "地址无效，请检查是否为完整的 http(s) 地址"
	case isInstance(err, "SSLError"):
		return "TLS 证书校验失败"
	case isInstance(err, "Timeout"):
		return "连接上游超时"
	case isInstance(err, "ConnectionError"):
		return "无法连接上游"
	}
	if class := pythonClass(err); class == "ValueError" || class == "StrategyReferenceError" {
		return err.Error()
	}
	return "更新失败（" + strings.TrimPrefix(safeExceptionDetails(err), "exception_type=") + "）"
}

// timedelta(seconds=…) bound and the datetime range.
var (
	maxTimedeltaDays   = big.NewInt(999999999)
	minDatetimeSeconds = time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC).Unix()
	maxDatetimeSeconds = time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC).Unix()
)

// addSeconds is `t + timedelta(seconds=delay)` for an aware datetime, with
// Python's OverflowErrors (timedelta magnitude, result outside years
// 1–9999 in t's own offset).
func addSeconds(t time.Time, delay *big.Int) (time.Time, error) {
	// timedelta(seconds=s) floors s / 86400 into days (|days| <= 999999999).
	days := new(big.Int).Div(delay, big.NewInt(86400)) // Euclidean = floor for a positive divisor
	if new(big.Int).Abs(days).Cmp(maxTimedeltaDays) > 0 {
		return time.Time{}, pyErr("OverflowError", "days=%s; must have magnitude <= 999999999", days.String())
	}
	seconds := delay.Int64()
	_, offset := t.Zone()
	local := t.Unix() + int64(offset) + seconds
	if local < minDatetimeSeconds || local > maxDatetimeSeconds {
		return time.Time{}, pyErr("OverflowError", "date value out of range")
	}
	return time.Unix(t.Unix()+seconds, int64(t.Nanosecond())).In(t.Location()), nil
}

// nowUTC is datetime.now(timezone.utc) (microsecond resolution).
func nowUTC() time.Time {
	return currentEnv().Clock.Now().UTC().Truncate(time.Microsecond)
}

// nowISO is task_status.now_iso() on the environment's clock.
func nowISO() string { return currentEnv().Clock.Now().UTC().Format("2006-01-02T15:04:05Z") }

// tzSuffix is the UTC offset of an ISO time ([+-]HH[[:]MM[[:]SS[.ffffff]]]).
var tzSuffix = regexp.MustCompile(`^[+-][0-9]{2}(:?[0-9]{2}(:?[0-9]{2}([.,][0-9]+)?)?)?$`)

// parseTime is task_status.parse_time keeping the parsed UTC offset (an
// aware datetime keeps its tzinfo in Python, and isoformat prints it):
// taskstatus.ParseTime gives the instant; the offset is the difference to
// the naive local fields before the offset suffix. Sub-second offsets are
// cut to seconds.
func parseTime(value any) (time.Time, bool) {
	instant, ok := taskstatus.ParseTime(value)
	if !ok {
		return time.Time{}, false
	}
	text := strings.ReplaceAll(value.(string), "Z", "+00:00")
	if i := strings.LastIndexAny(text, "+-"); i >= 7 && tzSuffix.MatchString(text[i:]) {
		if local, ok := taskstatus.FromISOFormat(text[:i]); ok {
			offset := local.Sub(instant)
			return instant.In(time.FixedZone("", int(offset/time.Second))), true
		}
	}
	return instant, true
}

// attributeGet is Python's AttributeError for value.get(...).
func attributeGet(value any) error {
	return pyErr("AttributeError", "'%s' object has no attribute 'get'", pyTypeName(value))
}

// NextRunAt is next_run_at(entry, interval, baseline, now): when a source is
// due next, nil when its automatic updates are off. A source that never ran
// and has no cache is due now (now, or the current time when now is nil).
// entry is a task-status entry (`statuses.get(key) or {}`: anything a
// hand-edited file holds; .get on a non-object is Python's AttributeError).
func NextRunAt(entry any, interval jsonx.Number, baseline any, now *time.Time) (*time.Time, error) {
	if !jsonx.Truthy(interval) {
		return nil, nil
	}
	object, ok := entry.(*jsonx.Object)
	if !ok {
		return nil, attributeGet(entry)
	}
	last, ok := parseTime(object.Value("last_run_at"))
	if !ok {
		last, ok = parseTime(baseline)
	}
	if !ok {
		if now != nil {
			return now, nil
		}
		current := nowUTC()
		return &current, nil
	}
	delay, _ := interval.BigInt()
	if jsonx.Equal(object.Value("last_status"), "failed") {
		failures, err := pyIntValue(orDefault(object.Value("consecutive_failures"), jsonx.Int(1)))
		if err != nil {
			return nil, err
		}
		// max(1, failures) - 1, capped at 10 (failures may be any int).
		exponent := int64(0)
		switch {
		case failures.Cmp(big.NewInt(11)) >= 0:
			exponent = 10
		case failures.Sign() > 0:
			exponent = failures.Int64() - 1
		}
		retry := big.NewInt(RetrySeconds << exponent)
		if retry.Cmp(delay) < 0 {
			delay = retry
		}
	}
	due, err := addSeconds(last, delay)
	if err != nil {
		return nil, err
	}
	return &due, nil
}

// metadataOf is `cache.get('metadata') or {}`.
func metadataOf(cache *jsonx.Object) (*jsonx.Object, error) {
	value := cache.Value("metadata")
	if !jsonx.Truthy(value) {
		return jsonx.NewObject(), nil
	}
	metadata, ok := value.(*jsonx.Object)
	if !ok {
		return nil, attributeGet(value)
	}
	return metadata, nil
}

// SubscriptionCacheTime is subscription_cache_time(sub): when the stored
// nodes of the current URL were fetched (the cache's updated_at value), nil
// when unknown.
func SubscriptionCacheTime(ctx context.Context, sub *jsonx.Object) (any, error) {
	id, err := requiredString(sub, "id")
	if err != nil {
		return nil, err
	}
	cache, err := substore.LoadSubscriptionCache(ctx, id)
	if err != nil {
		return nil, err
	}
	if cache == nil || !jsonx.Truthy(cache.Value("nodes")) {
		return nil, nil
	}
	metadata, err := metadataOf(cache)
	if err != nil {
		return nil, err
	}
	if jsonx.Equal(metadata.Value("url"), sub.Value("url")) {
		return cache.Value("updated_at"), nil
	}
	return nil, nil
}

// roundHalfEven is _PyTime_RoundHalfEven.
func roundHalfEven(x float64) float64 {
	rounded := math.Round(x)
	if math.Abs(x-rounded) == 0.5 {
		rounded = 2.0 * math.Round(x/2.0)
	}
	return rounded
}

// mtimeSeconds is the whole seconds of datetime.fromtimestamp(st_mtime):
// st_mtime is sec + nsec * 1e-9 as a double, and fromtimestamp rounds its
// fraction to microseconds half to even (a carry adds a second).
func mtimeSeconds(sec, nsec int64) int64 {
	value := float64(sec) + float64(nsec)*1e-9
	intpart, floatpart := math.Modf(value)
	floatpart = roundHalfEven(floatpart * 1e6)
	if floatpart >= 1e6 {
		intpart += 1.0
	} else if floatpart < 0 {
		intpart -= 1.0
	}
	return int64(intpart)
}

// RuleCacheTime is rule_cache_time(rule): the modification time of the
// rule's cache file ("2026-11-14T12:00:00Z"), nil when there is none.
func RuleCacheTime(ctx context.Context, rule *jsonx.Object) (any, error) {
	path, err := rulefetch.RuleCacheReadPath(ctx, rule, nil)
	if err != nil {
		if repo.IsValueError(err) || isOSError(err) {
			return nil, nil
		}
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, nil
	}
	mtime := info.ModTime()
	seconds := mtimeSeconds(mtime.Unix(), int64(mtime.Nanosecond()))
	return time.Unix(seconds, 0).UTC().Format("2006-01-02T15:04:05Z"), nil
}

// isURLSource is `item.get('source_type', 'url') == 'url'`.
func isURLSource(item *jsonx.Object) bool {
	return jsonx.Equal(item.GetOr("source_type", "url"), "url")
}

// IsLibrarySource is is_library_source(rule): whether this declaration is
// a stored library source the task refreshes.
func IsLibrarySource(ctx context.Context, rule *jsonx.Object) (bool, error) {
	if !isURLSource(rule) || !jsonx.Truthy(rule.Value("url")) || rule.Value("id") == nil {
		return false, nil
	}
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return false, err
	}
	shared, err := r.GetShared(ctx)
	if err != nil {
		return false, err
	}
	stored, err := nextObject(shared.GetOr("rule_library", []any{}), func(item *jsonx.Object) bool {
		return jsonx.Equal(item.Value("id"), rule.Value("id"))
	})
	if err != nil || stored == nil {
		return false, err
	}
	for _, key := range []string{"url", "behavior", "format", "source_type"} {
		if !jsonx.Equal(stored.Value(key), rule.Value(key)) {
			return false, nil
		}
	}
	return true, nil
}

// nextObject is `next((item for item in value if match(item)), None)`
// where match calls item.get: items are visited in order and the search
// stops at the first match, so an item that is not an object fails only
// when it is reached (AttributeError), like objectsOf otherwise.
func nextObject(value any, match func(*jsonx.Object) bool) (*jsonx.Object, error) {
	list, ok := value.([]any)
	if !ok {
		_, err := objectsOf(value)
		return nil, err
	}
	for _, item := range list {
		object, ok := item.(*jsonx.Object)
		if !ok {
			return nil, attributeGet(item)
		}
		if match(object) {
			return object, nil
		}
	}
	return nil, nil
}

// objectsOf iterates a list of objects read with .get(key, []); an item
// that is not an object fails where Python's item.get would.
func objectsOf(value any) ([]*jsonx.Object, error) {
	list, ok := value.([]any)
	if !ok {
		if object, isObject := value.(*jsonx.Object); isObject {
			// Iterating a dict yields its keys (strings): .get fails.
			if object.Len() > 0 {
				return nil, attributeGet("")
			}
			return nil, nil
		}
		if value == nil {
			return nil, pyErr("TypeError", "'NoneType' object is not iterable")
		}
		if text, isText := value.(string); isText {
			if text != "" {
				return nil, attributeGet("")
			}
			return nil, nil
		}
		return nil, pyErr("TypeError", "'%s' object is not iterable", pyTypeName(value))
	}
	out := make([]*jsonx.Object, 0, len(list))
	for _, item := range list {
		object, ok := item.(*jsonx.Object)
		if !ok {
			return nil, attributeGet(item)
		}
		out = append(out, object)
	}
	return out, nil
}

// recordStatus is task_status.record(key, …) through the notifications.
func recordStatus(ctx context.Context, key string, result taskstatus.Result) error {
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return err
	}
	store := notifications.TaskStatus(ctx, r.DataDir())
	store.Now = currentEnv().Clock.Now
	_, err = store.Record(key, result)
	return err
}

// taskStatusOf is the task-status store of the repository (reads).
func taskStatusOf(ctx context.Context) (*taskstatus.Store, error) {
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	store := taskstatus.New(r.DataDir())
	store.Now = currentEnv().Clock.Now
	return store, nil
}

func elapsedMs(start float64) float64 { return (currentEnv().Clock.Monotonic() - start) * 1000 }

// RefreshSubscription is refresh_subscription(sub, trigger): download and
// store one subscription, record the fetch and the run; it returns (nodes,
// result, cache). A failure keeps the previous nodes and is returned after
// it was recorded and logged.
func RefreshSubscription(ctx context.Context, sub *jsonx.Object, trigger string) ([]any, *substore.SubscriptionResult, *jsonx.Object, error) {
	ctx = detached(ctx)
	id, err := requiredString(sub, "id")
	if err != nil {
		return nil, nil, nil, err
	}
	key := SubscriptionKey(id)
	started, clock := nowISO(), currentEnv().Clock.Monotonic()
	nodes, result, cache, failure := DownloadSubscriptionNodes(ctx, sub)
	if failure != nil {
		previous, err := substore.LoadSubscriptionCache(ctx, id)
		if err != nil {
			return nil, nil, nil, err
		}
		retained := jsonx.Truthy(previous.Value("nodes"))
		if _, err := RecordFetch(ctx, id, FetchRecord{OK: false, FromCache: retained,
			Count: orDefault(previous.Value("count"), jsonx.Int(0))}); err != nil {
			return nil, nil, nil, err
		}
		reason := DescribeFailure(failure)
		status := taskstatus.Result{OK: false, StartedAt: started, Trigger: trigger, Error: reason,
			DurationMs: elapsedMs(clock), CacheRetained: &retained}
		if retained {
			status.CacheUpdatedAt, _ = previous.Value("updated_at").(string)
		}
		if err := recordStatus(ctx, key, status); err != nil {
			return nil, nil, nil, err
		}
		suffix := ""
		if retained {
			suffix = "，继续使用原有缓存"
		}
		logger.Warningf("订阅「%s」更新失败%s：%s", logx.LogText(orDefault(sub.Value("name"), id), 120), suffix, reason)
		return nil, nil, nil, failure
	}
	if _, err := RecordFetch(ctx, id, FetchRecord{OK: true, Count: jsonx.Int(len(subs.FilterSubscriptionItems(sub, nodes))),
		Traffic: result.Traffic}); err != nil {
		return nil, nil, nil, err
	}
	updated, _ := cache.Value("updated_at").(string)
	if err := recordStatus(ctx, key, taskstatus.Result{OK: true, StartedAt: started, Trigger: trigger,
		DurationMs: elapsedMs(clock), CacheUpdatedAt: updated}); err != nil {
		return nil, nil, nil, err
	}
	return nodes, result, cache, nil
}

// RuleOptions are the keyword arguments of refresh_rule_source and
// read_rule_source.
type RuleOptions struct {
	// Config is config_data (nil: None).
	Config *jsonx.Object
	// Fetch replaces the download (fetch=); it returns string or []byte.
	Fetch func(url string) (any, error)
	// Timeout of the download (0: RuleTimeout for refresh_rule_source, 10 s
	// for read_rule_source).
	Timeout time.Duration
	// AllowStale serves validated previous bytes when the refresh fails.
	AllowStale bool
}

func (o RuleOptions) refresh(timeout time.Duration) rulefetch.RefreshOptions {
	if o.Timeout != 0 {
		timeout = o.Timeout
	}
	return rulefetch.RefreshOptions{Timeout: timeout, Fetch: o.Fetch, AllowStale: o.AllowStale,
		Client: currentEnv().ruleClient()}
}

func ruleCacheTimeText(ctx context.Context, rule *jsonx.Object) (string, error) {
	value, err := RuleCacheTime(ctx, rule)
	text, _ := value.(string)
	return text, err
}

// RefreshRuleSource is refresh_rule_source(rule, trigger, config_data=,
// fetch=, timeout=, allow_stale=): download and validate one library
// source and record the run. It returns the bytes and the refresh error
// when validated previous bytes were served (AllowStale); a failure is
// recorded (and logged for scheduled runs) and returned.
func RefreshRuleSource(ctx context.Context, rule *jsonx.Object, trigger string, opts RuleOptions) ([]byte, error, error) {
	ctx = detached(ctx)
	id, present := rule.Get("id")
	if !present {
		return nil, nil, pyErr("KeyError", "'id'")
	}
	key := RuleSourceKey(id)
	started, clock := nowISO(), currentEnv().Clock.Monotonic()
	content, refreshErr, failure := rulefetch.RefreshRemoteRule(ctx, rule, opts.Config, opts.refresh(RuleTimeout*time.Second))
	if failure != nil {
		cached, err := rulefetch.ReadValidatedRuleCache(ctx, rule, opts.Config)
		if err != nil {
			return nil, nil, err
		}
		retained := cached != nil
		status := taskstatus.Result{OK: false, StartedAt: started, Trigger: trigger, Error: DescribeFailure(failure),
			DurationMs: elapsedMs(clock), CacheRetained: &retained}
		if retained {
			if status.CacheUpdatedAt, err = ruleCacheTimeText(ctx, rule); err != nil {
				return nil, nil, err
			}
		}
		if err := recordStatus(ctx, key, status); err != nil {
			return nil, nil, err
		}
		if trigger == "schedule" {
			suffix := ""
			if retained {
				suffix = "，继续使用上一次有效缓存"
			}
			logger.Warningf("规则集「%s」更新失败%s：%s", logx.LogText(orDefault(rule.Value("name"), id), 120), suffix,
				DescribeFailure(failure))
		}
		return nil, nil, failure
	}
	duration := elapsedMs(clock)
	if refreshErr != nil {
		// AllowStale served the previous bytes; refresh_remote_rule logged it.
		retained := true
		updated, err := ruleCacheTimeText(ctx, rule)
		if err != nil {
			return nil, nil, err
		}
		if err := recordStatus(ctx, key, taskstatus.Result{OK: false, StartedAt: started, Trigger: trigger,
			Error: DescribeFailure(refreshErr), DurationMs: duration, CacheRetained: &retained, CacheUpdatedAt: updated}); err != nil {
			return nil, nil, err
		}
	} else if err := recordStatus(ctx, key, taskstatus.Result{OK: true, StartedAt: started, Trigger: trigger,
		DurationMs: duration, CacheUpdatedAt: nowISO()}); err != nil {
		return nil, nil, err
	}
	return content, refreshErr, nil
}

// ReadRuleSource is read_rule_source(rule, config_data, fetch=, timeout=):
// a consumer's view of a remote source, (bytes, refresh error). Library
// sources come from the cache the scheduled task maintains; only a source
// without a valid cache yet is downloaded now. Declarations outside the
// library have no task and still refresh on each read.
func ReadRuleSource(ctx context.Context, rule *jsonx.Object, opts RuleOptions) ([]byte, error, error) {
	ctx = detached(ctx)
	opts.AllowStale = true
	library, err := IsLibrarySource(ctx, rule)
	if err != nil {
		return nil, nil, err
	}
	if !library {
		return rulefetch.RefreshRemoteRule(ctx, rule, opts.Config, opts.refresh(10*time.Second))
	}
	cached, err := rulefetch.ReadValidatedRuleCache(ctx, rule, opts.Config)
	if err != nil {
		return nil, nil, err
	}
	if cached != nil {
		store, err := taskStatusOf(ctx)
		if err != nil {
			return nil, nil, err
		}
		entry, err := store.Get(RuleSourceKey(rule.Value("id")))
		if err != nil {
			return nil, nil, err
		}
		if jsonx.Equal(entry.Value("last_status"), "failed") {
			message := orDefault(entry.Value("last_error"), "规则集更新失败")
			return cached, &StaleRuleCache{Message: jsonx.Str(message)}, nil
		}
		return cached, nil, nil
	}
	if opts.Timeout == 0 {
		opts.Timeout = 10 * time.Second
	}
	return RefreshRuleSource(ctx, rule, "pull", opts)
}

// job is one due refresh: its task key, kind and resource.
type job struct {
	key      string
	kind     string
	resource *jsonx.Object
}

// entryOr is `statuses.get(key) or {}`.
func entryOr(statuses *jsonx.Object, key string) any {
	return orDefault(statuses.Value(key), jsonx.NewObject())
}

// due is _due(now): the sources whose refresh is due.
func due(ctx context.Context, now *time.Time) ([]job, error) {
	if now == nil {
		current := nowUTC()
		now = &current
	}
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	shared, err := r.GetShared(ctx)
	if err != nil {
		return nil, err
	}
	store, err := taskStatusOf(ctx)
	if err != nil {
		return nil, err
	}
	statuses, err := store.Load()
	if err != nil {
		return nil, err
	}
	var jobs []job
	subscriptions, err := objectsOf(shared.GetOr("subscriptions", []any{}))
	if err != nil {
		return nil, err
	}
	for _, sub := range subscriptions {
		if !jsonx.Truthy(sub.GetOr("enabled", true)) || !jsonx.Truthy(sub.Value("url")) {
			continue
		}
		id, present := sub.Get("id")
		if !present {
			return nil, pyErr("KeyError", "'id'")
		}
		key := SubscriptionKey(id)
		entry := entryOr(statuses, key)
		baseline, err := SubscriptionCacheTime(ctx, sub)
		if err != nil {
			return nil, err
		}
		at, err := NextRunAt(entry, SubscriptionInterval(sub), baseline, now)
		if err != nil {
			return nil, err
		}
		if at != nil && !at.After(*now) {
			jobs = append(jobs, job{key, "subscription", sub})
		}
	}
	library, err := objectsOf(shared.GetOr("rule_library", []any{}))
	if err != nil {
		return nil, err
	}
	for _, rule := range library {
		if !isURLSource(rule) || !jsonx.Truthy(rule.Value("url")) || !jsonx.Truthy(rule.GetOr("enabled", true)) {
			continue
		}
		id, present := rule.Get("id")
		if !present {
			return nil, pyErr("KeyError", "'id'")
		}
		key := RuleSourceKey(id)
		entry := entryOr(statuses, key)
		baseline, err := RuleCacheTime(ctx, rule)
		if err != nil {
			return nil, err
		}
		at, err := NextRunAt(entry, RuleInterval(rule), baseline, now)
		if err != nil {
			return nil, err
		}
		if at != nil && !at.After(*now) {
			jobs = append(jobs, job{key, "rule_source", rule})
		}
	}
	return jobs, nil
}

// run is _run(kind, resource, trigger): failures are recorded in
// task_status and logged by the refresh itself; the request scope is reset
// afterwards.
func run(ctx context.Context, kind string, resource *jsonx.Object, trigger string) {
	scope := repo.WithScope(ctx)
	defer repo.ResetConfigContext(scope)
	defer func() {
		if p := recover(); p != nil {
			logger.Errorf("scheduled refresh %s panicked: %v", kind, p)
		}
	}()
	if kind == "subscription" {
		RefreshSubscription(scope, resource, trigger)
	} else {
		RefreshRuleSource(scope, resource, trigger, RuleOptions{})
	}
}

// RunRound is run_round(now): refresh every due source that is not running
// already, with at most Workers at a time; it returns how many ran.
func RunRound(ctx context.Context, now *time.Time) (int, error) {
	candidates, err := due(ctx, now)
	if err != nil {
		return 0, err
	}
	var jobs []job
	for _, j := range candidates {
		if claim(j.key) {
			jobs = append(jobs, j)
		}
	}
	if len(jobs) == 0 {
		return 0, nil
	}
	slots := make(chan struct{}, Workers)
	var wg sync.WaitGroup
	for _, j := range jobs {
		wg.Add(1)
		slots <- struct{}{}
		go func(j job) {
			defer wg.Done()
			defer func() { <-slots }()
			defer release(j.key)
			run(ctx, j.kind, j.resource, "schedule")
		}(j)
	}
	wg.Wait()
	return len(jobs), nil
}

// FindResource is find_resource(kind, resource_id): the subscription or
// library rule of a task; LookupError when it is gone, ValueError when it
// has no address to download.
func FindResource(ctx context.Context, kind, resourceID string) (*jsonx.Object, error) {
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	shared, err := r.GetShared(ctx)
	if err != nil {
		return nil, err
	}
	list := "rule_library"
	if kind == "subscription" {
		list = "subscriptions"
	}
	item, err := nextObject(shared.GetOr(list, []any{}), func(item *jsonx.Object) bool {
		return jsonx.Equal(item.Value("id"), resourceID)
	})
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, &LookupError{Message: "任务对应的来源已不存在，请刷新后重试"}
	}
	if !jsonx.Truthy(item.Value("url")) || !isURLSource(item) {
		return nil, pyErr("ValueError", "这个来源没有订阅地址，不需要更新")
	}
	return item, nil
}

// RunNow is run_now(kind, resource_id): start one refresh in the
// background (trigger "manual"); the result lands in task_status. It
// returns the task key; TaskBusy while the task runs.
func RunNow(ctx context.Context, kind, resourceID string) (string, error) {
	resource, err := FindResource(ctx, kind, resourceID)
	if err != nil {
		return "", err
	}
	key := RuleSourceKey(resourceID)
	if kind == "subscription" {
		key = SubscriptionKey(resourceID)
	}
	if !claim(key) {
		return "", &TaskBusy{Message: "这个任务正在运行，请稍后查看结果"}
	}
	background := context.WithoutCancel(ctx)
	go func() {
		defer release(key)
		run(background, kind, resource, "manual")
	}()
	return key, nil
}
