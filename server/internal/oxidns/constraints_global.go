package oxidns

import (
	"net/netip"
	"regexp"
	"strconv"
	"strings"

	"configflow/internal/weburl"
)

// Cross-field rules of OxiDNS 1.6.1's config and network deserializers. Only
// errors are reported; nothing is rewritten.

func deferredEnv(n *Node) bool { return isStr(n) && anyEnv(n.Value) }

// upstreamHost is (scheme, host) of an upstream address; ok false when it
// cannot be read or is deferred.
func upstreamHost(value *Node) (string, string) {
	if !isStr(value) || deferredEnv(value) {
		return "", ""
	}
	raw := value.Value
	if !strings.Contains(raw, "//") {
		raw = "udp://" + raw
	}
	u, err := weburl.Parse(raw)
	if err != nil {
		return "", ""
	}
	return strings.ToLower(u.Scheme), weburl.Hostname(u)
}

func literalIP(host string) bool {
	_, err := netip.ParseAddr(host)
	return err == nil
}

var (
	bootstrap6 = regexp.MustCompile(`^\[([^\]]+)\]:([0-9]+)$`)
	bootstrap4 = regexp.MustCompile(`^([^:]+):([0-9]+)$`)
)

// bootstrapSocket: legacy bootstrap is a SocketAddr (no :port shorthand).
func bootstrapSocket(value *Node) bool {
	if !isStr(value) {
		return false
	}
	text := strings.TrimSpace(value.Value)
	for _, form := range []struct {
		re *regexp.Regexp
		v6 bool
	}{{bootstrap6, true}, {bootstrap4, false}} {
		if m := form.re.FindStringSubmatch(text); m != nil {
			addr, err := netip.ParseAddr(m[1])
			port, perr := strconv.Atoi(m[2])
			return err == nil && addr.Is6() == form.v6 && (perr == nil && port <= 65535)
		}
		if form.v6 && strings.HasPrefix(text, "[") {
			continue
		}
	}
	return false
}

