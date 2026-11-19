package server

import (
	"configflow/internal/jsondecode"
	"context"
	"encoding/json"
	"strings"

	"configflow/internal/agents"
	apirules "configflow/internal/api/rules"
	"configflow/internal/gateway/host"
	"configflow/internal/gateway/installation"
	"configflow/internal/gateway/onboarding"
	"configflow/internal/repo"
	"configflow/internal/routers"
	"configflow/internal/setup"
	"configflow/internal/yamlx"
	"errors"
)

// wireSetupContent joins the existing C32 source owner. A21 and A22 install
// their callbacks when those modules are merged; absent owners fail closed.
func wireSetupContent() {
	setup.RegisterExternalOwners(setup.ExternalOwners{ReadRouteContent: func(ctx context.Context, config json.RawMessage, item routers.RuleSet, library *routers.LibraryRule, options setup.RouteReadOptions) (string, error) {
		var raw json.RawMessage
		if library != nil {
			raw = library.Raw
		}
		return apirules.ReadRulesetContent(ctx, config, item.Raw, raw, apirules.ContentOptions{AllowNetwork: options.AllowNetwork, ConnectTimeout: options.ConnectTimeout, ReadTimeout: options.ReadTimeout})
	}})
}

// wireSetupOnboarding joins A21's pure DNS helpers and C36's ownership owner.
// HTTP preparation has no target host addresses, matching Python's empty
// default; A22 supplies discovered addresses to AssertReady at installation.
func wireSetupOnboarding() {
	onboarding.SetAdoptOwnedWrite(setup.AdoptOwnedWrite)
	setup.RegisterExternalOwners(setup.ExternalOwners{DNS: &setup.DNSOwner{
		LocalUpstreams: onboarding.LocalUpstreams,
		PrepareMihomoDNS: func(document *yamlx.Node) (*yamlx.Node, error) {
			return onboarding.PrepareMihomoDNS(document, nil, onboarding.CheckDNSLoop)
		},
		DefaultLocalDNS: onboarding.DefaultLocalDNS,
		DNSTarget:       onboarding.DNSTarget,
		Nameservers:     onboarding.DefaultMihomoNameservers,
	}})
}

// wireSetupInstallation supplies C36 with actual durable publication evidence
// and A20's status transport. All callbacks are read-only and keep cancellation.
func wireSetupInstallation() {
	service := func(ctx context.Context, r *repo.Repository) (*installation.Service, error) {
		m, e := agents.Get(repo.WithRepository(ctx, r))
		if e != nil {
			return nil, e
		}
		return &installation.Service{Repository: r, Manager: m}, nil
	}
	ownerError := func(ctx context.Context, s *installation.Service, e error) error {
		if e == nil {
			return nil
		}
		e = s.PublicError(ctx, e)
		var i *installation.Error
		if errors.As(e, &i) {
			return &setup.Error{Message: i.Message, Status: i.Status, Cause: e}
		}
		return e
	}
	setup.RegisterExternalOwners(setup.ExternalOwners{Installation: &setup.InstallationOwner{
		Latest: func(ctx context.Context, r *repo.Repository, profile string) (*setup.InstallationState, error) {
			s, e := service(ctx, r)
			if e != nil {
				return nil, e
			}
			b, e := s.Latest(ctx, profile)
			if e != nil {
				return nil, ownerError(ctx, s, e)
			}
			var response struct {
				Session json.RawMessage `json:"session"`
			}
			if e = jsondecode.Unmarshal(b, &response); e != nil {
				return nil, e
			}
			return installationState(response.Session)
		},
		HasInstallation: func(ctx context.Context, r *repo.Repository, profile string) (bool, error) {
			s := &installation.Service{Repository: r}
			v, e := s.HasInstallation(ctx, profile)
			return v, ownerError(ctx, s, e)
		},
		GatewayStatus: func(ctx context.Context, r *repo.Repository, id, profile string) (setup.GatewayStatus, error) {
			s, e := service(ctx, r)
			if e != nil {
				return setup.GatewayStatus{}, e
			}
			bridge := host.Bridge{Repository: r, Manager: s.Manager}
			reply, e := bridge.Call(ctx, id, "status", "GET", profile, nil)
			if ctx.Err() != nil {
				return setup.GatewayStatus{}, ctx.Err()
			}
			if e != nil {
				return setup.GatewayStatus{}, ownerError(ctx, s, e)
			}
			return gatewayStatus(reply), nil
		},
	}})
}

