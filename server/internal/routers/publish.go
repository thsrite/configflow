package routers

import (
	"configflow/internal/routers/device"
	"configflow/internal/routers/oplock"
	"configflow/internal/routers/ros"
	"configflow/internal/routers/routeedit"
	"configflow/internal/routers/spec"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"configflow/internal/agents/diag"
	"configflow/internal/ctxx"
	"configflow/internal/lifecycle"
	"configflow/internal/logx"
	"configflow/internal/notifications"
	"configflow/internal/repo"
)

// PublishRequest is the caller's selected record and expected preview revision.
// The caller holds L1 and passes its held context; Publish acquires only L2.
type PublishRequest struct {
	ProfileID string
	RouterID  string
	Expected  string
	Router    *spec.Router
	Repo      *repo.Repository
	Agents    AgentSource
	NewDriver func(*spec.Router) Driver
	Loader    spec.ContentLoader
}

// PublishStatus identifies the handler-independent release outcome.
type PublishStatus int

const (
	PublishOK PublishStatus = iota
	PublishFailed
	PublishRouterChanged
	PublishBusy
	PublishBlocked
	PublishStale
	PublishDeviceError
)

// PublishOutcome retains the execution result when persistence itself fails.
type PublishOutcome struct {
	Status  PublishStatus
	Message string
	Plan    *Plan
	Result  *Result
	Err     error
}

// LastRelease is the persisted, sanitized summary of a publication.
type LastRelease struct {
	At          string `json:"at"`
	OK          bool   `json:"ok"`
	Fingerprint string `json:"fingerprint"`
	FailedStage string `json:"failed_stage"`
	Message     string `json:"message"`
}

// CaptureManual is wired by R14. Failure is logged, never a successful baseline.
var CaptureManual = func(context.Context, string, json.RawMessage, *spec.Router, Driver, *spec.Desired) error {
	return device.ErrNotPorted
}
var releaseLogger = logx.Get("backend.routes.routers")

// NewDriver constructs one vendor driver per inspection or publication.
func NewDriver(r *spec.Router) Driver {
	switch r.Type {
	case "routeros":
		return NewRouterOS(r.Endpoint, r.Username, r.Password, r.VerifyTLS)
	case "ikuai":
		return NewIkuai(r.Endpoint, r.Username, r.Password, r.VerifyTLS)
	}
	return nil
}

// StartPublish starts a tracked owner and transfers the caller's L1 release.
// The result is delivered only after driver and both locks have been released.
func StartPublish(ctx context.Context, req PublishRequest, unlock func()) (<-chan PublishOutcome, error) {
	out := make(chan PublishOutcome, 1)
	err := lifecycle.SpawnerFrom(ctx).Spawn(ctx, "router-publish-"+req.RouterID, func(work context.Context) {
		outcome := func() (result PublishOutcome) {
			if unlock != nil {
				defer unlock()
			}
			defer func() {
				if recover() != nil {
					// The payload may carry credentials; record only that it happened.
					releaseLogger.Errorf(MessagePublishPanic, req.RouterID)
					result = PublishOutcome{Err: lifecycle.ErrPanic}
				}
			}()
			return Publish(work, req)
		}()
		out <- outcome
		close(out)
	})
	if err != nil {
		if unlock != nil {
			unlock()
		}
		close(out)
		return nil, err
	}
	return out, nil
}

// ReleaseLabel returns the display name used in release summaries for r.
func ReleaseLabel(r *spec.Router) string {
	return diag.EventMessage(device.OrText(r.Name, r.ID), []any{r.Password})
}
func captureRelease(ctx context.Context, req PublishRequest, cfg json.RawMessage, r *spec.Router, d Driver, desired *spec.Desired) {
	if err := ctx.Err(); err != nil {
		return
	}
	if err := CaptureManual(ctx, req.ProfileID, cfg, captureRouter(r, d), d, desired); err != nil {
		releaseLogger.Warningf(messageCaptureFailed, ReleaseLabel(r), diag.EventMessage(err.Error(), []any{r.Password}))
	}
}

// captureRouter is the router as the rule-sync baseline must see it (Python
// _capture_sync): the IPv6 manifests this release just published replace the
// pre-release ones, so the control signature matches the stored record.
func captureRouter(r *spec.Router, d Driver) *spec.Router {
	holder, ok := d.(ros.ManifestHolder)
	if !ok {
		return r
	}
	m := holder.Manifests()
	out := *r
	out.ClearSource()
	out.Extra = maps.Clone(r.Extra)
	if out.Extra == nil {
		out.Extra = map[string]json.RawMessage{}
	}
	for key, v := range map[string]ros.Manifest{"ipv6_fakeip_state": m.IPv6FakeIP, "ipv6_routes_state": m.IPv6Routes} {
		if !v.Set {
			continue
		}
		out.Extra[key] = json.RawMessage("null")
		if len(v.Value) > 0 {
			out.Extra[key] = slices.Clone(v.Value)
		}
	}
	return &out
}
func refreshRelease(ctx context.Context) {
	if ctx.Err() == nil {
		notifications.Safely(func() error { return notifications.RefreshTasks(ctx, "") })
	}
}
func publishError(r *spec.Router, err error) PublishOutcome {
	var re *device.RouterError
	if errors.As(err, &re) {
		safe := routeedit.SafeError(err, r.Endpoint, r.Password)
		releaseLogger.Warningf(messagePublishStopped, ReleaseLabel(r), safe)
		return PublishOutcome{Status: PublishDeviceError, Message: safe.Error()}
	}
	return PublishOutcome{Err: err}
}

