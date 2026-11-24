package routers

import (
	"configflow/internal/jsondecode"
	"configflow/internal/routers/device"
	"configflow/internal/routers/oplock"
	"configflow/internal/routers/rospolicy"
	"configflow/internal/routers/routeedit"
	"configflow/internal/routers/spec"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"configflow/internal/agents/diag"
	"configflow/internal/lifecycle"
	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/redact"
	"configflow/internal/repo"
)

// ManagementService owns router configuration and manual-route writes. Existing
// router writes require the caller's L1 job context; the service acquires L2.
type ManagementService struct {
	Repo      *repo.Repository
	NewDriver func(*spec.Router) Driver
}

var managementLogger = logx.Get("backend.routes.routers")

// ConfigView is one generation of a profile as router routes read it.
type ConfigView struct {
	// Config is the resolved configuration with the OxiDNS Agent projection,
	// borrowed from the repository snapshot (read only). Only ConfigRecords
	// sets it.
	Config *model.Config
	// Routers are the profile's router records, decoded with the R5 fallback.
	Routers []*spec.Router
	// Credentials are the credential values redact.CollectCredentials finds
	// in the encoded resolved configuration; responses built from this view
	// hide them.
	Credentials redact.Secrets
}

// Encoded returns the encoded configuration of a ConfigRecords view.
func (v *ConfigView) Encoded() (json.RawMessage, error) {
	if v.Config == nil {
		return nil, errors.New("router config view without configuration")
	}
	return json.Marshal(v.Config)
}

// Records loads a profile's R5-decoded router records and the credential
// values of its resolved configuration, from one generation. The repository
// remembers the values per generation, so this resolves nothing when cached.
func (s *ManagementService) Records(ctx context.Context, profile string) (*ConfigView, error) {
	stored, credentials, err := redact.ProfileWithCredentials(ctx, s.Repo, profile)
	if err != nil {
		return nil, err
	}
	routers, err := spec.StoredRouters(&stored.Routers)
	if err != nil {
		return nil, err
	}
	return &ConfigView{Routers: routers, Credentials: credentials}, nil
}

// ConfigRecords is Records with the resolved configuration of the same
// generation, for readers of the whole configuration.
func (s *ManagementService) ConfigRecords(ctx context.Context, profile string) (*ConfigView, error) {
	config, credentials, err := redact.ConfigWithCredentials(ctx, s.Repo, profile)
	if err != nil {
		return nil, err
	}
	routers, err := spec.StoredRouters(&config.Profile.Routers)
	if err != nil {
		return nil, err
	}
	return &ConfigView{Config: config, Routers: routers, Credentials: credentials}, nil
}
func (s *ManagementService) open(r *spec.Router) (Driver, error) {
	if err := r.ValidateForDevice(); err != nil {
		return nil, err
	}
	factory := s.NewDriver
	if factory == nil {
		factory = NewDriver
	}
	d := factory(r)
	if d == nil {
		return nil, device.Error(device.MessageOperationFormat)
	}
	return d, nil
}

// Create validates and appends one router inside a repository transaction.
func (s *ManagementService) Create(ctx context.Context, profile string, body json.RawMessage) (*spec.Router, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r, err := spec.Normalize(body, nil)
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
	managementLogger.Infof(messageManagementCreated, s.safeLog(ctx, profile, r, device.OrText(r.Name, r.ID)), r.Type)
	return r, nil
}
func replaceManagementRouter(config json.RawMessage, id string, next *spec.Router) (json.RawMessage, error) {
	var p model.Profile
	if err := json.Unmarshal(config, &p); err != nil {
		return nil, err
	}
	out := make([]model.Router, 0, len(p.Routers.Items)+1)
	appendNext := func() error {
		if next == nil {
			return nil
		}
		raw, err := next.MarshalJSON()
		if err != nil {
			return err
		}
		var stored model.Router
		if err = json.Unmarshal(raw, &stored); err != nil {
			return err
		}
		out = append(out, stored)
		return nil
	}
	for _, r := range p.Routers.Items {
		if id != "" && r.ID == id {
			if err := appendNext(); err != nil {
				return nil, err
			}
		} else {
			out = append(out, r)
		}
	}
	if id == "" {
		if err := appendNext(); err != nil {
			return nil, err
		}
	}
	p.Routers.Items = out
	return model.Marshal(&p)
}