// installationState reads A22's public view as Python's check does: Agent
// members are raw registry values, so only a JSON false is "disabled" and a
// member of another type is a non-matching value, never a decoding failure.
func installationState(view json.RawMessage) (*setup.InstallationState, error) {
	var v struct {
		ID            string                     `json:"id"`
		Phase         string                     `json:"phase"`
		ConfigChanged bool                       `json:"config_changed"`
		TargetChanged bool                       `json:"target_changed"`
		Agents        map[string]json.RawMessage `json:"agents"`
	}
	if err := jsondecode.Unmarshal(view, &v); err != nil {
		return nil, err
	}
	if v.ID == "" {
		return nil, nil
	}
	agent := func(b json.RawMessage) setup.InstalledAgent {
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(b, &fields)
		member := func(key string) string {
			var s string
			_ = json.Unmarshal(fields[key], &s)
			return s
		}
		a := setup.InstalledAgent{ID: member("id"), Name: member("name"), Host: member("host"), Status: member("status")}
		if string(fields["enabled"]) == "false" {
			disabled := false
			a.Enabled = &disabled
		}
		return a
	}
	return &setup.InstallationState{ID: v.ID, Phase: v.Phase, ConfigChanged: v.ConfigChanged, TargetChanged: v.TargetChanged, Agents: setup.AgentPair{Mihomo: agent(v.Agents["mihomo"]), OxiDNS: agent(v.Agents["oxidns"])}}, nil
}

// gatewayStatus reads an Agent status reply with the checks' own tests:
// truthiness for records and markers, "is True" for confirmations and the
// text "1" for forwarding. A member of another type is never a decoding error,
// and an unreadable journal counts as a remaining record (A20 HasRecord).
func gatewayStatus(reply host.Reply) setup.GatewayStatus {
	object := func(b json.RawMessage) map[string]json.RawMessage {
		var o map[string]json.RawMessage
		_ = json.Unmarshal(b, &o)
		return o
	}
	truthy := func(b json.RawMessage) bool {
		var v any
		if json.Unmarshal(b, &v) != nil {
			return false
		}
		switch v := v.(type) {
		case bool:
			return v
		case float64:
			return v != 0
		case string:
			return v != ""
		case []any:
			return len(v) > 0
		case map[string]any:
			return len(v) > 0
		}
		return false
	}
	isTrue := func(b json.RawMessage) *bool {
		if strings.TrimSpace(string(b)) != "true" {
			return nil
		}
		v := true
		return &v
	}
	text := func(b json.RawMessage) string {
		var s string
		_ = json.Unmarshal(b, &s)
		return s
	}
	var status setup.GatewayStatus
	if f := object(reply["ipv4_forwarding"]); f != nil {
		status.IPv4Forwarding = &setup.ForwardingStatus{Current: text(f["current"]), Persisted: isTrue(f["persisted"])}
	}
	if t := object(reply["tun"]); t != nil {
		status.TUN = &setup.TUNStatus{Configured: isTrue(t["configured"]), Present: isTrue(t["present"]), GatewayMarker: truthy(t["gateway_marker"]), Device: text(t["device"])}
	}
	if m := object(reply["managed"]); m != nil {
		status.Managed = setup.ManagedStatus{Active: truthy(m["active"]), Pending: truthy(m["pending"]), Healthy: isTrue(m["healthy"])}
		if truthy(m["error"]) {
			status.Managed.Error = text(m["error"])
			if status.Managed.Error == "" {
				status.Managed.Error = "unreadable"
			}
		}
	} else if truthy(reply["managed"]) {
		status.Managed = setup.ManagedStatus{Active: true, Error: "unreadable"}
	}
	status.EnabledInConfig = truthy(reply["enabled_in_config"])
	return status
}
