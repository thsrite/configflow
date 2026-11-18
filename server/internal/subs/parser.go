package subs

import (
	"strings"

	"configflow/internal/jsonx"
)

// getOr is dict.get(key, fallback): the fallback only for a missing key.
func getOr(o *jsonx.Object, key string, fallback any) any {
	if v, ok := o.Get(key); ok {
		return v
	}
	return fallback
}

// ParseURIList is parse_uri_list: share links, YAML/JSON proxy objects.
func ParseURIList(content string) []*jsonx.Object {
	content = pyStrip(content)
	head := content
	if r := []rune(content); len(r) > 50 {
		head = string(r[:50])
	}
	if strings.HasPrefix(content, "{") || strings.HasPrefix(content, "name:") ||
		strings.HasPrefix(content, "type:") || strings.Contains(head, "name:") {
		if node := ParseYAMLObject(content); node != nil {
			return []*jsonx.Object{node}
		}
	}
	if strings.HasPrefix(content, "- name:") || strings.HasPrefix(content, "- type:") {
		if data, err := readSubscriptionYAML(content, false); err == nil {
			if list, ok := data.([]any); ok {
				nodes := []*jsonx.Object{}
				for _, item := range list {
					if obj, ok := item.(*jsonx.Object); ok {
						obj.Set("_raw_object", true)
						nodes = append(nodes, obj)
					}
				}
				return nodes
			}
		}
	}
	nodes := []*jsonx.Object{}
	for _, line := range strings.Split(content, "\n") {
		line = pyStrip(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "- ") {
			line = pyStrip(line[2:])
		}
		var node *jsonx.Object
		var err error
		switch {
		case strings.HasPrefix(line, "ss://"):
			node, err = parseShadowsocks(line)
		case strings.HasPrefix(line, "ssr://"):
			node, err = parseShadowsocksR(line)
		case strings.HasPrefix(line, "vmess://"):
			node, err = parseVMess(line)
		case strings.HasPrefix(line, "vless://"):
			node, err = parseVless(line)
		case strings.HasPrefix(line, "trojan://"):
			node, err = parseTrojan(line)
		case strings.HasPrefix(line, "hysteria://") || strings.HasPrefix(line, "hysteria2://"):
			node, err = parseHysteria(line)
		case strings.HasPrefix(line, "{") && strings.HasSuffix(line, "}"):
			node = ParseYAMLObject(line)
		default:
			continue
		}
		if err == nil && node != nil {
			nodes = append(nodes, node)
		}
	}
	return nodes
}

// ParseYAMLObject is parse_yaml_object: a JSON or YAML mapping marked with
// `_raw_object: true`, or nil.
func ParseYAMLObject(line string) *jsonx.Object {
	if v, err := readSubscriptionJSON(line); err == nil {
		if obj, ok := v.(*jsonx.Object); ok {
			obj.Set("_raw_object", true)
			return obj
		}
	}
	v, err := readSubscriptionYAML(line, false)
	if err != nil {
		return nil
	}
	obj, ok := v.(*jsonx.Object)
	if !ok {
		return nil
	}
	obj.Set("_raw_object", true)
	return obj
}

func mkNode(name any, typ, server string, port jsonx.Number, params *jsonx.Object) *jsonx.Object {
	return jsonx.Obj("name", name, "type", typ, "server", server, "port", port, "params", params)
}

func parseShadowsocks(uri string) (*jsonx.Object, error) {
	uri = uri[len("ss://"):]
	if !strings.Contains(uri, "@") {
		return nil, nil
	}
	name := "SS Node"
	if u, n, ok := strings.Cut(uri, "#"); ok {
		uri, name = u, n
	}
	var method, password, serverInfo string
	if strings.Count(uri, "@") == 1 {
		var userInfo string
		userInfo, serverInfo, _ = strings.Cut(uri, "@")
		ok := false
		if decoded, err := decodeSubscriptionText(userInfo); err == nil {
			if m, p, found := strings.Cut(decoded, ":"); found {
				method, password, ok = m, p, true
			}
		}
		if !ok {
			var err error
			if method, password, err = unpack2(userInfo, ":"); err != nil {
				return nil, err
			}
		}
	} else {
		decoded, err := decodeSubscriptionText(uri)
		if err != nil {
			return nil, err
		}
		userInfo, si, err := unpack2(decoded, "@")
		if err != nil {
			return nil, err
		}
		serverInfo = si
		if method, password, err = unpack2(userInfo, ":"); err != nil {
			return nil, err
		}
		if s, n, ok := strings.Cut(serverInfo, "#"); ok {
			serverInfo, name = s, n
		}
	}
	server, port, err := unpack2(serverInfo, ":")
	if err != nil {
		return nil, err
	}
	portNumber, err := pyIntStr(port)
	if err != nil {
		return nil, err
	}
	return mkNode(name, "ss", server, portNumber, jsonx.Obj("cipher", method, "password", password)), nil
}

