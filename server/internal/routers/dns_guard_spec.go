package routers

import (
	"configflow/internal/routers/device"
	"configflow/internal/routers/ros"
	"fmt"
	"net/netip"
	"slices"
	"strings"

	"configflow/internal/netipx"
)

func guardConnected(ctx *NatContext, address netip.Addr) (string, string, error) {
	if ctx == nil || ctx.Routes == nil {
		return "", "", device.Error(messageGuardRoutes)
	}
	best := -1
	selected := []Row{}
	var network netip.Prefix
	for _, row := range ctx.Routes {
		if device.FirstNonEmpty(row.Text("routing-table"), "main") != "main" || !row.Flag("connect") || !row.Flag("active") || row.Flag("disabled") {
			continue
		}
		n, ok := netipx.ParseNetwork4(row.Text("dst-address"), false)
		if !ok || !n.Contains(address) {
			continue
		}
		if n.Bits() > best {
			best = n.Bits()
			selected = nil
			network = n
		}
		if n.Bits() == best {
			selected = append(selected, row)
		}
	}
	if len(selected) == 0 {
		return "", "", device.Error(messageGuardDisconnected)
	}
	if len(selected) != 1 {
		return "", "", device.Error(messageGuardMultiple)
	}
	row := selected[0]
	iface := ros.Interface(row.Text("gateway"))
	immediate := iface
	if row.Set("immediate-gw") {
		immediate = ros.Interface(row.Text("immediate-gw"))
	}
	if iface == "" || iface != immediate {
		return "", "", device.Error(messageGuardInterface)
	}
	return iface, network.String(), nil
}

func guardDesiredSpec(settings DNSGuard, desired *Desired, nat *NatContext) (*DNSGuardSpec, error) {
	dns := desired.DNSSettings
	if dns == nil {
		return nil, device.Error(messageGuardAddress)
	}
	if dns.Error != "" {
		return nil, device.Error(fmt.Sprintf(messageGuardDNS, dns.Error))
	}
	address, ok := netipx.ParseAddr4(dns.Server)
	singles := []netip.Addr{}
	if dns.HA != nil {
		address, ok = netipx.ParseAddr4(dns.HA.VIP)
		if !ok || dns.HA.Hosts == nil {
			return nil, device.Error(messageGuardHA)
		}
		singles = append(singles, address)
		for _, host := range dns.HA.Hosts {
			a, valid := netipx.ParseAddr4(host)
			if !valid {
				return nil, device.Error(messageGuardHA)
			}
			singles = append(singles, a)
		}
	} else {
		if !ok {
			return nil, device.Error(messageGuardAddress)
		}
		singles = append(singles, address)
	}
	iface, network, err := guardConnected(nat, address)
	if err != nil {
		return nil, err
	}
	targets := slices.Clone(desired.ProxyCIDRs)
	if desired.ROSPolicy != nil {
		targets = append(targets, desired.ROSPolicy.CIDRs...)
	}
	for _, a := range singles {
		if a != address && dns.HA != nil {
			i, n, e := guardConnected(nat, a)
			if e != nil || i != iface || n != network {
				return nil, device.Error(fmt.Sprintf(messageGuardHAConnected, a, address, network))
			}
		}
	}
	for _, a := range singles {
		for _, target := range targets {
			n, valid := netipx.ParseNetwork4(target.CIDR, false)
			if !valid {
				return nil, device.Error(device.MessageOperationFormat)
			}
			if n.Contains(a) {
				if dns.HA != nil {
					return nil, device.Error(fmt.Sprintf(messageGuardHAProxy, a))
				}
				return nil, device.Error(messageGuardProxy)
			}
		}
	}
	if gateway, valid := netipx.ParseAddr4(desired.ProxyGateway); valid {
		singles = append(singles, gateway)
	}
	bypass := []netip.Prefix{}
	exempt := []string{}
	for _, a := range singles {
		bypass = append(bypass, netip.PrefixFrom(a, 32))
		exempt = append(exempt, a.String())
	}
	for _, text := range settings.Bypass {
		n, valid := netipx.ParseNetwork4(text, false)
		if !valid {
			return nil, device.Error(device.MessageOperationFormat)
		}
		bypass = append(bypass, n)
		if n.Bits() >= 30 {
			for a := n.Addr(); n.Contains(a); a = a.Next() {
				exempt = append(exempt, a.String())
			}
		}
	}
	collapsed, _ := netipx.Collapse(bypass)
	spec := &DNSGuardSpec{Agent: address.String(), Interface: iface, Network: network, Bypass: []string{}, BlockDoT: settings.BlockDoT, Failover: settings.Failover}
	for _, n := range collapsed {
		spec.Bypass = append(spec.Bypass, n.String())
	}
	if dns.HA != nil {
		unique := []string{}
		for _, a := range exempt {
			if !slices.Contains(unique, a) {
				unique = append(unique, a)
			}
		}
		unique = unique[:min(len(unique), 64)]
		spec.Exempt = &unique
	}
	return spec, nil
}

func guardExempt(spec *DNSGuardSpec) []string {
	if spec.Exempt != nil {
		return *spec.Exempt
	}
	hosts := []string{}
	for _, cidr := range spec.Bypass {
		n, ok := netipx.ParseNetwork4(cidr, true)
		if ok && n.Bits() >= 30 {
			for a := n.Addr(); n.Contains(a); a = a.Next() {
				hosts = append(hosts, a.String())
			}
		}
	}
	return hosts[:min(len(hosts), 64)]
}