func collectGlobalIssues(document *nativeDoc) []Issue {
	var issues []Issue
	issue := func(at Path, message, code string) {
		issues = append(issues, Issue{Path: at, Message: message, Code: code, Severity: "error"})
	}
	nonblank := func(value *Node, at Path) {
		if isStr(value) && !deferredEnv(value) && strings.TrimSpace(value.Value) == "" {
			issue(at, "必须填写非空文本", "constraint")
		}
	}
	tlsPair := func(config *Node, at Path, required bool) {
		cert, key := get(config, "cert"), get(config, "key")
		if !required && isNone(cert) && isNone(key) {
			return
		}
		for _, item := range []struct {
			name  string
			value *Node
		}{{"cert", cert}, {"key", key}} {
			if isNone(item.value) {
				issue(at.with(item.name), "启用 TLS 时必须同时配置证书和私钥", "constraint")
			} else {
				nonblank(item.value, at.with(item.name))
			}
		}
	}
	if !isMap(document) {
		return issues
	}
	level := get(mapOr(get(document, "log")), "level")
	if isStr(level) && !deferredEnv(level) && !containsString([]string{"off", "trace", "debug", "info", "warn", "error"}, strings.ToLower(level.Value)) {
		issue(P("log", "level"), "日志级别必须为 off、trace、debug、info、warn 或 error", "enum")
	}
	ssl := mapOr(get(mapOr(get(mapOr(get(document, "api")), "http")), "ssl"))
	tlsPair(ssl, P("api", "http", "ssl"), false)
	if isTrue(get(ssl, "require_client_cert")) && isNone(get(ssl, "client_ca")) {
		issue(P("api", "http", "ssl", "client_ca"), "要求客户端证书时必须配置 client_ca", "constraint")
	}
	if !isNone(get(ssl, "client_ca")) {
		nonblank(get(ssl, "client_ca"), P("api", "http", "ssl", "client_ca"))
	}
	outbound := mapOr(get(mapOr(get(document, "network")), "outbound"))
	profiles := mapOr(get(outbound, "profiles"))
	profileReference := func(value *Node, at Path, strictName, allowEmpty bool) {
		if !isStr(value) || deferredEnv(value) {
			return
		}
		name := strings.TrimSpace(value.Value)
		if name == "" {
			if !allowEmpty {
				issue(at, "出站配置名称不能为空", "constraint")
			}
			return
		}
		if strictName && name != value.Value {
			issue(at, "出站配置名称不能包含首尾空白", "constraint")
		}
		if !has(profiles, name) {
			issue(at, "引用的出站配置未在 network.outbound.profiles 中定义", "reference")
		}
	}
	profileReference(get(outbound, "default"), P("network", "outbound", "default"), true, false)
	pairs(profiles, func(name string, raw *Node) bool {
		at := P("network", "outbound", "profiles", name)
		if strings.TrimSpace(name) == "" || name != strings.TrimSpace(name) {
			issue(at, "出站配置名称必须为不含首尾空白的非空文本", "constraint")
		}
		profile := mapOr(raw)
		resolver, proxy := get(profile, "resolver"), get(profile, "proxy")
		if isStr(resolver) && !deferredEnv(resolver) && strings.ToLower(strings.TrimSpace(resolver.Value)) != "system" {
			issue(at.with("resolver"), "解析器简写只支持 system", "enum")
		}
		if isStr(proxy) && !deferredEnv(proxy) && !containsString([]string{"none", "direct"}, strings.ToLower(strings.TrimSpace(proxy.Value))) {
			issue(at.with("proxy"), "代理简写只支持 none 或 direct", "enum")
		}
		resolver = mapOr(resolver)
		usesProxy := equal(get(resolver, "proxy"), S("profile"))
		if usesProxy && !isStr(get(mapOr(proxy), "socks5")) {
			issue(at.with("resolver", "proxy"), "使用 profile 代理时，当前出站配置必须设置 socks5 代理", "constraint")
		}
		for index, server := range items(get(resolver, "nameservers")) {
			server = mapOr(server)
			itemAt := at.with("resolver", "nameservers", index)
			scheme, host := upstreamHost(get(server, "addr"))
			if host != "" && !literalIP(host) && isNone(get(server, "dial_addr")) {
				issue(itemAt.with("dial_addr"), "域名型 nameserver 必须配置 dial_addr，避免引导解析循环", "constraint")
			}
			if usesProxy && containsString([]string{"udp", "quic", "doq", "h3"}, scheme) {
				issue(itemAt.with("addr"), "UDP、DoQ 和 DoH3 nameserver 不支持 profile SOCKS5 代理", "constraint")
			}
		}
		return true
	})
	for index, plugin := range items(get(document, "plugins")) {
		plugin = mapOr(plugin)
		kind, args := str(get(plugin, "type")), mapOr(get(plugin, "args"))
		if !isStr(get(plugin, "type")) {
			kind = ""
		}
		at := P("plugins", index, "args")
		if kind == "tcp_server" || kind == "http_server" {
			tlsPair(args, at, kind == "http_server" && isTrue(get(args, "enable_http3")))
		}
		if kind == "http_server" {
			for offset, entry := range items(get(args, "entries")) {
				route := get(mapOr(entry), "path")
				if isStr(route) && !deferredEnv(route) && !strings.HasPrefix(route.Value, "/") {
					issue(at.with("entries", offset, "path"), "HTTP 路径必须以 / 开头", "format")
				}
			}
		}
		if containsString([]string{"download", "http_request", "upgrade", "ip_selector"}, kind) {
			profileReference(get(args, "outbound"), at.with("outbound"), false, kind != "ip_selector")
		}
		if kind != "forward" {
			continue
		}
		for offset, upstream := range items(get(args, "upstreams")) {
			upstream = mapOr(upstream)
			itemAt := at.with("upstreams", offset)
			profileReference(get(upstream, "outbound"), itemAt.with("outbound"), false, false)
			minimum, maximum := get(upstream, "min_conns"), get(upstream, "max_conns")
			if isNone(maximum) {
				maximum = I(64)
			}
			if lo, ok := intOf(minimum); ok {
				if hi, ok := intOf(maximum); ok && lo.Cmp(hi) > 0 {
					issue(itemAt.with("min_conns"), "不能大于有效 max_conns（未配置时为 64）", "constraint")
				}
			}
			_, host := upstreamHost(get(upstream, "addr"))
			bootstrap := get(upstream, "bootstrap")
			if host != "" && !literalIP(host) && isNone(get(upstream, "dial_addr")) && isStr(bootstrap) &&
				!deferredEnv(bootstrap) && !bootstrapSocket(bootstrap) {
				issue(itemAt.with("bootstrap"), "引导 DNS 必须为 IP:端口 或 [IPv6]:端口", "format")
			}
		}
	}
	return issues
}
