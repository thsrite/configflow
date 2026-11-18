package gateway

import (
	"encoding/json"
	"net/netip"

	"configflow/internal/convert"
	"configflow/internal/jsonx"
	"configflow/internal/yamlx"
)

// Wire registers the gateway functions the Mihomo generator and its base
// settings checks call (convert.MihomoHooks: ApplyLinuxGateway,
// GatewayEnabled, FakeIPNetworkError, MihomoDNSLoop). Listed in server.wirePackages.
func Wire() {
	convert.RegisterMihomoHooks(Hooks())
}

// Hooks are the convert hooks this package provides.
func Hooks() convert.MihomoHooks {
	return convert.MihomoHooks{
		MihomoDNSLoop: func(dns *jsonx.Object, hosts []netip.Addr) (string, any, bool, error) {
			data, err := json.Marshal(dns)
			if err != nil {
				return "", nil, false, err
			}
			node, err := yamlx.FromJSON(data)
			if err != nil {
				return "", nil, false, err
			}
			match := MihomoDNSLoop(node, hosts)
			if match == nil {
				return "", nil, false, nil
			}
			return match.Key, match.Upstream, true, nil
		},
		ApplyLinuxGateway: ApplyJSONX,
		GatewayEnabled: func(config *jsonx.Object) (bool, error) {
			settings, err := SettingsOf(config)
			return settings.Enabled, err
		},
		FakeIPNetworkError: func(value string) error {
			_, err := ParseFakeIPRange(value)
			return err
		},
	}
}
