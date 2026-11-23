package routes

import (
	"os"
	"path/filepath"
	"strings"
)

// Return routing sends replies back through the main router, so traffic the
// gateway forwards out the interface it came in on would otherwise make the
// kernel answer clients with ICMP redirects pointing at the router. Disable
// them while return routing is active and restore the recorded values on
// removal; a failed write is logged and never fails the release.

// sysctlRoot is replaceable in tests.
var sysctlRoot = "/proc/sys"

func sendRedirectsPath(iface string) string {
	return filepath.Join(sysctlRoot, "net", "ipv4", "conf", iface, "send_redirects")
}

// disableSendRedirects sets send_redirects=0 for "all" and the LAN interface,
// recording each original value once. It reports whether the record changed.
func disableSendRedirects(state *returnRoutingState, iface string) bool {
	changed := false
	for _, name := range []string{"all", iface} {
		if name == "" {
			continue
		}
		path := sendRedirectsPath(name)
		current, err := os.ReadFile(path)
		if err != nil {
			logger.Warnf("return routing: cannot read %s: %v", path, err)
			continue
		}
		value := strings.TrimSpace(string(current))
		if state.SendRedirects == nil {
			state.SendRedirects = map[string]string{}
		}
		if _, recorded := state.SendRedirects[name]; !recorded {
			state.SendRedirects[name] = value
			changed = true
		}
		if value != "0" {
			if err = os.WriteFile(path, []byte("0"), 0644); err != nil {
				logger.Warnf("return routing: cannot disable %s: %v", path, err)
			}
		}
	}
	return changed
}

// restoreSendRedirects writes back the values recorded before return routing.
func restoreSendRedirects(state *returnRoutingState) {
	for name, value := range state.SendRedirects {
		if value == "" {
			continue
		}
		if err := os.WriteFile(sendRedirectsPath(name), []byte(value), 0644); err != nil {
			logger.Warnf("return routing: cannot restore send_redirects for %s: %v", name, err)
		}
	}
	state.SendRedirects = nil
}

// ensureSendRedirects re-applies the setting when a replay is skipped, and
// records original values for state written by Agents that did not track them.
func ensureSendRedirects(cfg *Config, iface string) {
	unlock, err := lockReturnJournal(cfg)
	if err != nil {
		return
	}
	defer unlock()
	state, err := loadReturnState(cfg)
	if err != nil || state == nil || !state.Active {
		return
	}
	if disableSendRedirects(state, iface) {
		if err = saveReturnState(cfg, state); err != nil {
			logger.Warnf("return routing: cannot record send_redirects: %v", err)
		}
	}
}