func (s *ManagementService) locked(ctx context.Context, profile string, old *spec.Router) (context.Context, func(), *spec.Router, error) {
	return s.lockedFor(ctx, profile, old, true)
}
func (s *ManagementService) lockedFor(ctx context.Context, profile string, old *spec.Router, write bool) (context.Context, func(), *spec.Router, error) {
	if write {
		ctx = lifecycle.Detach(ctx)
	}
	if old == nil {
		return ctx, nil, nil, device.KindError(device.KindConflict, messageManagementMissing)
	}
	if err := ctx.Err(); err != nil {
		return ctx, nil, nil, err
	}
	held, release, err := oplock.RouterOperationLock(ctx, *old, write)
	if err != nil {
		return ctx, nil, nil, err
	}
	view, err := s.Records(held, profile)
	if err == nil {
		for _, r := range view.Routers {
			if r.ID == old.ID && spec.SameReleaseRouter(r, old) {
				return held, release, r, nil
			}
		}
		err = device.KindError(device.KindConflict, messageManagementChanged)
	}
	release()
	return ctx, nil, nil, err
}

// Save updates settings or deletes an unowned record under L2 and the repo CAS.
func (s *ManagementService) Save(ctx context.Context, profile string, old *spec.Router, body json.RawMessage, remove bool) (*spec.Router, error) {
	work, release, r, err := s.locked(ctx, profile, old)
	if err != nil {
		return nil, err
	}
	defer release()
	var next *spec.Router
	if remove {
		for _, check := range []struct{ field, message string }{{"ros_policy_state", messageDeletePolicy}, {"ipv6_fakeip_state", messageDeleteIPv6Fake}, {"ipv6_routes_state", messageDeleteIPv6Routes}, {"dns_guard_state", messageDeleteDNS}, {"ipv6_pd_guard_state", messageDeletePD}} {
			if r.State(check.field) != nil {
				return nil, device.KindError(device.KindConflict, check.message)
			}
		}
	} else {
		next, err = spec.Normalize(body, r)
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
		managementLogger.Infof(messageManagementDeleted, s.safeLog(work, profile, r, device.OrText(r.Name, r.ID)))
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
			if key != "password" && key != "last_probe" && !device.JSONEqual(before[key], after[key]) {
				changed = append(changed, key)
			}
		}
		slices.Sort(changed)
		if r.Password != next.Password {
			changed = append(changed, "password")
		}
		managementLogger.Infof(messageManagementUpdated, s.safeLog(work, profile, r, device.OrText(next.Name, next.ID)), device.OrText(strings.Join(changed, "、"), messageManagementNoChange))
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
	*device.RouterInfo
	Message string `json:"message,omitempty"`
}

