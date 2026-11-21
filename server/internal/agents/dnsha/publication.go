package dnsha

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"configflow/internal/agents"
	"configflow/internal/agents/rulesync"
	"configflow/internal/deploy"
	"configflow/internal/model"
)

func pending(d model.Deployment) bool { return d.Pending != nil && *d.Pending }
func message(d model.Deployment, fallback string) string {
	if d.Message != nil && *d.Message != "" {
		return *d.Message
	}
	if d.Error != nil && *d.Error != "" {
		return *d.Error
	}
	return fallback
}
func (s *Service) order(ctx context.Context, j *model.HAJournal, members []*agent) ([]string, error) {
	all := true
	for _, a := range members {
		all = all && haEnabled(a)
	}
	if j.Enable != nil && !all {
		return []string{j.Enable.PeerID, j.OwnerAgentID}, nil
	}
	holders := []string{}
	for _, a := range members {
		r, e := s.runtime(ctx, a)
		if e != nil {
			return nil, e
		}
		if r.VIPPresent {
			holders = append(holders, a.ID)
		}
	}
	if len(holders) != 1 {
		return nil, refuse(msgUniqueHolder)
	}
	out := []string{}
	for _, a := range members {
		if a.ID != holders[0] {
			out = append(out, a.ID)
		}
	}
	return append(out, holders[0]), nil
}
func (s *Service) containerBoundary(ctx context.Context, members []*agent) error {
	for _, a := range members {
		if a.DeploymentMethod == "docker" {
			return s.requireProtocols(ctx, members)
		}
	}
	return nil
}
func (s *Service) fail(ctx context.Context, j *model.HAJournal, msg, status string) error {
	if !validJournalChildren(j) {
		return refuse(msgJournalChanged)
	}
	if e := ctx.Err(); e != nil {
		return e
	}
	list, e := s.all(ctx)
	if e != nil {
		return e
	}
	for id, did := range j.Children.All() {
		a := selected(list, id)
		if a == nil {
			continue
		}
		d, _ := a.Deployments.Get(did)
		if yes(d.Submitted) && !childTerminal(d.Status) && d.Status != "ready" {
			j.Status = "unknown"
			j.Message = msg + msgPendingResult
			return s.save(ctx, j)
		}
	}
	if j.Handoff != nil {
		j.HandoffFailure = &model.HAHandoffFailure{Message: msg, Status: status}
		if e = s.save(ctx, j); e != nil {
			return e
		}
		e = s.releaseHandoff(ctx, j)
		if e == nil {
			e = s.settleFailure(ctx, j)
		}
		var hp *HandoffPending
		if errors.As(e, &hp) {
			j.Status = "unknown"
			j.Phase = "handoff"
			j.Message = msg + "；" + hp.Message
			return s.save(ctx, j)
		}
		if e != nil {
			return e
		}
	}
	for id, did := range j.Children.All() {
		a := selected(list, id)
		if a != nil {
			if d, ok := a.Deployments.Get(did); ok && !childTerminal(d.Status) && (!yes(d.Submitted) || d.Status == "ready") {
				if e = s.retire(ctx, id, did, fmt.Sprintf(msgNotContinued, msg), !yes(d.Submitted)); e != nil {
					return e
				}
			}
		}
		s.m.FinishDeploymentPreparation(id, did)
	}
	j.Status = status
	j.Message = msg
	return s.save(ctx, j)
}
func (s *Service) step(ctx context.Context, j *model.HAJournal) error {
	if j.HandoffFailure != nil {
		return s.fail(ctx, j, j.HandoffFailure.Message, j.HandoffFailure.Status)
	}
	members, e := s.members(ctx, j)
	if e != nil {
		return e
	}
	p, b, e := s.readPayload(ctx, j)
	if e != nil {
		return e
	}
	owner := selected(members, j.OwnerAgentID)
	if owner == nil {
		return refuse(msgInFlightIdentity)
	}
	if b == nil && j.Kind != nil && *j.Kind == "rules" {
		receipt, e := deploy.NewReceipts(s.m.Repository().DataDir()).LoadAgent(ctx, owner.Agent, true)
		if e != nil {
			return e
		}
		if receipt == nil || receipt.Manifest.DeploymentID != p.BaselineID {
			return refuse(msgRuleBaseline)
		}
		baseline := rulesync.Baseline{Receipt: receipt}
		baseline.Bundle, e = rulesync.FetchSnapshot(ctx, s.m, owner.Agent, baseline)
		if e != nil {
			return e
		}
		source, e := deploy.SourceFromJSON(p.Source, nil)
		if e != nil {
			return e
		}
		b, e = rulesync.PrepareCandidate(ctx, owner.target, source, baseline, rulesync.Settings{Library: true}, j.OwnerDeploymentID, true)
		if e != nil {
			return e
		}
		if e = s.writePayload(ctx, j, p, b); e != nil {
			return e
		}
	}
	if b == nil {
		src, e := deploy.SourceFromJSON(p.Source, nil)
		if e != nil {
			return e
		}
		prepared, e := s.prepare(ctx, s.m, owner.target, src, deploy.PrepareOptions{BaseURL: p.BaseURL, DeploymentID: j.OwnerDeploymentID, PreferCached: true, StoreDownloads: true})
		if e != nil {
			return e
		}
		b = prepared.Bundle
		if b == nil {
			return refuse(msgPayloadDigest)
		}
		if e = s.writePayload(ctx, j, p, b); e != nil {
			return e
		}
	}
	if e = requireListener(b); e != nil {
		return e
	}
	if e = s.prepareHandoff(ctx, j, members, b); e != nil {
		return e
	}
	if len(j.Order) == 0 {
		j.Order, e = s.order(ctx, j, members)
		if e != nil {
			return e
		}
		if e = s.save(ctx, j); e != nil {
			return e
		}
	}
	if len(j.Order) != 2 || j.Order[0] == j.Order[1] {
		return refuse(msgJournalChanged)
	}
	for index, id := range j.Order {
		did, ok := journalChildID(j, id)
		a := selected(members, id)
		if !ok || a == nil {
			return refuse(msgInFlightIdentity)
		}
		st, _ := a.Deployments.Get(did)
		j.ActiveAgentID = ptr(id)
		j.Phase = "standby"
		label := "备机"
		if index == 1 {
			j.Phase = "primary"
			label = "主机"
		}
		if index == 1 && yes(j.Activate) {
			first := j.Order[0]
			fdid, _ := journalChildID(j, first)
			d, e := s.poll(ctx, first, fdid)
			if e != nil {
				return e
			}
			if d.Status != "succeeded" || pending(d) {
				j.Status = "applying"
				if pending(d) {
					j.Status = "unknown"
				}
				j.Message = msgWaitStandby
				return s.save(ctx, j)
			}
			r, e := s.runtime(ctx, selected(members, first))
			if e != nil {
				return e
			}
			if !r.Healthy {
				return s.fail(ctx, j, msgStandbyUnhealthy, "failed")
			}
		}
		var result model.Deployment
		if !yes(st.Submitted) {
			if childTerminal(st.Status) {
				return s.fail(ctx, j, message(st, msgAborted), "failed")
			}
			if e = s.containerBoundary(ctx, members); e != nil {
				return e
			}
			if index == 1 && yes(j.Activate) {
				if e = s.ensureHandoff(ctx, j, members); e != nil {
					return e
				}
			}
			j.ActiveAgentID = ptr(id)
			j.Phase = "standby"
			if index == 1 {
				j.Phase = "primary"
			}
			j.Status = "preparing"
			if yes(j.Activate) {
				j.Status = "applying"
			}
			j.Message = msgPublishingStandby
			if index == 1 {
				j.Message = msgPublishingPrimary
			}
			if yes(j.HandoffRequired) {
				j.Message = fmt.Sprintf(msgPublishingNonHolder, name(a))
				if index == 1 {
					j.Message = fmt.Sprintf(msgStagingHolder, name(a))
				}
			}
			if e = s.save(ctx, j); e != nil {
				return e
			}
			child, e := childBundle(b, id, did)
			if e != nil {
				return e
			}
			o, e := publicationOptions(p)
			if e != nil {
				return e
			}
			o.Activate = yes(j.Activate) && !(index == 1 && yes(j.HandoffRequired))
			if e = ctx.Err(); e != nil {
				return e
			}
			r, e := deploy.PublishDirect(ctx, s.m, id, child, o)
			if e != nil {
				return e
			}
			if r != nil {
				result = *r
			}
			if e != nil {
				return e
			}
		} else {
			result, e = s.poll(ctx, id, did)
			if e != nil {
				return e
			}
		}
		if pending(result) || result.Status == "unknown" {
			j.Status = "unknown"
			j.Message = fmt.Sprintf(msgConfirmResult, label)
			return s.save(ctx, j)
		}
		if result.Status == "failed" || result.Status == "rolled_back" || result.Status == "rollback_failed" {
			status := "failed"
			if result.Status == "rollback_failed" {
				status = "rollback_failed"
			}
			return s.fail(ctx, j, fmt.Sprintf(msgMemberFailed, label, s.safe(ctx, message(result, msgSeeRecord))), status)
		}
		if result.Status == "ready" && yes(j.Activate) {
			if e = s.containerBoundary(ctx, members); e != nil {
				return e
			}
			if index == 1 {
				if e = s.ensureHandoff(ctx, j, members); e != nil {
					return e
				}
			}
			j.ActiveAgentID = ptr(id)
			j.Phase = "standby"
			if index == 1 {
				j.Phase = "primary"
			}
			if index == 1 && yes(j.HandoffRequired) {
				j.Message = fmt.Sprintf(msgActivatingHolder, name(a))
			}
			if e = s.save(ctx, j); e != nil {
				return e
			}
			result, e = s.activateChild(ctx, id, did)
			if e != nil {
				return e
			}
			if result.HTTPStatus != nil && (*result.HTTPStatus == 400 || *result.HTTPStatus == 404 || *result.HTTPStatus == 405) {
				return s.fail(ctx, j, message(result, msgActivateRefused), "failed")
			}
			if result.Status != "succeeded" || pending(result) {
				j.Status = "applying"
				if result.Status == "unknown" {
					j.Status = "unknown"
				}
				j.Message = fmt.Sprintf(msgWaitActivation, label)
				return s.save(ctx, j)
			}
		} else if result.Status != "succeeded" && result.Status != "ready" {
			j.Status = "preparing"
			if yes(j.Activate) {
				j.Status = "applying"
			}
			j.Message = fmt.Sprintf(msgWaitPublish, label)
			return s.save(ctx, j)
		}
		if yes(j.Activate) && result.Status != "succeeded" {
			return s.save(ctx, j)
		}
	}
	if !yes(j.Activate) {
		j.Status = "ready"
		j.Message = msgReady
		j.ActiveAgentID = nil
		return s.save(ctx, j)
	}
	for _, a := range members {
		r, e := s.runtime(ctx, a)
		if e != nil {
			return e
		}
		if !r.Healthy {
			return s.fail(ctx, j, msgUnhealthy, "failed")
		}
	}
	if e = s.finishHandoff(ctx, j, members); e != nil {
		return e
	}
	if j.Enable != nil {
		j.Phase = "keepalived"
		j.Status = "applying"
		j.Message = msgKeepalived
		if e = s.save(ctx, j); e != nil {
			return e
		}
		if e = s.configureKeepalived(ctx, j.OwnerAgentID, j.Enable, j.RuleSyncSettings); e != nil {
			if errors.Is(e, errHALockBusy) {
				return nil
			}
			return e
		}
	}
	j.Status = "succeeded"
	j.Phase = "done"
	j.ActiveAgentID = nil
	j.Message = msgSucceeded
	return s.save(ctx, j)
}
func (s *Service) recovery(ctx context.Context, j *model.HAJournal) error {
	if !validJournalChildren(j) {
		return refuse(msgJournalChanged)
	}
	resolved := true
	for id, did := range j.Children.All() {
		a, e := s.get(ctx, id)
		if e != nil {
			return e
		}
		var d model.Deployment
		if a != nil {
			d, _ = a.Deployments.Get(did)
		}
		// A member recovered from the Agents page is stored as rolling_back
		// (or unknown) until the Agent is asked again; ask for every member
		// that is not settled yet so the recovery outcome reaches the journal.
		if !recoverySettled(d) {
			d, e = s.poll(ctx, id, did)
			if e != nil {
				return e
			}
		}
		if !recoverySettled(d) {
			resolved = false
		}
	}
	if resolved {
		j.Status = "failed"
		j.Message = msgRecovered
	}
	return s.save(ctx, j)
}

