package routers

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"configflow/internal/agents/diag"
	"configflow/internal/lifecycle"
	"configflow/internal/logx"
	"configflow/internal/repo"
)

// ManagementService owns router configuration and manual-route writes. Existing
// router writes require the caller's L1 job context; the service acquires L2.
type ManagementService struct {
	Repo      *repo.Repository
	NewDriver func(*Router) Driver
}

var managementLogger = logx.Get("backend.routes.routers")

// Records loads the resolved configuration and R5-decoded router records.
func (s *ManagementService) Records(ctx context.Context, profile string) (json.RawMessage, []*Router, error) {
	doc, err := s.Repo.GetCompatConfig(ctx, profile, repo.ResourceRoots{})
	if err != nil {
		return nil, nil, err
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return nil, nil, err
	}
	return raw, routersOf(raw), nil
}
func (s *ManagementService) open(r *Router) (Driver, error) {
	factory := s.NewDriver
	if factory == nil {
		factory = NewDriver
	}
	d := factory(r)
	if d == nil {
		return nil, routerError(messageOperationFormat)
	}
	return d, nil
}

// Create validates and appends one router inside a repository transaction.
func (s *ManagementService) Create(ctx context.Context, profile string, body json.RawMessage) (*Router, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r, err := Normalize(body, nil)
	if err != nil {
		return nil, err
	}
	err = managementTransaction(ctx, s.Repo, profile, func(work context.Context, current json.RawMessage) (json.RawMessage, error) {
		if err := work.Err(); err != nil {
			return nil, err
		}
		return replaceManagementRouter(current, "", r)
	})
	if err != nil {
		return nil, err
	}
	managementLogger.Infof(messageManagementCreated, releaseLabel(r), r.Type)
	return r, nil
}
func replaceManagementRouter(config json.RawMessage, id string, next *Router) (json.RawMessage, error) {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(config, &doc); err != nil {
		return nil, err
	}
	var collection map[string]json.RawMessage
	_ = json.Unmarshal(doc["routers"], &collection)
	if collection == nil {
		collection = map[string]json.RawMessage{}
	}
	var rows []json.RawMessage
	_ = json.Unmarshal(collection["items"], &rows)
	out := []json.RawMessage{}
	for _, row := range rows {
		var key struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(row, &key)
		if id != "" && key.ID == id {
			if next != nil {
				encoded, err := json.Marshal(next)
				if err != nil {
					return nil, err
				}
				out = append(out, encoded)
			}
			continue
		}
		out = append(out, row)
	}
	if id == "" && next != nil {
		encoded, err := json.Marshal(next)
		if err != nil {
			return nil, err
		}
		out = append(out, encoded)
	}
	var err error
	collection["items"], err = json.Marshal(out)
	if err != nil {
		return nil, err
	}
	doc["routers"], err = json.Marshal(collection)
	if err != nil {
		return nil, err
	}
	return json.Marshal(doc)
}
func (s *ManagementService) locked(ctx context.Context, profile string, old *Router) (context.Context, func(), *Router, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return ctx, nil, nil, err
	}
	held, release, err := RouterOperationLock(ctx, *old, true)
	if err != nil {
		return ctx, nil, nil, err
	}
	_, rows, err := s.Records(held, profile)
	if err == nil {
		for _, r := range rows {
			if r.ID == old.ID && sameReleaseRouter(r, old) {
				return held, release, r, nil
			}
		}
		err = rosError(KindConflict, messageManagementChanged)
	}
	release()
	return ctx, nil, nil, err
}

