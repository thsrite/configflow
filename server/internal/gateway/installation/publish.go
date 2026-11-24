package installation

import (
	"configflow/internal/jsonresp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"configflow/internal/deploy"
	"configflow/internal/lifecycle"
	"configflow/internal/provider"
)

// Publish records immutable task IDs before spawning the single tracked worker.
// A contending request observes the same state instead of starting another job.
func (s *Service) Publish(ctx context.Context, id string, body json.RawMessage) (json.RawMessage, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	o := object(body)
	retry, ok := boolMember(o, "retry_failed")
	if !only(o, "retry_failed") || !ok {
		return nil, failure(msgPublishBody, 400)
	}
	path, err := s.path(id)
	if err != nil {
		return nil, err
	}
	ctx, release, ok := publications.TryLock(ctx, path)
	if !ok {
		return s.Get(ctx, id)
	}
	transferred := false
	defer func() {
		if !transferred {
			release()
		}
	}()
	var captured *Record
	err = s.locked(ctx, id, func(ctx context.Context) error {
		r, err := s.read(ctx, id)
		if err != nil {
			return err
		}
		if _, err = s.pair(ctx, r); err != nil {
			return err
		}
		if err = s.resolveNetwork(ctx, r); err != nil {
			return err
		}
		captured = r
		if r.Phase == "published" {
			return nil
		}
		failed := false
		for _, d := range r.Deployments {
			if terminal(taskOf(d).Status) {
				failed = true
			}
		}
		if failed && !retry {
			return failure(msgRetry, 409)
		}
		if failed || len(r.Deployments) == 0 {
			for _, service := range services {
				if len(r.Deployments[service]) == 0 || terminal(taskOf(r.Deployments[service]).Status) {
					did, e := random(16, true)
					if e != nil {
						return e
					}
					r.Deployments[service] = jsonresp.Value(map[string]string{"deployment_id": did, "status": "preparing"})
				}
			}
		}
		r.Phase = "publishing"
		r.Remedy = jsonresp.Value(nil)
		r.Message = msgStart
		return s.write(ctx, r)
	})
	if err != nil {
		return nil, err
	}
	if captured.Phase == "published" {
		return s.public(ctx, captured)
	}
	// The worker inherits the L1 lease annotation; the session L2 acquisition
	// follows it. Only the release closure mutates lease ownership.
	spawner := s.Spawner
	if spawner == nil {
		spawner = lifecycle.DefaultSpawner{}
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	err = spawner.Spawn(ctx, "gateway-onboarding-"+id[:8], func(job context.Context) {
		defer release()
		if err := s.RunPublication(job, id); err != nil && job.Err() == nil {
			if record, e := s.read(job, id); e == nil {
				logger.Warningf(msgFailedLog, s.safeLabel(job, record), truncate(record.Message, 300))
			}
		}
	})
	if err != nil {
		return nil, err
	}
	transferred = true
	logger.Infof(msgStartLog, s.safeLabel(ctx, captured))
	return s.public(ctx, captured)
}
func (s *Service) progress(ctx context.Context, id, service string, result json.RawMessage) error {
	return s.locked(ctx, id, func(ctx context.Context) error {
		r, err := s.read(ctx, id)
		if err != nil {
			return err
		}
		fields := object(r.Deployments[service])
		if fields == nil {
			return failure(msgCorrupt, 409)
		}
		incoming := object(result)
		for _, key := range []string{"deployment_id", "status", "message", "error", "rollback_error", "success", "pending"} {
			if value, ok := incoming[key]; ok {
				fields[key] = value
			}
		}
		r.Deployments[service] = jsonresp.Value(fields)
		t := taskOf(result)
		message := t.Message
		if message == "" {
			message = t.Status
		}
		if message == "" {
			message = msgAwaitDevice
		}
		r.Message = fmt.Sprintf(msgProgress, service, message)
		// Sanitize before persistence too, using every Agent's credentials, including
		// values in discarded metadata and URL-encoded sibling references.
		r.Message, err = s.safeText(ctx, r.Message)
		if err != nil {
			return err
		}
		r.Deployments[service], err = s.safeMetadata(ctx, r.Deployments[service])
		if err != nil {
			return err
		}
		return s.write(ctx, r)
	})
}
func (s *Service) publishService(ctx context.Context, r *Record, service string) (answer json.RawMessage, err error) {
	pair, err := s.pair(ctx, r)
	if err != nil {
		return nil, err
	}
	a := pair[service]
	did := taskOf(r.Deployments[service]).DeploymentID
	reservation, err := s.Manager.ReserveDeployment(ctx, a.ID, did)
	if err != nil {
		return nil, err
	}
	b, err := encodeResult(reservation)
	if err != nil {
		return nil, err
	}
	reserved := taskOf(b)
	if reserved.Existing {
		return s.deployment(ctx, a.ID, did, false)
	}
	if !reserved.Success {
		message := reserved.Message
		if message == "" {
			message = msgDeviceBusy
		}
		return nil, failure(message, 409)
	}
	defer s.Manager.FinishDeploymentPreparation(a.ID, did)
	defer func() {
		if err != nil && ctx.Err() == nil {
			task, e := s.recordFailed(ctx, a.ID, did)
			if e == nil {
				e = s.progress(ctx, r.ID, service, task)
			}
			if e != nil {
				err = errors.Join(err, e)
			}
		}
	}()
	source, _, err := s.savedSnapshot(ctx, r)
	if err != nil {
		return nil, err
	}
	target, err := deploy.NewTypedTarget(a.record)
	if err != nil {
		return nil, err
	}
	prepared, err := deploy.PrepareContent(ctx, s.Manager, target, source, deploy.PrepareOptions{BaseURL: r.ServerURL, DeploymentID: did, StoreDownloads: true})
	if err != nil {
		return nil, err
	}
	if _, err = s.pair(ctx, r); err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if len(prepared.Providers) > 0 {
		pv := provider.New(provider.Options{Repo: s.Repository})
		if err = pv.CommitArtifacts(ctx, r.ProfileID, prepared.Providers); err != nil {
			return nil, err
		}
	}
	profile, kind := r.ProfileID, service
	result, err := deploy.Push(ctx, s.Manager, a.ID, deploy.PushInput{ConfigContent: prepared.ConfigContent, Bundle: prepared.Bundle, Publish: deploy.PublishOptions{Activate: true, ConfigRevision: &deploy.Revision{Value: r.ConfigRevision}, BaseURL: r.ServerURL, ResourceConfig: source, SourceSnapshot: source}, ExpectedProfileID: &profile, ExpectedServiceType: &kind})
	if err != nil {
		return nil, err
	}
	return encodeResult(result)
}

// RunPublication resumes exactly the durable IDs in Mihomo/OxiDNS order. Shutdown
// cancellation leaves the resumable checkpoint intact instead of inventing a retry.
func (s *Service) RunPublication(ctx context.Context, id string) (err error) {
	ctx = lifecycle.Detach(ctx)
	if err = ctx.Err(); err != nil {
		return err
	}
	defer func() {
		if err == nil || ctx.Err() != nil {
			return
		}
		original := err
		e := s.locked(ctx, id, func(ctx context.Context) error {
			r, e := s.read(ctx, id)
			if e != nil {
				return e
			}
			var prep *deploy.PreparationError
			_ = errors.As(original, &prep)
			message, e := s.safeText(ctx, failureMessage(original))
			if e != nil {
				return e
			}
			r.Phase = "publish_failed"
			r.Message = truncate(message, 1200)
			r.Remedy = jsonresp.Value(nil)
			if prep != nil && prep.Remedy != nil {
				r.Remedy = jsonresp.Value(prep.Remedy)
			}
			return s.write(ctx, r)
		})
		if e != nil {
			err = errors.Join(original, e)
		}
	}()
	r, err := s.read(ctx, id)
	if err != nil {
		return err
	}
	for _, service := range services {
		if _, err = s.pair(ctx, r); err != nil {
			return err
		}
		reply, e := s.publishService(ctx, r, service)
		if e != nil {
			return e
		}
		deadline := s.now(ctx).Add(300 * time.Second)
		for {
			if err = ctx.Err(); err != nil {
				return err
			}
			if err = s.progress(ctx, id, service, reply); err != nil {
				return err
			}
			t := taskOf(reply)
			if t.Status == "succeeded" && t.HTTPStatus == 0 {
				break
			}
			if terminal(t.Status) {
				message := t.Error
				if message == "" {
					message = t.Message
				}
				if message == "" {
					message = t.Status
				}
				return failure(fmt.Sprintf(msgServiceFailed, service, message), 409)
			}
			if (t.HTTPStatus == 400 || t.HTTPStatus == 401 || t.HTTPStatus == 403 || t.HTTPStatus == 404 || t.HTTPStatus == 409 || t.HTTPStatus == 422) && !t.Pending {
				message := t.Message
				if message == "" {
					message = msgRejected
				}
				return failure(message, 409)
			}
			if !s.now(ctx).Before(deadline) {
				return failure(msgTimeout, 409)
			}
			if t.Status == "ready" && t.HTTPStatus == 0 {
				if _, err = s.pair(ctx, r); err != nil {
					return err
				}
				reply, err = s.deployment(ctx, r.AgentIDs[service], t.DeploymentID, true)
				if err != nil {
					return err
				}
				if taskOf(reply).Status == "ready" {
					if err = pause(ctx); err != nil {
						return err
					}
				}
				continue
			}
			if err = pause(ctx); err != nil {
				return err
			}
			if _, err = s.pair(ctx, r); err != nil {
				return err
			}
			reply, err = s.deployment(ctx, r.AgentIDs[service], taskOf(r.Deployments[service]).DeploymentID, false)
			if err != nil {
				return err
			}
		}
	}
	if _, err = s.pair(ctx, r); err != nil {
		return err
	}
	return s.locked(ctx, id, func(ctx context.Context) error {
		final, e := s.read(ctx, id)
		if e != nil {
			return e
		}
		final.Phase = "published"
		final.Message = msgPublished
		if final.Takeover == "full_tun" {
			final.Message = msgPublishedFull
		}

		if e = s.write(ctx, final); e != nil {
			return e
		}
		logger.Infof(msgPublishedLog, s.safeLabel(ctx, final))
		return nil
	})
}
func pause(ctx context.Context) error {
	timer := lifecycle.ClockFrom(ctx).NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C():
		return nil
	}
}
func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) > max {
		return string(r[:max])
	}
	return s
}

// failureMessage is Python's str(exc) for InstallationError and every other
// ValueError (deployment preparation, strategy/base-settings/remote-rule and
// profile validation, safety refusals), otherwise the generic resumable text.
func failureMessage(err error) string {
	var public *Error
	if errors.As(err, &public) || deploy.IsPreparationFailure(err) {
		return err.Error()
	}
	return msgPublishFailed
}
