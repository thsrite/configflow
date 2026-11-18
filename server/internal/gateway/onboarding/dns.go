package onboarding

import (
	"net/netip"

	"configflow/internal/gateway"
	"configflow/internal/yamlx"
	"gopkg.in/yaml.v3"
)

const (
	// DefaultMihomoPrimaryDNS is the first encrypted bootstrap-free resolver.
	DefaultMihomoPrimaryDNS = DefaultLocalDNS
	// DefaultMihomoSecondaryDNS is the second encrypted bootstrap-free resolver.
	DefaultMihomoSecondaryDNS = "https://1.12.12.12/dns-query"
)

// DefaultMihomoNameservers returns a fresh list of the wizard's DNS defaults.
// Callers cannot mutate another operation's defaults.
func DefaultMihomoNameservers() []string {
	return []string{DefaultMihomoPrimaryDNS, DefaultMihomoSecondaryDNS}
}

// PrepareMihomoDNS fills missing nameservers and rejects circular upstreams.
// The caller owns document, parsed through yamlx; it is edited in place without
// expanding its alias graph. A nil document selects the initial Mihomo template.
// Every other base field is retained. The returned node performs no I/O and is
// ready for yamlx.Marshal. On error the temporary node must be discarded.
func PrepareMihomoDNS(document *yamlx.Node, localAddresses []netip.Addr, check DNSLoopChecker) (*yamlx.Node, error) {
	for _, address := range localAddresses {
		if !address.IsValid() {
			return nil, invalid(msgLocalAddresses)
		}
	}
	empty := document == nil
	if empty {
		var err error
		document, err = defaultMihomo()
		if err != nil {
			return nil, err
		}
	}
	dns := field(document, "dns")
	if document.Kind != yaml.MappingNode || dns != nil && (yamlx.Resolve(dns) == nil || yamlx.Resolve(dns).Kind != yaml.MappingNode) {
		return nil, invalid(msgBaseMapping)
	}
	if dns == nil {
		dns = yamlx.Map()
		yamlx.Set(document, "dns", dns)
	} else {
		dns = yamlx.Resolve(dns)
	}
	if empty || !nonempty(field(dns, "nameserver")) {
		nameservers := yamlx.Seq()
		for _, resolver := range DefaultMihomoNameservers() {
			nameservers.Content = append(nameservers.Content, yamlx.Str(resolver))
		}
		yamlx.Set(dns, "nameserver", nameservers)
	}
	// The caller supplies the shared checker; missing wiring fails closed.
	if check == nil {
		return nil, invalid(msgWaitingDNS)
	}
	loop, err := check(dns, localAddresses)
	if err != nil {
		return nil, err
	}
	if loop {
		return nil, invalid(msgDNSLoop)
	}
	yamlx.MaterializeAliases(document)
	return document, nil
}

// CheckDNSLoop adapts the shared gateway validator without duplicating its
// upstream parsing. Host addresses are supplied only by the target-host owner.
func CheckDNSLoop(dns *yamlx.Node, localAddresses []netip.Addr) (bool, error) {
	return gateway.MihomoDNSLoop(dns, localAddresses) != nil, nil
}
