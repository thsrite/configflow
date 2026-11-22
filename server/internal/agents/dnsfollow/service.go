package dnsfollow

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"

	"configflow/internal/deploy"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/yamlx"
)

// IsUserError identifies refusals Python exposes as a conflict.
func IsUserError(err error) bool {
	var e *Error
	return errors.As(err, &e) || deploy.IsPreparationFailure(err) || errors.Is(err, repo.ErrRejected)
}

func (s *Service) leased(ctx context.Context, fn func(context.Context) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	held, release, ok := followLocks.TryLock(ctx, s.m.Repository().DataDir())
	if !ok {
		if err := ctx.Err(); err != nil {
			return err
		}
		return refusal(msgBusy)
	}
	defer release()
	if err := held.Err(); err != nil {
		return err
	}
	return fn(held)
}

// Options reads selectable upstreams and public binding state without device I/O.
func (s *Service) Options(ctx context.Context, id string) (OptionsView, error) {
	out := OptionsView{Success: true, Sources: []SourceView{}, Upstreams: []UpstreamView{}}
	target, err := s.require(ctx, id, "oxidns")
	if err != nil {
		return out, err
	}
	src, err := s.config(ctx, target)
	if err != nil {
		return out, err
	}
	base := ""
	if target.AppliedContent != nil {
		base = target.AppliedContent.BaseURL
	}
	_, doc, err := document(src, base)
	if err != nil {
		return out, err
	}
	list, err := s.all(ctx)
	if err != nil {
		return out, err
	}
	out.Binding = target.bindingRaw
	out.State = target.stateRaw
	for _, a := range list {
		if a.ServiceType == "mihomo" && enabled(a) {
			out.Sources = append(out.Sources, SourceView{a.ID, sourceName(a), suggestedHost(a.Host)})
		}
	}
	for _, e := range entries(doc) {
		address := text(yamlx.Get(e.entry, "addr"))
		if _, err := scheme(address); err == nil {
			out.Upstreams = append(out.Upstreams, UpstreamView{e.tag, e.index, s.safe(address, list)})
		}
	}
	return s.publicOptions(out, list)
}

// Configure selects a single upstream and records a binding for the next source publication.
func (s *Service) Configure(ctx context.Context, id string, body json.RawMessage) (out OptionsView, err error) {
	err = deploy.RunDetached(ctx, "dns-follow-configure", func(ctx context.Context) error {
		err := s.leased(ctx, func(ctx context.Context) error {
			fields, err := requestBinding(body)
			if err != nil {
				return err
			}
			target, err := s.require(ctx, id, "oxidns")
			if err != nil {
				return err
			}
			if err = mutable(target); err != nil {
				return err
			}
			source, err := s.require(ctx, stringMember(fields, "source_agent_id"), "mihomo")
			if err != nil {
				return err
			}
			binding, err := selection(fields)
			if err != nil {
				return err
			}
			if haEnabled(target) && binding.Connection != "network" {
				return refusal(msgHANetwork)
			}
			if binding.Connection == "auto" {
				if err = s.sameNamespace(ctx, target, source); err != nil {
					return err
				}
			}
			src, err := s.config(ctx, target)
			if err != nil {
				return err
			}
			base := ""
			if target.AppliedContent != nil {
				base = target.AppliedContent.BaseURL
			}
			settings, doc, err := document(src, base)
			if err != nil {
				return err
			}
			entry, err := boundEntry(doc, binding)
			if err != nil {
				return err
			}
			address := text(yamlx.Get(entry, "addr"))
			if _, err = scheme(address); err != nil {
				return err
			}
			if i := pinnedRule(src, settings, address); i != 0 {
				return pinnedError(i)
			}
			identity := make([]byte, 16)
			if _, err = rand.Read(identity); err != nil {
				return err
			}
			binding.ID = hex.EncodeToString(identity)
			binding.SourceProfileID = source.ProfileID
			binding.TargetProfileID = target.ProfileID
			binding.UpstreamSignature, err = signature(entry)
			if err != nil {
				return err
			}
			if source.LatestDeployment != nil && source.LatestDeployment.DeploymentID != "" {
				binding.AfterDeploymentID = ptr(source.LatestDeployment.DeploymentID)
			}
			return s.update(ctx, func(_ context.Context, list []*agent) error {
				current := selected(list, id)
				if current == nil {
					return refusal("oxidns" + msgMissingAgent)
				}
				if err := mutable(current); err != nil {
					return err
				}
				currentSource := selected(list, source.ID)
				if current.ProfileID != target.ProfileID || currentSource == nil || currentSource.ProfileID != binding.SourceProfileID {
					return refusal(msgConfigureChanged)
				}
				for _, other := range list {
					if other.ID != id && other.DNSFollow != nil && other.DNSFollow.TargetProfileID == target.ProfileID {
						return refusal(msgProfileTaken)
					}
				}
				current.DNSFollow = binding
				current.DNSFollowJob = nil
				current.dropJob = true
				current.DNSFollowState = newState()
				current.DNSFollowState.Status = "waiting"
				current.DNSFollowState.Message = msgWaiting
				current.DNSFollowState.UpdatedAt = timestamp(s.now())
				return nil
			})
		})
		if err != nil {
			return err
		}
		out, err = s.Options(ctx, id)
		return err
	})
	return
}

