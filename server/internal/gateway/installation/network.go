package installation

import (
	"configflow/internal/gateway/host"
	"configflow/internal/jsondecode"
	"configflow/internal/jsonresp"
	"configflow/internal/lifecycle"
	"context"
	"encoding/json"
	"net/netip"
)

func (s *Service) pair(ctx context.Context, r *Record) (map[string]*agent, error) {
	revision, err := s.Repository.ConfigRevision(ctx, r.ProfileID)
	if err != nil {
		return nil, err
	}
	if revision != r.ConfigRevision {
		return nil, failure(msgConfigChanged, 409)
	}
	if len(r.AgentIDs) != 2 || r.AgentIDs["mihomo"] == "" || r.AgentIDs["oxidns"] == "" {
		return nil, failure(msgNeedPair, 409)
	}
	out := map[string]*agent{}
	for _, service := range services {
		a, e := s.agent(ctx, r.AgentIDs[service])
		if e != nil {
			return nil, e
		}
		if e = match(r, a, service, true); e != nil {
			return nil, e
		}
		if !a.usable {
			return nil, failure(msgOffline, 409)
		}
		id := taskOf(r.Deployments[service]).DeploymentID
		if _, exists := a.Deployments[id]; exists && a.Latest.DeploymentID != id {
			return nil, failure(msgOtherDeployment, 409)
		}
		out[service] = a
	}
	return out, nil
}
func (s *Service) status(ctx context.Context, r *Record) (host.Reply, error) {
	bridge := host.Bridge{Repository: s.Repository, Manager: s.Manager}
	reply, err := bridge.Call(ctx, r.AgentIDs["mihomo"], "status", "GET", r.ProfileID, nil)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, publicOwnerError(err)
	}
	return reply, nil
}
func statusError(reply host.Reply, fallback string) error {
	if !flag(reply["success"]) || !flag(reply["supported"]) || text(reply["detection_error"]) != "" {
		message := text(reply["detection_error"])
		if message == "" {
			message = text(reply["unsupported_reason"])
		}
		if message == "" {
			message = fallback
		}
		return failure(message, 409)
	}
	return nil
}
func (s *Service) resolveNetwork(ctx context.Context, r *Record) error {
	if !r.AutoNetwork || r.Network.Interface != "" {
		return nil
	}
	if _, err := s.pair(ctx, r); err != nil {
		return err
	}
	reply, err := s.status(ctx, r)
	if err != nil {
		return err
	}
	if err = statusError(reply, msgAutoRead); err != nil {
		return err
	}
	spec, err := host.ParseSpec(object(reply["detection"])["suggested"])
	if err != nil {
		return failure(msgAutoSpec, 409)
	}
	if spec.Address != r.Network.Address {
		return failure(msgAutoAddress, 409)
	}
	if _, err = s.pair(ctx, r); err != nil {
		return err
	}
	candidate := *r
	candidate.Network = spec
	if _, _, err = s.savedSnapshot(ctx, &candidate); err != nil {
		return err
	}
	r.Network = spec
	return nil
}

// UpdateNetwork confirms actual host observations without changing its address,
// route or forwarding state. It only commits the reviewed session parameters.
func (s *Service) UpdateNetwork(ctx context.Context, id string, body json.RawMessage) (json.RawMessage, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	o := object(body)
	automatic := len(o) == 1 && string(o["auto_network"]) == "true"
	if !automatic && (len(o) != 1 || o["network"] == nil) {
		return nil, failure(msgNetworkBody, 400)
	}
	var result json.RawMessage
	err := s.locked(ctx, id, func(ctx context.Context) error {
		r, err := s.read(ctx, id)
		if err != nil {
			return err
		}
		if r.Phase != "enrolled" && r.Phase != "publish_failed" && r.Phase != "published" {
			return failure(msgNetworkPhase, 409)
		}
		if _, err = s.pair(ctx, r); err != nil {
			return err
		}
		reply, err := s.status(ctx, r)
		if err != nil {
			return err
		}
		if err = statusError(reply, msgNetworkRead); err != nil {
			return err
		}
		detection := object(reply["detection"])
		if detection == nil {
			return failure(msgDetection, 409)
		}
		input := o["network"]
		if automatic {
			input = detection["suggested"]
		}
		spec, err := host.ParseSpec(input)
		if err != nil {
			if automatic {
				return failure(msgAutoManual, 409)
			}
			return publicOwnerError(err)
		}
		if spec.Address != r.Network.Address {
			return failure(msgNetworkAddress, 409)
		}
		if !matchesDetection(detection, spec) {
			return failure(msgNetworkActual, 409)
		}
		managed := object(reply["managed"])
		for _, key := range []string{"active", "pending"} {
			if value, ok := managed[key]; ok && string(value) != "true" && string(value) != "false" && string(value) != "null" {
				return failure(msgNetworkActive, 409)
			}
		}
		if (flag(managed["active"]) || flag(managed["pending"])) && spec != r.Network {
			live, e := host.ParseSpec(managed["spec"])
			if e != nil || spec != live {
				return failure(msgNetworkActive, 409)
			}
		}
		if _, err = s.pair(ctx, r); err != nil {
			return err
		}
		candidate := *r
		candidate.Network = spec
		if _, _, err = s.savedSnapshot(ctx, &candidate); err != nil {
			return err
		}
		r.Network = spec
		r.AutoNetwork = automatic
		if err = s.write(ctx, r); err != nil {
			return err
		}
		result, err = s.public(ctx, r)
		return err
	})
	return result, err
}
func matchesDetection(d map[string]json.RawMessage, spec host.Spec) bool {
	var observed struct {
		Interfaces []struct {
			Name      string   `json:"name"`
			Addresses []string `json:"addresses"`
		} `json:"interfaces"`
		Routes []struct {
			Interface string `json:"interface"`
			Gateway   string `json:"gateway"`
		} `json:"default_routes"`
	}
	if jsondecode.Unmarshal(jsonresp.Value(d), &observed) != nil || len(observed.Routes) != 1 || observed.Routes[0].Interface != spec.Interface || observed.Routes[0].Gateway != spec.UpstreamGateway {
		return false
	}
	for _, i := range observed.Interfaces {
		if i.Name == spec.Interface {
			for _, a := range i.Addresses {
				p, e := netip.ParsePrefix(a)
				if e == nil && p.Addr().String() == spec.Address && p.Masked().String() == spec.LANCIDR {
					return true
				}
			}
		}
	}
	return false
}