func parseShadowsocksR(uri string) (*jsonx.Object, error) {
	uri = uri[len("ssr://"):]
	decoded, err := decodeSubscriptionText(uri)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(decoded, "/")
	mainPart := parts[0]
	paramsPart := ""
	if len(parts) > 1 {
		paramsPart = parts[1]
	}
	fields := strings.SplitN(mainPart, ":", 6)
	if len(fields) != 6 {
		return nil, errValue
	}
	server, port, protocol, method, obfs, passwordB64 := fields[0], fields[1], fields[2], fields[3], fields[4], fields[5]
	password, err := decodeSubscriptionText(passwordB64)
	if err != nil {
		return nil, err
	}
	params := map[string]string{}
	if _, query, ok := strings.Cut(paramsPart, "?"); ok {
		for _, item := range strings.Split(query, "&") {
			if key, value, ok := strings.Cut(item, "="); ok {
				if params[key], err = decodeSubscriptionText(value); err != nil {
					return nil, err
				}
			}
		}
	}
	pick := func(key, fallback string) string {
		if v, ok := params[key]; ok {
			return v
		}
		return fallback
	}
	portNumber, err := pyIntStr(port)
	if err != nil {
		return nil, err
	}
	return mkNode(pick("remarks", "SSR Node"), "ssr", server, portNumber, jsonx.Obj(
		"cipher", method,
		"password", password,
		"protocol", protocol,
		"obfs", obfs,
		"protocol-param", pick("protoparam", ""),
		"obfs-param", pick("obfsparam", ""),
	)), nil
}

func parseVMess(uri string) (*jsonx.Object, error) {
	uri = uri[len("vmess://"):]
	decoded, err := decodeSubscriptionText(uri)
	if err != nil {
		return nil, err
	}
	value, err := jsonx.ParseString(decoded)
	if err != nil {
		return nil, err
	}
	data, ok := value.(*jsonx.Object)
	if !ok {
		return nil, pyErr("'" + pyTypeName(value) + "' object has no attribute 'get'")
	}
	port, err := pyInt(getOr(data, "port", jsonx.Int(0)))
	if err != nil {
		return nil, err
	}
	aid, err := pyInt(getOr(data, "aid", jsonx.Int(0)))
	if err != nil {
		return nil, err
	}
	tls, _ := getOr(data, "tls", "").(string)
	wsOpts := jsonx.NewObject()
	if net, _ := data.Get("net"); net == "ws" {
		wsOpts = jsonx.Obj("path", getOr(data, "path", "/"),
			"headers", jsonx.Obj("Host", getOr(data, "host", "")))
	}
	node := jsonx.Obj(
		"name", getOr(data, "ps", "VMess Node"),
		"type", "vmess",
		"server", getOr(data, "add", ""),
		"port", port,
		"params", jsonx.Obj(
			"uuid", getOr(data, "id", ""),
			"alterId", aid,
			"cipher", getOr(data, "scy", "auto"),
			"network", getOr(data, "net", "tcp"),
			"tls", tls == "tls" && isString(getOr(data, "tls", "")),
			"ws-opts", wsOpts,
		),
	)
	return node, nil
}

func isString(v any) bool { _, ok := v.(string); return ok }

// qsParams are the dict(parse_qs) of vless/trojan: a string for one value, a
// list of strings for repeated keys.
type qsParams map[string]any

func newQSParams(query string) qsParams {
	keys, values := ParseQS(query)
	params := qsParams{}
	for _, k := range keys {
		v := values[k]
		if len(v) == 1 {
			params[k] = v[0]
		} else {
			params[k] = jsonx.ListOf(v)
		}
	}
	return params
}

func (p qsParams) get(key string, fallback any) any {
	if v, ok := p[key]; ok {
		return v
	}
	return fallback
}

// eq is `p.get(key) == s`.
func (p qsParams) eq(key, s string) bool {
	v, ok := p[key].(string)
	return ok && v == s
}

