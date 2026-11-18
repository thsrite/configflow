package server

import (
	"context"
	"encoding/json"

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
				Session *setup.InstallationState `json:"session"`
			}
			if e = json.Unmarshal(b, &response); e != nil {
				return nil, e
			}
			return response.Session, nil
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
			b, e := json.Marshal(reply)
			if e != nil {
				return setup.GatewayStatus{}, e
			}
			var status setup.GatewayStatus
			e = json.Unmarshal(b, &status)
			return status, e
		},
	}})
}
