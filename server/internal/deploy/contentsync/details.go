package contentsync

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"configflow/internal/deploy"
	"configflow/internal/deploy/pushdiff"
	"configflow/internal/lifecycle"
)

func answer(result Result, diff *pushdiff.Diff, version string) Answer {
	if diff == nil {
		return Answer{Result: result}
	}
	data, _ := json.Marshal(diff)
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(data, &fields)
	fields["status"], _ = json.Marshal(result.Status)
	if result.CheckedAt != "" {
		fields["checked_at"], _ = json.Marshal(result.CheckedAt)
	}
	if version != "" {
		fields["version"], _ = json.Marshal(version)
	}
	raw, _ := json.Marshal(fields)
	return Answer{Result: result, raw: raw}
}
func (c *Checker) buildDetail(ctx context.Context, t deploy.Target, b *deploy.Bundle, key deploy.InputKey, result Result) (d *detail) {
	// A failed itemized listing preserves the successful status comparison.
	failed := func() *detail {
		version, _ := id()
		version = version[:min(16, len(version))]
		diff := pushdiff.Diff{Status: "changed", Reason: "diff_failed"}
		return &detail{key: key, answer: answer(result, &diff, version), version: version, expires: c.now(ctx).Add(c.opts.ErrorTTL), ttl: c.opts.ErrorTTL}
	}
	defer func() {
		if recover() != nil {
			logger.Error("Content comparison listing panicked")
			d = failed()
		}
	}()
	var receipt *deploy.Receipt
	var err error
	if c.opts.LoadReceipt != nil {
		receipt, err = c.opts.LoadReceipt(ctx, t)
		if err != nil {
			return failed()
		}
	}
	state, err := deploy.ContentView(t)
	if err != nil {
		return failed()
	}
	files := map[string]*pushdiff.FileEntries{}
	diff, err := pushdiff.Build(pushdiff.AppliedView{ServiceType: t.ServiceType, Files: state.Applied.Files}, b, receipt, files)
	if err != nil {
		return failed()
	}
	version, err := id()
	if err != nil {
		return failed()
	}
	version = version[:16]
	d = &detail{key: key, answer: answer(result, &diff, version), version: version, files: files, groups: map[string]map[string]bool{}, expires: c.now(ctx).Add(c.opts.SuccessTTL), ttl: c.opts.SuccessTTL}
	for path, f := range files {
		d.size += f.Size()
		d.groups[path] = map[string]bool{}
	}
	for _, f := range diff.Files {
		if groups := d.groups[f.Path]; groups != nil && f.Details != nil {
			for _, g := range f.Details.Groups {
				groups[g.Change] = true
			}
		}
	}
	return d
}
func (c *Checker) removeDetail(id string) {
	if d := c.details[id]; d != nil {
		c.bytes -= d.size
		c.detailOrder.Remove(d.order)
		delete(c.details, id)
	}
}
func (c *Checker) storeDetail(ctx context.Context, id string, d *detail) {
	c.removeDetail(id)
	d.order = c.detailOrder.PushBack(id)
	c.details[id] = d
	c.bytes += d.size
	now := c.now(ctx)
	d.expires = now.Add(d.ttl)
	for other, item := range c.details {
		if other != id && !now.Before(item.expires) {
			c.removeDetail(other)
		}
	}
	for len(c.details) > 1 && (len(c.details) > c.opts.MaxDetails || c.bytes > c.opts.MaxListBytes) {
		c.removeDetail(c.detailOrder.Front().Value.(string))
	}
}

// PushDiff shares Status's workers and preparation. An evicted, expired or
// failed list is rebuilt even when the changed status itself is still fresh.
func (c *Checker) PushDiff(ctx context.Context, t deploy.Target, s *deploy.Source, url string) Answer {
	ctx = lifecycle.Detach(ctx)
	key, state, unavailable := c.input(ctx, t, s, url)
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return Answer{Result: unknown("check_failed")}
	}
	if unavailable != nil {
		c.mu.Unlock()
		return Answer{Result: *unavailable}
	}
	e := c.get(t.ID, key)
	now := c.now(ctx)
	if d := c.details[t.ID]; d != nil && d.key == key && now.Before(d.expires) {
		a := d.answer.clone()
		c.mu.Unlock()
		return a
	}
	if e.result != nil && now.Before(e.expires) && e.result.Status != "changed" {
		r := *e.result
		c.mu.Unlock()
		return Answer{Result: r}
	}
	held, token, ok := c.reserve(ctx, t.ID)
	c.mu.Unlock()
	if !ok {
		return Answer{Result: Result{Status: "checking"}}
	}
	return Answer{Result: c.submit(held, t, s, url, state, e, token, true)}
}

