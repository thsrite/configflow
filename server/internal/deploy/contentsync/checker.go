package contentsync

import (
	"container/list"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"sync"
	"time"

	"configflow/internal/deploy"
	"configflow/internal/deploy/pushdiff"
	"configflow/internal/lifecycle"
	"configflow/internal/lockx"
	"configflow/internal/logx"
	"configflow/internal/repo"
)

var fingerprint = regexp.MustCompile(`^v1:[0-9a-f]{64}$`)
var logger = logx.Get("backend.agents.content_sync")

type entry struct {
	id      string
	key     deploy.InputKey
	result  *Result
	expires time.Time
	order   *list.Element
}
type detail struct {
	key     deploy.InputKey
	answer  Answer
	expires time.Time
	ttl     time.Duration
	version string
	size    int
	files   map[string]*pushdiff.FileEntries
	groups  map[string]map[string]bool
	order   *list.Element
}
type reservation struct {
	once   sync.Once
	unlock func()
	cancel context.CancelFunc
}

// Checker owns one Manager's statuses, itemized comparisons and active jobs.
// All protected state is memory-only; preparation and decoding run unlocked.
type Checker struct {
	mu                 sync.Mutex
	opts               Options
	slots              chan struct{}
	keys               *lockx.Keyed[string]
	entries            map[string]*entry
	details            map[string]*detail
	running            map[string]*reservation
	order, detailOrder *list.List
	bytes              int
	generation         uint64
	closed             bool
}

// New creates an isolated Checker. It does not start any goroutine or timer.
func New(opts Options) *Checker {
	if opts.Workers <= 0 {
		opts.Workers = Workers
	}
	if opts.SuccessTTL <= 0 {
		opts.SuccessTTL = SuccessTTL
	}
	if opts.ErrorTTL <= 0 {
		opts.ErrorTTL = ErrorTTL
	}
	if opts.MaxEntries <= 0 {
		opts.MaxEntries = MaxEntries
	}
	opts.MaxEntries = max(opts.MaxEntries, opts.Workers)
	if opts.MaxDetails <= 0 {
		opts.MaxDetails = MaxDetails
	}
	if opts.MaxListBytes <= 0 {
		opts.MaxListBytes = MaxListBytes
	}
	if opts.Spawner == nil {
		opts.Spawner = lifecycle.DefaultSpawner{}
	}
	return &Checker{opts: opts, slots: make(chan struct{}, opts.Workers), keys: lockx.NewKeyed[string](lockx.LevelJob, "agent-content-sync"), entries: map[string]*entry{}, details: map[string]*detail{}, running: map[string]*reservation{}, order: list.New(), detailOrder: list.New()}
}
func unknown(reason string) Result { return Result{Status: "unknown", Reason: reason} }
func (c *Checker) now(ctx context.Context) time.Time {
	clock := c.opts.Clock
	if clock == nil {
		clock = lifecycle.ClockFrom(ctx)
	}
	return clock.Now()
}
func (c *Checker) input(ctx context.Context, t deploy.Target, s *deploy.Source, url string) (deploy.InputKey, deploy.ContentState, *Result) {
	if t.ID == "" || s == nil {
		return deploy.InputKey{}, deploy.ContentState{}, resultPtr(unknown("invalid_inputs"))
	}
	state, err := deploy.ContentView(t)
	if err != nil {
		return deploy.InputKey{}, state, resultPtr(unknown("invalid_inputs"))
	}
	unavailable := func(reason string) (deploy.InputKey, deploy.ContentState, *Result) {
		return deploy.InputKey{}, state, resultPtr(unknown(reason))
	}
	if state.Pending {
		return unavailable("deployment_pending")
	}
	if !state.HasBaseline {
		if state.PreviouslyPushed {
			return unavailable("baseline_missing")
		}
		return deploy.InputKey{}, state, &Result{Status: "never-pushed"}
	}
	if !state.ValidBaseline || !fingerprint.MatchString(state.Applied.Fingerprint) {
		return unavailable("baseline_invalid")
	}
	profile := t.ProfileID
	if profile == "" {
		profile = "default"
		if c.opts.DefaultProfileID != nil {
			profile, err = c.opts.DefaultProfileID(ctx)
			if err != nil {
				return unavailable("invalid_inputs")
			}
		}
	}
	if state.Applied.ProfileID != profile || !s.MatchesProfile(profile) {
		return unavailable("profile_mismatch")
	}
	service := t.ServiceType
	if service == "" {
		service = "mihomo"
	}
	if state.Applied.ServiceType != service {
		return unavailable("service_mismatch")
	}
	if state.Applied.ConfigVersion == "" || state.Applied.ConfigVersion != state.ConfigVersion {
		return unavailable("version_mismatch")
	}
	if state.Applied.DeploymentID == "" || state.Applied.ConfigPath == "" {
		return unavailable("baseline_invalid")
	}
	key, err := deploy.ContentInputKey(t, s, url)
	if err != nil {
		return unavailable("invalid_inputs")
	}
	return key, state, nil
}
func resultPtr(r Result) *Result        { return &r }
func (c *Checker) removeEntry(e *entry) { delete(c.entries, e.id); c.order.Remove(e.order) }
func (c *Checker) get(id string, key deploy.InputKey) *entry {
	e := c.entries[id]
	if e == nil || e.key != key {
		if e != nil {
			c.removeEntry(e)
		}
		e = &entry{id: id, key: key}
		e.order = c.order.PushBack(e)
		c.entries[id] = e
	} else {
		c.order.MoveToBack(e.order)
	}
	for node := c.order.Front(); len(c.entries) > c.opts.MaxEntries && node != nil; {
		next := node.Next()
		old := node.Value.(*entry)
		if c.running[old.id] == nil {
			c.removeEntry(old)
		}
		node = next
	}
	return e
}