// Probe records both ordinary and malformed-response failures, preserving every
// other stored field and rejecting a concurrent change of the selected record.
func (s *ManagementService) Probe(ctx context.Context, profile string, r *spec.Router) (ProbeResult, error) {
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
		var re *device.RouterError
		if !errors.As(err, &re) {
			return result, err
		}
		// Python stores str(error). Only credentials are hidden here: the
		// diagnostic URL filter would erase the redirect origin the user needs.
		result.Message = s.safeText(ctx, profile, r, re.Message)
	} else {
		result.OK = true
		result.RouterInfo = info
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	err = managementTransaction(ctx, s.Repo, profile, func(work context.Context, current json.RawMessage) (json.RawMessage, error) {
		live := spec.FindSyncRouter(current, r.ID)
		if live == nil || !spec.SameReleaseRouter(live, r) {
			return nil, device.KindError(device.KindConflict, messageManagementChanged)
		}
		raw, e := spec.ReleaseRouterRecord(live)
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
	var p model.Profile
	if err := json.Unmarshal(config, &p); err != nil {
		return nil, err
	}
	for i := range p.Routers.Items {
		r := &p.Routers.Items[i]
		if r.ID != id {
			continue
		}
		before, err := model.Marshal(r)
		if err != nil {
			return nil, err
		}
		var old map[string]json.RawMessage
		if err = json.Unmarshal(before, &old); err != nil {
			return nil, err
		}
		for key, value := range fields {
			if !device.JSONEqual(value, old[key]) {
				if err = r.SetJSON(key, value); err != nil {
					return nil, err
				}
			}
		}
	}
	return model.Marshal(&p)
}

// EditRoute validates, locks, rebuilds and optionally applies a manual route plan.
func (s *ManagementService) EditRoute(ctx context.Context, profile string, r *spec.Router, body json.RawMessage, apply bool) (*routeedit.RoutePlan, *routeedit.RouteResult, error) {
	if _, err := routeedit.NormalizeRouteRequest(body); err != nil {
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
	rows, err := routeedit.ReadRoutes(work, d)
	if err != nil {
		return nil, nil, err
	}
	plan, err := routeedit.BuildRoutePlan(*current, rows, body)
	if err != nil || !apply {
		return plan, nil, err
	}
	var confirmation struct {
		Fingerprint string `json:"fingerprint"`
	}
	_ = jsondecode.Unmarshal(body, &confirmation)
	if confirmation.Fingerprint == "" || confirmation.Fingerprint != plan.Fingerprint {
		return plan, nil, device.KindError(device.KindConflict, messageRoutePreviewStale)
	}
	result, err := routeedit.ExecuteRoutePlan(work, d, rows, plan)
	if err != nil {
		managementLogger.Warningf(messageManagementRouteFailed, s.safeLog(work, profile, current, device.OrText(current.Name, current.ID)), s.safeLog(work, profile, current, err.Error()))
		return plan, result, err
	}
	note := ""
	if !result.Changed {
		note = messageManagementUnchanged
	}
	managementLogger.Infof(messageManagementRoute, s.safeLog(work, profile, current, device.OrText(current.Name, current.ID)), s.safeLog(work, profile, current, plan.Summary), note)
	return plan, result, nil
}

// SavePolicyWorkspace commits the source and router draft in one transaction.
func (s *ManagementService) SavePolicyWorkspace(ctx context.Context, profile string, r *spec.Router, body WorkspaceRequest) error {
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
func (s *ManagementService) SaveSyncSettings(ctx context.Context, profile string, r *spec.Router, body json.RawMessage) error {
	work, release, _, err := s.locked(ctx, profile, r)
	if err != nil {
		return err
	}
	defer release()
	return managementTransaction(work, s.Repo, profile, func(held context.Context, current json.RawMessage) (json.RawMessage, error) {
		live := spec.FindSyncRouter(current, r.ID)
		if live == nil {
			return nil, device.Invalid(messageManagementMissing)
		}
		normalized, err := spec.Normalize(body, live)
		if err != nil {
			return nil, err
		}
		raw, err := spec.ReleaseRouterRecord(live)
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
func ManagementRouters(config json.RawMessage) []*spec.Router { return spec.RoutersOf(config) }

// PolicyObjects reads under the same L2 and complete stored-record guard as
// writes. R5 display defaults never erase changes from the optimistic proof.
func (s *ManagementService) PolicyObjects(ctx context.Context, profile string, r *spec.Router) ([]rospolicy.ROSPolicyObject, error) {
	work, release, current, err := s.lockedFor(ctx, profile, r, false)
	if err != nil {
		return nil, err
	}
	defer release()
	d, err := s.open(current)
	if err != nil {
		return nil, err
	}
	defer d.Close()
	ros, ok := d.(*RouterOS)
	if !ok {
		return nil, device.Invalid(messageManagementPolicyOnly)
	}
	return rospolicy.ReadPolicyObjects(work, ros.Client, current.Extra["ros_policy_state"])
}

func (s *ManagementService) safeLog(ctx context.Context, profile string, r *spec.Router, value string) string {
	return eventText(s.safeText(ctx, profile, r, value))
}

// safeText hides stored and peer credentials without the diagnostic filters.
func (s *ManagementService) safeText(ctx context.Context, profile string, r *spec.Router, value string) string {
	if ctx.Err() != nil {
		return redact.Redacted
	}
	credentials, err := redact.ConfigCredentials(ctx, s.Repo, profile, nil)
	if err != nil {
		return redact.Redacted
	}
	seeds, err := redact.ExternalPayloadSecrets(ctx, nil)
	if err == nil {
		seeds, err = redact.AgentRecordCredentials(ctx, seeds)
	}
	if err != nil {
		return redact.Redacted
	}
	return managementSafeText(nil, redact.NewSecrets(seeds, credentials), r, value)
}
func managementLogText(config json.RawMessage, seeds redact.Secrets, r *spec.Router, value string) string {
	return eventText(managementSafeText(config, seeds, r, value))
}
func eventText(value string) string {
	if value == redact.Redacted {
		return value
	}
	return diag.EventMessage(value, nil)
}
func managementSafeText(config json.RawMessage, seeds redact.Secrets, r *spec.Router, value string) string {
	if r == nil {
		return redact.Redacted
	}
	seeds = redact.NewSecrets(seeds, []string{r.Password})
	raw := redact.SanitizeCredentials(value, redact.CollectCredentials(config, seeds))
	var safe string
	if json.Unmarshal(raw, &safe) != nil {
		return redact.Redacted
	}
	return safe
}
