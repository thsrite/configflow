// Package gwbase holds what common/linux_gateway.py and common/router_split.py
// share, so internal/gateway (agents A19–A22) and internal/routersplit
// (routers R02) can both import it without importing each other (Python's
// router_split ↔ linux_gateway import cycle). Coordinator decision 2 of
// PROGRESS-routers.md: exactly these three symbols, no logic.
package gwbase

// LinuxGatewayError is common/linux_gateway.LinuxGatewayError (a
// ValueError): a gateway setting cannot safely be applied. Message is
// str(exc).
type LinuxGatewayError struct{ Message string }

func (e *LinuxGatewayError) Error() string { return e.Message }

// PythonClass is the Python exception class name.
func (e *LinuxGatewayError) PythonClass() string { return "LinuxGatewayError" }

// TUNFilterKeys is linux_gateway.TUN_FILTER_KEYS: the Mihomo TUN keys that
// let some connections bypass the TUN. The order matters (messages list the
// keys in this order).
var TUNFilterKeys = []string{
	"include-interface", "exclude-interface", "include-uid", "include-uid-range",
	"exclude-uid", "exclude-uid-range", "include-mac-address", "exclude-mac-address",
	"include-android-user", "include-package", "exclude-package",
}

// RestoreKey is linux_gateway_draft.RESTORE_KEY: the mihomo key holding the
// gateway editor's restore point.
const RestoreKey = "linux_gateway_restore_point"