// Status inspects snapshots/cache and schedules at most one job, never waiting
// for preparation. Request cancellation is detached here, not in HTTP handlers.
func (c *Checker) Status(ctx context.Context, t deploy.Target, s *deploy.Source, url string) Result {
	ctx = lifecycle.Detach(ctx)
	key, state, unavailable := c.input(ctx, t, s, url)
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return unknown("check_failed")
	}
	if unavailable != nil {
		if e := c.entries[t.ID]; e != nil {
			c.removeEntry(e)
		}
		c.mu.Unlock()
		return *unavailable
	}
	e := c.get(t.ID, key)
	if e.result != nil && c.now(ctx).Before(e.expires) {
		r := *e.result
		c.mu.Unlock()
		return r
	}
	held, token, ok := c.reserve(ctx, t.ID)
	c.mu.Unlock()
	if !ok {
		return Result{Status: "checking"}
	}
	return c.submit(held, t, s, url, state, e, token, false)
}
func (c *Checker) reserve(ctx context.Context, id string) (context.Context, *reservation, bool) {
	if c.running[id] != nil {
		return ctx, nil, false
	}
	select {
	case c.slots <- struct{}{}:
	default:
		return ctx, nil, false
	}
	held, unlock, ok := c.keys.TryLock(ctx, id)
	if !ok {
		<-c.slots
		return ctx, nil, false
	}
	work, cancel := context.WithCancel(held)
	token := &reservation{unlock: unlock, cancel: cancel}
	c.running[id] = token
	return work, token, true
}
func (c *Checker) release(id string, token *reservation) {
	token.once.Do(func() {
		token.cancel()
		token.unlock()
		<-c.slots
		if c.running[id] == token {
			delete(c.running, id)
		}
	})
}
func (c *Checker) submit(ctx context.Context, t deploy.Target, s *deploy.Source, url string, state deploy.ContentState, e *entry, token *reservation, detailRequest bool) Result {
	t = deploy.SnapshotTarget(t)
	err := c.opts.Spawner.Spawn(ctx, "agent-content-sync", func(work context.Context) {
		// Spawn deliberately detaches its input cancellation. Reattach this
		// owner's reservation cancellation to the supplied task context, keeping
		// the Runtime's fresh scope, clock and shutdown cancellation as well.
		work, cancel := context.WithCancel(work)
		stop := context.AfterFunc(ctx, cancel)
		defer stop()
		defer cancel()
		if ctx.Err() != nil {
			cancel() // Close may have run before the admitted callback started.
		}
		c.check(work, t, s, url, state, e, token)
	})
	if err != nil {
		c.mu.Lock()
		c.release(t.ID, token)
		r := unknown("check_failed")
		if !detailRequest && !c.closed && c.entries[t.ID] == e {
			e.result = &r
			e.expires = c.now(ctx).Add(c.opts.ErrorTTL)
		}
		c.mu.Unlock()
		return r
	}
	return Result{Status: "checking"}
}
func id() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}
func (c *Checker) prepare(ctx context.Context, t deploy.Target, s *deploy.Source, url string, state deploy.ContentState) (*deploy.Bundle, error) {
	ctx = repo.WithScope(ctx)
	defer repo.ResetConfigContext(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.opts.Prepare == nil {
		return nil, fmt.Errorf("content preparation is unavailable")
	}
	name, err := id()
	if err != nil {
		return nil, err
	}
	bundle, err := c.opts.Prepare(ctx, t, s, url, "check-"+name)
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if bundle == nil {
		return nil, fmt.Errorf("content preparation returned no bundle")
	}
	return deploy.Retarget(bundle, state.Applied.ConfigPath)
}
func checkedAt(t time.Time) string {
	if t.Nanosecond()/1000 == 0 {
		return t.UTC().Format(time.RFC3339)
	}
	return t.UTC().Format("2006-01-02T15:04:05.000000Z")
}
func (c *Checker) check(ctx context.Context, t deploy.Target, s *deploy.Source, url string, state deploy.ContentState, e *entry, token *reservation) {
	result := unknown("check_failed")
	ttl := c.opts.ErrorTTL
	var d *detail
	defer func() {
		if recover() != nil {
			logger.Error("Content comparison worker panicked")
			result = unknown("check_failed")
			ttl = c.opts.ErrorTTL
			d = nil
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		defer c.release(t.ID, token)
		if !c.closed && ctx.Err() == nil && c.entries[t.ID] == e {
			e.result = &result
			e.expires = c.now(ctx).Add(ttl)
			if d != nil {
				c.storeDetail(ctx, t.ID, d)
			} else if result.Status != "changed" {
				c.removeDetail(t.ID)
			}
		}
	}()
	bundle, err := c.prepare(ctx, t, s, url, state)
	if err != nil {
		return
	}
	fp, err := deploy.Fingerprint(bundle.Manifest())
	if err != nil {
		return
	}
	result = Result{Status: "changed", CheckedAt: checkedAt(c.now(ctx))}
	if fp == state.Applied.Fingerprint {
		result.Status = "synced"
	}
	ttl = c.opts.SuccessTTL
	if result.Status == "changed" {
		d = c.buildDetail(ctx, t, bundle, e.key, result)
	}
}