// Save updates settings or deletes an unowned record under L2 and the repo CAS.
func (s *ManagementService) Save(ctx context.Context, profile string, old *Router, body json.RawMessage, remove bool) (*Router, error) {
	work, release, r, err := s.locked(ctx, profile, old)
	if err != nil {
		return nil, err
	}
	defer release()
	var next *Router
	if remove {
		for _, check := range []struct{ field, message string }{{"ros_policy_state", messageDeletePolicy}, {"ipv6_fakeip_state", messageDeleteIPv6Fake}, {"ipv6_routes_state", messageDeleteIPv6Routes}, {"dns_guard_state", messageDeleteDNS}, {"ipv6_pd_guard_state", messageDeletePD}} {
			if r.State(check.field) != nil {
				return nil, rosError(KindConflict, check.message)
			}
		}
	} else {
		next, err = Normalize(body, r)
		if err != nil {
			return nil, err
		}
		if next.Type != r.Type || next.Endpoint != r.Endpoint || next.Username != r.Username || next.Password != r.Password || next.VerifyTLS != r.VerifyTLS {
			next.Extra = cloneExtra(next.Extra)
			delete(next.Extra, "last_probe")
		}
	}
	if err = saveManagementSettings(work, s.Repo, profile, r, next); err != nil {
		return nil, err
	}
	repo.ResetConfigContext(work)
	if remove {
		managementLogger.Infof(messageManagementDeleted, releaseLabel(r))
	} else {
		oldRaw, _ := json.Marshal(r)
		newRaw, _ := json.Marshal(next)
		var before, after map[string]json.RawMessage
		_ = json.Unmarshal(oldRaw, &before)
		_ = json.Unmarshal(newRaw, &after)
		changed := []string{}
		keys := map[string]bool{}
		for key := range before {
			keys[key] = true
		}
		for key := range after {
			keys[key] = true
		}
		for key := range keys {
			if key != "password" && key != "last_probe" && !jsonEqual(before[key], after[key]) {
				changed = append(changed, key)
			}
		}
		slices.Sort(changed)
		if r.Password != next.Password {
			changed = append(changed, "password")
		}
		managementLogger.Infof(messageManagementUpdated, releaseLabel(next), orText(strings.Join(changed, "、"), messageManagementNoChange))
	}
	return next, nil
}
func cloneExtra(v map[string]json.RawMessage) map[string]json.RawMessage {
	out := map[string]json.RawMessage{}
	for key, raw := range v {
		out[key] = raw
	}
	return out
}

// ProbeResult is the only field persisted by a capability probe.
type ProbeResult struct {
	At string `json:"at"`
	OK bool   `json:"ok"`
	*RouterInfo
	Message string `json:"message,omitempty"`
}

// Probe records both ordinary and malformed-response failures, preserving every
// other stored field and rejecting a concurrent change of the selected record.
func (s *ManagementService) Probe(ctx context.Context, profile string, r *Router) (ProbeResult, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return ProbeResult{}, err
	}
	d, err := s.open(r)
	if err != nil {
		return ProbeResult{}, err
	}
	defer d.Close()
	result := ProbeResult{At: lifecycle.ClockFrom(ctx).Now().Local().Format("2006-01-02T15:04:05")}
	info, err := d.Probe(ctx)
	if err != nil {
		var re *RouterError
		if !errors.As(err, &re) {
			return result, err
		}
		result.Message = diag.EventMessage(re.Message, []any{r.Password})
	} else {
		result.OK = true
		result.RouterInfo = info
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	err = managementTransaction(ctx, s.Repo, profile, func(work context.Context, current json.RawMessage) (json.RawMessage, error) {
		live := findSyncRouter(current, r.ID)
		if live == nil || !sameReleaseRouter(live, r) {
			return nil, rosError(KindConflict, messageManagementChanged)
		}
		raw, e := releaseRouterRecord(live)
		if e != nil {
			return nil, e
		}
		var fields map[string]json.RawMessage
		if e = json.Unmarshal(raw, &fields); e != nil {
			return nil, e
		}
		fields["last_probe"], e = json.Marshal(result)
		if e != nil {
			return nil, e
		}
		return replaceManagementRawRouter(current, r.ID, fields)
	})
	return result, err
}
func replaceManagementRawRouter(config json.RawMessage, id string, fields map[string]json.RawMessage) (json.RawMessage, error) {
	var doc map[string]json.RawMessage
	_ = json.Unmarshal(config, &doc)
	var collection map[string]json.RawMessage
	_ = json.Unmarshal(doc["routers"], &collection)
	var rows []json.RawMessage
	_ = json.Unmarshal(collection["items"], &rows)
	for i, row := range rows {
		var key struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(row, &key)
		if key.ID == id {
			encoded, err := json.Marshal(fields)
			if err != nil {
				return nil, err
			}
			rows[i] = encoded
		}
	}
	var err error
	collection["items"], err = json.Marshal(rows)
	if err != nil {
		return nil, err
	}
	doc["routers"], err = json.Marshal(collection)
	if err != nil {
		return nil, err
	}
	return json.Marshal(doc)
}

