package dnsha

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"configflow/internal/agents"
	"configflow/internal/agents/rulesync"
	"configflow/internal/deploy"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
)

var services = struct {
	sync.Mutex
	byManager map[*agents.Manager]*Service
}{byManager: map[*agents.Manager]*Service{}}

// ForManager shares the owner between HTTP hooks and boot reconciliation.
func ForManager(m *agents.Manager) *Service {
	services.Lock()
	defer services.Unlock()
	if s := services.byManager[m]; s != nil {
		return s
	}
	s := New(m, Options{})
	if m.OnClose(func() { services.Lock(); delete(services.byManager, m); services.Unlock() }) {
		services.byManager[m] = s
	}
	return s
}

// Register supplies HA hooks and the 0/2-second boot loop.
func Register() {
	installHooks()
	agents.OnManagerInit(func(_ context.Context, m *agents.Manager) { Wire(m) })
	rulesync.SetPairAdvance(func(ctx context.Context, m *agents.Manager, id string) { _, _ = ForManager(m).Advance(ctx, id) })
	lifecycle.RegisterPeriodic(lifecycle.Periodic{Name: "oxidns-ha-publish", Start: lifecycle.AtBoot, Interval: func(context.Context) time.Duration { return PollInterval }, Round: func(ctx context.Context) error {
		m, e := agents.Get(ctx)
		if e != nil {
			return e
		}
		return ForManager(m).Reconcile(ctx)
	}})
}

// Publish reserves a pair, leaving single-device publications to deploy.
func (s *Service) Publish(ctx context.Context, id string, b *deploy.Bundle, o deploy.PublishOptions) (out *agents.Result, handled bool, err error) {
	err = deploy.RunDetached(ctx, "oxidns-ha-publish", func(ctx context.Context) error {
		members, e := s.pair(ctx, id)
		if e != nil {
			return e
		}
		if len(members) == 0 {
			return nil
		}
		handled = true
		if b == nil {
			return refuse(msgPayloadDigest)
		}
		a, e := s.get(ctx, id)
		if e != nil {
			return e
		}
		previous := a.OxiDNSHAPublication
		did := b.Manifest().DeploymentID
		if previous != nil {
			if old, _ := journalChildID(previous, id); old == did {
				_, original, e := s.readPayload(ctx, previous)
				if e != nil {
					return e
				}
				if original == nil {
					return refuse(msgDifferentID)
				}
				of, e := deploy.Fingerprint(original.Manifest())
				if e != nil {
					return e
				}
				nf, e := deploy.Fingerprint(b.Manifest())
				if e != nil {
					return e
				}
				if of != nf {
					return refuse(msgDifferentID)
				}
				if _, e = s.advance(ctx, id); e != nil {
					return e
				}
				out, e = s.deployment(ctx, id, did, false)
				return e
			}
			if previous.Status == "rollback_failed" {
				if _, e = s.advance(ctx, id); e != nil {
					return e
				}
			}
		}
		held, release, ok := s.lease(ctx, []string{members[0].ID, members[1].ID})
		if !ok {
			return refuse(msgPairBusy)
		}
		e = func() error {
			defer release()
			follow := a.DNSFollowState
			if b.Manifest().ReloadOnly || (follow != nil && follow.Status == "publishing" && follow.TargetDeploymentID != nil && *follow.TargetDeploymentID == did) {
				if !sameBaseline(members) {
					return refuse(msgDifferentBaseline)
				}
			}
			j, e := s.reserve(held, members, id, did, o.Activate, nil)
			if e != nil {
				return e
			}
			var source, resource json.RawMessage
			if o.SourceSnapshot != nil {
				source, e = o.SourceSnapshot.Raw()
				if e != nil {
					return e
				}
			}
			if o.ResourceConfig != nil {
				resource, e = o.ResourceConfig.Raw()
				if e != nil {
					return e
				}
			}
			p := &payload{Source: source, Resource: resource, Revision: revisionRaw(o.ConfigRevision), BaseURL: o.BaseURL}
			e = s.writePayload(held, j, p, b)
			if e == nil {
				e = s.beginChildren(held, j)
			}
			if e == nil {
				e = s.save(held, j)
			}
			if e != nil {
				if held.Err() != nil {
					return held.Err()
				}
				if fail := s.fail(held, j, msgPrepareFailed, "failed"); fail != nil {
					return fail
				}
			}
			return e
		}()
		if e != nil {
			return e
		}
		if _, e = s.advance(ctx, id); e != nil {
			return e
		}
		out, e = s.deployment(ctx, id, did, false)
		return e
	})
	return
}
func sameBaseline(members []*agent) bool {
	fingerprint := ""
	for _, a := range members {
		if a.AppliedContent == nil || a.AppliedContent.Fingerprint == "" {
			return false
		}
		if fingerprint != "" && fingerprint != a.AppliedContent.Fingerprint {
			return false
		}
		fingerprint = a.AppliedContent.Fingerprint
	}
	return len(members) == 2
}

