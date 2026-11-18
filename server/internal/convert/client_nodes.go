package convert

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"configflow/internal/model"
	"configflow/internal/yamlx"
)

// ClientSource is one parsed URI or structured engine node, with normalized
// parameter placement. The parser owner retains all parsing business.
type ClientSource struct {
	Node   model.Node
	Params *yamlx.Node
}

// ClientCollaborators are temporary typed boundaries to N8-9 subs/rules.
type ClientCollaborators struct {
	ParseNodes  func(string) ([]ClientSource, error)
	FilterNodes func([]model.Node, *string, *string) ([]model.Node, error)
}

// Check validates the complete production registration.
func (c ClientCollaborators) Check() error {
	if c.ParseNodes == nil {
		return &HookMissingError{Hook: "parse_uri_list", Provider: "subs"}
	}
	if c.FilterNodes == nil {
		return &HookMissingError{Hook: "filter_candidates", Provider: "rules"}
	}
	return nil
}

type clientNode struct {
	name, kind, server, port string
	options                  clientOptions
}

func resolveClientNode(input model.Node) (*clientNode, error) {
	node := input
	if err := clientNodeText(input, "name"); err != nil {
		return nil, err
	}
	var params *yamlx.Node
	var err error
	if input.ProxyString != nil {
		// Syntax remains the URI owner's concern. Native graph policy failures
		// cannot fall through a legacy parser that would silently drop them.
		// JSON has no aliases and owns duplicate-key/surrogate semantics. Only
		// non-JSON text is subject to the YAML graph policy before URI parsing.
		if !json.Valid([]byte(*input.ProxyString)) {
			if _, err := yamlx.Parse([]byte(*input.ProxyString)); errors.Is(err, yamlx.ErrAliasBudget) || errors.Is(err, yamlx.ErrAliasRecursion) || errors.Is(err, yamlx.ErrPolicy) {
				return nil, err
			}
		}
		parse := CurrentMihomoEnvironment().Clients.ParseNodes
		if parse == nil {
			return nil, &HookMissingError{Hook: "parse_uri_list", Provider: "subs"}
		}
		nodes, err := parse(*input.ProxyString)
		if err != nil {
			return nil, err
		}
		if len(nodes) == 0 {
			return nil, nil
		}
		node, params = nodes[0].Node, nodes[0].Params
		node.Name = input.Name
		if input.ID != "" {
			node.ID = input.ID
		}
	} else {
		params, err = node.Params.Node()
		if err != nil {
			return nil, err
		}
	}
	if err := clientNodeText(node, "type", "server"); err != nil {
		return nil, err
	}
	options, err := decodeClientOptions(params)
	if err != nil {
		return nil, err
	}
	port := fmt.Sprint(int64(node.Port))
	// Valid number spellings are retained under R2; invalid fields use R5 zero.
	if !model.IssueUnder(model.Issues(node), "port") {
		data, err := model.Marshal(node)
		if err != nil {
			return nil, err
		}
		record, err := yamlx.FromJSON(data)
		if err != nil {
			return nil, err
		}
		original := yamlx.Get(record, "port")
		if original != nil && (original.ShortTag() == "!!int" || original.ShortTag() == "!!float") {
			port = original.Value
		}
	}
	return &clientNode{node.Name, strings.ToLower(node.Type), node.Server, port, options}, nil
}
func clientQuantity(value *clientText) bool {
	if value == nil || *value == "" {
		return false
	}
	// Numeric quantities such as MTU/keepalive use zero to disable an option.
	text := string(*value)
	if number, err := strconv.ParseFloat(text, 64); err == nil && number == 0 {
		// ParseFloat owns the syntax. Inspect only the decimal significand to
		// distinguish exact zero from underflow without allocating 10^exponent.
		if end := strings.IndexAny(text, "eE"); end >= 0 {
			text = text[:end]
		}
		return strings.ContainsAny(text, "123456789")
	}
	return true
}
func clientQuote(value string) string { return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"` }
func clientValue(value string) string {
	if strings.Contains(value, ",") {
		return clientQuote(value)
	}
	return value
}
func firstClientList(values clientTextList) string {
	if len(values) > 0 {
		return string(values[0])
	}
	return ""
}
func flagText(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

// SurgeNode renders one typed node and its optional WireGuard section.
func SurgeNode(input model.Node) (string, string, error) {
	node, err := resolveClientNode(input)
	if err != nil || node == nil {
		return "", "", err
	}
	n, o := node, node.options
	if n.kind == "" {
		return "", "", nil
	}
	join := func(parts []string) (string, string, error) { return strings.Join(parts, ", "), "", nil }
	udp := clientFlag(o.UDP) || clientFlag(o.UDPRelay)
	switch n.kind {
	case "ss":
		parts := []string{n.name + " = ss", n.server, n.port, "encrypt-method=" + clientOption(o.Cipher, "aes-256-gcm"), "password=" + clientOption(o.Password, "")}
		if udp {
			parts = append(parts, "udp-relay=true")
		}
		if value := clientOption(o.TestURL, ""); value != "" {
			parts = append(parts, "test-url="+value)
		}
		return join(parts)
	case "vmess":
		parts := []string{n.name + " = vmess", n.server, n.port, "username=" + clientOption(o.UUID, "")}
		if clientFlag(o.TLS) {
			parts = append(parts, "tls=true")
		}
		if clientOption(o.Network, "tcp") == "ws" {
			path, host := "/", ""
			if o.WS != nil {
				if len(o.WS.Path) > 0 {
					path = string(o.WS.Path[0])
				}
				host = firstClientList(o.WS.Headers.Host)
			}
			parts = append(parts, "ws=true", "ws-path="+path)
			if host != "" {
				parts = append(parts, "ws-headers=Host:"+host)
			}
		}
		if udp {
			parts = append(parts, "udp-relay=true")
		}
		return join(parts)
	case "trojan", "hysteria2", "anytls":
		parts := []string{n.name + " = " + n.kind, n.server, n.port, "password=" + clientOption(o.Password, "")}
		if value := clientOption(o.SNI, ""); value != "" {
			parts = append(parts, "sni="+value)
		}
		if clientFlag(o.SkipCertVerify) {
			parts = append(parts, "skip-cert-verify=true")
		}
		if udp {
			parts = append(parts, "udp-relay=true")
		}
		return join(parts)
	case "http", "https":
		parts := []string{n.name + " = " + n.kind, n.server, n.port}
		username, password := clientOption(o.Username, ""), clientOption(o.Password, "")
		if username != "" && password != "" {
			parts = append(parts, username, password)
		}
		return join(parts)
	case "snell":
		parts := []string{n.name + " = snell", n.server, n.port, "psk=" + firstClientText(o.PSK, o.Password)}
		version := clientOption(o.Version, "4")
		if o.Version == nil || clientQuantity(o.Version) {
			parts = append(parts, "version="+version)
		}
		if clientFlag(o.Reuse) {
			parts = append(parts, "reuse=true")
		}
		if udp {
			parts = append(parts, "udp-relay=true")
		}
		mode, host := clientOption(o.Obfs, ""), clientOption(o.ObfsHost, "")
		if o.ObfsOptions != nil {
			if value := clientOption(o.ObfsOptions.Mode, ""); value != "" {
				mode = value
			}
			if value := clientOption(o.ObfsOptions.Host, ""); value != "" {
				host = value
			}
		}
		if mode == "shadow-tls" {
			password := ""
			if o.ObfsOptions != nil {
				password = clientOption(o.ObfsOptions.Password, "")
			}
			parts = append(parts, "shadow-tls-password="+password)
			if host != "" {
				parts = append(parts, "shadow-tls-sni="+host)
			}
			if o.ObfsOptions != nil && clientQuantity(o.ObfsOptions.Version) {
				parts = append(parts, "shadow-tls-version="+string(*o.ObfsOptions.Version))
			}
		} else if mode == "http" || mode == "tls" {
			parts = append(parts, "obfs="+mode)
			if host != "" {
				parts = append(parts, "obfs-host="+host)
			}
		}
		return join(parts)
	case "tuic", "tuic-v5":
		parts := []string{n.name + " = tuic-v5", n.server, n.port}
		if value := clientOption(o.UUID, ""); value != "" {
			parts = append(parts, "uuid="+value)
		}
		if password := clientOption(o.Password, ""); password != "" {
			parts = append(parts, "password="+password)
		} else if token := clientOption(o.Token, ""); token != "" {
			parts = append(parts, "token="+token)
		}
		alpn := surgeALPN(o, n.name)
		if alpn != "" {
			parts = append(parts, "alpn="+alpn)
		}
		if value := clientOption(o.SNI, ""); value != "" {
			parts = append(parts, "sni="+value)
		}
		if clientFlag(o.SkipCertVerify) {
			parts = append(parts, "skip-cert-verify=true")
		}
		if udp {
			parts = append(parts, "udp-relay=true")
		}
		return join(parts)
	case "vless":
		parts := []string{n.name + " = vmess", n.server, n.port, "username=" + clientOption(o.UUID, "")}
		if clientFlag(o.TLS) || clientOption(o.Security, "") == "tls" {
			parts = append(parts, "tls=true")
			if sni := firstClientText(o.SNI, o.ServerName); sni != "" {
				parts = append(parts, "sni="+sni)
			}
		}
		switch clientOption(o.Network, "tcp") {
		case "ws":
			transport := o.WS
			if transport == nil {
				transport = o.WSConfig
			}
			path, host := "/", clientOption(o.Host, "")
			if transport != nil {
				if value := firstClientList(transport.Path); value != "" {
					path = value
				}
				if value := firstClientList(transport.Headers.Host); value != "" {
					host = value
				}
			}
			parts = append(parts, "ws=true", "ws-path="+path)
			if host != "" {
				parts = append(parts, "ws-headers=Host:"+host)
			}
		case "grpc":
			service := clientOption(o.ServiceName, "")
			if o.GRPC != nil {
				if value := clientOption(o.GRPC.ServiceName, ""); value != "" {
					service = value
				}
			}
			if service != "" {
				parts = append(parts, "grpc-service-name="+service)
			}
		}
		if clientFlag(o.SkipCertVerify) {
			parts = append(parts, "skip-cert-verify=true")
		}
		if udp {
			parts = append(parts, "udp-relay=true")
		}
		return join(parts)
	case "wireguard", "wg":
		sectionName := clientOption(o.SectionName, strings.NewReplacer(" ", "-", "_", "-").Replace(n.name))
		parts := []string{n.name + " = wireguard", "section-name = " + sectionName}
		if clientQuantity(o.MTU) {
			parts = append(parts, "mtu="+string(*o.MTU))
		}
		section := []string{"[WireGuard " + sectionName + "]"}
		if key := firstClientText(o.PrivateKey, o.PrivateKeyAlt); key != "" {
			section = append(section, "private-key = "+key)
		}
		if address := firstClientText(o.SelfIP, o.IP, o.Address); address != "" {
			section = append(section, "self-ip = "+address)
		}
		dns := o.DNS
		if len(dns) == 0 {
			dns = o.DNSServer
		}
		if len(dns) > 0 && strings.Join(dns.strings(), "") != "" {
			section = append(section, "dns-server = "+strings.Join(dns.strings(), ", "))
		}
		if clientQuantity(o.MTU) {
			section = append(section, "mtu = "+string(*o.MTU))
		}
		if key := firstClientText(o.PublicKey, o.PublicKeyAlt, o.PeerPublicKey); key != "" {
			endpoint := clientOption(o.Endpoint, "")
			if endpoint == "" {
				endpoint = n.server + ":" + n.port
			}
			allowed := o.AllowedIPs
			if len(allowed) == 0 || strings.Join(allowed.strings(), "") == "" {
				allowed = o.AllowedIPsAlt
			}
			allowedText := "0.0.0.0/0, ::/0"
			if len(allowed) > 0 && strings.Join(allowed.strings(), "") != "" {
				allowedText = strings.Join(allowed.strings(), ", ")
			}
			peer := []string{"public-key = " + key, "endpoint = " + endpoint, `allowed-ips = "` + allowedText + `"`}
			if value := clientKeepalive(o.Keepalive, o.PersistentKeepalive); value != "" {
				peer = append(peer, "keepalive = "+value)
			}
			if value := firstClientText(o.PresharedKey, o.PresharedKeyAlt); value != "" {
				peer = append(peer, "preshared-key = "+value)
			}
			if value := clientOption(o.ClientID, ""); value != "" {
				peer = append(peer, "client-id = "+value)
			} else if o.Reserved.Text != "" {
				peer = append(peer, "client-id = "+o.Reserved.Text)
			} else if len(o.Reserved.Numbers) > 0 {
				values := make([]string, len(o.Reserved.Numbers))
				for i, n := range o.Reserved.Numbers {
					values[i] = n.String()
				}
				peer = append(peer, "client-id = "+strings.Join(values, "/"))
			}
			section = append(section, "peer = ("+strings.Join(peer, ", ")+")")
		}
		return strings.Join(parts, ", "), strings.Join(section, "\n"), nil
	}
	return "", "", nil
}

func clientTransport(o clientOptions, allowHTTP bool) []string {
	network := clientOption(o.Network, "tcp")
	if network == "" || network == "tcp" {
		return []string{"transport=tcp"}
	}
	if network == "ws" {
		path, host := clientOption(o.Path, "/"), clientOption(o.Host, "")
		if o.WS != nil {
			if v := firstClientList(o.WS.Path); v != "" {
				path = v
			}
			if v := firstClientList(o.WS.Headers.Host); v != "" {
				host = v
			}
		}
		if path == "" {
			path = "/"
		}
		parts := []string{"transport=ws", "path=" + clientValue(path)}
		if host != "" {
			parts = append(parts, "host="+clientValue(host))
		}
		return parts
	}
	if network == "http" && allowHTTP {
		path, host := "/", ""
		if o.HTTP != nil {
			if value := firstClientList(o.HTTP.Path); value != "" {
				path = value
			}
			host = firstClientList(o.HTTP.Headers.Host)
		}
		parts := []string{"transport=http", "path=" + clientValue(path)}
		if host != "" {
			parts = append(parts, "host="+clientValue(host))
		}
		return parts
	}
	return nil
}
func clientTLS(o clientOptions) []string {
	var parts []string
	if sni := firstClientText(o.ServerName, o.SNI); sni != "" {
		parts = append(parts, "sni="+clientValue(sni))
	}
	parts = append(parts, "skip-cert-verify="+flagText(clientFlag(o.SkipCertVerify)))
	if len(o.ALPN) > 0 && strings.Join(o.ALPN.strings(), "") != "" {
		parts = append(parts, "alpn="+clientQuote(strings.Join(o.ALPN.strings(), ",")))
	}
	if o.Reality != nil && clientOption(o.Reality.PublicKey, "") != "" {
		parts = append(parts, "public-key="+clientQuote(string(*o.Reality.PublicKey)))
		if value := clientOption(o.Reality.ShortID, ""); value != "" {
			parts = append(parts, "short-id="+value)
		}
	}
	return parts
}

var clientBandwidth = regexp.MustCompile(`^[0-9]+`)

// LoonNode renders one typed protocol node for a Loon Proxy section.
func LoonNode(input model.Node) (string, error) {
	node, err := resolveClientNode(input)
	if err != nil || node == nil {
		return "", err
	}
	n, o := node, node.options
	name := clientSanitizeName(n.name)
	udp := "udp=" + flagText(clientFlag(o.UDP) || clientFlag(o.UDPRelay))
	finish := func(parts []string) (string, error) { return strings.Join(parts, ","), nil }
	switch n.kind {
	case "ss":
		parts := []string{name + " = Shadowsocks", n.server, n.port, clientOption(o.Cipher, "aes-256-gcm"), clientQuote(clientOption(o.Password, ""))}
		plugin := clientOption(o.Plugin, "")
		switch plugin {
		case "obfs", "simple-obfs":
			mode, host := "http", ""
			if o.PluginOptions != nil {
				mode = clientOption(o.PluginOptions.Mode, "http")
				host = clientOption(o.PluginOptions.Host, "")
			}
			parts = append(parts, "obfs-name="+mode)
			if host != "" {
				parts = append(parts, "obfs-host="+clientValue(host))
			}
		case "shadow-tls":
			password, host, version := "", "", "3"
			if o.PluginOptions != nil {
				password = clientOption(o.PluginOptions.Password, "")
				host = clientOption(o.PluginOptions.Host, "")
				version = clientOption(o.PluginOptions.Version, "3")
			}
			parts = append(parts, "shadow-tls-password="+clientValue(password))
			if host != "" {
				parts = append(parts, "shadow-tls-sni="+clientValue(host))
			}
			parts = append(parts, "shadow-tls-version="+version)
		case "":
		default:
			return "", nil
		}
		if clientFlag(o.UDPOverTCP) {
			parts = append(parts, "udp-over-tcp=true")
		}
		return finish(append(parts, "fast-open="+flagText(clientFlag(o.TFO)), udp))
	case "ssr":
		parts := []string{name + " = ShadowsocksR", n.server, n.port, clientOption(o.Cipher, ""), clientQuote(clientOption(o.Password, "")), "protocol=" + clientOption(o.Protocol, "origin")}
		if value := clientOption(o.ProtocolParam, ""); value != "" {
			parts = append(parts, "protocol-param="+clientValue(value))
		}
		parts = append(parts, "obfs="+clientOption(o.Obfs, "plain"))
		if value := clientOption(o.ObfsParam, ""); value != "" {
			parts = append(parts, "obfs-param="+clientValue(value))
		}
		return finish(append(parts, "fast-open="+flagText(clientFlag(o.TFO)), udp))
	case "vmess", "vless", "trojan":
		transport := clientTransport(o, n.kind != "trojan")
		if transport == nil {
			return "", nil
		}
		var parts []string
		tls := clientFlag(o.TLS)
		switch n.kind {
		case "vmess":
			cipher := clientOption(o.Cipher, "auto")
			if !slices.Contains(LoonVmessCiphers, cipher) {
				cipher = "auto"
			}
			parts = []string{name + " = VMess", n.server, n.port, cipher, clientQuote(clientOption(o.UUID, ""))}
			parts = append(parts, transport...)
			parts = append(parts, "alterId="+clientOption(o.AlterID, "0"))
		case "vless":
			flow := clientOption(o.Flow, "")
			if flow != "" && flow != "xtls-rprx-vision" {
				return "", nil
			}
			tls = tls || slices.Contains([]string{"tls", "reality"}, clientOption(o.Security, "")) || (o.Reality != nil && (clientOption(o.Reality.PublicKey, "") != "" || clientOption(o.Reality.ShortID, "") != "" || o.Reality.Fields.Len() > 0))
			parts = []string{name + " = VLESS", n.server, n.port, clientQuote(clientOption(o.UUID, ""))}
			parts = append(parts, transport...)
			if flow != "" {
				parts = append(parts, "flow="+flow)
			}
		case "trojan":
			parts = []string{name + " = Trojan", n.server, n.port, clientQuote(clientOption(o.Password, ""))}
			parts = append(parts, transport...)
		}
		if n.kind != "trojan" {
			parts = append(parts, "over-tls="+flagText(tls))
		}
		if tls || n.kind == "trojan" {
			parts = append(parts, clientTLS(o)...)
		}
		if n.kind == "trojan" {
			parts = append(parts, "fast-open="+flagText(clientFlag(o.TFO)))
		}
		return finish(append(parts, udp))
	case "hysteria2":
		parts := []string{name + " = Hysteria2", n.server, n.port, clientQuote(clientOption(o.Password, ""))}
		parts = append(parts, clientTLS(o)...)
		if clientOption(o.Obfs, "") == "salamander" && clientOption(o.ObfsPassword, "") != "" {
			parts = append(parts, "salamander-password="+clientValue(string(*o.ObfsPassword)))
		}
		if ports := clientOption(o.Ports, ""); ports != "" {
			parts = append(parts, "server-ports="+clientQuote(ports))
			if clientQuantity(o.HopInterval) {
				parts = append(parts, "hop-interval="+string(*o.HopInterval))
			}
		}
		if down := clientOption(o.Down, ""); down != "" {
			if value := clientBandwidth.FindString(strings.TrimLeftFunc(down, unicode.IsSpace)); value != "" {
				parts = append(parts, "download-bandwidth="+value)
			}
		}
		return finish(append(parts, "fast-open="+flagText(clientFlag(o.TFO)), "udp=true"))
	case "anytls":
		parts := []string{name + " = AnyTLS", n.server, n.port, clientQuote(clientOption(o.Password, ""))}
		parts = append(parts, clientTLS(o)...)
		if clientQuantity(o.IdleSessionTimeout) {
			parts = append(parts, "idle-session-timeout="+string(*o.IdleSessionTimeout))
		}
		return finish(append(parts, udp))
	case "http", "https", "socks5":
		tls := n.kind == "https" || clientFlag(o.TLS)
		scheme := n.kind
		if scheme != "socks5" {
			scheme = "http"
			if tls {
				scheme = "https"
			}
		}
		parts := []string{name + " = " + scheme, n.server, n.port}
		username, password := clientOption(o.Username, ""), clientOption(o.Password, "")
		if username != "" || password != "" {
			parts = append(parts, clientValue(username), clientQuote(password))
		}
		if n.kind == "socks5" {
			parts = append(parts, "over-tls="+flagText(tls))
		}
		if tls {
			parts = append(parts, clientTLS(o)...)
		}
		if n.kind == "socks5" {
			parts = append(parts, udp)
		}
		return finish(parts)
	case "wireguard", "wg":
		private, public := firstClientText(o.PrivateKey, o.PrivateKeyAlt), firstClientText(o.PublicKey, o.PublicKeyAlt, o.PeerPublicKey)
		if private == "" || public == "" {
			return "", nil
		}
		parts := []string{name + " = WireGuard"}
		if address := firstClientText(o.IP, o.SelfIP, o.Address); address != "" {
			before, _, _ := strings.Cut(address, "/")
			parts = append(parts, "interface-ip="+before)
		}
		if address := clientOption(o.IPv6, ""); address != "" {
			before, _, _ := strings.Cut(address, "/")
			parts = append(parts, "interface-ipv6="+before)
		}
		parts = append(parts, "private-key="+clientQuote(private))
		if clientQuantity(o.MTU) {
			parts = append(parts, "mtu="+string(*o.MTU))
		}
		dns := o.DNS.strings()
		if len(dns) == 1 {
			dns = strings.Split(dns[0], ",")
			for i := range dns {
				dns[i] = strings.TrimSpace(dns[i])
			}
		}
		for _, address := range dns {
			if address != "" && !strings.Contains(address, ":") {
				parts = append(parts, "dns="+address)
				break
			}
		}
		for _, address := range dns {
			if strings.Contains(address, ":") {
				parts = append(parts, "dnsv6="+address)
				break
			}
		}
		if value := clientKeepalive(o.PersistentKeepalive, o.Keepalive); value != "" {
			parts = append(parts, "keepalive="+value)
		}
		allowed := o.AllowedIPs
		if len(allowed) == 0 || strings.Join(allowed.strings(), "") == "" {
			allowed = o.AllowedIPsAlt
		}
		allowedText := "0.0.0.0/0"
		if len(allowed) > 0 && strings.Join(allowed.strings(), "") != "" {
			allowedText = strings.Join(allowed.strings(), ",")
		}
		endpoint := clientOption(o.Endpoint, "")
		if endpoint == "" {
			endpoint = n.server + ":" + n.port
			if strings.Contains(n.server, ":") {
				endpoint = "[" + n.server + "]:" + n.port
			}
		}
		peer := []string{"public-key=" + clientQuote(public)}
		if value := firstClientText(o.PreSharedKey, o.PresharedKey, o.PresharedKeyAlt); value != "" {
			peer = append(peer, "preshared-key="+clientQuote(value))
		}
		if len(o.Reserved.Numbers) > 0 {
			values := make([]string, len(o.Reserved.Numbers))
			for i, n := range o.Reserved.Numbers {
				values[i] = n.String()
			}
			peer = append(peer, "reserved=["+strings.Join(values, ",")+"]")
		}
		peer = append(peer, "allowed-ips="+clientQuote(allowedText), "endpoint="+endpoint)
		parts = append(parts, "peers=[{"+strings.Join(peer, ",")+"}]")
		enabledUDP := o.UDP == nil || *o.UDP
		return finish(append(parts, "udp="+flagText(enabledUDP)))
	}
	return "", nil
}
func clientSanitizeName(text string) string {
	return strings.TrimSpace(strings.NewReplacer(",", " ", "=", " ").Replace(text))
}

func clientKeepalive(values ...*clientText) string {
	for _, value := range values {
		if clientQuantity(value) {
			return string(*value)
		}
	}
	return ""
}

func clientNodeText(node model.Node, members ...string) error {
	for _, member := range members {
		if err := storedText(&node, kindNode, node.ID, member); err != nil {
			return err
		}
	}
	return nil
}
