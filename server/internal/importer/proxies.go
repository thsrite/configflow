package importer

import (
	"encoding/json"
	"fmt"
	"strings"
)

func newProxy(name, kind, server string, port int) Proxy {
	return Proxy{Name: name, Type: kind, Server: server, Port: jsonValue(port), Options: map[string]json.RawMessage{}}
}
func tlsOptions(proxy *Proxy, options map[string]string, sniKey string, loon bool) {
	sni := first(options["sni"], options["peer"], options["tls-name"], options["obfs-host-sni"])
	if loon {
		sni = first(options["sni"], options["tls-name"])
	}
	if sni != "" {
		proxy.set(sniKey, sni)
	}
	if _, ok := options["skip-cert-verify"]; ok {
		proxy.set("skip-cert-verify", enabled(options["skip-cert-verify"]))
	} else if !loon {
		if value, ok := options["allowinsecure"]; ok {
			proxy.set("skip-cert-verify", enabled(value))
		}
	}
	if options["alpn"] != "" {
		proxy.set("alpn", nonemptySplit(options["alpn"], ","))
	}
	if loon && options["public-key"] != "" {
		proxy.set("reality-opts", map[string]string{"public-key": options["public-key"], "short-id": options["short-id"]})
	}
}
func nonemptySplit(text, separator string) []string {
	out := []string{}
	for _, part := range strings.Split(text, separator) {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
func surgeTransport(proxy *Proxy, options map[string]string) {
	obfs := strings.ToLower(options["obfs"])
	if enabled(options["ws"]) || obfs == "websocket" || obfs == "ws" {
		headers := map[string]string{}
		for _, part := range strings.Split(options["ws-headers"], "|") {
			key, value, ok := strings.Cut(part, ":")
			if key = strings.TrimSpace(key); ok && key != "" {
				headers[key] = unquote(value)
			}
		}
		if options["obfs-host"] != "" && headers["Host"] == "" {
			headers["Host"] = options["obfs-host"]
		}
		ws := struct {
			Path    string            `json:"path"`
			Headers map[string]string `json:"headers,omitempty"`
		}{first(options["ws-path"], options["path"], options["obfs-path"], "/"), headers}
		proxy.set("network", "ws")
		proxy.set("ws-opts", ws)
	} else if obfs == "grpc" {
		proxy.set("network", "grpc")
		if options["path"] != "" {
			proxy.set("grpc-opts", map[string]string{"grpc-service-name": options["path"]})
		}
	}
}
func loonTransport(proxy *Proxy, options map[string]string) error {
	transport := strings.ToLower(option(options, "transport", "tcp"))
	switch transport {
	case "ws":
		optionsOut := struct {
			Path    string            `json:"path"`
			Headers map[string]string `json:"headers,omitempty"`
		}{Path: first(options["path"], "/")}
		if options["host"] != "" {
			optionsOut.Headers = map[string]string{"Host": options["host"]}
		}
		proxy.set("network", "ws")
		proxy.set("ws-opts", optionsOut)
	case "http":
		optionsOut := struct {
			Path    []string            `json:"path"`
			Headers map[string][]string `json:"headers,omitempty"`
		}{Path: []string{first(options["path"], "/")}}
		if options["host"] != "" {
			optionsOut.Headers = map[string][]string{"Host": {options["host"]}}
		}
		proxy.set("network", "http")
		proxy.set("http-opts", optionsOut)
	case "", "tcp":
	default:
		return fmt.Errorf(msgTransport, transport)
	}
	return nil
}
func parseSurgeProxy(name, value string, sections sections, plan *Plan) (*Proxy, error) {
	args := splitArgs(value, ',')
	kind := strings.ToLower(strings.TrimSpace(at(args, 0)))
	if kind == "direct" || kind == "reject" || kind == "reject-tinygif" || kind == "reject-drop" {
		policy := "REJECT"
		if kind == "direct" {
			policy = "DIRECT"
		}
		plan.Aliases[name] = policy
		return nil, nil
	}
	var rest []string
	if len(args) > 0 {
		rest = args[1:]
	}
	pos, options := splitOptions(rest)
	if kind == "wireguard" {
		return surgeWireguard(name, options, sections)
	}
	if len(pos) < 2 {
		return nil, fmt.Errorf(msgMissingEndpoint)
	}
	number, err := port(pos[1])
	if err != nil {
		return nil, err
	}
	proxy := newProxy(name, kind, pos[0], number)
	extra := pos[2:]
	if enabled(option(options, "udp-relay", options["udp"])) {
		proxy.set("udp", true)
	}
	if enabled(options["tfo"]) || enabled(options["fast-open"]) {
		proxy.set("tfo", true)
	}
	switch kind {
	case "ss":
		proxy.set("cipher", first(options["encrypt-method"], options["method"], "aes-128-gcm"))
		proxy.set("password", option(options, "password", at(extra, 0)))
		obfs := strings.ToLower(options["obfs"])
		if obfs == "http" || obfs == "tls" {
			proxy.set("plugin", "obfs")
			proxy.set("plugin-opts", map[string]string{"mode": obfs, "host": options["obfs-host"]})
		} else if obfs == "websocket" || obfs == "ws" {
			proxy.set("plugin", "v2ray-plugin")
			proxy.set("plugin-opts", struct {
				Mode string `json:"mode"`
				Host string `json:"host"`
				Path string `json:"path"`
				TLS  bool   `json:"tls"`
			}{"websocket", options["obfs-host"], first(options["path"], options["obfs-uri"], "/"), enabled(options["tls"])})
		}
		if options["shadow-tls-password"] != "" {
			proxy.set("plugin", "shadow-tls")
			proxy.set("plugin-opts", struct {
				Password string `json:"password"`
				Host     string `json:"host"`
				Version  int    `json:"version"`
			}{options["shadow-tls-password"], options["shadow-tls-sni"], integerOr(options["shadow-tls-version"], 3)})
		}
	case "vmess", "vless":
		proxy.set("uuid", first(options["username"], options["password"], at(extra, 0)))
		if kind == "vmess" {
			proxy.set("cipher", first(options["method"], options["encrypt-method"], "auto"))
			proxy.set("alterId", integerOr(options["alterid"], 0))
		}
		if options["flow"] != "" || options["xtls"] != "" {
			proxy.set("flow", first(options["flow"], "xtls-rprx-vision"))
		}
		if enabled(options["tls"]) || enabled(options["over-tls"]) {
			proxy.set("tls", true)
		}
		tlsOptions(&proxy, options, "servername", false)
		if key := first(options["publickey"], options["public-key"]); key != "" {
			proxy.set("tls", true)
			proxy.set("reality-opts", map[string]string{"public-key": key, "short-id": first(options["shortid"], options["short-id"])})
		}
		surgeTransport(&proxy, options)
	case "trojan":
		proxy.set("password", option(options, "password", at(extra, 0)))
		tlsOptions(&proxy, options, "sni", false)
		surgeTransport(&proxy, options)
	case "http", "https", "socks5", "socks5-tls":
		proxy.Type = "http"
		if strings.HasPrefix(kind, "socks5") {
			proxy.Type = "socks5"
		}
		username, password := option(options, "username", at(extra, 0)), option(options, "password", at(extra, 1))
		if username != "" {
			proxy.set("username", username)
		}
		if password != "" {
			proxy.set("password", password)
		}
		if kind == "https" || kind == "socks5-tls" || enabled(options["tls"]) {
			proxy.set("tls", true)
			tlsOptions(&proxy, options, "sni", false)
		}
	case "snell":
		proxy.set("psk", options["psk"])
		proxy.set("version", integerOr(options["version"], 4))
		if enabled(options["reuse"]) {
			proxy.set("reuse", true)
		}
		if options["shadow-tls-password"] != "" {
			obfs := map[string]json.RawMessage{"mode": jsonValue("shadow-tls"), "host": jsonValue(options["shadow-tls-sni"]), "password": jsonValue(options["shadow-tls-password"])}
			if options["shadow-tls-version"] != "" {
				obfs["version"] = jsonValue(integerOr(options["shadow-tls-version"], 3))
			}
			proxy.set("obfs-opts", obfs)
		} else if options["obfs"] != "" {
			proxy.set("obfs-opts", map[string]string{"mode": options["obfs"], "host": options["obfs-host"]})
		}
	case "hysteria2", "hy2":
		proxy.Type = "hysteria2"
		proxy.set("password", option(options, "password", at(extra, 0)))
		tlsOptions(&proxy, options, "sni", false)
		if options["download-bandwidth"] != "" {
			proxy.set("down", options["download-bandwidth"]+" Mbps")
		}
		if password := first(options["obfs-password"], options["salamander-password"]); password != "" {
			proxy.set("obfs", "salamander")
			proxy.set("obfs-password", password)
		}
		if options["port-hopping"] != "" {
			proxy.set("ports", strings.ReplaceAll(options["port-hopping"], ";", ","))
		}
		proxy.set("udp", true)
	case "tuic", "tuic-v5":
		proxy.Type = "tuic"
		for _, key := range []string{"uuid", "password", "token"} {
			if options[key] != "" {
				proxy.set(key, options[key])
			}
		}
		tlsOptions(&proxy, options, "sni", false)
		if proxy.Options["alpn"] == nil {
			proxy.set("alpn", []string{"h3"})
		}
		proxy.set("udp", true)
	case "anytls":
		proxy.set("password", option(options, "password", at(extra, 0)))
		tlsOptions(&proxy, options, "sni", false)
	default:
		return nil, fmt.Errorf(msgNodeType, kind)
	}
	return &proxy, nil
}
func surgeWireguard(name string, options map[string]string, sections sections) (*Proxy, error) {
	section := options["section-name"]
	lines, exists := sections.find("WireGuard " + section)
	if !exists {
		return nil, fmt.Errorf(msgMissingWireGuard, section)
	}
	fields := map[string]string{}
	for _, line := range contentLines(lines) {
		key, value, ok := strings.Cut(line, "=")
		if ok {
			fields[strings.ToLower(strings.TrimSpace(key))] = strings.TrimSpace(value)
		}
	}
	text := strings.TrimSpace(fields["peer"])
	if strings.HasPrefix(text, "(") && strings.HasSuffix(text, ")") {
		text = text[1 : len(text)-1]
	}
	_, peer := splitOptions(splitArgs(text, ','))
	proxy, err := wireguardBase(name, peer["endpoint"], fields["private-key"], peer["public-key"])
	if err != nil {
		return nil, err
	}
	for key, target := range map[string]string{"self-ip": "ip", "self-ip-v6": "ipv6"} {
		if fields[key] != "" {
			proxy.set(target, fields[key])
		}
	}
	if fields["dns-server"] != "" {
		proxy.set("dns", nonemptySplit(fields["dns-server"], ","))
	}
	if fields["mtu"] != "" {
		proxy.set("mtu", integer(fields["mtu"]))
	}
	applyWireguardPeer(proxy, peer)
	if peer["keepalive"] != "" {
		proxy.set("persistent-keepalive", integer(peer["keepalive"]))
	}
	if peer["client-id"] != "" {
		proxy.set("reserved", peer["client-id"])
	}
	return proxy, nil
}
func wireguardBase(name, endpoint, private, public string) (*Proxy, error) {
	index := strings.LastIndex(endpoint, ":")
	host, number := "", endpoint
	if index >= 0 {
		host, number = endpoint[:index], endpoint[index+1:]
	} else {
		number = endpoint
	}
	port, err := port(number)
	if err != nil {
		return nil, err
	}
	proxy := newProxy(name, "wireguard", strings.Trim(host, "[]"), port)
	proxy.set("private-key", private)
	proxy.set("public-key", public)
	proxy.set("udp", true)
	return &proxy, nil
}
func applyWireguardPeer(proxy *Proxy, peer map[string]string) {
	if peer["allowed-ips"] != "" {
		proxy.set("allowed-ips", nonemptySplit(peer["allowed-ips"], ","))
	}
	if peer["preshared-key"] != "" {
		proxy.set("pre-shared-key", peer["preshared-key"])
	}
}
