package ipv6

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"configflow/internal/deploy"
	"configflow/internal/lifecycle"
	"configflow/internal/routers"
)

type lastCheck struct {
	AgentID   string  `json:"agent_id"`
	ProfileID string  `json:"profile_id"`
	Key       string  `json:"key"`
	CheckedAt string  `json:"checked_at"`
	Result    *Report `json:"result"`
}

func now(ctx context.Context) string { return lifecycle.ClockFrom(ctx).Now().Format(time.RFC3339) }
func (s *Service) key(ctx context.Context, a Document, profile string, config Document) string {
	agent := identity(a)
	agent["status"] = raw("offline")
	if online(a) {
		agent["status"] = raw("online")
	}
	agent["version"] = a["version"]
	agent["verification"] = a["ipv6_verification_settings"]
	latest := selectFields(object(a["latest_deployment"]), "deployment_id", "status", "config_version", "config_revision")
	latest["config_revision"] = a["config_revision"]
	latest["config_version"] = a["config_version"]
	candidate := s.lookup(ctx, text(object(a["ipv6_verification_settings"])["client_agent_id"]))
	var client Document
	if candidate != nil {
		client = selectFields(candidate, "id", "host", "profile_id", "enabled")
	}
	return digest(struct {
		Profile                           string
		Agent, Deployment, Config, Client Document
	}{profile, agent, latest, prepareConfig(config), client})
}
func (s *Service) lastPath(id string) string {
	sum := sha256.Sum256([]byte(id))
	return filepath.Join(s.Manager.Repository().DataDir(), ".ipv6-readiness", hex.EncodeToString(sum[:])+".json")
}
func (s *Service) last(id, profile string) *lastCheck {
	b, e := os.ReadFile(s.lastPath(id))
	if e != nil {
		return nil
	}
	var r lastCheck
	if json.Unmarshal(b, &r) != nil || r.AgentID != id || r.ProfileID != profile || r.Result == nil || r.Key == "" {
		return nil
	}
	return &r
}
func (s *Service) remember(ctx context.Context, a Document, profile string, config Document, result *Report) (string, error) {
	if e := ctx.Err(); e != nil {
		return "", e
	}
	copy := *result
	copy.CheckedAt = ""
	copy.Cached = nil
	copy.RefreshError = nil
	copy.Stale = nil
	copy.Message = ""
	copy.ChangedRoutes = nil
	copy.Publication = ""
	record := lastCheck{text(a["id"]), profile, s.key(ctx, a, profile, config), now(ctx), &copy}
	path := s.lastPath(record.AgentID)
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return "", fmt.Errorf("create IPv6 check directory: %w", e)
	}
	file, e := os.CreateTemp(filepath.Dir(path), ".write-")
	if e != nil {
		return "", fmt.Errorf("create IPv6 check: %w", e)
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if _, e = file.Write(raw(record)); e != nil {
		return "", fmt.Errorf("write IPv6 check: %w", e)
	}
	if e = file.Close(); e != nil {
		return "", e
	}
	if e = ctx.Err(); e != nil {
		return "", e
	}
	if e = os.Rename(file.Name(), path); e != nil {
		return "", fmt.Errorf("replace IPv6 check: %w", e)
	}
	return record.CheckedAt, nil
}

// Status reuses the last profile-bound check or refreshes it. A failed refresh
// retains the previous observation and states whether its saved inputs changed.
func (s *Service) Status(ctx context.Context, id, profile string, refresh bool) (*Report, error) {
	var result *Report
	e := deploy.RunDetached(ctx, "ipv6-readiness", func(ctx context.Context) error {
		held, release, e := operations.Lock(ctx, id, 0)
		if e != nil {
			return e
		}
		defer release()
		ctx = held
		if e = ctx.Err(); e != nil {
			return e
		}
		a, e := s.agent(ctx, id, profile)
		if e != nil {
			return e
		}
		config, revision, e := s.snapshot(ctx, profile)
		if e != nil {
			return e
		}
		key := s.key(ctx, a, profile, config)
		last := s.last(id, profile)
		published := s.publication(ctx, a, config, revision, false)
		if last != nil && last.Key == key && !refresh {
			result = last.Result
			result.ConfigRevision = &revision
			result.CheckedAt = last.CheckedAt
			result.Cached = ptr(true)
			result.Publication = published
			return nil
		}
		if !online(a) {
			e = failure("AGENT_OFFLINE", 409, msgOffline)
		} else {
			result, e = s.inspect(ctx, a, profile, config)
		}
		if e != nil {
			var boundary *Error
			if !errors.As(e, &boundary) || last == nil {
				return e
			}
			result = last.Result
			result.CheckedAt = last.CheckedAt
			result.Cached = ptr(true)
			result.Stale = ptr(last.Key != key)
			result.RefreshError = &RefreshError{boundary.Code, boundary.Message, now(ctx)}
			result.Publication = published
			return nil
		}
		result.ConfigRevision = &revision
		result.CheckedAt, e = s.remember(ctx, a, profile, config, result)
		result.Cached = ptr(false)
		result.Publication = published
		return e
	})
	return result, e
}

