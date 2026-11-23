package ipv6

import (
	"configflow/internal/jsondecode"
	"configflow/internal/jsonresp"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"sync"

	"configflow/internal/deploy"
	"configflow/internal/lifecycle"
	"configflow/internal/redact"
)

var verificationSlots = make(chan struct{}, 8)
var verificationWorkers = make(chan struct{}, 2)

func sha(value string) string {
	if len(value) != 64 {
		return ""
	}
	if _, e := hex.DecodeString(value); e != nil {
		return ""
	}
	return strings.ToLower(value)
}
func (s *Service) source(ctx context.Context, a Document, did string, expected *int64) (Document, *int64, string, error) {
	config, revision, e := s.snapshot(ctx, text(a["profile_id"]))
	if e != nil {
		return nil, nil, "", e
	}
	rev := &revision
	hash := ""
	if did != "" {
		task := object(object(a["deployments"])[did])
		if text(task["status"]) != "succeeded" || yes(task["manual_rollback"]) || text(task["profile_id"]) != text(a["profile_id"]) || text(task["service_type"]) != "mihomo" || text(object(a["latest_deployment"])["deployment_id"]) != did {
			return nil, nil, "", failure("DEPLOYMENT_CHANGED", 409, msgDeploymentChanged)
		}
		var r int64
		if json.Unmarshal(task["config_revision"], &r) == nil {
			rev = &r
		} else {
			rev = nil
		}
		receipt, e := s.receipt(ctx, a)
		if e != nil || receipt == nil || receipt.Manifest.DeploymentID != did {
			return nil, nil, "", failure("SNAPSHOT_UNAVAILABLE", 409, msgSnapshot)
		}
		config = object(receipt.Source)
		if config == nil {
			return nil, nil, "", failure("SNAPSHOT_UNAVAILABLE", 409, msgSnapshot)
		}
		hash = sha(text(task["config_version"]))
	}
	if expected != nil && (rev == nil || *expected != *rev) {
		return nil, nil, "", failure("CONFIG_CHANGED", 409, msgConfigChanged)
	}
	return config, rev, hash, nil
}
func (s *Service) reusable(ctx context.Context, a, report Document, automatic bool, did string, all []Document) bool {
	if report == nil {
		return false
	}
	if text(report["status"]) == "running" && !expired(ctx, report) {
		return true
	}
	return automatic || did != "" && text(report["status"]) == "ok" && len(s.findings(ctx, a, report, all)) == 0
}
func (s *Service) newRun(ctx context.Context, a Document, revision *int64, did string, all []Document) Document {
	saved := settings(a)
	scope, message := "mihomo_runtime", msgManualRunning
	if saved.ClientAgentID != "" {
		scope = "client_path"
	}
	if did != "" {
		message = msgAutoRunning
	}
	var deployment *string
	if did != "" {
		deployment = &did
	}
	return Document{"status": jsonresp.Value("running"), "scope": jsonresp.Value(scope), "checks": jsonresp.Value([]Check{}), "started_at": jsonresp.Value(now(ctx)), "started_epoch": jsonresp.Value(float64(lifecycle.ClockFrom(ctx).Now().UnixNano()) / 1e9), "run_id": jsonresp.Value(newID()), "profile_id": a["profile_id"], "config_revision": jsonresp.Value(revision), "deployment_id": jsonresp.Value(deployment), "settings_fingerprint": jsonresp.Value(digest(saved)), "target_fingerprint": jsonresp.Value(digest(verificationIdentity(a))), "applied_fingerprint": jsonresp.Value(digest(applied(a))), "client_fingerprint": clientFingerprint(a, all), "message": jsonresp.Value(message)}
}