// Deployment aggregates the authoritative pair journal while retaining child details.
func (s *Service) Deployment(ctx context.Context, id, did string) (out *agents.Result, err error) {
	err = deploy.RunDetached(ctx, "oxidns-ha-deployment", func(ctx context.Context) error { var e error; out, e = s.deployment(ctx, id, did, true); return e })
	return
}
func (s *Service) deployment(ctx context.Context, id, did string, reconcile bool) (*agents.Result, error) {
	a, e := s.get(ctx, id)
	if e != nil {
		return nil, e
	}
	var j *model.HAJournal
	if a != nil {
		j = a.OxiDNSHAPublication
	}
	child := ""
	if j != nil {
		child, _ = journalChildID(j, id)
	}
	if child == "" || child != did {
		r, e := s.m.GetDeploymentDirect(ctx, id, did)
		if e != nil {
			return nil, e
		}
		d, e := resultModel(r)
		if e != nil {
			return nil, e
		}
		if d.HAPublication != nil {
			return aggregate(&d, d.HAPublication), nil
		}
		return r, nil
	}
	if reconcile {
		if _, e = s.advance(ctx, id); e != nil {
			return nil, e
		}
		a, e = s.get(ctx, id)
		if e != nil {
			return nil, e
		}
		if a == nil {
			return nil, refuse(msgJournalChanged)
		}
		j = a.OxiDNSHAPublication
	}
	d, ok := a.Deployments.Get(did)
	if !ok {
		_ = json.Unmarshal(rawObject(struct {
			ID string `json:"deployment_id"`
		}{did}), &d)
	}
	return aggregate(&d, summary(j)), nil
}

// Activate stages neither member twice and requires current VIP ownership proofs.
func (s *Service) Activate(ctx context.Context, id, did string) (out *agents.Result, err error) {
	err = deploy.RunDetached(ctx, "oxidns-ha-activate", func(ctx context.Context) error {
		a, e := s.get(ctx, id)
		if e != nil {
			return e
		}
		var j *model.HAJournal
		if a != nil {
			j = a.OxiDNSHAPublication
		}
		child := ""
		if j != nil {
			child, _ = journalChildID(j, id)
		}
		if child == "" || child != did {
			if r := conflict(a); r != nil {
				out = r
				return nil
			}
			out, e = s.m.ActivateDeploymentDirect(ctx, id, did)
			return e
		}
		held, release, ok := s.lease(ctx, keys(j))
		if !ok {
			out = rejection(msgActivateBusy)
			return nil
		}
		e = func() error {
			defer release()
			a, e = s.get(held, id)
			if e != nil {
				return e
			}
			if a == nil || a.OxiDNSHAPublication == nil || a.OxiDNSHAPublication.ID != j.ID {
				return refuse(msgJournalChanged)
			}
			j = a.OxiDNSHAPublication
			if j.Status == "succeeded" {
				out, e = s.deployment(held, id, did, false)
				return e
			}
			if j.Status != "ready" {
				out = rejection(msgNotReady)
				return nil
			}
			members, e := s.members(held, j)
			if e == nil {
				j.Order, e = s.order(held, j, members)
			}
			if e != nil {
				if IsUserError(e) || errors.Is(e, ErrStatusUnavailable) {
					out = rejection(msgRolesUnknown)
					return nil
				}
				return e
			}
			j.Activate = ptr(true)
			j.Status = "applying"
			j.Message = msgActivating
			return s.save(held, j)
		}()
		if e != nil || out != nil {
			return e
		}
		if _, e = s.advance(ctx, id); e != nil {
			return e
		}
		out, e = s.deployment(ctx, id, did, false)
		return e
	})
	return
}

