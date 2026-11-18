package ippresence

import (
	"configflow/internal/convert"
	"configflow/internal/jsonx"
	"configflow/internal/oxidns"
	"configflow/internal/routersplit"
)

// Wire registers routers R02's hooks (listed in server.wirePackages):
// convert.MihomoHooks.IsFullTun = router_split.is_full_tun and the OxiDNS
// rule_ip_presence.classical_ip_lines hook. It lives here, not in
// routersplit, so routersplit (imported by internal/gateway) does not import
// internal/oxidns.
func Wire() {
	convert.RegisterMihomoHooks(convert.MihomoHooks{IsFullTun: isFullTun})
	oxidns.SetNativeClassicalIPLines(PeekTypedClassicalIPLines)
}

func isFullTun(config *jsonx.Object) (bool, error) {
	return routersplit.IsFullTun(config), nil
}
