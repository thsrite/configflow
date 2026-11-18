package server

import (
	"context"
	"encoding/json"

	apirules "configflow/internal/api/rules"
	"configflow/internal/routers"
	"configflow/internal/setup"
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