// Prepare binds host changes to a fresh check and unchanged saved identity. It
// holds shared router publication/job locks but never writes a router.
func (s *Service) Prepare(ctx context.Context, id, profile string, body json.RawMessage) (*Report, error) {
	input := object(body)
	if len(input) != 2 || input["interface"] == nil || input["check_token"] == nil {
		return nil, failure("INVALID_REQUEST", 400, msgPrepareBody)
	}
	token := text(input["check_token"])
	if len(token) != 64 {
		return nil, failure("INVALID_REQUEST", 400, msgCheckFirst)
	}
	var result *Report
	e := deploy.RunDetached(ctx, "ipv6-prepare", func(ctx context.Context) error {
		held, release, e := operations.Lock(ctx, id, 0)
		if e != nil {
			return e
		}
		defer release()
		ctx = held
		if e = ctx.Err(); e != nil {
			return e
		}
		a, e := s.agent(ctx, id, profile)
		if e != nil {
			return e
		}
		config, _, e := s.snapshot(ctx, profile)
		if e != nil {
			return e
		}
		rs := linked(config, id)
		slices.SortFunc(rs, func(a, b *routers.Router) int { return strings.Compare(a.ID, b.ID) })
		// Try-only job locks retain L1 semantics even under the Agent operation.
		var jobs []func()
		defer func() {
			for i := len(jobs) - 1; i >= 0; i-- {
				jobs[i]()
			}
		}()
		seen := map[string]bool{}
		for _, r := range rs {
			key := r.Type + "\n" + strings.TrimRight(r.Endpoint, "/")
			if seen[key] {
				continue
			}
			seen[key] = true
			_, unlock, ok := routers.JobLock(ctx, profile, r.ID, r)
			if !ok {
				return failure("BUSY", 409, msgRouterBusy)
			}
			jobs = append(jobs, unlock)
		}
		ctx, unlock, e := routers.RouterOperationLocks(ctx, rs, true)
		if e != nil {
			return e
		}
		defer unlock()
		report, e := s.inspect(ctx, a, profile, config)
		if e != nil {
			return e
		}
		if token != report.CheckToken || text(input["interface"]) != report.PrepareInterface {
			return failure("CHECK_CHANGED", 409, msgCheckChanged)
		}
		if !report.CanPrepare {
			return failure("PREPARE_UNAVAILABLE", 409, msgPrepareUnavailable)
		}
		current, e := s.agent(ctx, id, profile)
		if e != nil {
			return e
		}
		saved, _, e := s.snapshot(ctx, profile)
		if e != nil {
			return e
		}
		if digest(prepareConfig(saved)) != digest(prepareConfig(config)) || digest(identity(current)) != digest(identity(a)) {
			return failure("CHECK_CHANGED", 409, msgPrepareChanged)
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		host := object(report.Host)
		prepared, e := s.request(ctx, a, "prepare", Document{"interface": input["interface"], "interface_index": host["interface_index"]})
		if e != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return failure("PREPARE_FAILED", 502, fmt.Sprintf(msgPrepareFailed, e.Error()))
		}
		result, e = s.inspect(ctx, a, profile, config)
		if e != nil {
			return e
		}
		result.Message = msgPrepared
		result.ChangedRoutes = ptr(0)
		var checks []json.RawMessage
		_ = json.Unmarshal(prepared["checks"], &checks)
		for _, b := range checks {
			v := object(b)
			if text(v["id"]) == "tun_recovery" {
				message := text(v["message"])
				if message == "" {
					message = msgTUNRecheck
				}
				result.Checks = append(result.Checks, check("prepare_tun_recovery", msgTUNRecovery, "warning", message))
			}
		}
		result.CheckedAt, e = s.remember(ctx, a, profile, config, result)
		result.Cached = ptr(false)
		return e
	})
	return result, e
}

// ProbeResult explicitly reports bounded TCP reachability rather than a client path.
type ProbeResult struct {
	Success      bool              `json:"success"`
	AgentID      string            `json:"agent_id"`
	ProfileID    string            `json:"profile_id"`
	Results      []json.RawMessage `json:"results,omitempty"`
	Connectivity Connectivity      `json:"connectivity"`
}

// Probe tests saved directly reachable node candidates using the Agent owner.
func (s *Service) Probe(ctx context.Context, id, profile string) (*ProbeResult, error) {
	a, e := s.agent(ctx, id, profile)
	if e != nil {
		return nil, e
	}
	config, _, e := s.snapshot(ctx, profile)
	if e != nil {
		return nil, e
	}
	settings, doc := ReadConfiguration(config)
	if len(settings.Errors) > 0 || !yes(settings.IPv6) {
		return nil, failure("IPV6_DISABLED", 409, msgProbeDisabled)
	}
	result := &ProbeResult{Success: true, AgentID: id, ProfileID: profile, Connectivity: Connectivity{Status: "not_checked", Message: msgNoTargets}}
	targets := ProbeTargets(config, doc)
	if len(targets) == 0 {
		return result, nil
	}
	passed := false
	details := []string{}
	for i, target := range targets {
		response, e := s.request(ctx, a, "probe", Document{"host": raw(target.Host), "port": raw(target.Port)})
		if e != nil {
			return nil, e
		}
		connection := object(response["connectivity"])
		if connection == nil {
			connection = Document{"status": raw("error"), "message": raw(msgNoProbeResult)}
		}
		result.Results = append(result.Results, raw(connection))
		passed = passed || text(connection["status"]) == "ok"
		message := text(connection["message"])
		if message == "" {
			message = text(connection["status"])
		}
		details = append(details, fmt.Sprintf(msgProbeDetail, i+1, message))
	}
	status, message := "error", msgProbeFailed
	if passed {
		status, message = "ok", msgProbeOK
	}
	result.Connectivity = Connectivity{Status: status, Message: message, Scope: "node_transport", Details: details}
	return result, nil
}
