package netfilter

import (
	"net"
	"net/netip"
	"strings"
)

const (
	// Plain iptables and nft add a rule on every run; firewalld and ufw skip duplicates.
	runOnceNote         = "执行一次即可；重复执行会多出相同的规则，不影响使用。"
	iptablesPersistNote = "这些规则重启后会丢失。Debian/Ubuntu 可安装 iptables-persistent 后执行 netfilter-persistent save；Alpine 执行 rc-service iptables save。执行一次即可；重复执行会多出相同的规则，不影响使用。"
	dockerPersistNote   = "这两条规则重启后会丢失（重启 Docker 不会清空 DOCKER-USER）。Debian/Ubuntu 可安装 iptables-persistent 后执行 netfilter-persistent save；Alpine 执行 rc-service iptables save。执行一次即可；重复执行会多出相同的规则，不影响使用。"
	nftPersistNote      = "nft 命令只改当前规则，重启后会丢失；请把这些规则写进 /etc/nftables.conf（或系统加载的 nftables 配置）里对应的链。执行一次即可；重复执行会多出相同的规则，不影响使用。"
	firewalldNote       = "把 TUN 设备放进只含它的 configflow-tun 区域（接受从它进来的流量），再用 configflow-fwd 策略只放行转发进这个区域（即进入 TUN 设备）的流量；其他区域和网卡不受影响。"
	firewalldSavedNote  = "已用 --permanent 保存，重启后仍有效；可以重复执行，提示 ALREADY_ENABLED 可以忽略。"
	firewalldDNSNote    = "在局域网网卡所在的区域开放 DNS 服务（53/udp 和 53/tcp），只影响这个区域的网卡。"
	ufwNote             = "UFW 会自动保存，重启后仍有效。"
	ufwBeforeRulesNote  = "UFW 规则会自动保存；/etc/ufw/before.rules 的改动在 ufw reload 后生效，重启后仍有效。"

	// Only a may_drop result carries this text, and it is cached for 5 seconds
	// (routes.forwardFilterWarningTTL); the next refresh after that rechecks.
	verifyPrefix = "执行后等 5 秒以上再回到这里刷新状态（发现拦截时检查结果缓存 5 秒）；再在客户端"
)

// FirewalldZone holds only the TUN device; its ACCEPT target takes what
// enters from the device. FirewalldPolicy accepts forwarding from any zone into
// that zone: a policy matches by egress zone, so it only accepts packets whose
// output interface is the TUN device, and no other zone or interface is opened
// (reusing trusted would also open forwarding to whatever else the operator
// put there, such as a LAN network as a trusted source).
const (
	FirewalldZone   = "configflow-tun"
	FirewalldPolicy = "configflow-fwd"
)

// The zone and the policy are only created when missing: --new-zone and
// --new-policy fail with NAME_CONFLICT when re-run, while every other command
// only warns (ALREADY_ENABLED). Exact strings, the backend allows no other
// compound line.
const (
	FirewalldNewZoneCommand   = "firewall-cmd --permanent --info-zone=" + FirewalldZone + " >/dev/null 2>&1 || firewall-cmd --permanent --new-zone=" + FirewalldZone
	FirewalldNewPolicyCommand = "firewall-cmd --permanent --info-policy=" + FirewalldPolicy + " >/dev/null 2>&1 || firewall-cmd --permanent --new-policy=" + FirewalldPolicy
)

// UFWDNATRule accepts only traffic the host's own NAT rules redirected to it
// (auto-redirect's local port); no port is opened.
const UFWDNATRule = "-A ufw-before-input -m conntrack --ctstate DNAT -j ACCEPT"

// UFWBeforeRulesCommand adds UFWDNATRule to /etc/ufw/before.rules once, right
// after the required chain declarations, so it survives reloads and reboots.
const UFWBeforeRulesCommand = "grep -q -- '" + UFWDNATRule + "' /etc/ufw/before.rules || sed -i '/^# End required lines/a " + UFWDNATRule + "' /etc/ufw/before.rules"

// SourceLabel names a source in operator-facing text.
func SourceLabel(kind, backend, binary string) string {
	switch kind {
	case "docker":
		return "Docker"
	case "ufw":
		return "UFW"
	case "firewalld":
		return "firewalld"
	case "nftables":
		return "nftables"
	}
	if binary == "iptables-legacy" {
		return "iptables（legacy）"
	}
	return "iptables"
}