// Disable removes a binding only after its accepted publication has ended.
func (s *Service) Disable(ctx context.Context, id string) (out DisabledView, err error) {
	err = deploy.RunDetached(ctx, "dns-follow-disable", func(ctx context.Context) error {
		return s.leased(ctx, func(ctx context.Context) error {
			if _, err := s.require(ctx, id, "oxidns"); err != nil {
				return err
			}
			return s.update(ctx, func(_ context.Context, list []*agent) error {
				a := selected(list, id)
				if a == nil {
					return refusal("oxidns" + msgMissingAgent)
				}
				// Disabling is the explicit repair of a damaged state: only a
				// real run (and an active HA pair) refuses it.
				if activeHA(a) {
					return refusal(msgHAActive)
				}
				if running(state(a).Status) {
					return refusal(msgRunning)
				}
				a.DNSFollow = nil
				a.DNSFollowState = nil
				a.DNSFollowJob = nil
				a.dropBinding = true
				a.dropState = true
				a.dropJob = true
				return nil
			})
		})
	})
	out.Success = err == nil
	return
}

// Retry durably schedules one new attempt after a confirmed failure.
func (s *Service) Retry(ctx context.Context, id string) (out RetryView, err error) {
	err = deploy.RunDetached(ctx, "dns-follow-retry", func(ctx context.Context) error {
		return s.leased(ctx, func(ctx context.Context) error {
			target, e := s.require(ctx, id, "oxidns")
			if e != nil {
				return e
			}
			previous := state(target)
			if target.DNSFollow == nil || previous.Status != "failed" {
				return refusal(msgRetry)
			}
			if child, ok := target.Deployments.Get(value(previous.TargetDeploymentID)); ok && !terminal(child.Status) {
				return refusal(msgUnconfirmed)
			}
			e = s.update(ctx, func(_ context.Context, list []*agent) error {
				a := selected(list, id)
				if a == nil {
					return refusal("oxidns" + msgMissingAgent)
				}
				a.DNSFollowJob = nil
				a.dropJob = true
				a.DNSFollowState = newState()
				// Preserve source_deployment_id:null for pre-publication failures.
				if err := json.Unmarshal([]byte(`{"events":[],"source_deployment_id":null}`), a.DNSFollowState); err != nil {
					return err
				}
				a.DNSFollowState.Status = "waiting"
				a.DNSFollowState.SourceDeploymentID = previous.SourceDeploymentID
				a.DNSFollowState.RetryRequested = ptr(true)
				a.DNSFollowState.Attempt = ptr(value(previous.Attempt) + 1)
				a.DNSFollowState.UpdatedAt = timestamp(s.now())
				return nil
			})
			if e != nil {
				return e
			}
			out.State, _, e = s.saveState(ctx, id, "waiting", msgRetryQueued, statePatch{}, nil)
			out.Success = e == nil
			return e
		})
	})
	return
}

func newState() *model.DNSFollowState {
	var st model.DNSFollowState
	_ = json.Unmarshal([]byte(`{"events":[]}`), &st)
	return &st
}

func applyPatch(st model.DNSFollowState, p statePatch) (model.DNSFollowState, error) {
	b, err := json.Marshal(st)
	if err != nil {
		return st, err
	}
	var f model.Fields
	if err = json.Unmarshal(b, &f); err != nil {
		return st, err
	}
	set := func(key string, v any) error {
		b, e := json.Marshal(v)
		if e != nil {
			return e
		}
		return f.Set(key, b)
	}
	if p.sourceID != nil {
		if err = set("source_deployment_id", p.sourceID); err != nil {
			return st, err
		}
	}
	if p.targetID != nil || p.clearTarget {
		if err = set("target_deployment_id", p.targetID); err != nil {
			return st, err
		}
	}
	if p.attempt != nil {
		if err = set("attempt", p.attempt); err != nil {
			return st, err
		}
	}
	if p.retry != nil {
		if err = set("retry_requested", p.retry); err != nil {
			return st, err
		}
	}
	if p.address != nil {
		if err = set("address", p.address); err != nil {
			return st, err
		}
	}
	if p.draftSynced != nil {
		if err = set("draft_synced", p.draftSynced); err != nil {
			return st, err
		}
	}
	b, err = json.Marshal(f)
	if err == nil {
		err = json.Unmarshal(b, &st)
	}
	return st, err
}
func (s *Service) saveState(ctx context.Context, id, status, message string, p statePatch, before func(context.Context, []*agent) error) (out *model.DNSFollowState, written bool, err error) {
	var prior model.DNSFollowState
	var target *agent
	var logAgents []*agent
	err = s.update(ctx, func(tx context.Context, list []*agent) error {
		if before != nil {
			if e := before(tx, list); e != nil {
				return e
			}
		}
		target = selected(list, id)
		if target == nil {
			return refusal("oxidns" + msgMissingAgent)
		}
		logAgents = list
		prior = state(target)
		next, e := applyPatch(prior, p)
		if e != nil {
			return e
		}
		message = s.safe(message, list)
		events, e := s.appendEvents(prior.Events, status, message, timestamp(s.now()), list)
		if e != nil {
			return e
		}
		next.Status = status
		next.Message = message
		next.Events = events
		// Keep original updated_at during equality: time alone never commits.
		if before == nil && equalJSON(next, prior) {
			copy := prior
			out = &copy
			return errUnchanged
		}
		next.UpdatedAt = timestamp(s.now())
		target.DNSFollowState = &next
		out = &next
		written = true
		return nil
	})
	if errors.Is(err, errUnchanged) {
		return out, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if (status == "succeeded" || status == "failed") && prior.Status != status {
		if e := s.recordOutcome(ctx, id, status, out.Message); e != nil {
			return out, true, e
		}
	}
	if e := ctx.Err(); e != nil {
		return out, true, e
	}
	s.logState(target, prior, *out, logAgents)
	return out, written, nil
}
