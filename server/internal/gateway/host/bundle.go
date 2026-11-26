package host

import (
	"configflow/internal/gateway"
	"configflow/internal/model"
	"configflow/internal/yamlx"
	"gopkg.in/yaml.v3"
)

// RequireGatewayBundleAgent guards a frozen bundle's main file, even when a
// caller bypasses profile preparation. The caller supplies its validated main
// member; archive validation stays with deploy.Bundle.
func RequireGatewayBundleAgent(a *model.Agent, main []byte) error {
	if a != nil && (a.ServiceType != "" && a.ServiceType != "mihomo" || Supports(a, MinVersion)) {
		return nil
	}
	node, err := yamlx.Parse(main)
	if err != nil {
		return err
	}
	marker := yamlx.Get(node, gateway.MarkerKey)
	enabled := yamlx.Get(marker, "enabled")
	if yamlx.Resolve(marker) != nil && yamlx.Resolve(marker).Kind == yaml.MappingNode && enabled != nil {
		var value bool
		if yamlx.Decode(enabled, &value) == nil && value {
			return RequireGatewayAgent(a)
		}
	}
	return nil
}