// remedy turns a plan's blocked flows into allow rules for exactly those
// flows: forwarding into and out of the TUN device, redirected TCP (conntrack
// status DNAT), TCP from the TUN device to the host, and DNS (53/udp and
// 53/tcp) from the LAN interfaces to the host.
func (in *inspection) remedy(plan *remedyPlan) Remedy {
	device := in.target.Device
	remedy := Remedy{Source: plan.source, Label: plan.label, Device: device}
	if !DevicePattern.MatchString(device) {
		return remedy
	}
	forward := plan.flows[FlowForward] || plan.flows[FlowDNS]
	redirect, tun := plan.flows[FlowRedirectTCP], plan.flows[FlowTUNTCP]
	// DNS is only opened on named LAN interfaces, never on every interface.
	var lan []string
	if plan.flows[FlowLocalDNS] {
		for _, name := range in.lan {
			if DevicePattern.MatchString(name) {
				lan = append(lan, name)
			}
		}
	}
	var commands []string
	switch plan.source {
	case "docker":
		if forward {
			commands = []string{plan.binary + " -I DOCKER-USER -o " + device + " -j ACCEPT", plan.binary + " -I DOCKER-USER -i " + device + " -j ACCEPT"}
		}
		remedy.PersistNote = dockerPersistNote
	case "ufw":
		if forward {
			commands = append(commands, "ufw route allow in on "+device, "ufw route allow out on "+device)
		}
		if tun {
			commands = append(commands, "ufw allow in on "+device)
		}
		for _, name := range lan {
			commands = append(commands, "ufw allow in on "+name+" to any port 53")
		}
		remedy.PersistNote = ufwNote
		if redirect {
			commands = append(commands, UFWBeforeRulesCommand, "ufw reload")
			remedy.PersistNote = ufwBeforeRulesNote
		}
	case "firewalld":
		// firewalld accepts redirected traffic (ct status dnat) itself; a
		// release that does not has no rule for it and is left to the operator.
		// The zone accepts what enters from the TUN device; forwarding into it
		// needs the policy as well, also when only the input hook blocked
		// (the device's previous zone may have allowed that forwarding).
		notes := []string{}
		if forward || tun {
			commands = append(commands,
				FirewalldNewZoneCommand,
				"firewall-cmd --permanent --zone="+FirewalldZone+" --set-target=ACCEPT",
				"firewall-cmd --permanent --zone="+FirewalldZone+" --change-interface="+device,
				FirewalldNewPolicyCommand,
				"firewall-cmd --permanent --policy="+FirewalldPolicy+" --add-ingress-zone=ANY",
				"firewall-cmd --permanent --policy="+FirewalldPolicy+" --add-egress-zone="+FirewalldZone,
				"firewall-cmd --permanent --policy="+FirewalldPolicy+" --set-target=ACCEPT")
			notes = append(notes, firewalldNote)
		}
		if len(lan) > 0 {
			// The LAN interface's own zone; a TUN zone never serves the LAN.
			added := false
			for _, zone := range plan.lanZones {
				if zone != FirewalldZone {
					commands = append(commands, "firewall-cmd --permanent --zone="+zone+" --add-service=dns")
					added = true
				}
			}
			if added {
				notes = append(notes, firewalldDNSNote)
			}
		}
		if len(commands) > 0 {
			commands = append(commands, "firewall-cmd --reload")
		}
		if len(notes) > 0 {
			notes = append(notes, firewalldSavedNote)
		}
		remedy.PersistNote = strings.Join(notes, "")
	case "nftables":
		if !nftNamePattern.MatchString(plan.table) || !nftNamePattern.MatchString(plan.chain) || (plan.family != "ip" && plan.family != "inet") {
			return remedy
		}
		// One shell argument keeps nft's own double quotes around the device name.
		prefix := "nft 'insert rule " + plan.family + " " + plan.table + " " + plan.chain + " "
		if forward {
			commands = append(commands, prefix+`oifname "`+device+`" accept'`, prefix+`iifname "`+device+`" accept'`)
		}
		if redirect {
			commands = append(commands, prefix+"ct status dnat accept'")
		}
		if tun {
			commands = append(commands, prefix+`iifname "`+device+`" accept'`)
		}
		for _, name := range lan {
			commands = append(commands, prefix+`iifname "`+name+`" udp dport 53 accept'`, prefix+`iifname "`+name+`" tcp dport 53 accept'`)
		}
		remedy.PersistNote = nftPersistNote
	default:
		if forward {
			commands = append(commands, plan.binary+" -I FORWARD -o "+device+" -j ACCEPT", plan.binary+" -I FORWARD -i "+device+" -j ACCEPT")
		}
		if redirect {
			commands = append(commands, plan.binary+" -I INPUT -m conntrack --ctstate DNAT -j ACCEPT")
		}
		if tun {
			commands = append(commands, plan.binary+" -I INPUT -i "+device+" -j ACCEPT")
		}
		for _, name := range lan {
			commands = append(commands, plan.binary+" -I INPUT -i "+name+" -p udp --dport 53 -j ACCEPT", plan.binary+" -I INPUT -i "+name+" -p tcp --dport 53 -j ACCEPT")
		}
		remedy.PersistNote = iptablesPersistNote
	}
	if len(commands) > maxCommands {
		commands = commands[:maxCommands]
	}
	remedy.Commands = commands
	if len(commands) == 0 {
		remedy.PersistNote = ""
	}
	return remedy
}

