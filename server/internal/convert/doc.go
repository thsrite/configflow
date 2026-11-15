// Package convert is module C12 of the config domain: the converter helpers
// that the rule fetch, Sub-Store client, latency, provider delivery, setup
// and domain discovery code share, ported first to break Python's lazy
// import cycles.
//
//   - common.go: backend/converters/common.py
//   - mihomo_nodes.go: the node-conversion group of
//     backend/converters/mihomo.py and backend/utils/proxy_utils.py
//   - dialers.go, dialers_api.go: backend/utils/dialer_references.py
//   - easytier.go: backend/common/mihomo_easytier.py
//
// Configuration values are jsonx values. Python's intentional exceptions
// are *DialerReferenceError and *EasyTierSettingsError; the exceptions
// Python raises by accident on malformed input are *PyError.
//
// Sub-Store conversion (C13) is injected through ProxyStringConverter.
package convert
