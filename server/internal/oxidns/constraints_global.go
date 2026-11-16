package oxidns

import (
	"strings"

	"configflow/internal/convert"
	"configflow/internal/jsonx"
	"configflow/internal/pystr"
	"configflow/internal/urlx"
)

// Port of backend/common/oxidns_global_constraints.py (module C23):
// cross-field rules of OxiDNS 1.6.1's config and network deserializers.

// envAny is the global module's _ENV.search: "${" + at least one non-"}"
// character + "}" (no "$$" exception).
func envAny(value string) bool {
	for i := 0; i+1 < len(value); i++ {
		if value[i] == '$' && value[i+1] == '{' {
			if j := strings.IndexByte(value[i+2:], '}'); j > 0 {
				return true
			}
		}
	}
	return false
}

func globalDeferred(v any) bool {
	s, ok := v.(string)
	return ok && envAny(s)
}

// mapping is _mapping(value).
func mapping(v any) any {
	if isDict(v) {
		return v
	}
	return jsonx.NewObject()
}

// globalEndpoint is _endpoint(value): (scheme.lower(), hostname) or
// ("", "") for None.
func globalEndpoint(value any) (string, string) {
	text, ok := value.(string)
	if !ok || envAny(text) {
		return "", ""
	}
	raw := text
	if !strings.Contains(text, "//") {
		raw = "udp://" + text
	}
	parts, err := urlx.Split(raw)
	if err != nil {
		return "", ""
	}
	return pystr.Lower(parts.Scheme), parts.Hostname()
}

func literalIP(value string) bool {
	_, ok := convert.ParseIPAddress(value)
	return ok
}

// portUpTo65535 is 0 <= int(port) <= 65535 for a run of decimal digits.
func portUpTo65535(port string) bool {
	if runeLen(port) > 4300 {
		return false // int() refuses more than 4300 digits
	}
	value := 0
	for _, r := range port {
		value = value*10 + isDecimalValue(r)
		if value > 65535 {
			return false
		}
	}
	return true
}

func isDecimalValue(r rune) int { return pystr.Decimal(r) }

// bootstrapSocket is _bootstrap_socket(value).
func bootstrapSocket(value any) bool {
	text, ok := value.(string)
	if !ok {
		return false
	}
	text = pystr.Strip(text)
	if strings.HasPrefix(text, "[") {
		if end := strings.LastIndex(text, "]:"); end > 1 {
			host, port := text[1:end], text[end+2:]
			if !strings.Contains(host, "]") && decimalDigits(port) {
				address, ok := convert.ParseIPAddress(host)
				return ok && !address.Is4() && portUpTo65535(port)
			}
		}
	}
	host, port, found := strings.Cut(text, ":")
	if !found || host == "" || !decimalDigits(port) {
		return false
	}
	address, ok := convert.ParseIPAddress(host)
	return ok && address.Is4() && portUpTo65535(port)
}

// CollectGlobalIssues is collect_global_issues(document): path-addressed
// issues {path, message, code} (no severity: the caller defaults it).
func CollectGlobalIssues(document any) (issues []any, err error) {
	defer catch(&err)
	return collectGlobalIssues(document), nil
}