// Start reserves a bounded verification and spawns a Runtime-owned worker. It
// never changes deployment success; each stored result keeps captured inputs.
func (s *Service) Start(ctx context.Context, id, profile string, body json.RawMessage, automatic bool) (*VerificationResult, error) {
	input := object(body)
	if input == nil {
		return nil, failure("INVALID_REQUEST", 400, msgVerifyBody)
	}
	for k := range input {
		if k != "deployment_id" && k != "expected_config_revision" {
			return nil, failure("INVALID_REQUEST", 400, msgVerifyBody)
		}
	}
	did := text(input["deployment_id"])
	if b, ok := input["deployment_id"]; ok && string(b) != "null" && (did == "" || len([]rune(did)) > 128) {
		return nil, failure("INVALID_REQUEST", 400, msgDeploymentID)
	}
	var expected *int64
	if b, ok := input["expected_config_revision"]; ok {
		var n int64
		if json.Unmarshal(b, &n) != nil || string(b) == "null" || n < 0 {
			return nil, failure("INVALID_REQUEST", 400, msgRevision)
		}
		expected = &n
	}
	var result *VerificationResult
	e := deploy.RunDetached(ctx, "ipv6-verification-reserve", func(ctx context.Context) error {
		if e := ctx.Err(); e != nil {
			return e
		}
		a, e := s.agent(ctx, id, profile)
		if e != nil {
			return e
		}
		key := did
		if key == "" {
			key = "manual"
		}
		b, _ := runs(a).Get(key)
		old := object(b)
		if automatic && old != nil {
			result, e = s.Result(ctx, id, profile, did)
			return e
		}
		config, revision, hash, e := s.source(ctx, a, did, expected)
		if e != nil {
			return e
		}
		c, _ := ReadConfiguration(config)
		if !yes(c.IPv6) {
			result = notApplicable(id, profile)
			return nil
		}
		all, e := s.all(ctx)
		if e != nil {
			return e
		}
		if s.reusable(ctx, a, old, automatic, did, all) {
			result, e = s.Result(ctx, id, profile, did)
			return e
		}
		select {
		case verificationSlots <- struct{}{}:
		default:
			return failure("BUSY", 409, msgQueueBusy)
		}
		var once sync.Once
		release := func() { once.Do(func() { <-verificationSlots }) }
		spawned := false
		defer func() {
			if !spawned {
				release()
			}
		}()
		report := s.newRun(ctx, a, revision, did, all)
		reserved := false
		e = s.update(ctx, id, func(ctx context.Context, current Document, all []Document) error {
			if current == nil || len(s.findings(ctx, current, report, all)) > 0 {
				return failure("AGENT_CHANGED", 409, msgVerifyChanged)
			}
			m := runs(current)
			for k, b := range m.All() {
				r := object(b)
				if expired(ctx, r) {
					interrupt(ctx, r, msgInterrupted)
					m.Set(k, jsonresp.Value(r))
				}
			}
			b, _ := m.Get(key)
			if s.reusable(ctx, current, object(b), automatic, did, all) {
				current["ipv6_verifications"] = jsonresp.Value(m)
				return nil
			}
			for _, b := range m.All() {
				if text(object(b)["status"]) == "running" {
					return failure("BUSY", 409, msgVerifyBusy)
				}
			}
			m.Set(key, jsonresp.Value(report))
			keys := m.Keys()
			for _, k := range keys[:max(0, len(keys)-7)] {
				if k != key {
					m.Delete(k)
				}
			}
			current["ipv6_verifications"] = jsonresp.Value(m)
			reserved = true
			return nil
		})
		if e != nil {
			return e
		}
		if reserved {
			e = lifecycle.Spawn(ctx, "ipv6-verification", func(job context.Context) {
				defer release()
				defer func() {
					if recover() != nil {
						logger.Error("IPv6 verification worker did not complete")
						if job.Err() == nil {
							_ = s.failRun(job, id, key, text(report["run_id"]))
						}
					}
				}()
				select {
				case verificationWorkers <- struct{}{}:
				case <-job.Done():
					return
				}
				defer func() { <-verificationWorkers }()
				held, unlock, e := operations.Lock(job, id, 0)
				if e != nil {
					return
				}
				defer unlock()
				s.execute(held, a, config, settings(a), hash, report, key)
			})
			if e != nil {
				if ctx.Err() == nil {
					_ = s.failRun(ctx, id, key, text(report["run_id"]))
				}
				return e
			}
			spawned = true
		}
		result, e = s.Result(ctx, id, profile, did)
		return e
	})
	return result, e
}
func (s *Service) failRun(ctx context.Context, id, key, runID string) error {
	return s.update(ctx, id, func(ctx context.Context, a Document, _ []Document) error {
		if a == nil {
			return nil
		}
		m := runs(a)
		b, _ := m.Get(key)
		r := object(b)
		if text(r["run_id"]) == runID && text(r["status"]) == "running" {
			interrupt(ctx, r, msgTaskInterrupted)
			m.Set(key, jsonresp.Value(r))
			a["ipv6_verifications"] = jsonresp.Value(m)
		}
		return nil
	})
}
func (s *Service) published(ctx context.Context, a, config Document, revision *int64) (string, string) {
	if revision == nil {
		return "unknown", ""
	}
	receipt, e := s.receipt(ctx, a)
	if e != nil || receipt == nil || text(object(a["applied_content"])["deployment_id"]) != receipt.Manifest.DeploymentID {
		return "unknown", ""
	}
	status := s.publication(ctx, a, config, *revision, true)
	if status != "synced" {
		if status == "changed" {
			return status, ""
		}
		return "unknown", ""
	}
	hash := sha(text(a["config_version"]))
	if hash == "" {
		return "unknown", ""
	}
	return "synced", hash
}
func (s *Service) execute(ctx context.Context, a, config Document, saved Settings, hash string, report Document, key string) {
	checks, e := s.executeChecks(ctx, a, config, saved, hash, report)
	if ctx.Err() != nil {
		return
	}
	if e != nil {
		message := msgFailed
		var boundary *Error
		if errors.As(e, &boundary) {
			message = boundary.Message
		}
		checks = append(checks, check("verification_failed", msgVerifyIncomplete, "error", message))
	}
	seeds, seedError := s.credentials(ctx, config)
	if seedError != nil {
		return
	}
	seeds = redact.CollectCredentials(jsonresp.Value(a), seeds)
	_ = jsondecode.Unmarshal(diagnosticProjection(jsonresp.Value(checks), seeds), &checks)
	_ = s.update(ctx, text(a["id"]), func(ctx context.Context, current Document, all []Document) error {
		if current == nil {
			return nil
		}
		m := runs(current)
		b, _ := m.Get(key)
		stored := object(b)
		if text(stored["run_id"]) != text(report["run_id"]) || text(stored["status"]) != "running" {
			return nil
		}
		if changed := s.findings(ctx, current, report, all); len(changed) > 0 {
			checks = []Check{check("target_changed", msgTargetChanged, "unknown", msgInvalidated, changed...)}
		}
		status := "ok"
		required := 0
		for _, c := range checks {
			if c.ID == "client_not_configured" {
				continue
			}
			required++
			if c.Status == "error" {
				status = "error"
			} else if c.Status != "ok" && status != "error" {
				status = "warning"
			}
		}
		if required == 0 {
			status = "warning"
		}
		message := msgHostDone
		if saved.ClientAgentID != "" {
			message = msgClientDone
		}
		stored["status"] = jsonresp.Value(status)
		stored["checks"] = jsonresp.Value(checks)
		stored["checked_at"] = jsonresp.Value(now(ctx))
		stored["message"] = jsonresp.Value(message)
		m.Set(key, jsonresp.Value(stored))
		current["ipv6_verifications"] = jsonresp.Value(m)
		return nil
	})
}
