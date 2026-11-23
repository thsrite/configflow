package ipv6

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"configflow/internal/agents"
	"configflow/internal/deploy"
	"configflow/internal/deploy/contentsync"
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
		task["ipv6_verification_requested"] = raw(true)
		tasks.Set(did, raw(task))
		a["deployments"] = raw(tasks)
		if text(object(a["latest_deployment"])["deployment_id"]) == did {
			a["latest_deployment"] = raw(task)
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
	report["status"] = raw("error")
	report["checks"] = raw([]Check{check("verification_unavailable", msgUnavailable, "error", boundary.Message)})
	report["checked_at"] = raw(now(ctx))
	report["error_code"] = raw(boundary.Code)
	report["message"] = raw(boundary.Message)
	return s.update(ctx, text(a["id"]), func(ctx context.Context, current Document, all []Document) error {
		latest := object(current["latest_deployment"])
		if current == nil || len(s.findings(ctx, current, report, all)) > 0 || text(latest["deployment_id"]) != did || text(latest["status"]) != "succeeded" || yes(latest["manual_rollback"]) {
			return nil
		}
		m := runs(current)
		if _, ok := m.Get(did); !ok {
			m.Set(did, raw(report))
			current["ipv6_verifications"] = raw(m)
		}
		return nil
	})
}

// Tick performs one scheduler round. Bad records do not stop healthy peers.
// IdleRounds belongs to the caller, so a new Manager cannot inherit old state.
func (s *Service) Tick(ctx context.Context) (bool, error) {
	if e := ctx.Err(); e != nil {
		return false, e
	}
	all, e := s.all(ctx)
	if e != nil {
		return false, e
	}
	idle := true
	for _, a := range all {
		needsExpiry := false
		for _, b := range runs(a).All() {
			r := object(b)
			if text(r["status"]) == "running" {
				idle = false
			}
			needsExpiry = needsExpiry || expired(ctx, r)
		}
		if needsExpiry {
			if e = s.update(ctx, text(a["id"]), func(ctx context.Context, current Document, _ []Document) error {
				if current == nil {
					return nil
				}
				m := runs(current)
				for key, b := range m.All() {
					r := object(b)
					if expired(ctx, r) {
						interrupt(ctx, r, msgInterrupted)
						m.Set(key, raw(r))
					}
				}
				current["ipv6_verifications"] = raw(m)
				return nil
			}); e != nil {
				return false, e
			}
		}
	}
	all, e = s.all(ctx)
	if e != nil {
		return false, e
	}
	for _, a := range all {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		if text(a["service_type"]) != "mihomo" || string(a["enabled"]) == "false" {
			continue
		}
		latest := object(a["latest_deployment"])
		did := text(latest["deployment_id"])
		if did == "" || !yes(latest["ipv6_verification_requested"]) {
			continue
		}
		status := text(latest["status"])
		if status != "succeeded" && status != "failed" && status != "rolled_back" && status != "rollback_failed" {
			idle = false
			task, e := s.Manager.GetDeploymentModel(ctx, text(a["id"]), did)
			if e != nil {
				continue
			}
			data, err := model.Marshal(task)
			if err != nil {
				continue
			}
			latest = object(data)
		}
		if text(latest["status"]) != "succeeded" || yes(latest["manual_rollback"]) {
			continue
		}
		if _, ok := runs(a).Get(did); ok {
			continue
		}
		idle = false
		_, e = s.Start(ctx, text(a["id"]), text(a["profile_id"]), raw(Document{"deployment_id": raw(did)}), true)
		var boundary *Error
		if errors.As(e, &boundary) {
			switch boundary.Code {
			case "SNAPSHOT_UNAVAILABLE", "CLIENT_UNAVAILABLE", "CLIENT_UPGRADE_REQUIRED", "CONFIG_CHANGED", "INVALID_REQUEST", "UPGRADE_REQUIRED", "AGENT_AUTH_FAILED":
				if e = s.unavailable(ctx, a, did, boundary); e != nil {
					return false, e
				}
			}
		} else if e != nil && ctx.Err() == nil {
			logger.Warning("IPv6 automatic verification could not be scheduled")
		}
	}
	return idle, nil
}

// Wire installs A14 enrollment and the last Manager-init scheduler hook. The
// checker accessor comes from A13 through server composition, never a new cache.
func Wire(checker func(*agents.Manager) *contentsync.Checker) {
	deploy.SetIPv6Enroll(func(ctx context.Context, m *agents.Manager, id, did string, src *deploy.Source) error {
		b, e := src.Raw()
		if e != nil {
			return e
		}
		return (&Service{Manager: m, Checker: checker}).Enroll(ctx, id, did, object(b))
	})

	agents.OnManagerInit(func(ctx context.Context, m *agents.Manager) {
		owner := &schedulerOwner{service: &Service{Manager: m, Checker: checker}, rounds: agents.NewIdleRounds(nil)}
		scheduler.Lock()
		scheduler.owner = owner
		scheduler.Unlock()
		m.OnClose(func() {
			scheduler.Lock()
			if scheduler.owner == owner {
				scheduler.owner = nil
			}
			scheduler.Unlock()
		})
		lifecycle.RegisterPeriodic(lifecycle.Periodic{Name: "ipv6-verification-scheduler", FirstDelay: 0, Interval: func(context.Context) time.Duration { return 5 * time.Second }, Round: schedulerRound})
	})
}

type schedulerOwner struct {
	service *Service
	rounds  *agents.IdleRounds
}

var scheduler struct {
	sync.Mutex
	owner *schedulerOwner
}

func schedulerRound(ctx context.Context) error {
	scheduler.Lock()
	owner := scheduler.owner
	scheduler.Unlock()
	if owner == nil {
		return nil
	}
	head, due, e := owner.rounds.Due(ctx, owner.service.Manager.Repository())
	if e != nil || !due {
		return e
	}
	idle, e := owner.service.Tick(ctx)
	if e == nil {
		owner.rounds.Done(owner.service.Manager.Repository(), head, idle)
	}
	return e
}