// ServiceOperation fences restart/update/uninstall against a pair reservation.
func (s *Service) ServiceOperation(ctx context.Context, id string, op func() (*agents.Result, error), uninstall bool) (out *agents.Result, err error) {
	err = deploy.RunDetached(ctx, "oxidns-ha-service-operation", func(ctx context.Context) error {
		held, release, ok := s.lease(ctx, []string{id})
		if !ok {
			out = rejection(msgOperationBusy)
			return nil
		}
		defer release()
		a, e := s.get(held, id)
		if e != nil {
			return e
		}
		if r := conflict(a); r != nil {
			out = r
			return nil
		}
		if uninstall && haEnabled(a) {
			out = rejection(msgUninstall)
			return nil
		}
		if e = held.Err(); e != nil {
			return e
		}
		out, e = op()
		return e
	})
	return
}

// Rules presents pair publications through the native rule-update task envelope.
func (s *Service) Rules(ctx context.Context, id, method string) (out *agents.Result, code int, err error) {
	err = deploy.RunDetached(ctx, "oxidns-ha-rules", func(ctx context.Context) error {
		members, e := s.pair(ctx, id)
		if e != nil {
			return e
		}
		if len(members) == 0 {
			return refuse(msgPairUnavailable)
		}
		a, e := s.get(ctx, id)
		if e != nil {
			return e
		}
		j := a.OxiDNSHAPublication
		isRules := j != nil && j.Kind != nil && *j.Kind == "rules"
		if method == "POST" && !(active(a) && isRules) {
			if active(a) {
				return refuse(msgRulesBusy)
			}
			receipt, e := deploy.NewReceipts(s.m.Repository().DataDir()).Load(ctx, a.target.Record, false)
			if e != nil {
				return e
			}
			if receipt == nil || !sameBaseline(members) {
				return refuse(msgRulesBaseline)
			}
			held, release, ok := s.lease(ctx, []string{members[0].ID, members[1].ID})
			if !ok {
				return refuse(msgPairBusy)
			}
			e = func() error {
				defer release()
				did, e := randomID()
				if e != nil {
					return e
				}
				j, e = s.reserve(held, members, id, "rules-"+did, true, nil)
				if e != nil {
					return e
				}
				j.Kind = ptr("rules")
				p := &payload{Source: receipt.Source, Resource: receipt.Source, BaseURL: receipt.BaseURL, BaselineID: receipt.Manifest.DeploymentID}
				e = s.writePayload(held, j, p, nil)
				if e == nil {
					e = s.beginChildren(held, j)
				}
				if e == nil {
					e = s.save(held, j)
				}
				if e != nil {
					if held.Err() != nil {
						return held.Err()
					}
					if fail := s.fail(held, j, msgRulesPrepare, "failed"); fail != nil {
						return fail
					}
				}
				return e
			}()
			if e != nil {
				return e
			}
			isRules = true
		}
		if !isRules {
			out = rulesResult("", "idle", "idle", nil)
			code = 200
			return nil
		}
		if _, e = s.advance(ctx, id); e != nil {
			return e
		}
		a, e = s.get(ctx, id)
		if e != nil {
			return e
		}
		if a != nil && a.OxiDNSHAPublication != nil {
			j = a.OxiDNSHAPublication
		}
		status, phase := "running", "reloading"
		code = 202
		if j.Status == "succeeded" {
			status, phase, code = "succeeded", "done", 200
		} else if terminal(j.Status) {
			status, phase, code = "failed", "done", 200
		} else if j.Phase == "preparing" {
			phase = "downloading"
		}
		out = rulesResult("ha-"+j.ID, status, phase, j)
		return nil
	})
	return
}
