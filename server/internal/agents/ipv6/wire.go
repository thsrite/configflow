package ipv6

import (
	"configflow/internal/jsonresp"
	"context"
	"encoding/json"
	"errors"

	"configflow/internal/agents"
	"configflow/internal/lifecycle"
	"configflow/internal/logx"
	"configflow/internal/model"
)

var logger = logx.Get("backend.agents.ipv6_verification")

// Enroll marks only newly prepared publications whose captured source enables
// valid IPv6. Historical publications are never enrolled by the scheduler.
func (s *Service) Enroll(ctx context.Context, id, did string, config Document) error {
	ctx = lifecycle.Detach(ctx)
	if e := ctx.Err(); e != nil {
		return e
	}
	c, _ := ReadConfiguration(config)
	if !yes(c.IPv6) || len(c.Errors) > 0 {
		return nil
	}
	return s.update(ctx, id, func(ctx context.Context, a Document, _ []Document) error {
		if a == nil {
			return nil
		}
		// The history map is trimmed by insertion order (A14 keeps the newest
		// 30); an ordered map keeps that order and unknown tasks unchanged.
		var tasks model.OrderedMap[json.RawMessage]
		if json.Unmarshal(a["deployments"], &tasks) != nil {
			return nil
		}
		b, _ := tasks.Get(did)
		task := object(b)
		if task == nil {
			return nil
		}
		task["ipv6_verification_requested"] = jsonresp.Value(true)
		tasks.Set(did, jsonresp.Value(task))
		a["deployments"] = jsonresp.Value(tasks)
		if text(object(a["latest_deployment"])["deployment_id"]) == did {
			a["latest_deployment"] = jsonresp.Value(task)
		}
		return nil
	})
}
func (s *Service) unavailable(ctx context.Context, a Document, did string, boundary *Error) error {
	all, e := s.all(ctx)
	if e != nil {
		return e
	}
	var revision *int64
	_ = json.Unmarshal(object(a["latest_deployment"])["config_revision"], &revision)
	report := s.newRun(ctx, a, revision, did, all)
	report["status"] = jsonresp.Value("error")
	report["checks"] = jsonresp.Value([]Check{check("verification_unavailable", msgUnavailable, "error", boundary.Message)})
	report["checked_at"] = jsonresp.Value(now(ctx))
	report["error_code"] = jsonresp.Value(boundary.Code)
	report["message"] = jsonresp.Value(boundary.Message)
	return s.update(ctx, text(a["id"]), func(ctx context.Context, current Document, all []Document) error {
		latest := object(current["latest_deployment"])
		if current == nil || len(s.findings(ctx, current, report, all)) > 0 || text(latest["deployment_id"]) != did || text(latest["status"]) != "succeeded" || yes(latest["manual_rollback"]) {
			return nil
		}
		m := runs(current)
		if _, ok := m.Get(did); !ok {
			m.Set(did, jsonresp.Value(report))
			current["ipv6_verifications"] = jsonresp.Value(m)
		}
		return nil
	})
}

// Tick performs one scheduler round. Bad records do not stop healthy peers.
// It reports whether the round found nothing running (idle).
func (s *Service) Tick(ctx context.Context) (bool, error) {
	if e := ctx.Err(); e != nil {
		return false, e
	}
	all, e := s.Manager.ReadAgents(ctx, true, nil)
	if e != nil {
		return false, e
	}
	idle := true
	for i := range all {
		a := &all[i]
		needsExpiry := false
		for _, run := range a.IPv6Verifications.All() {
			if run.Status == "running" {
				idle = false
				needsExpiry = needsExpiry || run.StartedEpoch == 0 || expiredStart(ctx, run.StartedEpoch)
			}
		}
		if needsExpiry {
			if e = s.update(ctx, a.ID, func(ctx context.Context, current Document, _ []Document) error {
				if current == nil {
					return nil
				}
				m := runs(current)
				for key, b := range m.All() {
					r := object(b)
					if expired(ctx, r) {
						interrupt(ctx, r, msgInterrupted)
						m.Set(key, jsonresp.Value(r))
					}
				}
				current["ipv6_verifications"] = jsonresp.Value(m)
				return nil
			}); e != nil {
				return false, e
			}
		}
	}
	all, e = s.Manager.ReadAgents(ctx, true, nil)
	if e != nil {
		return false, e
	}
	for i := range all {
		a := &all[i]
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		if a.ServiceType != "mihomo" || a.Enabled != nil && !*a.Enabled {
			continue
		}
		latest := a.LatestDeployment
		if latest == nil || latest.DeploymentID == "" || latest.IPv6VerificationRequested == nil || !*latest.IPv6VerificationRequested {
			continue
		}
		did := latest.DeploymentID
		status := latest.Status
		if status != "succeeded" && status != "failed" && status != "rolled_back" && status != "rollback_failed" {
			idle = false
			task, e := s.Manager.GetDeploymentModel(ctx, a.ID, did)
			if e != nil {
				continue
			}
			latest = task
		}
		if latest == nil || latest.Status != "succeeded" || latest.ManualRollback != nil && *latest.ManualRollback {
			continue
		}
		if _, ok := a.IPv6Verifications.Get(did); ok {
			continue
		}
		idle = false
		_, e = s.Start(ctx, a.ID, a.ProfileID, jsonresp.Value(Document{"deployment_id": jsonresp.Value(did)}), true)
		var boundary *Error
		if errors.As(e, &boundary) {
			switch boundary.Code {
			case "SNAPSHOT_UNAVAILABLE", "CLIENT_UNAVAILABLE", "CLIENT_UPGRADE_REQUIRED", "CONFIG_CHANGED", "INVALID_REQUEST", "UPGRADE_REQUIRED", "AGENT_AUTH_FAILED":
				if !agents.FreshHeartbeat(a.LastHeartbeat, nil) {
					a.Status = "offline"
				}
				doc, err := agentDocument(a)
				if err != nil {
					return false, err
				}
				if e = s.unavailable(ctx, doc, did, boundary); e != nil {
					return false, e
				}
			}
		} else if e != nil && ctx.Err() == nil {
			logger.Warning("IPv6 automatic verification could not be scheduled")
		}
	}
	return idle, nil
}