// Publish rebuilds the preview and executes it while its caller holds L1.
func Publish(ctx context.Context, req PublishRequest) PublishOutcome {
	work := lifecycle.Detach(ctx)
	if err := work.Err(); err != nil {
		return PublishOutcome{Err: err}
	}
	if req.Router == nil || req.Repo == nil {
		return PublishOutcome{Err: device.Error(device.MessageOperationFormat)}
	}
	held, release, err := oplock.RouterOperationLock(work, *req.Router, true)
	if err != nil {
		if work.Err() != nil {
			return PublishOutcome{Err: work.Err()}
		}
		return PublishOutcome{Status: PublishBusy, Message: device.MessageEditBusy}
	}
	defer release()
	work = held
	if err = work.Err(); err != nil {
		return PublishOutcome{Err: err}
	}
	cfg, err := req.Repo.ConfigWithOxiDNSAgents(work, req.ProfileID, repo.ResourceRoots{})
	if err != nil {
		return PublishOutcome{Err: err}
	}
	config, err := json.Marshal(cfg)
	if err != nil {
		return PublishOutcome{Err: err}
	}
	current := (*spec.Router)(nil)
	for _, r := range spec.RoutersOf(config) {
		if r.ID == req.RouterID {
			current = r
			break
		}
	}
	if current == nil || !spec.SameReleaseRouter(current, req.Router) {
		return PublishOutcome{Status: PublishRouterChanged, Message: messageRouterChangedWaiting}
	}
	factory := req.NewDriver
	if factory == nil {
		factory = NewDriver
	}
	driver := factory(current)
	if driver == nil {
		return PublishOutcome{Err: device.ErrNotPorted}
	}
	defer driver.Close()
	inspection, err := Inspect(work, InspectRequest{ProfileID: req.ProfileID, Config: config, Router: current, Driver: driver, Loader: req.Loader, Agents: req.Agents, Repo: req.Repo})
	if err != nil {
		return publishError(current, err)
	}
	plan, err := BuildPlan(work, current, inspection, driver)
	if err != nil {
		return publishError(current, err)
	}
	label := ReleaseLabel(current)
	if len(plan.Blockers) > 0 {
		releaseLogger.Warningf(messagePublishBlocked, label, diag.EventMessage(strings.Join(plan.Blockers, "；"), []any{current.Password}))
		return PublishOutcome{Status: PublishBlocked, Message: strings.Join(plan.Blockers, "；"), Plan: plan}
	}
	if req.Expected == "" || req.Expected != plan.Fingerprint {
		releaseLogger.Warningf(messagePublishStale, label)
		return PublishOutcome{Status: PublishStale, Message: messageStaleFingerprint, Plan: plan}
	}
	result := &Result{OK: true, Log: []ReleaseEntry{}, Rollback: []ReleaseEntry{}}
	if plan.Empty {
		captureRelease(work, req, config, current, driver, inspection.Desired)
	} else {
		var agent ReturnAgent
		needsAgent := current.ManageReturnRouting
		for _, stage := range plan.Stages {
			for _, op := range stage.Ops {
				needsAgent = needsAgent || op.OpKind() == "return_routing_remove"
			}
		}
		if needsAgent {
			if req.Agents == nil {
				return PublishOutcome{Err: device.ErrNotPorted}
			}
			agent = req.Agents.ReturnAgent(inspection.Gateway)
		}
		count := planOps(plan)
		releaseLogger.Infof(messagePublishStart, label, count)
		result = Execute(work, plan, driver, agent)
		logRelease(current, count, result)
	}
	if work.Err() != nil && result.Wrote {
		abortPublicationResult(result, work.Err())
	}
	outcome := PublishOutcome{Status: PublishOK, Plan: plan, Result: result}
	publication, err := releasePublication(work, current, plan, result, driver)
	if err != nil {
		outcome.Err = err
		return outcome
	}
	expected, err := spec.ReleaseRouterRecord(current)
	if err != nil {
		outcome.Err = err
		return outcome
	}
	if result.Aborted && result.Wrote {
		err = saveAbortedPublication(work, req.Repo, req.ProfileID, expected, publication)
	} else {
		if err = work.Err(); err == nil {
			err = req.Repo.SaveRouterPublicationJSON(work, req.ProfileID, expected, publication)
		}
		// Cancellation may arrive inside the repository's pre-commit guard.
		// A rejected ordinary transaction committed nothing; preserve the
		// landed device state in the one approved local transaction instead.
		if err != nil && work.Err() != nil && result.Wrote && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
			abortPublicationResult(result, work.Err())
			publication, err = releasePublication(work, current, plan, result, driver)
			if err == nil {
				err = saveAbortedPublication(work, req.Repo, req.ProfileID, expected, publication)
			}
		}
	}
	if err != nil {
		outcome.Err = err
		return outcome
	}
	repo.ResetConfigContext(work)
	refreshRelease(work)
	if plan.Empty {
		releaseLogger.Infof(messagePublishEmpty, label)
	} else if result.OK {
		captureRelease(work, req, config, current, driver, inspection.Desired)
	}
	if !result.OK {
		outcome.Status = PublishFailed
		detail := messageRollbackComplete
		if slices.ContainsFunc(result.Rollback, func(e ReleaseEntry) bool { return e.Status == "failed" }) {
			detail = messageReleaseRollbackIncomplete
		}
		failed := ""
		if result.FailedStage != nil {
			failed = *result.FailedStage
		}
		outcome.Message = fmt.Sprintf(messagePublishFailedStage, failed, detail)
		if result.Aborted {
			outcome.Message = messageReleaseAborted
		}
	}
	return outcome
}
func releasePublication(ctx context.Context, r *spec.Router, p *Plan, result *Result, d Driver) (repo.RouterPublicationJSON, error) {
	failure := []string{}
	for _, entry := range append(slices.Clone(result.Log), result.Rollback...) {
		if entry.Status == "failed" {
			failure = append(failure, entry.Summary)
		}
	}
	failed := ""
	if result.FailedStage != nil {
		failed = *result.FailedStage
	}
	message := diag.EventMessage(strings.Join(failure, "；"), []any{r.Password})
	if result.Aborted {
		message += fmt.Sprintf(messageReleaseAbortedProgress, result.Applied, planOps(p))
	}
	last := LastRelease{At: lifecycle.ClockFrom(ctx).Now().Local().Format("2006-01-02T15:04:05"), OK: result.OK, Fingerprint: p.Fingerprint, FailedStage: failed, Message: message}
	b, err := json.Marshal(last)
	if err != nil {
		return repo.RouterPublicationJSON{}, err
	}
	out := repo.RouterPublicationJSON{LastRelease: repo.JSONUpdate{Set: true, Value: b}}
	if holder, ok := d.(ros.ManifestHolder); ok && !p.Empty {
		m := holder.Manifests()
		slot := func(v ros.Manifest) repo.JSONUpdate { return repo.JSONUpdate{Set: v.Set, Value: v.Value} }
		out.Manifest = slot(m.Policy)
		out.DNSGuard = slot(m.DNSGuard)
		out.IPv6FakeIP = slot(m.IPv6FakeIP)
		out.IPv6Routes = slot(m.IPv6Routes)
		out.IPv6PDGuard = slot(m.IPv6PDGuard)
	}
	return out, nil
}

