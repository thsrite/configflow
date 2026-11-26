package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"configflow/internal/agents"
	"configflow/internal/gateway/host"
	"configflow/internal/gateway/installation"
	"configflow/internal/jsondecode"
	"configflow/internal/repo"
	"configflow/internal/routers"
	"configflow/internal/routers/device"
	"configflow/internal/routers/spec"
	"configflow/internal/rulesource"
	"configflow/internal/setup"
)

// buildSetupOwners constructs setup.ExternalOwners directly from the wired
// server instances (O17 W11 / review L5), with no global registry or assembly
// snapshot. Preserves ReadOfflineRouteContent (f49f11acc).
func buildSetupOwners(ag *agentServices, rs *rulesource.Service, rtr *routers.Service) setup.ExternalOwners {
	service := func(r *repo.Repository) *installation.Service {
		return &installation.Service{Repository: r, Manager: ag.manager}
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
	return setup.ExternalOwners{
		DNS: setup.DefaultDNSOwner(),
		ReadOfflineRouteContent: func(ctx context.Context, profile json.RawMessage, item spec.RuleSet, library *spec.LibraryRule) (string, error) {
			var raw json.RawMessage
			if library != nil {
				raw = library.Raw
			}
			return rs.ReadOfflineJSON(ctx, profile, item.Raw, raw)
		},
		ReadRouteContent: func(ctx context.Context, config json.RawMessage, item spec.RuleSet, library *spec.LibraryRule, options setup.RouteReadOptions) (string, error) {
			var raw json.RawMessage
			if library != nil {
				raw = library.Raw
			}
			return rs.ReadJSON(ctx, config, item.Raw, raw, rulesource.ContentOptions{
				AllowNetwork:   options.AllowNetwork,
				ConnectTimeout: options.ConnectTimeout,
				ReadTimeout:    options.ReadTimeout,
			})
		},
		Installation: &setup.InstallationOwner{
			Latest: func(ctx context.Context, r *repo.Repository, profile string) (*setup.InstallationState, error) {
				s := service(r)
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
				s := service(r)
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
		},
		VerifyRouter: func(ctx context.Context, r *repo.Repository, profileID string, config json.RawMessage, router *spec.Router) (setup.RouterVerification, error) {
			return verifySetupRouter(ctx, rtr, ag.manager, r, profileID, config, router)
		},
	}
}

// verifySetupRouter inspects one router of the setup profile without writing to
// the device or the Agents. Device and plan failures (*routers.RouterError)
// become the wizard's 502 with the router's own message, as on the router page.
func verifySetupRouter(ctx context.Context, rtr *routers.Service, m *agents.Manager, r *repo.Repository, profileID string, config json.RawMessage, router *spec.Router) (setup.RouterVerification, error) {
	if err := router.ValidateForDevice(); err != nil {
		return setup.RouterVerification{}, routerFailure(err)
	}
	d := routers.NewDriver(router)
	if d == nil {
		return setup.RouterVerification{}, fmt.Errorf("verify router: unsupported type %q", router.Type)
	}
	defer d.Close()
	inspection, err := rtr.Inspect(ctx, routers.InspectRequest{ProfileID: profileID, Config: config, Router: router, Driver: d, Agents: routers.ManagerAgentSource{Manager: m}, Repo: r})
	if err != nil {
		return setup.RouterVerification{}, routerFailure(err)
	}
	plan, err := routers.BuildPlan(ctx, router, inspection, d)
	if err != nil {
		return setup.RouterVerification{}, routerFailure(err)
	}
	return setup.RouterVerification{Blockers: plan.Blockers, Empty: plan.Empty}, nil
}

// routerFailure maps the router owner's public errors to wizard statuses and
// keeps every other error (storage, cancellation) on the internal path.
func routerFailure(err error) error {
	var re *device.RouterError
	var invalid *device.ValidationError
	switch {
	case errors.As(err, &invalid):
		return &setup.Error{Message: err.Error(), Status: 400, Cause: err}
	case errors.As(err, &re):
		return &setup.Error{Message: err.Error(), Status: 502, Cause: err}
	}
	return err
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
