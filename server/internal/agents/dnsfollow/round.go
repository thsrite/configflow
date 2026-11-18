package dnsfollow

import (
	"context"
	"errors"

	"configflow/internal/deploy"
	"configflow/internal/repo"
)

// Tick reconciles one bounded round; settled stored-only results rest until a
// commit or the shared recheck interval. Accepted work survives browser closure.
func (s *Service) Tick(ctx context.Context) error {
	return deploy.RunDetached(ctx, "dns-follow-round", func(ctx context.Context) error {
		ctx = repo.WithScope(repo.WithRepository(ctx, s.m.Repository()))
		defer repo.ResetConfigContext(ctx)
		head, due, err := s.rounds.Due(ctx, s.m.Repository())
		if err != nil || !due {
			return err
		}
		idle, err := s.round(ctx)
		if err == nil {
			s.rounds.Done(s.m.Repository(), head, idle)
		}
		return err
	})
}
func (s *Service) targetInFlight(ctx context.Context, target *agent) (bool, error) {
	if activeHA(target) {
		return true, nil
	}
	l := latest(target)
	if l.DeploymentID == "" || terminal(l.Status) {
		return false, nil
	}
	child, e := s.poll(ctx, target, l.DeploymentID)
	return !terminal(child.Status), e
}
func (s *Service) round(ctx context.Context) (bool, error) {
	idle := true
	err := s.leased(ctx, func(ctx context.Context) error {
		list, e := s.all(ctx)
		if e != nil {
			return e
		}
		for _, target := range list {
			if target.DNSFollow == nil {
				continue
			}
			if e = ctx.Err(); e != nil {
				return e
			}
			storedOnly := true
			e = func() error {
				st, b := state(target), target.DNSFollow
				if running(st.Status) {
					idle = false
					storedOnly = false
					return s.resume(ctx, target)
				}
				if _, e := listed(list, target.ID, "oxidns"); e != nil {
					return e
				}
				source, e := listed(list, b.SourceAgentID, "mihomo")
				if e != nil {
					return e
				}
				if b.ID == "" || b.SourceAgentID == "" || b.SourceProfileID == "" || b.TargetProfileID == "" || target.ProfileID != b.TargetProfileID || source.ProfileID != b.SourceProfileID {
					return refusal(msgBindingChanged)
				}
				ls := latest(source)
				if ls.DeploymentID != "" && !terminal(ls.Status) {
					idle = false
					storedOnly = false
					ls, e = s.poll(ctx, source, ls.DeploymentID)
					if e != nil {
						return e
					}
				}
				id := ls.DeploymentID
				if id == "" || ls.Status != "succeeded" || value(ls.ManualRollback) || id == value(b.AfterDeploymentID) {
					return nil
				}
				if id == value(st.SourceDeploymentID) && !value(st.RetryRequested) {
					return nil
				}
				idle = false
				storedOnly = false
				busy, e := s.targetInFlight(ctx, target)
				if e != nil {
					return e
				}
				if busy {
					if st.Status == "waiting" && st.Message == WaitingForTarget {
						return nil
					}
					_, _, e = s.saveState(ctx, target.ID, "waiting", WaitingForTarget, statePatch{}, nil)
					return e
				}
				current, e := s.get(ctx, target.ID)
				if e != nil {
					return e
				}
				source, e = s.require(ctx, b.SourceAgentID, "mihomo")
				if e != nil {
					return e
				}
				if latest(source).DeploymentID != id {
					return nil
				}
				return s.run(ctx, current, source, b, ls)
			}()
			if e == nil {
				continue
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			var originalFollow *Error
			storedRefusal := errors.As(e, &originalFollow)
			message := msgUnexpected
			if IsUserError(e) {
				message = e.Error()
			}
			current, readErr := s.get(ctx, target.ID)
			if readErr != nil {
				return readErr
			}
			if current == nil {
				continue
			}
			st := state(current)
			id := value(st.TargetDeploymentID)
			child, found := current.Deployments.Get(id)
			if found && !terminal(child.Status) && value(child.Submitted) {
				idle = false
				_, _, e = s.saveState(ctx, target.ID, "publishing", msgPendingPrefix+message, statePatch{}, nil)
			} else {
				if found && !terminal(child.Status) {
					idle = false
					if e = s.failPreparation(ctx, current, id, message); e != nil {
						return e
					}
				}
				prefix := msgUnavailablePrefix
				if value(st.SourceDeploymentID) != "" {
					prefix = msgFailedPrefix
				}
				var written bool
				_, written, e = s.saveState(ctx, target.ID, "failed", prefix+message, statePatch{}, nil)
				if written || !storedOnly || !storedRefusal {
					idle = false
				}
			}
			if e != nil {
				return e
			}
		}
		return nil
	})
	var busy *Error
	if errors.As(err, &busy) && busy.Message == msgBusy {
		return false, nil
	}
	return idle, err
}