// planOps counts the device and Agent operations of a plan.
func planOps(p *Plan) int {
	count := 0
	for _, stage := range p.Stages {
		count += len(stage.Ops)
	}
	return count
}

// saveAbortedPublication is the sole R12 local commit exception. Its caller has
// established Aborted && Wrote. No device I/O or reads enter this region.
func saveAbortedPublication(ctx context.Context, r *repo.Repository, profileID string, expected json.RawMessage, p repo.RouterPublicationJSON) error {
	return r.SaveRouterPublicationJSON(ctxx.Uninterruptible(ctx), profileID, expected, p)
}
func logRelease(r *spec.Router, count int, result *Result) {
	label := ReleaseLabel(r)
	safe := func(s string) string { return diag.EventMessage(s, []any{r.Password}) }
	for _, e := range result.Log {
		if e.Status == "warning" || e.Status == "manual" {
			releaseLogger.Warningf(messagePublishHint, label, e.Stage, safe(e.Summary))
		}
	}
	if result.OK {
		releaseLogger.Infof(messagePublishComplete, label, count)
		return
	}
	failed, stage := "", ""
	if result.FailedStage != nil {
		stage = *result.FailedStage
	}
	for _, e := range result.Log {
		if e.Status == "failed" {
			failed = e.Summary
		}
	}
	releaseLogger.Errorf(messagePublishLogFailed, label, stage, safe(failed))
	failures := []string{}
	for _, e := range result.Rollback {
		if e.Status == "failed" {
			failures = append(failures, e.Summary)
		}
	}
	if len(failures) > 0 {
		releaseLogger.Errorf(messagePublishRollbackFailed, label, safe(strings.Join(failures, "；")))
	} else if len(result.Rollback) > 0 {
		releaseLogger.Warningf(messagePublishRolledBack, label, len(result.Rollback))
	}
}

func abortPublicationResult(result *Result, err error) {
	if result.Aborted {
		return
	}
	stage := ""
	if len(result.Log) > 0 {
		stage = result.Log[len(result.Log)-1].Stage
	}
	result.OK = false
	result.Aborted = true
	result.FailedStage = &stage
	result.Log = append(result.Log, ReleaseEntry{Stage: stage, Summary: messageReleaseAborted, Status: "failed"})
}