func guardWatchScript(spec *DNSGuardSpec, down bool) string {
	if down {
		return fmt.Sprintf(`:delay 15s; :if ([/tool netwatch get [find name="cf-dnsguard"] status] = "down") do={/ip firewall nat disable [find comment="cf-dnsguard:hijack"]; :foreach c in=[/ip firewall connection find where dst-port=53 and dstnat and reply-src-address=%s] do={:do {/ip firewall connection remove $c} on-error={}}}`, spec.Agent)
	}
	parts := []string{"dst-port=53 and !dstnat", "dst-address!=" + spec.Agent}
	for _, host := range guardExempt(spec) {
		parts = append(parts, "src-address!="+host)
	}
	return `/ip firewall nat enable [find comment="cf-dnsguard:hijack"]; :foreach c in=[/ip firewall connection find where ` + strings.Join(parts, " and ") + `] do={:do {/ip firewall connection remove $c} on-error={}}`
}

// DNSGuardResources returns objects in safe creation order: bypass and hairpin
// first, hijack next, encrypted-DNS blocking and health monitoring last.
func DNSGuardResources(spec *DNSGuardSpec) []DNSGuardCreate {
	items := []DNSGuardCreate{}
	if spec == nil {
		return items
	}
	for _, cidr := range spec.Bypass {
		items = append(items, DNSGuardCreate{Role: "bypass:" + cidr, Path: ros.PathAddresses, Values: Row{"list": DNSGuardBypassList, "address": cidr, "comment": dnsGuardPrefix + "bypass", "disabled": "false"}})
	}
	for _, protocol := range []string{"udp", "tcp"} {
		items = append(items, DNSGuardCreate{Role: "hairpin-" + protocol, Path: ros.PathNAT, Values: Row{"chain": "srcnat", "action": "masquerade", "out-interface": spec.Interface, "src-address": spec.Network, "dst-address": spec.Agent, "protocol": protocol, "dst-port": "53", "comment": dnsGuardPrefix + "hairpin", "disabled": "false"}})
	}
	for _, protocol := range []string{"udp", "tcp"} {
		items = append(items, DNSGuardCreate{Role: "hijack-" + protocol, Path: ros.PathNAT, Top: true, Values: Row{"chain": "dstnat", "action": "dst-nat", "in-interface": spec.Interface, "src-address-list": "!" + DNSGuardBypassList, "dst-address": "!" + spec.Agent, "dst-address-type": "!local", "protocol": protocol, "dst-port": "53", "to-addresses": spec.Agent, "to-ports": "53", "comment": dnsGuardHijack, "disabled": "false"}})
	}
	if spec.BlockDoT {
		for _, protocol := range []string{"tcp", "udp"} {
			values := Row{"chain": "forward", "in-interface": spec.Interface, "protocol": protocol, "dst-port": "853", "src-address-list": "!" + DNSGuardBypassList, "comment": dnsGuardPrefix + "dot", "disabled": "false", "action": "drop"}
			if protocol == "tcp" {
				values["action"] = "reject"
				values["reject-with"] = "tcp-reset"
			}
			items = append(items, DNSGuardCreate{Role: "dot-" + protocol, Path: ros.PathFilter, Top: true, Values: values})
		}
	}
	if spec.Failover {
		items = append(items, DNSGuardCreate{Role: "netwatch", Path: ros.PathNetwatch, Values: Row{"name": dnsGuardWatch, "host": spec.Agent, "port": "53", "type": "tcp-conn", "interval": "10s", "timeout": "2s", "startup-delay": "30s", "up-script": guardWatchScript(spec, false), "down-script": guardWatchScript(spec, true), "comment": dnsGuardPrefix + "netwatch", "disabled": "false"}})
	}
	return items
}

func guardObjectName(path string) string {
	switch path {
	case ros.PathAddresses:
		return messageGuardObjectAddress
	case ros.PathFilter:
		return messageGuardObjectFilter
	case ros.PathNAT:
		return "NAT"
	default:
		return "Netwatch"
	}
}
func guardDescribe(role string, v Row) string {
	switch {
	case strings.HasPrefix(role, "bypass"):
		return fmt.Sprintf(messageGuardBypassDetail, v.Text("address"))
	case strings.HasPrefix(role, "hairpin"):
		return fmt.Sprintf(messageGuardHairpinDetail, strings.ToUpper(v.Text("protocol")), v.Text("src-address"), v.Text("dst-address"))
	case strings.HasPrefix(role, "hijack"):
		return fmt.Sprintf(messageGuardHijackDetail, strings.ToUpper(v.Text("protocol")), v.Text("to-addresses"), v.Text("in-interface"))
	case strings.HasPrefix(role, "dot"):
		action := messageGuardDrop
		if v.Is("action", "reject") {
			action = messageGuardReject
		}
		return fmt.Sprintf(messageGuardDotDetail, strings.ToUpper(v.Text("protocol")), action)
	default:
		return fmt.Sprintf(messageGuardWatchDetail, v.Text("host"))
	}
}
func guardSummary(spec *DNSGuardSpec, previous *DNSGuardManifest) string {
	if spec == nil {
		return messageGuardRemoveSummary
	}
	verb := messageGuardEnable
	if previous != nil {
		verb = messageGuardUpdate
	}
	out := fmt.Sprintf(messageGuardSummary, verb, spec.Interface, spec.Agent, strings.Join(spec.Bypass, ", "))
	extras := []string{}
	if spec.BlockDoT {
		extras = append(extras, messageGuardDot)
	}
	if spec.Failover {
		extras = append(extras, messageGuardFailover)
	}
	if len(extras) > 0 {
		out += "；" + strings.Join(extras, "，")
	}
	return out
}
