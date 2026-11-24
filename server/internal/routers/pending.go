package routers

import (
	"configflow/internal/routers/device"
	"configflow/internal/routers/spec"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"configflow/internal/agents/diag"
	"configflow/internal/lifecycle"
	"configflow/internal/logx"
	"configflow/internal/notifications"
	"configflow/internal/repo"
)

// PendingRequest selects an Agent and an optional explicit profile. A non-nil
// empty profile is invalid, never an alias for the all-profile scope.
type PendingRequest struct {
	AgentID       string
	ProfileID     *string
	Refresh       bool
	Repo          *repo.Repository
	Agents        AgentSource
	Cache         *PendingCache
	NewDriver     func(*spec.Router) Driver
	Rules         RuleContentLoader
	IPv6Hosts     IPv6HostFetcher
	Notifications *notifications.Service
}

// PendingResponse is the handler-independent upstream-router reminder payload.
type PendingResponse struct {
	AgentID        string         `json:"agent_id"`
	AgentProfileID string         `json:"agent_profile_id"`
	Routers        []PendingEntry `json:"routers"`
	Cached         bool           `json:"cached"`
	CheckedAt      string         `json:"checked_at"`
}
type pendingTarget struct {
	profile spec.PendingProfile
	config  json.RawMessage
	router  *spec.Router
}
type pendingSignatureProfile struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Revision json.RawMessage   `json:"revision"`
	Routers  []json.RawMessage `json:"routers"`
}
type pendingAgentIdentity struct {
	ID, Host               string
	Port                   int64
	ProfileID, ServiceType string
	Enabled                *bool
	Status, Version        string
}
type pendingSignature struct {
	Scope    *string
	Profiles []pendingSignatureProfile
	Agent    *pendingAgentIdentity
}

var defaultPendingCache = NewPendingCache(nil, nil)
var pendingLogger = logx.Get("backend.routers.pending")

func pendingTargets(ctx context.Context, req PendingRequest) ([]pendingTarget, string, string, error) {
	if req.Repo == nil || req.Agents == nil {
		return nil, "", "", device.ErrNotPorted
	}
	agent, err := req.Agents.Agent(ctx, req.AgentID)
	if err != nil {
		return nil, "", "", err
	}
	list, err := spec.PendingProfiles(ctx, req.Repo)
	if err != nil {
		return nil, "", "", err
	}
	if req.ProfileID != nil {
		if _, err = req.Repo.Profile(ctx, *req.ProfileID); err != nil {
			return nil, "", "", err
		}
	}
	targets := []pendingTarget{}
	signature := pendingSignature{Scope: req.ProfileID, Profiles: []pendingSignatureProfile{}}
	for _, profile := range list {
		if req.ProfileID != nil && profile.ID != *req.ProfileID {
			continue
		}
		// Most profiles link no router to this Agent: their stored routers
		// decide that without resolving and encoding the whole configuration.
		stored, err := req.Repo.Profile(ctx, profile.ID)
		if err != nil {
			return nil, "", "", err
		}
		routers, err := spec.StoredRouters(&stored.Routers)
		if err != nil {
			return nil, "", "", err
		}
		if !slices.ContainsFunc(routers, func(r *spec.Router) bool { return pendingLinked(r, req.AgentID) }) {
			continue
		}
		config, err := req.Repo.ConfigWithOxiDNSAgents(ctx, profile.ID, repo.ResourceRoots{})
		if err != nil {
			return nil, "", "", err
		}
		data, err := json.Marshal(config)
		if err != nil {
			return nil, "", "", err
		}
		linked := []json.RawMessage{}
		for _, r := range spec.RoutersOf(data) {
			if !pendingLinked(r, req.AgentID) {
				continue
			}
			original, err := spec.ReleaseRouterRecord(r)
			if err != nil {
				return nil, "", "", err
			}
			linked = append(linked, original)
			targets = append(targets, pendingTarget{profile, data, r})
		}
		if len(linked) > 0 {
			signature.Profiles = append(signature.Profiles, pendingSignatureProfile{profile.ID, profile.Name, profile.Revision, linked})
		}
	}
	profileID := ""
	if agent != nil && agent.Agent != nil {
		profileID = agent.ProfileID
		signature.Agent = &pendingAgentIdentity{agent.ID, agent.Host, int64(agent.Port), agent.ProfileID, agent.ServiceType, agent.Enabled, agent.Status, agent.Version}
	}
	if profileID == "" {
		profileID, err = req.Repo.DefaultProfileID(ctx)
		if err != nil {
			return nil, "", "", err
		}
	}
	bytes, err := CanonicalReleaseJSON(signature)
	if err != nil {
		return nil, "", "", err
	}
	sum := sha256.Sum256(bytes)
	return targets, profileID, hex.EncodeToString(sum[:]), nil
}

// pendingLinked reports whether r sends proxied traffic to the Agent. An empty
// Agent ID is a missing identity, never a match against routers without a
// proxy gateway.
func pendingLinked(r *spec.Router, agentID string) bool {
	return agentID != "" && r.LinkID("proxy_gateway_agent_id") == agentID
}