// EditRoute validates, locks, rebuilds and optionally applies a manual route plan.
func (s *ManagementService) EditRoute(ctx context.Context, profile string, r *Router, body json.RawMessage, apply bool) (*RoutePlan, *RouteResult, error) {
	if _, err := NormalizeRouteRequest(body); err != nil {
		return nil, nil, err
	}
	work, release, current, err := s.locked(ctx, profile, r)
	if err != nil {
		return nil, nil, err
	}
	defer release()
	d, err := s.open(current)
	if err != nil {
		return nil, nil, err
	}
	defer d.Close()
	rows, err := ReadRoutes(work, d)
	if err != nil {
		return nil, nil, err
	}
	plan, err := BuildRoutePlan(*current, rows, body)
	if err != nil || !apply {
		return plan, nil, err
	}
	var confirmation struct {
		Fingerprint string `json:"fingerprint"`
	}
	_ = json.Unmarshal(body, &confirmation)
	if confirmation.Fingerprint == "" || confirmation.Fingerprint != plan.Fingerprint {
		return plan, nil, rosError(KindConflict, messageRoutePreviewStale)
	}
	result, err := ExecuteRoutePlan(work, d, rows, plan)
	if err != nil {
		managementLogger.Warningf(messageManagementRouteFailed, releaseLabel(current), diag.EventMessage(err.Error(), []any{current.Password}))
		return plan, result, err
	}
	note := ""
	if !result.Changed {
		note = messageManagementUnchanged
	}
	managementLogger.Infof(messageManagementRoute, releaseLabel(current), plan.Summary, note)
	return plan, result, nil
}

// SavePolicyWorkspace commits the source and router draft in one transaction.
func (s *ManagementService) SavePolicyWorkspace(ctx context.Context, profile string, r *Router, body WorkspaceRequest) error {
	work, release, _, err := s.locked(ctx, profile, r)
	if err != nil {
		return err
	}
	defer release()
	return managementTransaction(work, s.Repo, profile, func(held context.Context, current json.RawMessage) (json.RawMessage, error) {
		if err := held.Err(); err != nil {
			return nil, err
		}
		return SaveWorkspace(current, r.ID, body)
	})
}

// SaveSyncSettings updates only the two automatic sync fields.
func (s *ManagementService) SaveSyncSettings(ctx context.Context, profile string, r *Router, body json.RawMessage) error {
	work, release, _, err := s.locked(ctx, profile, r)
	if err != nil {
		return err
	}
	defer release()
	return managementTransaction(work, s.Repo, profile, func(held context.Context, current json.RawMessage) (json.RawMessage, error) {
		live := findSyncRouter(current, r.ID)
		if live == nil {
			return nil, invalid(messageManagementMissing)
		}
		normalized, err := Normalize(body, live)
		if err != nil {
			return nil, err
		}
		raw, err := releaseRouterRecord(live)
		if err != nil {
			return nil, err
		}
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(raw, &fields)
		fields["auto_rule_sync"], _ = json.Marshal(normalized.AutoRuleSync)
		fields["rule_sync_interval"], _ = json.Marshal(normalized.RuleSyncInterval)
		return replaceManagementRawRouter(current, r.ID, fields)
	})
}

// ManagementRouters exposes the shared R5 decoder for a frozen HTTP snapshot.
func ManagementRouters(config json.RawMessage) []*Router { return routersOf(config) }
