package ippresence

import (
	"configflow/internal/convert"
	"configflow/internal/model"
	"configflow/internal/oxidns"
	"configflow/internal/routersplit"
)

// Wire registers routers R02's hooks (listed in server.wirePackages):
// convert.MihomoIntegrations.FullTun = routersplit.ReadMarkerProfile and the
// OxiDNS offline classical IP presence hook. It lives here, not in
// routersplit, so routersplit (imported by internal/gateway) does not import
// internal/oxidns.
func Wire() {
	convert.SetMihomoIntegrations(convert.MihomoIntegrations{FullTun: isFullTun})
	oxidns.SetNativeClassicalIPLines(PeekTypedClassicalIPLines)
}

func isFullTun(config *model.Config) (bool, error) {
	if config == nil {
		return false, nil
	}
	state, _ := routersplit.ReadMarkerProfile(config.Profile)
	return state == routersplit.StateValid, nil
}