// protocols names the protocols a flow was blocked for (finding keys
// "flow/proto"); a flow blocked without protocol detail counts for all.
func protocols(blocked map[string]bool, flow string, all ...string) map[string]bool {
	result := map[string]bool{}
	for _, proto := range all {
		if blocked[flow+"/"+proto] {
			result[proto] = true
		}
	}
	if len(result) == 0 && blocked[flow] {
		for _, proto := range all {
			result[proto] = true
		}
	}
	return result
}

// forwardedProtocols are the protocols that pass the forward hook: TCP only
// without auto-redirect (auto-redirect hands it to a local port).
func forwardedProtocols(target Target) []string {
	if target.AutoRedirect {
		return []string{"udp", "icmp"}
	}
	return []string{"tcp", "udp", "icmp"}
}

// forwardImpact describes forwarded traffic by blocked protocol: TCP only
// passes the forward hook without auto-redirect, and UFW, for one, accepts
// forwarded ping while it drops forwarded UDP.
func forwardImpact(target Target, blocked map[string]bool) (string, bool) {
	protos := protocols(blocked, FlowForward, forwardedProtocols(target)...)
	switch {
	case len(protos) == 0:
		return "", false
	case protos["tcp"] && protos["udp"] && protos["icmp"]:
		return "主路由转来的连接（包括 TCP、UDP 和 ping）都会失败", true
	}
	var names []string
	if protos["tcp"] {
		names = append(names, "TCP")
	}
	if protos["udp"] {
		names = append(names, "UDP（如 QUIC、语音通话、游戏）")
	}
	if protos["icmp"] {
		names = append(names, "ping")
	}
	return "主路由转来的 " + joinMixed(names, "和") + spaced("会失败", names[len(names)-1]), protos["tcp"]
}

// joinMixed joins words with a CJK conjunction, spacing it from Latin words
// only (no space next to a full-width parenthesis).
func joinMixed(words []string, conjunction string) string {
	text := ""
	for index, word := range words {
		if index > 0 {
			text += spaced(conjunction, words[index-1]) + " "
		}
		text += word
	}
	return text
}

func spaced(text, previous string) string {
	if strings.HasSuffix(previous, "）") {
		return text
	}
	return " " + text
}