func parseVless(uri string) (*jsonx.Object, error) {
	uri = uri[len("vless://"):]
	name := "VLESS Node"
	if u, n, ok := strings.Cut(uri, "#"); ok {
		uri, name = u, n
	}
	uuidPart, serverInfo, ok := strings.Cut(uri, "@")
	if !ok {
		return nil, nil
	}
	serverPort := serverInfo
	params := qsParams{}
	if sp, query, ok := strings.Cut(serverInfo, "?"); ok {
		serverPort = sp
		params = newQSParams(query)
	}
	i := strings.LastIndex(serverPort, ":")
	if i < 0 {
		return nil, nil
	}
	server, port := serverPort[:i], serverPort[i+1:]

	p := jsonx.Obj(
		"uuid", uuidPart,
		"encryption", params.get("encryption", "none"),
		"security", params.get("security", "none"),
		"type", params.get("type", "tcp"),
	)
	security, _ := params["security"].(string)
	if security == "tls" || security == "reality" {
		p.Set("sni", params.get("sni", ""))
		p.Set("fp", params.get("fp", ""))
	}
	if security == "reality" {
		p.Set("pbk", params.get("pbk", ""))
		p.Set("sid", params.get("sid", ""))
		p.Set("spx", params.get("spx", ""))
	}
	if security == "tls" {
		if ech := params.get("ech", ""); jsonx.Truthy(ech) {
			p.Set("ech", ech)
		}
	}
	switch {
	case params.eq("type", "ws"):
		p.Set("ws-opts", jsonx.Obj("path", params.get("path", "/"),
			"headers", jsonx.Obj("Host", params.get("host", ""))))
	case params.eq("type", "grpc"):
		p.Set("grpc-opts", jsonx.Obj("grpc-service-name", params.get("serviceName", "")))
	case params.eq("type", "tcp"):
		header := params.get("headerType", "none")
		if h, isStr := header.(string); !isStr || h != "none" {
			p.Set("network", "tcp")
			p.Set("tcp-opts", jsonx.Obj("header", jsonx.Obj("type", header)))
		}
	}
	if flow := params.get("flow", nil); jsonx.Truthy(flow) {
		p.Set("flow", flow)
	}
	if alpn := params.get("alpn", nil); jsonx.Truthy(alpn) {
		s, isStr := alpn.(string)
		if !isStr {
			return nil, pyErr("'list' object has no attribute 'split'")
		}
		p.Set("alpn", jsonx.ListOf(strings.Split(s, ",")))
	}
	portNumber, err := pyIntStr(port)
	if err != nil {
		return nil, err
	}
	return mkNode(name, "vless", server, portNumber, p), nil
}

func parseTrojan(uri string) (*jsonx.Object, error) {
	uri = uri[len("trojan://"):]
	name := "Trojan Node"
	if u, n, ok := strings.Cut(uri, "#"); ok {
		uri, name = u, n
	}
	password, serverInfo, err := unpack2(uri, "@")
	if err != nil {
		return nil, err
	}
	server, port, err := unpack2(serverInfo, ":")
	if err != nil {
		return nil, err
	}
	params := qsParams{}
	if p, query, ok := strings.Cut(port, "?"); ok {
		port = p
		params = newQSParams(query)
	}
	portNumber, err := pyIntStr(port)
	if err != nil {
		return nil, err
	}
	return mkNode(name, "trojan", server, portNumber, jsonx.Obj(
		"password", password,
		"sni", params.get("sni", ""),
		"skip-cert-verify", params.eq("allowInsecure", "true"),
	)), nil
}

func parseHysteria(uri string) (*jsonx.Object, error) {
	isV2 := strings.HasPrefix(uri, "hysteria2://")
	if isV2 {
		uri = uri[12:]
	} else {
		uri = uri[11:]
	}
	name := "Hysteria Node"
	if isV2 {
		name = "Hysteria2 Node"
	}
	if u, n, ok := strings.Cut(uri, "#"); ok {
		uri, name = u, n
	}
	auth, serverInfo := "", uri
	if a, s, ok := strings.Cut(uri, "@"); ok {
		auth, serverInfo = a, s
	}
	serverPort := serverInfo
	values := map[string][]string{}
	if sp, query, ok := strings.Cut(serverInfo, "?"); ok {
		serverPort = sp
		_, values = ParseQS(query)
	}
	first := func(key string) string {
		if v := values[key]; len(v) > 0 {
			return v[0]
		}
		return ""
	}
	server, port, err := unpack2(serverPort, ":")
	if err != nil {
		return nil, err
	}
	portNumber, err := pyIntStr(port)
	if err != nil {
		return nil, err
	}
	sni := first("peer")
	if sni == "" {
		sni = first("sni")
	}
	typ := "hysteria"
	if isV2 {
		typ = "hysteria2"
	}
	insecure := "0"
	if v := values["insecure"]; len(v) > 0 {
		insecure = v[0]
	}
	return mkNode(name, typ, server, portNumber, jsonx.Obj(
		"password", auth,
		"obfs", first("obfs"),
		"sni", sni,
		"skip-cert-verify", insecure == "1",
	)), nil
}
