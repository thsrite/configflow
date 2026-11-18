package ipv6

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"math"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	"configflow/internal/deploy"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
)

// Settings is the saved, profile-scoped verification target selection.
type Settings struct {
	ClientAgentID string `json:"client_agent_id"`
	TestURL       string `json:"test_url"`
}

func settings(a Document) Settings {
	d := object(a["ipv6_verification_settings"])
	return Settings{text(d["client_agent_id"]), text(d["test_url"])}
}
func applied(a Document) Document {
	return Document{"latest": raw(selectFields(object(a["latest_deployment"]), "deployment_id", "status", "manual_rollback", "config_version", "config_revision")), "config_revision": a["config_revision"], "config_version": a["config_version"], "applied_content": raw(object(a["applied_content"]))}
}
func clientFingerprint(a Document, all []Document) json.RawMessage {
	id := settings(a).ClientAgentID
	for _, c := range all {
		if text(c["id"]) == id && id != "" {
			return raw(digest(struct{ Identity, Applied Document }{identity(c), applied(c)}))
		}
	}
	return raw(nil)
}
func (s *Service) findings(ctx context.Context, a, report Document, all []Document) []string {
	out := []string{}
	for _, v := range []struct{ field, want, message string }{{"settings_fingerprint", digest(settings(a)), msgChangedSettings}, {"target_fingerprint", digest(identity(a)), msgChangedTarget}, {"applied_fingerprint", digest(applied(a)), msgChangedApplied}} {
		if text(report[v.field]) != v.want {
			out = append(out, v.message)
		}
	}
	if string(report["client_fingerprint"]) != string(clientFingerprint(a, all)) {
		out = append(out, msgChangedClient)
	}
	if text(report["deployment_id"]) == "" {
		revision, e := s.Manager.Repository().ConfigRevision(ctx, text(report["profile_id"]))
		if e != nil {
			out = append(out, msgChangedProfile)
		} else if string(report["config_revision"]) != string(raw(revision)) {
			out = append(out, msgChangedSaved)
		}
	}
	return out
}
func runs(a Document) model.Map[json.RawMessage] {
	var m model.Map[json.RawMessage]
	_ = json.Unmarshal(a["ipv6_verifications"], &m)
	return m
}
func expired(ctx context.Context, r Document) bool {
	if text(r["status"]) != "running" {
		return false
	}
	var start float64
	e := json.Unmarshal(r["started_epoch"], &start)
	now := float64(lifecycle.ClockFrom(ctx).Now().UnixNano()) / 1e9
	return e != nil || math.IsInf(start, 0) || math.IsNaN(start) || now-start > 300 || start > now+30
}
func interrupt(ctx context.Context, r Document, message string) {
	r["status"] = raw("error")
	r["checked_at"] = raw(now(ctx))
	r["message"] = raw(message)
	r["checks"] = raw([]Check{check("interrupted", msgVerifyIncomplete, "error", msgIncomplete)})
}
func newID() string { var id [16]byte; _, _ = rand.Read(id[:]); return hex.EncodeToString(id[:]) }

// VerificationResult is a stored verification projected for the current inputs.
type VerificationResult struct {
	Success      bool            `json:"success"`
	AgentID      string          `json:"agent_id"`
	ProfileID    string          `json:"profile_id"`
	Verification json.RawMessage `json:"verification"`
	Applicable   *bool           `json:"applicable,omitempty"`
	Message      string          `json:"message,omitempty"`
}

func notApplicable(id, profile string) *VerificationResult {
	return &VerificationResult{Success: true, AgentID: id, ProfileID: profile, Verification: raw(nil), Applicable: ptr(false), Message: msgNotApplicable}
}

// Result reads a report without persisting expiry/staleness overlays.
func (s *Service) Result(ctx context.Context, id, profile, did string) (*VerificationResult, error) {
	a, e := s.agent(ctx, id, profile)
	if e != nil {
		return nil, e
	}
	if did == "" {
		config, _, e := s.snapshot(ctx, profile)
		if e != nil {
			return nil, e
		}
		c, _ := ReadConfiguration(config)
		if !yes(c.IPv6) {
			return notApplicable(id, profile), nil
		}
	}
	var report Document
	m := runs(a)
	if did != "" {
		b, _ := m.Get(did)
		report = object(b)
	} else {
		latest := -math.MaxFloat64
		for _, b := range m.All() {
			r := object(b)
			var started float64
			_ = json.Unmarshal(r["started_epoch"], &started)
			if text(r["profile_id"]) == profile && started > latest {
				report = r
				latest = started
			}
		}
	}
	if report != nil && text(report["profile_id"]) != profile {
		report = nil
	}
	if report != nil {
		all, e := s.all(ctx)
		if e != nil {
			return nil, e
		}
		changed := s.findings(ctx, a, report, all)
		if len(changed) > 0 {
			report["status"] = raw("warning")
			report["stale"] = raw(true)
			report["message"] = raw(strings.Join(changed, "、") + msgRetry)
			report["checks"] = raw([]Check{check("result_stale", msgOld, "unknown", msgStale, changed...)})
		} else if expired(ctx, report) {
			interrupt(ctx, report, msgInterrupted)
		}
	}
	return &VerificationResult{Success: true, AgentID: id, ProfileID: profile, Verification: raw(report)}, nil
}
func supported(cap Document) bool {
	if text(cap["platform"]) != "linux" {
		return false
	}
	var protocols []json.RawMessage
	_ = json.Unmarshal(cap["ipv6_client_probe_protocols"], &protocols)
	return slices.ContainsFunc(protocols, func(b json.RawMessage) bool { return string(b) == "1" })
}
func (s *Service) client(ctx context.Context, source Document, id string, capabilities bool) (Document, error) {
	client := s.lookup(ctx, id)
	if client == nil || id == text(source["id"]) || text(client["profile_id"]) != text(source["profile_id"]) || strings.EqualFold(text(client["host"]), text(source["host"])) || text(client["deployment_method"]) != "shell" || text(client["service_type"]) == "surge" {
		return nil, failure("CLIENT_UNAVAILABLE", 409, msgClientUnavailable)
	}
	if string(client["enabled"]) == "false" || !online(client) {
		return nil, failure("CLIENT_UNAVAILABLE", 409, msgClientOffline)
	}
	if capabilities && !supported(s.capabilities(ctx, client)) {
		return nil, failure("CLIENT_UPGRADE_REQUIRED", 409, msgClientUpgrade)
	}
	return client, nil
}

