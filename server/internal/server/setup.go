package server

import (
	"context"
	"encoding/json"

	apirules "configflow/internal/api/rules"
	"configflow/internal/gateway/onboarding"
	"configflow/internal/routers"
	"configflow/internal/setup"
	"configflow/internal/yamlx"
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
