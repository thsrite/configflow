package importer

import (
	"fmt"
	"strings"
)

func parseLoonProxy(name, value string, plan *Plan) (*Proxy, error) {
	args := splitArgs(value, ',')
	kind := strings.ToLower(strings.TrimSpace(at(args, 0)))
	if kind == "direct" || kind == "reject" {
		plan.Aliases[name] = strings.ToUpper(kind)
		return nil, nil
	}
	var rest []string
	if len(args) > 0 {
		rest = args[1:]
	}
	if kind == "wireguard" {
		return loonWireguard(name, rest)
	}
	pos, options := splitOptions(rest)
	if len(pos) < 2 {
		return nil, fmt.Errorf(msgMissingEndpoint)
	}
	number, err := port(pos[1])
	if err != nil {
		return nil, err
	}
	proxy := newProxy(name, kind, pos[0], number)
	extra := pos[2:]
	if enabled(options["udp"]) {
		proxy.set("udp", true)
	}
	if enabled(options["fast-open"]) {
		proxy.set("tfo", true)
	}
	switch kind {
	case "shadowsocks":
		proxy.Type = "ss"
		cipher := "aes-128-gcm"
		if len(extra) > 0 {
			cipher = extra[0]
		}
		proxy.set("cipher", cipher)
		proxy.set("password", at(extra, 1))
		if options["obfs-name"] != "" {
			proxy.set("plugin", "obfs")
			proxy.set("plugin-opts", map[string]string{"mode": options["obfs-name"], "host": options["obfs-host"]})
		} else if options["shadow-tls-password"] != "" {
			proxy.set("plugin", "shadow-tls")
			proxy.set("plugin-opts", struct {
				Password string `json:"password"`
				Host     string `json:"host"`
				Version  int    `json:"version"`
			}{options["shadow-tls-password"], options["shadow-tls-sni"], integerOr(options["shadow-tls-version"], 3)})
		}
		if enabled(options["udp-over-tcp"]) {
			proxy.set("udp-over-tcp", true)
		}
	case "shadowsocksr":
		proxy.Type = "ssr"
		proxy.set("cipher", at(extra, 0))
		proxy.set("password", at(extra, 1))
		proxy.set("protocol", option(options, "protocol", "origin"))
		proxy.set("obfs", option(options, "obfs", "plain"))
		for _, key := range []string{"protocol-param", "obfs-param"} {
			if options[key] != "" {
				proxy.set(key, options[key])
			}
		}
	case "vmess":
		cipher := "auto"
		if len(extra) > 0 {
			cipher = extra[0]
		}
		proxy.set("cipher", cipher)
		proxy.set("uuid", at(extra, 1))
		proxy.set("alterId", integerOr(options["alterid"], 0))
		if err := loonTransport(&proxy, options); err != nil {
			return nil, err
		}
		if enabled(options["over-tls"]) {
			proxy.set("tls", true)
			tlsOptions(&proxy, options, "servername", true)
		}
	case "vless":
		proxy.set("uuid", at(extra, 0))
		if err := loonTransport(&proxy, options); err != nil {
			return nil, err
		}
		if options["flow"] != "" {
			proxy.set("flow", options["flow"])
		}
		if enabled(options["over-tls"]) || options["public-key"] != "" {
			proxy.set("tls", true)
			tlsOptions(&proxy, options, "servername", true)
		}
	case "trojan":
		proxy.set("password", at(extra, 0))
		if err := loonTransport(&proxy, options); err != nil {
			return nil, err
		}
		tlsOptions(&proxy, options, "sni", true)
	case "hysteria2":
		proxy.set("password", at(extra, 0))
		proxy.set("udp", true)
		tlsOptions(&proxy, options, "sni", true)
		if options["salamander-password"] != "" {
			proxy.set("obfs", "salamander")
			proxy.set("obfs-password", options["salamander-password"])
		}
		if options["server-ports"] != "" {
			proxy.set("ports", options["server-ports"])
		}
		if options["download-bandwidth"] != "" {
			proxy.set("down", options["download-bandwidth"]+" Mbps")
		}
	case "anytls":
		proxy.set("password", at(extra, 0))
		tlsOptions(&proxy, options, "sni", true)
	case "http", "https", "socks5":
		proxy.Type = "http"
		if kind == "socks5" {
			proxy.Type = "socks5"
		}
		if len(extra) > 0 {
			proxy.set("username", extra[0])
		}
		if len(extra) > 1 {
			proxy.set("password", extra[1])
		}
		if kind == "https" || enabled(options["over-tls"]) {
			proxy.set("tls", true)
			tlsOptions(&proxy, options, "sni", true)
		}
	default:
		return nil, fmt.Errorf(msgNodeType, kind)
	}
	return &proxy, nil
}
func loonWireguard(name string, args []string) (*Proxy, error) {
	_, options := splitOptions(args)
	text := strings.Trim(strings.TrimSpace(strings.Trim(strings.TrimSpace(options["peers"]), "[]")), "{}")
	_, peer := splitOptions(splitArgs(text, ','))
	proxy, err := wireguardBase(name, peer["endpoint"], options["private-key"], peer["public-key"])
	if err != nil {
		return nil, err
	}
	for key, target := range map[string]string{"interface-ip": "ip", "interface-ipv6": "ipv6"} {
		if options[key] != "" {
			proxy.set(target, options[key])
		}
	}
	dns := []string{}
	for _, key := range []string{"dns", "dnsv6"} {
		if options[key] != "" {
			dns = append(dns, options[key])
		}
	}
	if len(dns) > 0 {
		proxy.set("dns", dns)
	}
	if options["mtu"] != "" {
		proxy.set("mtu", integer(options["mtu"]))
	}
	if options["keepalive"] != "" {
		proxy.set("persistent-keepalive", integer(options["keepalive"]))
	}
	applyWireguardPeer(proxy, peer)
	if reserved := strings.Trim(peer["reserved"], "[]"); reserved != "" {
		out := []int{}
		for _, part := range strings.Split(reserved, ",") {
			out = append(out, integerOr(part, 0))
		}
		proxy.set("reserved", out)
	}
	return proxy, nil
}