// Candidate exposes identity and capability only, never credentials.
type Candidate struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Host      string  `json:"host"`
	Status    string  `json:"status"`
	Enabled   bool    `json:"enabled"`
	Platform  *string `json:"platform"`
	Supported bool    `json:"supported"`
}

// SettingsResult is the bounded eligible-client view.
type SettingsResult struct {
	Success    bool        `json:"success"`
	AgentID    string      `json:"agent_id"`
	ProfileID  string      `json:"profile_id"`
	Settings   Settings    `json:"settings"`
	Candidates []Candidate `json:"candidates"`
}

// VerificationSettings reads or atomically replaces saved settings. The write
// checks captured identity and running jobs again at the repository boundary.
func (s *Service) VerificationSettings(ctx context.Context, id, profile string, body json.RawMessage) (*SettingsResult, error) {
	a, e := s.agent(ctx, id, profile)
	if e != nil {
		return nil, e
	}
	if body != nil {
		values := object(body)
		if len(values) != 2 || values["client_agent_id"] == nil || values["test_url"] == nil {
			return nil, failure("INVALID_REQUEST", 400, msgSettingsBody)
		}
		var saved Settings
		if json.Unmarshal(body, &saved) != nil {
			return nil, failure("INVALID_REQUEST", 400, msgSettingsBody)
		}
		saved.ClientAgentID = strings.TrimSpace(saved.ClientAgentID)
		saved.TestURL = strings.TrimSpace(saved.TestURL)
		if utf8.RuneCountInString(saved.ClientAgentID) > 200 || utf8.RuneCountInString(saved.TestURL) > 2048 {
			return nil, failure("INVALID_REQUEST", 400, msgSettingsLong)
		}
		if saved.TestURL != "" {
			if e = validURL(saved.TestURL); e != nil {
				return nil, failure("INVALID_REQUEST", 400, e.Error())
			}
		}
		if saved.ClientAgentID != "" {
			if _, e = s.client(ctx, a, saved.ClientAgentID, true); e != nil {
				return nil, e
			}
		}
		e = deploy.RunDetached(ctx, "ipv6-settings", func(ctx context.Context) error {
			return s.update(ctx, id, func(ctx context.Context, current Document, _ []Document) error {
				if current == nil || digest(identity(current)) != digest(identity(a)) {
					return failure("PROFILE_CHANGED", 409, msgIdentityChanged)
				}
				for _, b := range runs(current).All() {
					if text(object(b)["status"]) == "running" {
						return failure("BUSY", 409, msgSettingsBusy)
					}
				}
				current["ipv6_verification_settings"] = raw(saved)
				return nil
			})
		})
		if e != nil {
			return nil, e
		}
		a, e = s.agent(ctx, id, profile)
		if e != nil {
			return nil, e
		}
	}
	all, e := s.all(ctx)
	if e != nil {
		return nil, e
	}
	candidates := []Document{}
	for _, v := range all {
		if c, e := s.client(ctx, a, text(v["id"]), false); e == nil {
			candidates = append(candidates, c)
			if len(candidates) == 32 {
				break
			}
		}
	}
	result := &SettingsResult{Success: true, AgentID: id, ProfileID: profile, Settings: settings(a), Candidates: make([]Candidate, len(candidates))}
	// Bounded caller-scoped fanout; every worker is joined before returning.
	var wg sync.WaitGroup
	slots := make(chan struct{}, 4)
	for i, c := range candidates {
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case slots <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-slots }()
			cap := s.capabilities(ctx, c)
			name := text(c["name"])
			if name == "" {
				name = text(c["id"])
			}
			var platform *string
			if value := text(cap["platform"]); value != "" {
				platform = &value
			}
			result.Candidates[i] = Candidate{text(c["id"]), name, text(c["host"]), text(c["status"]), string(c["enabled"]) != "false", platform, supported(cap)}
		}()
	}
	wg.Wait()
	if e = ctx.Err(); e != nil {
		return nil, e
	}
	return result, nil
}