// hint is the explanation shown with a may_drop result: what fails, per
// blocked flow, for this host's TUN settings.
func hint(target Target, blocked map[string]bool, complete, commands bool) string {
	device := displayDevice(target.Device)
	var impacts []string
	forward, forwardTCP := forwardImpact(target, blocked)
	if forward != "" {
		impacts = append(impacts, forward)
	}
	switch {
	case blocked[FlowRedirectTCP]:
		impacts = append(impacts, "主路由转来的 TCP 连接会失败（auto-redirect 把它们转给本机端口，被本机入站规则拦下）")
	case blocked[FlowTUNTCP] && !target.AutoRedirect && !forwardTCP:
		impacts = append(impacts, "主路由转来的 TCP 连接会失败（TUN 把它们交给本机的 Mihomo，被本机入站规则拦下）")
	}
	switch {
	case blocked[FlowDNS]:
		impacts = append(impacts, "把这台 Linux 当作 DNS 服务器的设备查不到域名（UDP 和 TCP 方式都会失败）")
	case blocked[FlowTUNTCP] && target.DNSHijack:
		impacts = append(impacts, "把这台 Linux 当作 DNS 服务器的设备，TCP 方式的 DNS 查询会失败")
	}
	localDNS := protocols(blocked, FlowLocalDNS, "udp", "tcp")
	if blocked[FlowLocalDNS] {
		switch dns := localDNS; {
		case dns["udp"] && dns["tcp"]:
			impacts = append(impacts, "客户端查不到域名（UDP 和 TCP 方式都失败）")
		case dns["udp"]:
			impacts = append(impacts, "客户端查不到域名（UDP 方式的 DNS 查询失败）")
		default:
			impacts = append(impacts, "客户端 TCP 方式的 DNS 查询会失败")
		}
	}
	// Only a complete view may say so: an input rule that might apply (a
	// conditional drop, or a drop after a conditional accept) is not reported.
	// Nor when clients cannot resolve names at all (UDP DNS to this host
	// fails): their TCP connections by name fail with it.
	resolves := !blocked[FlowDNS] && !localDNS["udp"]
	if target.AutoRedirect && complete && resolves && !blocked[FlowRedirectTCP] && (forward != "" || blocked[FlowTUNTCP]) {
		impacts = append(impacts, "其他 TCP 连接不受影响")
	}
	subject := "经过 Mihomo TUN（" + device + "）的流量"
	if blocked[FlowLocalDNS] {
		if len(impacts) == 1 {
			subject = "发到本机 53 端口的 DNS 查询（由本机的 DNS 服务应答，不经过 TUN）"
		} else {
			subject = "经过 Mihomo TUN（" + device + "）的流量和发到本机 53 端口的 DNS 查询"
		}
	}
	text := "这台 Linux 的防火墙可能拦截" + subject + "：" + strings.Join(impacts, "；") + "。"
	if commands {
		return text + "ConfigFlow 不会改动防火墙，需要时在这台 Linux 上执行下面的命令放行。"
	}
	return text + "ConfigFlow 不会改动防火墙，请在这台 Linux 上手动放行这些流量。"
}

// verify says how to confirm the commands worked, for the flows they fix.
func verify(target Target, blocked map[string]bool) string {
	var checks []string
	forward := protocols(blocked, FlowForward, forwardedProtocols(target)...)
	switch {
	case forward["icmp"] && forward["udp"]:
		checks = append(checks, "ping 一个走代理的地址，或打开一个使用 UDP 的应用（如 QUIC 网页、语音通话）")
	case forward["udp"]:
		checks = append(checks, "打开一个使用 UDP 的应用（如 QUIC 网页、语音通话）")
	case forward["icmp"]:
		checks = append(checks, "ping 一个走代理的地址")
	}
	if blocked[FlowRedirectTCP] || forward["tcp"] || (!target.AutoRedirect && blocked[FlowTUNTCP]) {
		checks = append(checks, "打开一个走代理的网页")
	}
	if blocked[FlowDNS] || blocked[FlowLocalDNS] || (blocked[FlowTUNTCP] && target.DNSHijack) {
		checks = append(checks, "用这台 Linux 作 DNS 解析一个域名")
	}
	if len(checks) == 0 {
		return verifyPrefix + "确认转发恢复正常。"
	}
	text := verifyPrefix
	if first := checks[0]; first != "" && first[0] < 0x80 {
		text += " "
	}
	if len(checks) == 1 {
		return text + checks[0] + "确认可用。"
	}
	return text + strings.Join(checks, "；再") + "，确认都可用。"
}

// TUNTarget reads the TUN settings that decide which paths are inspected.
// lan lists this host's LAN-side IPv4 addresses (LANAddresses): DNS to this
// host stays out of the TUN only when route-exclude-address covers all of them.
func TUNTarget(tun map[string]any, lan []net.IP) Target {
	target := Target{Device: "Meta"}
	if value, ok := tun["device"].(string); ok && value != "" {
		target.Device = value
	}
	target.AutoRedirect, _ = tun["auto-redirect"].(bool)
	if stack, ok := tun["stack"].(string); ok {
		target.Stack = strings.ToLower(strings.TrimSpace(stack))
	}
	target.DNSHijack = target.AutoRedirect && hijacksPort53(tun) && !excludesLAN(tun["route-exclude-address"], lan)
	target.TUNPeer = tunPeer(tun)
	target.LAN = lan
	return target
}

