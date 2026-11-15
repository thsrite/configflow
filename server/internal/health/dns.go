// Package health is C16's repository-facing half: node latency through the
// bundled node-probe worker (backend/utils/node_latency.py), the global
// node-test DNS settings and their check (backend/utils/node_dns.py), the
// locations of the per-Agent domain probe and region check stores, and the
// wiring (Agent delete hooks). The pure parts are in internal/health/check.
package health

import (
	"context"
	"math/big"
	"net/netip"
	"regexp"
	"strings"

	"configflow/internal/jsonx"
	"configflow/internal/pystr"
	"configflow/internal/repo"
	"configflow/internal/rules"
	"configflow/internal/urlx"
)

// ConfigKey is node_dns.CONFIG_KEY.
const ConfigKey = "node_test_dns_config"

// DirectDoH is node_dns.DIRECT_DOH (IP-form DoH on port 443: not taken over
// by a gateway's DNS hijack, no bootstrap lookup).
var DirectDoH = []string{"https://223.5.5.5/dns-query", "https://1.12.12.12/dns-query"}

const (
	DNSError       = "测速 DNS 须为 IPv4[:端口]、https://DoH 或 tls://DoT，最多 4 个；不支持凭据、查询参数或片段"
	BootstrapError = "引导 DNS 须为 IPv4 或 IPv4:端口，最多 4 个"
)

// CheckMessages is CHECK_MESSAGES.
var CheckMessages = map[string]string{
	"config":        "DNS 设置无效，请检查地址和引导 DNS",
	"dns_bootstrap": "引导 DNS 无法解析上游地址，请检查引导 DNS",
	"dns_fake_ip":   "DNS 返回了 Fake-IP，请改用返回真实 IP 的解析路径",
	"timeout":       "DNS 查询超时，请检查网络和服务状态",
	"tls":           "DNS TLS 验证失败，请检查上游地址和证书",
	"dns":           "DNS 未返回有效地址，请检查解析路径",
}

func valueError(message string) error { return &ValueError{message} }

func usableIPv4(addr netip.Addr) bool {
	return !addr.IsUnspecified() && !addr.IsMulticast()
}

