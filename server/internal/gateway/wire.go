package gateway

import (
	"configflow/internal/convert"
	"configflow/internal/jsonx"
)

// Wire registers the gateway functions the Mihomo generator and its base
// settings checks call (convert.MihomoHooks: ApplyLinuxGateway,
// GatewayEnabled, FakeIPNetworkError). Listed in server.wirePackages.
func Wire() {
	convert.RegisterMihomoHooks(Hooks())
}

// Hooks are the convert hooks this package provides.
func Hooks() convert.MihomoHooks {
	return convert.MihomoHooks{
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