// recoverySettled reports whether a member of a rollback_failed pair has
// reached a final local state: rollback_failed, rolling_back, unknown and
// pending replies still need the Agent's answer.
func recoverySettled(d model.Deployment) bool {
	return (d.Status == "succeeded" || d.Status == "failed" || d.Status == "rolled_back") && !pending(d)
}
func skipped(j *model.HAJournal) bool {
	return j == nil || j.Status == "succeeded" || j.Status == "failed" || j.Status == "ready"
}
func (s *Service) advance(ctx context.Context, id string) (*model.HAPublicationSummary, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	a, e := s.get(ctx, id)
	if e != nil {
		return nil, e
	}
	if a == nil {
		return nil, nil
	}
	j := a.OxiDNSHAPublication
	if skipped(j) {
		return summary(j), nil
	}
	held, release, ok := s.lease(ctx, keys(j))
	if !ok {
		return summary(j), ctx.Err()
	}
	defer release()
	a, e = s.get(held, id)
	if e != nil {
		return nil, e
	}
	if a == nil {
		return nil, nil
	}
	j = a.OxiDNSHAPublication
	if skipped(j) {
		return summary(j), nil
	}
	// The state machine below changes the journal in place before saving it;
	// the read returned the registry's shared generation.
	if j, e = ownJournal(j); e != nil {
		return nil, e
	}
	if j.Status == "rollback_failed" {
		e = s.recovery(held, j)
	} else {
		e = s.step(held, j)
	}
	if held.Err() != nil {
		return nil, held.Err()
	}
	// Election waits also stop at the initial shutdown signal, before the
	// Runtime's drain deadline cancels the job context. This is an interrupted
	// owner, not a failed publication; keep its durable intent for restart.
	if errors.Is(e, context.Canceled) && !agents.IsTransportError(e) {
		return nil, e
	}
	if e != nil {
		var hp *HandoffPending
		switch {
		case errors.As(e, &hp):
			j.Status = "checking"
			if hp.Unknown {
				j.Status = "unknown"
			}
			j.Phase = "handoff"
			j.Message = hp.Message
			e = s.save(held, j)
		case agents.IsTransportError(e) || errors.Is(e, ErrStatusUnavailable):
			j.Status = "unknown"
			j.Message = msgConnectionUnknown
			e = s.save(held, j)
		default:
			msg := msgUnexpected
			if IsUserError(e) {
				msg = s.safe(held, e.Error())
			}
			logger.Warningf("OxiDNS pair publication stopped (%T)", e)
			e = s.fail(held, j, msg, "failed")
		}
	}
	return summary(j), e
}

// Advance resumes one journal under pair/member leases and an owner lifetime.
func (s *Service) Advance(ctx context.Context, id string) (out *model.HAPublicationSummary, err error) {
	err = deploy.RunDetached(ctx, "oxidns-ha-advance", func(ctx context.Context) error { var e error; out, e = s.advance(ctx, id); return e })
	return
}

// Reconcile advances each distinct unfinished pair once; idle rounds do no writes.
func (s *Service) Reconcile(ctx context.Context) error {
	head, due, e := s.rounds.Due(ctx, s.m.Repository())
	if e != nil || !due {
		return e
	}
	list, e := s.all(ctx)
	if e != nil {
		return e
	}
	seen := []string{}
	for _, a := range list {
		j := a.OxiDNSHAPublication
		if j == nil || (!active(a) && j.Status != "rollback_failed") || j.Status == "ready" || slices.Contains(seen, j.ID) {
			continue
		}
		seen = append(seen, j.ID)
		s.rounds.Done(s.m.Repository(), head, false)
		if _, e = s.advance(ctx, a.ID); e != nil {
			return e
		}
	}
	s.rounds.Done(s.m.Repository(), head, len(seen) == 0)
	return nil
}
