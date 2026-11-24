package gateway

import (
	"net/netip"

	"configflow/internal/convert"
	"configflow/internal/model"
	"configflow/internal/routersplit"
	"configflow/internal/yamlx"
)

// Hooks returns the native gateway collaborators used by generation and validation.
func Hooks() convert.MihomoIntegrations {
	return convert.MihomoIntegrations{
		ApplyGateway: func(doc *yamlx.Node, config *model.Config, base string) error {
			return NewConfig(config).Apply(doc, base)
		},
		GatewayEnabled: func(config *model.Config) (bool, error) {
			settings, err := SettingsOf(config)
			return settings.Enabled, err
		},
		FakeIPNetworkError: func(value string) error { _, err := ParseFakeIPRange(value); return err },
		DNSLoop: func(dns *yamlx.Node, hosts []netip.Addr) (string, string, bool, error) {
			match := MihomoDNSLoop(dns, hosts)
			if match == nil {
				return "", "", false, nil
			}
			return match.Key, match.Upstream, true, nil
		},
		FullTun: func(config *model.Config) (bool, error) {
			if config == nil {
				return false, nil
			}
			state, _ := routersplit.ReadMarkerProfile(config.Profile)
			return state == routersplit.StateValid, nil
		},
	}
}
