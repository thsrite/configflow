package installer

import (
	"errors"
	"strings"
)

// Port of backend/agents/core_install_script.py: the standalone,
// fixed-layout Mihomo + OxiDNS core installer.

// GenerateCoreInstallScript is generate_core_install_script(installer_forwarding):
// the core-only installer; no Agent identity or access token is used.
// installerForwarding is for the core phase of an installer that enables and
// persists IPv4 forwarding itself.
func GenerateCoreInstallScript(installerForwarding bool) (string, error) {
	text := installCoresTemplate
	if installerForwarding {
		if strings.Count(text, coreForwardingBlock) != 1 {
			return "", errors.New("install-cores.sh forwarding block changed; review the installer-managed text")
		}
		text = replaceOnce(text, coreForwardingBlock, installerForwardingBlock)
	}
	return WithInstallOutput(text), nil
}
