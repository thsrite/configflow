package installation

import (
	"configflow/internal/agents"
	"configflow/internal/jsonresp"
	"configflow/internal/lifecycle"
	"context"
	"encoding/json"
)

func match(r *Record, a *agent, service string, profile bool) error {
	if a == nil || a.ServiceType != service || a.DeploymentMethod != "shell" || a.Host != r.Network.Address || a.Port != r.Ports[service] || a.Name != r.Name+"-"+service || profile && a.ProfileID != r.ProfileID {
		return failure(msgIdentity, 409)
	}
	return nil
}
func target(r *Record, data *agent, existing []*agent) error {
	if !r.AutoNetwork || r.Network.Address != "" {
		return nil
	}
	hosts := map[string]bool{}
	for _, a := range existing {
		if a.InstallationID == r.ID {
			hosts[a.Host] = true
		}
	}
	if len(hosts) > 1 {
		return failure(msgHostsDiffer, 409)
	}
	h, err := hostIPv4(data.Host)
	if err != nil {
		return err
	}
	if len(hosts) > 0 && !hosts[h] {
		return failure(msgHostPinned, 409)
	}
	r.Network.Address = h
	return nil
}
func (s *Service) registrationProfile(ctx context.Context, id, token string, data json.RawMessage, fallback string) (*Record, *agent, error) {
	r, err := s.read(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if err = s.authorize(ctx, r, token); err != nil {
		return nil, nil, err
	}
	// Python reads registration members with dict.get: an unrelated or wrongly
	// typed member never turns the service check into an identity refusal.
	a := agentFields(object(data))
	if a.ServiceType != "mihomo" && a.ServiceType != "oxidns" {
		return nil, nil, failure(msgService, 403)
	}
	if r.AutoNetwork && a.Host == "" {
		a.Host, err = hostIPv4(fallback)
		if err != nil {
			return nil, nil, err
		}
	}
	if err = target(r, &a, nil); err != nil {
		return nil, nil, err
	}
	if err = match(r, &a, a.ServiceType, false); err != nil {
		return nil, nil, err
	}
	return r, &a, nil
}

// RegistrationProfile validates the capability before the global registration
// key can be bypassed. Only an empty auto host uses the direct peer fallback.
func (s *Service) RegistrationProfile(ctx context.Context, id, token string, data json.RawMessage, fallback string) (string, json.RawMessage, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return "", nil, err
	}
	r, a, err := s.registrationProfile(ctx, id, token, data, fallback)
	if err != nil {
		return "", nil, err
	}
	o := object(data)
	o["host"] = jsonresp.Value(a.Host)
	o["profile_id"] = jsonresp.Value(r.ProfileID)
	return r.ProfileID, jsonresp.Value(o), nil
}

// Register records the exact capability marker atomically with the Agent and
// writes a durable registration receipt. A foreign capability cannot replay tokens.
func (s *Service) Register(ctx context.Context, id, token string, data json.RawMessage, existingToken string) (json.RawMessage, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var result json.RawMessage
	err := s.locked(ctx, id, func(ctx context.Context) error {
		r, a, err := s.registrationProfile(ctx, id, token, data, "")
		if err != nil {
			return err
		}
		list, err := s.allAgents(ctx)
		if err != nil {
			return err
		}
		if err = target(r, a, list); err != nil {
			return err
		}
		// registrationProfile used a temporary target for validation; pin against all
		// durable ownership markers again, including a crash before the session write.
		if r.AutoNetwork {
			stored, e := s.read(ctx, id)
			if e != nil {
				return e
			}
			if stored.Network.Address == "" {
				if e = target(stored, a, list); e != nil {
					return e
				}
				r.Network = stored.Network
			}
		}
		var existing *agent
		for _, v := range list {
			if v.Name == a.Name && v.Host == a.Host {
				existing = v
				break
			}
		}
		owned := existing != nil && existing.InstallationID == id
		if owned {
			if err = match(r, existing, a.ServiceType, true); err != nil {
				return err
			}
			if bound := r.AgentIDs[a.ServiceType]; bound != "" && bound != existing.ID {
				return failure(msgBound, 409)
			}
		} else if bound := r.AgentIDs[a.ServiceType]; bound != "" && (existing == nil || existing.ID != bound) {
			return failure(msgBoundChanged, 409)
		}
		o := object(data)
		o["profile_id"] = jsonresp.Value(r.ProfileID)

		input, err := agents.DecodeRegistrationInput(jsonresp.Value(o))
		if err != nil {
			return err
		}
		answer, err := s.Manager.RegisterIdentity(ctx, input, agents.RegistrationOptions{ExistingToken: existingToken, InstallationID: id, ReplayInstallation: true})
		if err != nil {
			return err
		}
		result, err = json.Marshal(answer)
		if err != nil {
			return err
		}

		r.Registered[a.ServiceType] = text(object(result)["id"])
		return s.write(ctx, r)
	})
	return result, err
}

// Enroll validates both explicit proofs in one repository transaction before
// rebinding either core. The session then records the exact two identities.
func (s *Service) Enroll(ctx context.Context, id, token string, body json.RawMessage) (json.RawMessage, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	o := object(body)
	proofs := object(o["agents"])
	if len(o) != 1 || o["agents"] == nil || len(proofs) != 2 || proofs["mihomo"] == nil || proofs["oxidns"] == nil {
		return nil, failure(msgEnroll, 400)
	}
	err := s.locked(ctx, id, func(ctx context.Context) error {
		r, err := s.read(ctx, id)
		if err != nil {
			return err
		}
		if err = s.authorize(ctx, r, token); err != nil {
			return err
		}
		if r.Network.Address == "" {
			return failure(msgAwaitRegistration, 409)
		}
		ids, err := s.bind(ctx, r, proofs)
		if err != nil {
			return err
		}
		r.AgentIDs = ids
		if r.Phase == "waiting" {
			r.Phase = "enrolled"
			r.Message = msgEnrolled
			logger.Infof(msgEnrolledLog, s.safeLabel(ctx, r))
		}
		return s.write(ctx, r)
	})
	if err != nil {
		return nil, err
	}
	return jsonresp.Value(struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}{true, msgEnrolledOK}), nil
}