// tunPeer is the address after the first tun.inet4-address (a list, or a
// string in older configs); zero for Mihomo's default or an unusable value.
func tunPeer(tun map[string]any) netip.Addr {
	var value string
	switch raw := tun["inet4-address"].(type) {
	case string:
		value = raw
	case []any:
		if len(raw) > 0 {
			value, _ = raw[0].(string)
		}
	}
	prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
	if err != nil || !prefix.Addr().Is4() || prefix.Bits() > 30 {
		return netip.Addr{}
	}
	return prefix.Addr().Next()
}

// hijacksPort53 follows Mihomo: without the key, dns-hijack is 0.0.0.0:53.
func hijacksPort53(tun map[string]any) bool {
	raw, present := tun["dns-hijack"]
	if !present || raw == nil {
		return true
	}
	entries, _ := raw.([]any)
	for _, item := range entries {
		value, _ := item.(string)
		value = strings.TrimSpace(value)
		for _, scheme := range []string{"tcp://", "udp://"} {
			value = strings.TrimPrefix(value, scheme)
		}
		index := strings.LastIndex(value, ":")
		if index < 0 || value[index+1:] != "53" {
			continue
		}
		switch strings.Trim(value[:index], "[]") {
		case "", "any", "0.0.0.0", "::":
			return true
		}
	}
	return false
}

// excludesLAN reports whether route-exclude-address covers every LAN IPv4
// address; an address it does not cover still has its DNS hijacked. A Docker
// or libvirt network in the list (172.16.0.0/12) does not count for the LAN.
func excludesLAN(raw any, lan []net.IP) bool {
	entries, _ := raw.([]any)
	var networks []*net.IPNet
	for _, item := range entries {
		value, _ := item.(string)
		_, network, err := net.ParseCIDR(strings.TrimSpace(value))
		if err != nil {
			ip := net.ParseIP(strings.TrimSpace(value))
			if ip == nil {
				continue
			}
			if v4 := ip.To4(); v4 != nil {
				ip = v4
			}
			network = &net.IPNet{IP: ip, Mask: net.CIDRMask(len(ip)*8, len(ip)*8)}
		}
		networks = append(networks, network)
	}
	covered := 0
	for _, ip := range lan {
		v4 := ip.To4()
		if v4 == nil || v4.IsLoopback() {
			continue
		}
		excluded := false
		for _, network := range networks {
			if network.Contains(v4) {
				excluded = true
				break
			}
		}
		if !excluded {
			return false
		}
		covered++
	}
	return covered > 0
}

// interfaceAddresses is one interface's name, flags and addresses.
type interfaceAddresses struct {
	name  string
	flags net.Flags
	ips   []net.IP
}

// LANAddresses lists the IPv4 addresses of the default-route interfaces, where
// LAN devices reach this host. Without a readable default route it falls back
// to every interface that is up and is not loopback, point-to-point (TUN) or a
// container or VM bridge. Errors give none.
func LANAddresses() []net.IP {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var list []interfaceAddresses
	for _, iface := range interfaces {
		addresses, err := iface.Addrs()
		if err != nil {
			continue
		}
		item := interfaceAddresses{name: iface.Name, flags: iface.Flags}
		for _, address := range addresses {
			if network, ok := address.(*net.IPNet); ok {
				item.ips = append(item.ips, network.IP)
			}
		}
		list = append(list, item)
	}
	return lanAddresses(defaultRouteInterfaces(DefaultProbe(), ""), list)
}

func lanAddresses(defaults []string, interfaces []interfaceAddresses) []net.IP {
	wanted := map[string]bool{}
	for _, name := range defaults {
		wanted[name] = true
	}
	var result []net.IP
	for _, iface := range interfaces {
		if len(wanted) > 0 {
			if !wanted[iface.name] {
				continue
			}
		} else if iface.flags&net.FlagUp == 0 || iface.flags&(net.FlagLoopback|net.FlagPointToPoint) != 0 || nonLANInterface(iface.name) {
			continue
		}
		for _, ip := range iface.ips {
			if v4 := ip.To4(); v4 != nil && !v4.IsLoopback() {
				result = append(result, v4)
			}
		}
	}
	return result
}

func nonLANInterface(name string) bool {
	for _, prefix := range nonLANPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}