func isASCIIDecimal(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// ipv4Server is _ipv4_server(value, message).
func ipv4Server(value any, message string) (string, error) {
	text, ok := value.(string)
	if !ok {
		return "", valueError(message)
	}
	host, port, separator := strings.Cut(pystr.Strip(text), ":")
	address, ok := parseIPv4(host)
	if !ok || !usableIPv4(address) {
		return "", valueError(message)
	}
	var portValue *big.Int
	if separator {
		// port.isascii() and port.isdecimal(): ASCII digits only.
		if !isASCIIDecimal(port) {
			return "", valueError(message)
		}
		portValue, _ = new(big.Int).SetString(port, 10)
		if portValue.Cmp(big.NewInt(1)) < 0 || portValue.Cmp(big.NewInt(65535)) > 0 {
			return "", valueError(message)
		}
	}
	out := address.String()
	if separator && portValue.Int64() != 53 {
		out += ":" + portValue.String()
	}
	return out, nil
}

var (
	dnsLabel     = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
	invalidChars = "?#\\"
)

// badPercentEscape is re.search(r'%(?![0-9a-fA-F]{2})', path).
func badPercentEscape(path string) bool {
	for i := 0; i < len(path); i++ {
		if path[i] != '%' {
			continue
		}
		if i+2 >= len(path) {
			return true
		}
		if !isHex(path[i+1]) || !isHex(path[i+2]) {
			return true
		}
	}
	return false
}

func isHex(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

// server is _server(value).
func server(value any) (string, error) {
	text, ok := value.(string)
	if !ok || pystr.Strip(text) == "" {
		return "", valueError(DNSError)
	}
	text = pystr.Strip(text)
	if !strings.Contains(text, "://") {
		return ipv4Server(text, DNSError)
	}
	if out, ok := encryptedServer(text); ok {
		return out, nil
	}
	return "", valueError(DNSError)
}

func encryptedServer(text string) (string, bool) {
	if len([]rune(text)) > 2048 || strings.ContainsAny(text, invalidChars) {
		return "", false
	}
	for _, r := range text {
		if pystr.IsSpace(r) || r < 32 || r == 127 {
			return "", false
		}
	}
	parsed, err := urlx.Split(text)
	if err != nil {
		return "", false
	}
	_, hasUser, _, hasPassword := userInfo(parsed.Netloc)
	if (parsed.Scheme != "https" && parsed.Scheme != "tls") || parsed.Hostname() == "" || hasUser || hasPassword {
		return "", false
	}
	encoded, ok := rules.IDNAEncode(parsed.Hostname())
	if !ok {
		return "", false
	}
	host := strings.ToLower(encoded)
	if address, _, isIP := ipAddress(host); isIP {
		if !address.Is4() || !usableIPv4(address) {
			return "", false
		}
	} else {
		if len(host) > 253 {
			return "", false
		}
		for _, label := range strings.Split(strings.TrimSuffix(host, "."), ".") {
			if !dnsLabel.MatchString(label) {
				return "", false
			}
		}
	}
	if badPercentEscape(parsed.Path) {
		return "", false
	}
	port, hasPort, err := parsed.Port()
	if err != nil || (hasPort && (port < 1 || port > 65535)) || strings.HasSuffix(parsed.Netloc, ":") {
		return "", false
	}
	if parsed.Scheme == "tls" && parsed.Path != "" {
		return "", false
	}
	defaultPort := 853
	if parsed.Scheme == "https" {
		defaultPort = 443
	}
	authority := host
	if hasPort && port != defaultPort {
		authority += ":" + jsonx.Int(port).String()
	}
	path := ""
	if parsed.Scheme == "https" {
		path = parsed.Path
		if path == "" {
			path = "/dns-query"
		}
	}
	return urlx.Unsplit(parsed.Scheme, authority, path, "", ""), true
}

// userInfo is SplitResult.username / .password of a netloc.
func userInfo(netloc string) (username string, hasUsername bool, password string, hasPassword bool) {
	at := strings.LastIndex(netloc, "@")
	if at < 0 {
		return "", false, "", false
	}
	username, password, hasPassword = strings.Cut(netloc[:at], ":")
	return username, true, password, hasPassword
}

// NormalizeDNSServers is normalize_dns_servers(value, bootstrap=…): at most
// four canonical addresses, duplicates removed.
func NormalizeDNSServers(value any, bootstrap bool) ([]any, error) {
	message := DNSError
	if bootstrap {
		message = BootstrapError
	}
	list, ok := value.([]any)
	if !ok || len(list) > 4 {
		return nil, valueError(message)
	}
	result := []any{}
	for _, entry := range list {
		var normalized string
		var err error
		if bootstrap {
			normalized, err = ipv4Server(entry, message)
		} else {
			normalized, err = server(entry)
		}
		if err != nil {
			return nil, err
		}
		seen := false
		for _, kept := range result {
			if kept == normalized {
				seen = true
				break
			}
		}
		if !seen {
			result = append(result, normalized)
		}
	}
	return result, nil
}

// needsBootstrap is _needs_bootstrap(server).
func needsBootstrap(server string) bool {
	if !strings.Contains(server, "://") {
		return false
	}
	parsed, err := urlx.Split(server)
	if err != nil {
		return true
	}
	_, ok := parseIPv4(parsed.Hostname())
	return !ok
}

// NormalizeConfig is normalize_config(value).
func NormalizeConfig(value any) (*jsonx.Object, error) {
	object, ok := value.(*jsonx.Object)
	mode := object.Value("mode")
	if !ok || object == nil || !(jsonx.Equal(mode, "system") || jsonx.Equal(mode, "custom")) {
		return nil, valueError("请选择系统 DNS 或自定义 DNS")
	}
	result := jsonx.Obj("mode", mode, "servers", []any{}, "bootstrap", []any{})
	if jsonx.Equal(mode, "custom") {
		servers, err := NormalizeDNSServers(object.GetOr("servers", []any{}), false)
		if err != nil {
			return nil, err
		}
		result.Set("servers", servers)
		bootstrap, err := NormalizeDNSServers(object.GetOr("bootstrap", []any{}), true)
		if err != nil {
			return nil, err
		}
		result.Set("bootstrap", bootstrap)
		if len(servers) == 0 {
			return nil, valueError("自定义 DNS 至少需要一个服务器地址")
		}
		needs := false
		for _, entry := range servers {
			if needsBootstrap(entry.(string)) {
				needs = true
			}
		}
		if needs && len(bootstrap) == 0 {
			return nil, valueError("域名形式的 DoH / DoT 需要指定引导 DNS（IPv4 或 IPv4:端口）")
		}
	}
	return result, nil
}

// LoadConfig is load_config(): the saved setting; before it existed (or
// with the removed "managed" mode) the legacy node_test_dns list, read
// without rewriting anything.
func LoadConfig(ctx context.Context) (*jsonx.Object, error) {
	system, err := repo.GetSystemConfig(ctx)
	if err != nil {
		return nil, err
	}
	settings := system.GetOr("system_config", jsonx.NewObject())
	settingsObject, ok := settings.(*jsonx.Object)
	if !ok || settingsObject == nil {
		return nil, &pyTypeError{"'" + typeName(settings) + "' object has no attribute 'get'"}
	}
	saved := settingsObject.Value(ConfigKey)
	if savedObject, ok := saved.(*jsonx.Object); ok && savedObject != nil && !jsonx.Equal(savedObject.Value("mode"), "managed") {
		return NormalizeConfig(savedObject)
	}
	servers, err := NormalizeDNSServers(settingsObject.GetOr("node_test_dns", []any{}), false)
	if err != nil {
		return nil, err
	}
	mode := "system"
	if len(servers) > 0 {
		mode = "custom"
	}
	return NormalizeConfig(jsonx.Obj("mode", mode, "servers", servers))
}

// configOf is `load_config() if value is None else normalize_config(value)`.
func configOf(ctx context.Context, value any) (*jsonx.Object, error) {
	if value == nil {
		return LoadConfig(ctx)
	}
	return NormalizeConfig(value)
}

// EffectiveSettings is effective_settings(value): dns_servers and
// dns_bootstrap; value nil reads the saved setting.
func EffectiveSettings(ctx context.Context, value any) (*jsonx.Object, error) {
	config, err := configOf(ctx, value)
	if err != nil {
		return nil, err
	}
	return jsonx.Obj("dns_servers", config.Value("servers"), "dns_bootstrap", config.Value("bootstrap")), nil
}

// DescribeSettings is describe_settings(value).
func DescribeSettings(ctx context.Context, value any) (*jsonx.Object, error) {
	config, err := configOf(ctx, value)
	if err != nil {
		return nil, err
	}
	effective, err := EffectiveSettings(ctx, config)
	if err != nil {
		return nil, err
	}
	out := jsonx.Obj("dns", config)
	out.Update(effective)
	return out, nil
}

// SaveConfig is save_config(value): stores the normalized setting and the
// node_test_dns mirror in system_config.
func SaveConfig(ctx context.Context, value any) (*jsonx.Object, error) {
	config, err := NormalizeConfig(value)
	if err != nil {
		return nil, err
	}
	effective, err := EffectiveSettings(ctx, config)
	if err != nil {
		return nil, err
	}
	repository, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	_, err = repository.UpdateSystemTransaction(ctx, func(_ context.Context, system *jsonx.Object) (*jsonx.Object, error) {
		target := system.SetDefault("system_config", jsonx.NewObject())
		targetObject, ok := target.(*jsonx.Object)
		if !ok || targetObject == nil {
			return nil, &pyTypeError{"'" + typeName(target) + "' object does not support item assignment"}
		}
		targetObject.Set(ConfigKey, config.DeepCopy())
		targetObject.Set("node_test_dns", jsonx.DeepCopy(effective.Value("dns_servers")))
		return nil, nil
	})
	if err != nil {
		return nil, err
	}
	repo.ResetConfigContext(ctx)
	out := jsonx.Obj("dns", config)
	out.Update(effective)
	return out, nil
}

// CheckSettings is check_settings(value): one query of a fixed public name
// through the worker's resolver with the given (unsaved) setting.
func CheckSettings(ctx context.Context, value any) (*jsonx.Object, error) {
	env := currentEnv
	options, err := EffectiveSettings(ctx, value)
	if err != nil {
		return nil, err
	}
	payload := jsonx.Obj("operation", "dns_check", "timeout_ms", jsonx.Int(5000))
	payload.Update(options)
	data, err := jsonx.Default(payload)
	if err != nil {
		return nil, err
	}
	binary, err := env.probeBinary()
	if err != nil {
		return nil, err
	}
	process, err := env.Runner.Run(ctx, Command{Argv: []string{binary}, Stdin: data, Timeout: checkTimeout, Env: env.workerEnv()})
	if err == ErrTimeout {
		return jsonx.Obj("success", false, "addresses", []any{}, "error_code", "timeout", "message", CheckMessages["timeout"]), nil
	}
	if err != nil {
		return nil, &ProbeUnavailable{"测速组件无法启动，请检查可执行权限和 CPU 架构"}
	}
	stdout, err := decodeText(process.Stdout)
	if err != nil {
		return nil, err
	}
	if _, err := decodeText(process.Stderr); err != nil {
		return nil, err
	}
	if process.ReturnCode != 0 {
		return nil, &ProbeUnavailable{"DNS 检测组件异常退出，请检查组件版本后重试"}
	}
	result, err := parseCheck(stdout)
	if err != nil {
		if _, ok := err.(*ProbeUnavailable); ok {
			return nil, err
		}
		return nil, &ProbeUnavailable{"DNS 检测组件返回了无效结果，请检查组件版本"}
	}
	return result, nil
}

var fakeIPNet = netip.MustParsePrefix("198.18.0.0/15")

// parseCheck validates the worker's dns_check answer; any non-nil error
// that is not a *ProbeUnavailable is Python's ValueError/TypeError/KeyError/
// AttributeError.
func parseCheck(stdout string) (*jsonx.Object, error) {
	parsed, err := jsonx.Parse([]byte(stdout))
	if err != nil {
		return nil, err
	}
	version, err := subscript(parsed, "version")
	if err != nil {
		return nil, err
	}
	if !jsonx.Equal(version, jsonx.Int(4)) {
		return nil, &ProbeUnavailable{versionMismatch}
	}
	resultValue, err := subscript(parsed, "dns_check")
	if err != nil {
		return nil, err
	}
	result, ok := resultValue.(*jsonx.Object)
	if !ok || result == nil {
		return nil, &pyTypeError{"no attribute 'get'"}
	}
	success, isBool := result.Value("success").(bool)
	addressList, isList := result.Value("addresses").([]any)
	if !isBool || !isList {
		return nil, valueError("")
	}
	for _, address := range addressList {
		if _, ok := address.(string); !ok {
			return nil, valueError("")
		}
	}
	addresses := []any{}
	var parsedAddresses []netip.Addr
	for _, address := range addressList {
		addr, scope, ok := ipAddress(address.(string))
		if !ok {
			return nil, valueError("")
		}
		text := ipString(addr, scope)
		parsedAddresses = append(parsedAddresses, addr)
		duplicate := false
		for _, kept := range addresses {
			if kept == text {
				duplicate = true
			}
		}
		if !duplicate {
			addresses = append(addresses, text)
		}
	}
	latency := result.Value("latency_ms")
	if latency != nil {
		number, ok := latency.(jsonx.Number)
		if !ok || !number.IsInt() || strings.HasPrefix(number.String(), "-") {
			return nil, valueError("")
		}
	}
	if success {
		if len(addresses) == 0 {
			return nil, valueError("")
		}
		for _, address := range addresses {
			addr, _, _ := ipAddress(address.(string))
			if addr.Is4() && fakeIPNet.Contains(addr) {
				return nil, valueError("")
			}
		}
	}
	code := result.GetOr("error_code", "dns")
	switch code.(type) {
	case []any, *jsonx.Object:
		return nil, &pyTypeError{"unhashable type"}
	}
	codeText, isString := code.(string)
	if _, known := CheckMessages[codeText]; !known || !isString {
		codeText = "dns"
	}
	out := jsonx.Obj("success", success, "addresses", addresses, "latency_ms", latency)
	if success {
		out.Set("message", "解析成功，返回真实 IP")
	} else {
		out.Set("error_code", codeText)
		out.Set("message", CheckMessages[codeText])
	}
	return out, nil
}

const versionMismatch = "测速组件版本不匹配，请更新 ConfigFlow 镜像；源码部署请重新构建 backend/node-probe"

// subscript is value[key] of a parsed JSON document: KeyError/TypeError as
// errors.
func subscript(value any, key string) (any, error) {
	object, ok := value.(*jsonx.Object)
	if !ok || object == nil {
		return nil, &pyTypeError{"not subscriptable by str"}
	}
	found, ok := object.Get(key)
	if !ok {
		return nil, &pyTypeError{"KeyError " + key}
	}
	return found, nil
}

type pyTypeError struct{ message string }

func (e *pyTypeError) Error() string { return e.message }

func typeName(v any) string {
	switch v.(type) {
	case nil:
		return "NoneType"
	case bool:
		return "bool"
	case string:
		return "str"
	case []any:
		return "list"
	case *jsonx.Object:
		return "dict"
	case jsonx.Number:
		if v.(jsonx.Number).IsInt() {
			return "int"
		}
		return "float"
	}
	return "object"
}