// AgentRouters reuses R12's read-only inspection and preview. Shared loads are
// tracked independently of the HTTP waiter, and never execute a release.
func AgentRouters(ctx context.Context, req PendingRequest) (*PendingResponse, error) {
	work := lifecycle.Detach(ctx)
	if err := work.Err(); err != nil {
		return nil, err
	}
	targets, profile, signature, err := pendingTargets(work, req)
	if err != nil {
		return nil, err
	}
	cache := req.Cache
	if cache == nil {
		cache = defaultPendingCache
	}
	out := &PendingResponse{AgentID: req.AgentID, AgentProfileID: profile, Routers: []PendingEntry{}, CheckedAt: pendingStamp(cache.clock)}
	if len(targets) == 0 {
		return out, nil
	}
	value, err := cache.Read(ctx, req.Repo.DataDir()+"\n"+req.AgentID, signature, req.Refresh, func(work context.Context) ([]PendingEntry, error) {
		return readPendingTargets(work, req, targets, cache)
	})
	if err != nil {
		return nil, err
	}
	out.Routers = value.Routers
	out.CheckedAt = value.CheckedAt
	out.Cached = value.Cached
	return out, nil
}

type pendingReadResult struct {
	index   int
	summary PendingSummary
	report  *Report
	plan    *Plan
}

func readPendingRouter(ctx context.Context, req PendingRequest, target pendingTarget) (out pendingReadResult) {
	out.summary = unknownPending(messagePendingInternalError)
	defer func() {
		if recover() != nil {
			out = pendingReadResult{summary: unknownPending(messagePendingInternalError)}
			pendingLogger.Error("Upstream router status failed: worker panic")
		}
	}()
	factory := req.NewDriver
	if factory == nil {
		factory = NewDriver
	}
	driver := factory(target.router)
	if driver == nil {
		pendingLogger.Warningf("Upstream router status failed for router %s: unsupported router type", target.router.ID)
		return out
	}
	defer driver.Close()
	in, err := Inspect(ctx, InspectRequest{ProfileID: target.profile.ID, Config: target.config, Router: target.router, Driver: driver, Rules: req.Rules, IPv6Hosts: req.IPv6Hosts, Agents: req.Agents, Repo: req.Repo})
	var plan *Plan
	if err == nil {
		plan, err = BuildPlan(ctx, target.router, in, driver)
	}
	if err != nil {
		var re *device.RouterError
		if errors.As(err, &re) {
			message := diag.FailureText(re.Error(), []any{target.router.Password}, 0)
			out.summary = unknownPending(device.OrText(message, messagePendingReadError))
		} else {
			pendingLogger.Warningf("Upstream router status failed: %s", diag.EventMessage(err.Error(), []any{target.router.Password}))
		}
		return out
	}
	out.summary = Summarize(in.Report, plan)
	out.report = in.Report
	out.plan = plan
	return out
}

func unknownPending(message string) PendingSummary {
	return PendingSummary{State: "unknown", Items: []PendingItem{}, Message: message}
}
func readPendingTargets(ctx context.Context, req PendingRequest, targets []pendingTarget, cache *PendingCache) ([]PendingEntry, error) {
	readCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	timer := cache.clock.NewTimer(PendingRouterTimeout)
	defer timer.Stop()
	jobs := make(chan int, len(targets))
	for i := range targets {
		jobs <- i
	}
	close(jobs)
	results := make(chan pendingReadResult, len(targets))
	workers := min(PendingWorkers, len(targets))
	for range workers {
		err := cache.spawner.Spawn(ctx, "upstream-pending-reader", func(context.Context) {
			for index := range jobs {
				if readCtx.Err() != nil {
					return
				}
				result := readPendingRouter(repo.WithScope(readCtx), req, targets[index])
				result.index = index
				if readCtx.Err() != nil {
					return
				}
				results <- result
			}
		})
		if err != nil {
			cancel()
			return nil, err
		}
	}
	done := make(map[int]pendingReadResult, len(targets))
	expired := false
	for len(done) < len(targets) && !expired {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C():
			expired = true
		case result := <-results:
			done[result.index] = result
		}
	}
	if expired {
		cancel()
		for {
			select {
			case result := <-results:
				done[result.index] = result
			default:
				goto drained
			}
		}
	}
drained:
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	entries := make([]PendingEntry, 0, len(targets))
	for i, target := range targets {
		result, ok := done[i]
		summary := unknownPending(fmt.Sprintf(messagePendingRouterTimeout, PendingRouterTimeout.Seconds()))
		if ok {
			summary = result.summary
		}
		if ok && result.report != nil {
			recordPendingNotice(ctx, req.Repo, target.profile.ID, target.config, target.router, result.report, result.plan)
		}
		entries = append(entries, PendingEntry{ID: target.router.ID, Name: device.OrText(target.router.Name, target.router.ID), Type: target.router.Type, ProfileID: target.profile.ID, ProfileName: device.OrText(target.profile.Name, target.profile.ID), PendingSummary: summary})
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}