func collectGlobalIssues(document any) []any {
	issues := []any{}
	fail := func(path []any, message, code string) {
		issues = append(issues, jsonx.Obj("path", path, "message", message, "code", code))
	}
	nonblank := func(value any, path []any) {
		if text, ok := value.(string); ok && !envAny(text) && pystr.Strip(text) == "" {
			fail(path, "必须填写非空文本", "constraint")
		}
	}
	tlsPair := func(config any, path []any, required bool) {
		cert, key := get(config, "cert", nil), get(config, "key", nil)
		if required || cert != nil || key != nil {
			if cert == nil {
				fail(appendPath(path, "cert"), "启用 TLS 时必须同时配置证书和私钥", "constraint")
			} else {
				nonblank(cert, appendPath(path, "cert"))
			}
			if key == nil {
				fail(appendPath(path, "key"), "启用 TLS 时必须同时配置证书和私钥", "constraint")
			} else {
				nonblank(key, appendPath(path, "key"))
			}
		}
	}
	if !isDict(document) {
		return issues
	}
	log := mapping(get(document, "log", nil))
	if level, ok := get(log, "level", nil).(string); ok && !envAny(level) &&
		!strIn(pystr.Lower(level), "off", "trace", "debug", "info", "warn", "error") {
		fail([]any{"log", "level"}, "日志级别必须为 off、trace、debug、info、warn 或 error", "enum")
	}
	http := mapping(get(mapping(get(document, "api", nil)), "http", nil))
	ssl := mapping(get(http, "ssl", nil))
	tlsPair(ssl, []any{"api", "http", "ssl"}, false)
	if get(ssl, "require_client_cert", nil) == true && get(ssl, "client_ca", nil) == nil {
		fail([]any{"api", "http", "ssl", "client_ca"}, "要求客户端证书时必须配置 client_ca", "constraint")
	}
	if get(ssl, "client_ca", nil) != nil {
		nonblank(get(ssl, "client_ca", nil), []any{"api", "http", "ssl", "client_ca"})
	}
	outbound := mapping(get(mapping(get(document, "network", nil)), "outbound", nil))
	profiles := mapping(get(outbound, "profiles", nil))
	profileReference := func(value any, path []any, strictName, allowEmpty bool) {
		text, ok := value.(string)
		if !ok || envAny(text) {
			return
		}
		name := pystr.Strip(text)
		if name == "" {
			if !allowEmpty {
				fail(path, "出站配置名称不能为空", "constraint")
			}
			return
		}
		if strictName && name != text {
			fail(path, "出站配置名称不能包含首尾空白", "constraint")
		}
		if !has(profiles, name) {
			fail(path, "引用的出站配置未在 network.outbound.profiles 中定义", "reference")
		}
	}
	profileReference(get(outbound, "default", nil), []any{"network", "outbound", "default"}, true, false)
	for _, nameValue := range iterate(profiles) {
		name := nameValue.(string)
		rawProfile := get(profiles, name, nil)
		path := []any{"network", "outbound", "profiles", name}
		if pystr.Strip(name) == "" || name != pystr.Strip(name) {
			fail(path, "出站配置名称必须为不含首尾空白的非空文本", "constraint")
		}
		profile := mapping(rawProfile)
		resolver, proxy := get(profile, "resolver", nil), get(profile, "proxy", nil)
		if text, ok := resolver.(string); ok && !envAny(text) && pystr.Lower(pystr.Strip(text)) != "system" {
			fail(appendPath(path, "resolver"), "解析器简写只支持 system", "enum")
		}
		if text, ok := proxy.(string); ok && !envAny(text) && !strIn(pystr.Lower(pystr.Strip(text)), "none", "direct") {
			fail(appendPath(path, "proxy"), "代理简写只支持 none 或 direct", "enum")
		}
		resolver = mapping(resolver)
		usesProxy := get(resolver, "proxy", nil) == "profile"
		if _, isText := get(mapping(proxy), "socks5", nil).(string); usesProxy && !isText {
			fail(appendPath(path, "resolver", "proxy"), "使用 profile 代理时，当前出站配置必须设置 socks5 代理", "constraint")
		}
		servers, _ := get(resolver, "nameservers", nil).([]any)
		for index, server := range servers {
			server = mapping(server)
			itemPath := appendPath(path, "resolver", "nameservers", jsonx.Int(index))
			scheme, host := globalEndpoint(get(server, "addr", nil))
			if host != "" && !literalIP(host) && get(server, "dial_addr", nil) == nil {
				fail(appendPath(itemPath, "dial_addr"), "域名型 nameserver 必须配置 dial_addr，避免引导解析循环", "constraint")
			}
			if usesProxy && strIn(scheme, "udp", "quic", "doq", "h3") {
				fail(appendPath(itemPath, "addr"), "UDP、DoQ 和 DoH3 nameserver 不支持 profile SOCKS5 代理", "constraint")
			}
		}
	}
	plugins, _ := get(document, "plugins", nil).([]any)
	for index, raw := range plugins {
		plugin := mapping(raw)
		kind, args := get(plugin, "type", nil), mapping(get(plugin, "args", nil))
		path := []any{"plugins", jsonx.Int(index), "args"}
		if strIn(kind, "tcp_server", "http_server") {
			tlsPair(args, path, kind == "http_server" && get(args, "enable_http3", nil) == true)
		}
		if kind == "http_server" {
			entries, _ := get(args, "entries", nil).([]any)
			for offset, entry := range entries {
				if route, ok := get(mapping(entry), "path", nil).(string); ok && !envAny(route) && !strings.HasPrefix(route, "/") {
					fail(appendPath(path, "entries", jsonx.Int(offset), "path"), "HTTP 路径必须以 / 开头", "format")
				}
			}
		}
		if strIn(kind, "download", "http_request", "upgrade", "ip_selector") {
			profileReference(get(args, "outbound", nil), appendPath(path, "outbound"), false, kind != "ip_selector")
		}
		if kind != "forward" {
			continue
		}
		upstreams, _ := get(args, "upstreams", nil).([]any)
		for offset, raw := range upstreams {
			upstream := mapping(raw)
			itemPath := appendPath(path, "upstreams", jsonx.Int(offset))
			profileReference(get(upstream, "outbound", nil), appendPath(itemPath, "outbound"), false, false)
			minimum, maximum := get(upstream, "min_conns", nil), get(upstream, "max_conns", nil)
			if maximum == nil {
				maximum = jsonx.Int(64)
			}
			if isInt(minimum) && isInt(maximum) && compareNumbers(minimum.(jsonx.Number), maximum.(jsonx.Number)) > 0 {
				fail(appendPath(itemPath, "min_conns"), "不能大于有效 max_conns（未配置时为 64）", "constraint")
			}
			_, host := globalEndpoint(get(upstream, "addr", nil))
			bootstrap := get(upstream, "bootstrap", nil)
			if text, ok := bootstrap.(string); ok && host != "" && !literalIP(host) && get(upstream, "dial_addr", nil) == nil &&
				!envAny(text) && !bootstrapSocket(text) {
				fail(appendPath(itemPath, "bootstrap"), "引导 DNS 必须为 IP:端口 或 [IPv6]:端口", "format")
			}
		}
	}
	return issues
}