// Entries reads outside the cache guard, after validating the retained list's
// expiry, inputs and version in that order. It never schedules preparation.
func (c *Checker) Entries(ctx context.Context, t deploy.Target, s *deploy.Source, url string, q EntriesQuery) (EntriesResult, error) {
	key, _, unavailable := c.input(ctx, t, s, url)
	c.mu.Lock()
	d := c.details[t.ID]
	stale := func(reason string) (EntriesResult, error) {
		c.mu.Unlock()
		return EntriesResult{Status: "stale", Reason: reason}, nil
	}
	if c.closed || d == nil || !c.now(ctx).Before(d.expires) {
		return stale("expired")
	}
	if unavailable != nil || d.key != key {
		return stale("changed")
	}
	if d.version != q.Version {
		return stale("replaced")
	}
	f := d.files[q.Path]
	valid := d.groups[q.Path][q.Change]
	c.mu.Unlock()
	if f == nil || !valid {
		return EntriesResult{}, ErrNoEntries
	}
	page := f.Page(q.Change, q.Offset, q.Limit, q.Query, q.Find)
	return EntriesResult{Status: "ok", Version: q.Version, File: q.Path, Change: q.Change, Page: &page}, nil
}

// CompareNow prepares synchronously in its caller, sharing fresh status results
// but not worker slots. Invalidated in-flight work is returned but never cached.
func (c *Checker) CompareNow(ctx context.Context, t deploy.Target, s *deploy.Source, url string) (out Result) {
	defer func() {
		if recover() != nil {
			logger.Error("Synchronous content comparison panicked")
			out = unknown("check_failed")
		}
	}()
	ctx = lifecycle.Detach(ctx)
	key, state, unavailable := c.input(ctx, t, s, url)
	if unavailable != nil {
		return *unavailable
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return unknown("check_failed")
	}
	if e := c.entries[t.ID]; e != nil && e.key == key && e.result != nil && c.now(ctx).Before(e.expires) {
		r := *e.result
		c.mu.Unlock()
		return r
	}
	generation := c.generation
	c.mu.Unlock()
	b, err := c.prepare(ctx, deploy.SnapshotTarget(t), s, url, state)
	if err != nil {
		return unknown("check_failed")
	}
	fp, err := deploy.Fingerprint(b.Manifest())
	if err != nil {
		return unknown("check_failed")
	}
	r := Result{Status: "changed", CheckedAt: checkedAt(c.now(ctx))}
	if fp == state.Applied.Fingerprint {
		r.Status = "synced"
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || ctx.Err() != nil {
		return unknown("check_failed")
	}
	if generation != c.generation {
		return r
	}
	e := c.get(t.ID, key)
	e.result = &r
	e.expires = c.now(ctx).Add(c.opts.SuccessTTL)
	return r
}

// Invalidate forgets results and details while running jobs retain their slots.
// Replacing active entries prevents those jobs from publishing stale answers.
func (c *Checker) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.generation++
	for _, e := range c.entries {
		if c.running[e.id] != nil {
			next := &entry{id: e.id, key: e.key, order: e.order}
			e.order.Value = next
			c.entries[e.id] = next
		} else {
			e.result = nil
			e.expires = time.Time{}
		}
	}
	c.details = map[string]*detail{}
	c.detailOrder.Init()
	c.bytes = 0
}

// Close invalidates the Checker and releases reservations, including jobs that
// have been admitted by a test/runtime spawner but have not started yet.
func (c *Checker) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.closed = true
	c.generation++
	for id, token := range c.running {
		c.release(id, token)
	}
	c.entries = map[string]*entry{}
	c.details = map[string]*detail{}
	c.order.Init()
	c.detailOrder.Init()
	c.bytes = 0
}

// ErrPreparationUnavailable is returned by composition before A14 is wired.
var ErrPreparationUnavailable = fmt.Errorf("content preparation is not wired")
