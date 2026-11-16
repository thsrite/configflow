package convert

import (
	"math/big"
	"net/netip"
	"strings"

	"configflow/internal/jsonx"
	"configflow/internal/pystr"
	"configflow/internal/weburl"
	"configflow/internal/yamlx"
)

// Port of backend/common/mihomo_base_settings.py (C18): the server-side
// checks of the Mihomo base settings form, takeover_mode and agent_context.

// Base settings constants (module level names of mihomo_base_settings.py).
var (
	BaseInboundPortKeys = []string{"mixed-port", "port", "socks-port", "redir-port", "tproxy-port"}
	LogLevels           = []string{"silent", "error", "warning", "info", "debug"}
	FindProcessModes    = []string{"always", "strict", "off"}
	FakeIPFilterModes   = []string{"blacklist", "whitelist", "rule"}
	TunStacks           = []string{"system", "gvisor", "mixed"}
	CacheAlgorithms     = []string{"lru", "arc"}
	SniffProtocols      = []string{"HTTP", "TLS", "QUIC"}
	DNSListKeys         = []string{"default-nameserver", "nameserver", "proxy-server-nameserver", "direct-nameserver", "fallback"}
)

const (
	// RemovedFingerprint is REMOVED_FINGERPRINT.
	RemovedFingerprint = "global-client-fingerprint 已在 Mihomo v1.19.27 移除，写了也不生效，加载时还会报错；" +
		"请删除这一项，需要时在节点上设置 client-fingerprint"
	// SniffPortsHint is SNIFF_PORTS_HINT.
	SniffPortsHint = "端口写成 80 或 8080-8880，多个用逗号分隔，范围 1–65535"
)

// DNSUpstreamPaths is DNS_UPSTREAM_PATHS.
var DNSUpstreamPaths = func() [][]string {
	var out [][]string
	for _, k := range append(append([]string(nil), DNSListKeys...), "nameserver-policy", "proxy-server-nameserver-policy") {
		out = append(out, []string{"dns", k})
	}
	return out
}()

type labelled struct {
	path  []string
	label string
}

var baseBooleans = []labelled{
	{[]string{"allow-lan"}, "allow-lan"},
	{[]string{"unified-delay"}, "unified-delay"},
	{[]string{"tcp-concurrent"}, "tcp-concurrent"},
	{[]string{"disable-keep-alive"}, "disable-keep-alive"},
	{[]string{"profile", "store-selected"}, "profile.store-selected"},
	{[]string{"profile", "store-fake-ip"}, "profile.store-fake-ip"},
	{[]string{"sniffer", "enable"}, "sniffer.enable"},
	{[]string{"dns", "respect-rules"}, "dns.respect-rules"},
	{[]string{"dns", "direct-nameserver-follow-policy"}, "dns.direct-nameserver-follow-policy"},
	{[]string{"dns", "prefer-h3"}, "dns.prefer-h3"},
}

type integerSetting struct {
	path      []string
	label     string
	low, high int64
}

var baseIntegers = func() []integerSetting {
	var out []integerSetting
	for _, k := range BaseInboundPortKeys {
		out = append(out, integerSetting{[]string{k}, k, 1, 65535})
	}
	return append(out,
		integerSetting{[]string{"tun", "mtu"}, "tun.mtu", 576, 65535},
		integerSetting{[]string{"keep-alive-idle"}, "keep-alive-idle", 1, 86400},
		integerSetting{[]string{"keep-alive-interval"}, "keep-alive-interval", 1, 86400},
	)
}()

type enumSetting struct {
	path    []string
	label   string
	options []string
}

var baseEnums = []enumSetting{
	{[]string{"log-level"}, "log-level", LogLevels},
	{[]string{"find-process-mode"}, "find-process-mode", FindProcessModes},
	{[]string{"dns", "fake-ip-filter-mode"}, "dns.fake-ip-filter-mode", FakeIPFilterModes},
	{[]string{"tun", "stack"}, "tun.stack", TunStacks},
	{[]string{"dns", "cache-algorithm"}, "dns.cache-algorithm", CacheAlgorithms},
}

var baseStringLists = func() []labelled {
	var out []labelled
	for _, k := range DNSListKeys {
		out = append(out, labelled{[]string{"dns", k}, "dns." + k})
	}
	return append(out,
		labelled{[]string{"dns", "fake-ip-filter"}, "dns.fake-ip-filter"},
		labelled{[]string{"sniffer", "skip-domain"}, "sniffer.skip-domain"},
		labelled{[]string{"sniffer", "force-domain"}, "sniffer.force-domain"},
	)
}()

var baseTexts = []labelled{
	{[]string{"external-ui"}, "external-ui"},
	{[]string{"global-ua"}, "global-ua"},
}

// BaseSettingsError is mihomo_base_settings.BaseSettingsError (a ValueError).
type BaseSettingsError struct{ Message string }

func (e *BaseSettingsError) Error() string { return e.Message }

func baseError(message string) { raise(&BaseSettingsError{message}) }

// missing is _MISSING.
type missingValue struct{}

var missing any = missingValue{}

func getPath(document any, path []string) any {
	value := document
	for _, k := range path {
		obj, ok := value.(*jsonx.Object)
		if !ok || obj == nil {
			return missing
		}
		next, present := obj.Get(k)
		if !present {
			return missing
		}
		value = next
	}
	return value
}

// exactType is type(v) for _same: int and float, date and datetime and
// bool are distinct types.
func exactType(v any) string {
	switch x := v.(type) {
	case missingValue:
		return "missing"
	case yamlx.Timestamp:
		if x.HasTime {
			return "datetime"
		}
		return "date"
	case []byte:
		return "bytes"
	}
	return pyTypeName(v)
}

// same is _same: equal values of the same Python types (True is not 1, a
// quoted "7890" is not 7890; dict key order does not matter).
func same(left, right any) bool {
	if exactType(left) != exactType(right) {
		return false
	}
	switch l := left.(type) {
	case missingValue:
		return true
	case []any:
		r := right.([]any)
		if len(l) != len(r) {
			return false
		}
		for i := range l {
			if !same(l[i], r[i]) {
				return false
			}
		}
		return true
	case *jsonx.Object:
		r := right.(*jsonx.Object)
		if l.Len() != r.Len() {
			return false
		}
		for k := range l.All() {
			if !r.Has(k) {
				return false
			}
		}
		for k, v := range l.All() {
			if !same(v, r.Value(k)) {
				return false
			}
		}
		return true
	case yamlx.Timestamp:
		r := right.(yamlx.Timestamp)
		if l.HasZone != r.HasZone {
			return false // naive == aware is False
		}
		return l.Time().Equal(r.Time())
	case []byte:
		return string(l) == string(right.([]byte))
	case jsonx.Number:
		r := right.(jsonx.Number)
		if l.IsFloat() {
			return l.Float64() == r.Float64()
		}
		return jsonx.Equal(l, r)
	}
	return jsonx.Equal(left, right)
}

// SplitHostPort is split_host_port: (host, port) of `host:port`, brackets
// removed; ok false when malformed. A port of Unicode digits that int()
// rejects is Python's ValueError (err).
func SplitHostPort(value any) (host string, port int, ok bool, err error) {
	defer catch(&err)
	host, port, ok = splitHostPort(value)
	return
}

func splitHostPort(value any) (string, int, bool) {
	text, isString := value.(string)
	if !isString {
		return "", 0, false
	}
	stripped := pystr.Strip(text)
	i := strings.LastIndexByte(stripped, ':')
	if i < 0 {
		return "", 0, false
	}
	host, port := stripped[:i], stripped[i+1:]
	if !pyIsDigit(port) {
		return "", 0, false
	}
	n := pyIntOf(port)
	value64, fits := n.Int64()
	if !fits || value64 < 1 || value64 > 65535 {
		return "", 0, false
	}
	if strings.HasPrefix(host, "[") {
		if !strings.HasSuffix(host, "]") {
			return "", 0, false
		}
		host = host[1 : len(host)-1]
	} else if strings.Contains(host, ":") {
		return "", 0, false
	}
	return host, int(value64), true
}

// ControllerScope is controller_scope: "lan" for a wildcard listener,
// "local" for loopback, otherwise "other".
func ControllerScope(host string) string {
	if host == "" || host == "0.0.0.0" || host == "::" {
		return "lan"
	}
	if pystr.Lower(host) == "localhost" {
		return "local"
	}
	addr, ok := pyIPAddress(host)
	if !ok {
		return "other"
	}
	// Python 3.11.17's IPv6Address.is_loopback also checks an IPv4-mapped address.
	plain := addr.WithZone("")
	if plain.Is4In6() {
		plain = plain.Unmap()
	}
	if plain.Is4() && plain.As4()[0] == 127 || plain == netip.IPv6Loopback() {
		return "local"
	}
	return "other"
}

// pyIPAddress is ipaddress.ip_address(text) for a string (zone kept).
func pyIPAddress(text string) (netip.Addr, bool) {
	if addr, ok := parseIPv4(text); ok {
		return addr, true
	}
	addr, ok := parseIPv6(text)
	if !ok {
		return netip.Addr{}, false
	}
	if i := strings.IndexByte(text, '%'); i >= 0 {
		addr = addr.WithZone(text[i+1:])
	}
	return addr, true
}

// pyIPAddressValue is ipaddress.ip_address(value) for any value; ok false
// for Python's TypeError/ValueError.
func pyIPAddressValue(value any) (netip.Addr, bool) {
	switch x := value.(type) {
	case string:
		return pyIPAddress(x)
	case bool:
		if x {
			return netip.AddrFrom4([4]byte{0, 0, 0, 1}), true
		}
		return netip.AddrFrom4([4]byte{}), true
	case jsonx.Number:
		if !x.IsInt() {
			return netip.Addr{}, false
		}
		n, _ := x.BigInt()
		if n.Sign() < 0 {
			return netip.Addr{}, false
		}
		if n.BitLen() <= 32 {
			v := n.Uint64()
			return netip.AddrFrom4([4]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}), true
		}
		if n.BitLen() <= 128 {
			var b [16]byte
			n.FillBytes(b[:])
			return netip.AddrFrom16(b), true
		}
	}
	return netip.Addr{}, false
}

func listenPort(value any) int {
	_, port, ok := splitHostPort(value)
	if !ok {
		return 0
	}
	return port
}

type portOwner struct {
	port  string // canonical int text
	owner string
}

func isExactInt(v any) (jsonx.Number, bool) {
	n, ok := v.(jsonx.Number)
	return n, ok && n.IsInt()
}

func intBetween(n jsonx.Number, low, high int64) bool {
	v, ok := n.BigInt()
	if !ok {
		return false
	}
	return v.Cmp(big.NewInt(low)) >= 0 && v.Cmp(big.NewInt(high)) <= 0
}

func portOwners(document *jsonx.Object) []portOwner {
	var owners []portOwner
	for _, k := range BaseInboundPortKeys {
		if n, ok := isExactInt(document.Value(k)); ok && intBetween(n, 1, 65535) {
			owners = append(owners, portOwner{n.String(), k})
		}
	}
	if port := listenPort(document.Value("external-controller")); port != 0 {
		owners = append(owners, portOwner{jsonx.Int(port).String(), "external-controller"})
	}
	if dns, ok := document.Value("dns").(*jsonx.Object); ok && dns.GetOr("enable", true) != false {
		if port := listenPort(dns.Value("listen")); port != 0 {
			owners = append(owners, portOwner{jsonx.Int(port).String(), "dns.listen"})
		}
	}
	if listeners, ok := document.Value("listeners").([]any); ok {
		for _, item := range listeners {
			obj, ok := item.(*jsonx.Object)
			if !ok || jsonx.Equal(obj.Value("name"), RegionProbeListener) {
				continue
			}
			n, ok := isExactInt(obj.Value("port"))
			if !ok {
				continue
			}
			name := obj.Value("name")
			if !jsonx.Truthy(name) {
				name = "未命名"
			}
			owners = append(owners, portOwner{n.String(), "listeners（" + fstr(name) + "）"})
		}
	}
	return owners
}

func checkTypes(document *jsonx.Object, changed func([]string) bool) {
	for _, b := range baseBooleans {
		value := getPath(document, b.path)
		if _, isBool := value.(bool); changed(b.path) && value != missing && !isBool {
			baseError(b.label + " 只能填写 true 或 false，不要加引号")
		}
	}
	for _, s := range baseIntegers {
		value := getPath(document, s.path)
		if changed(s.path) && value != missing {
			if n, ok := isExactInt(value); !ok || !intBetween(n, s.low, s.high) {
				baseError(s.label + " 需为 " + jsonx.Int(s.low).String() + "–" + jsonx.Int(s.high).String() + " 的整数")
			}
		}
	}
	fingerprint := []string{"global-client-fingerprint"}
	if changed(fingerprint) && getPath(document, fingerprint) != missing {
		baseError(RemovedFingerprint)
	}
	for _, e := range baseEnums {
		value := getPath(document, e.path)
		if !changed(e.path) || value == missing {
			continue
		}
		if _, isBool := value.(bool); isBool && e.path[0] == "find-process-mode" {
			continue
		}
		text, ok := value.(string)
		if !ok || !inStrings(pystr.Lower(pystr.Strip(text)), e.options...) {
			baseError(e.label + " 只能是 " + strings.Join(e.options, " / ") + " 之一")
		}
	}
	for _, l := range baseStringLists {
		value := getPath(document, l.path)
		if changed(l.path) && value != missing && value != nil {
			list, ok := value.([]any)
			valid := ok
			if ok {
				for _, item := range list {
					s, isString := item.(string)
					if !isString || pystr.Strip(s) == "" {
						valid = false
						break
					}
				}
			}
			if !valid {
				baseError(l.label + " 需为文本列表，每行一项，不能有空项")
			}
		}
	}
	for _, t := range baseTexts {
		value := getPath(document, t.path)
		if changed(t.path) && value != missing {
			s, ok := value.(string)
			if !ok || pystr.Strip(s) == "" {
				baseError(t.label + " 需为非空文本；不需要时删除该项")
			}
		}
	}
}

// SniffPortError is sniff_port_error: true unless value is a port (80) or a
// range ("8080-8880") of ports 1–65535.
func SniffPortError(value any) (bad bool, err error) {
	defer catch(&err)
	return sniffPortError(value), nil
}

func sniffPortError(value any) bool {
	if n, ok := isExactInt(value); ok {
		return !intBetween(n, 1, 65535)
	}
	text, ok := value.(string)
	if !ok {
		return true
	}
	low, high, separator := strings.Cut(pystr.Strip(text), "-")
	low = pystr.Strip(low)
	if separator {
		high = pystr.Strip(high)
	} else {
		high = low
	}
	if !pyIsDigit(low) || !pyIsDigit(high) {
		return true
	}
	l, _ := pyIntOf(low).BigInt()
	h, _ := pyIntOf(high).BigInt()
	return !(l.Cmp(big.NewInt(1)) >= 0 && l.Cmp(h) <= 0 && h.Cmp(big.NewInt(65535)) <= 0)
}

func checkFakeRange(document *jsonx.Object, changed func([]string) bool) {
	path := []string{"dns", "fake-ip-range"}
	value := getPath(document, path)
	if !changed(path) || value == missing {
		return
	}
	invalid := false
	if text, ok := value.(string); !ok {
		invalid = true
	} else {
		hook := currentHooks().FakeIPNetworkError
		if hook == nil {
			raise(&HookMissingError{Hook: "fake_ip_network", Provider: gatewayProvider})
		}
		invalid = hook(text) != nil
	}
	if invalid {
		baseError("dns.fake-ip-range 需为 IPv4 网段（/8 至 /30），例如 198.18.0.1/16，" +
			"不能是回环、多播或链路本地地址")
	}
}

func checkSniff(document *jsonx.Object, changed func([]string) bool) {
	sniff := getPath(document, []string{"sniffer", "sniff"})
	if sniff == missing || sniff == nil {
		return
	}
	obj, ok := sniff.(*jsonx.Object)
	if !ok {
		if changed([]string{"sniffer", "sniff"}) {
			baseError("sniffer.sniff 需为对象，按协议 HTTP / TLS / QUIC 分别填写")
		}
		return
	}
	for k, item := range obj.All() {
		if !inStrings(pystr.Upper(k), SniffProtocols...) {
			if changed([]string{"sniffer", "sniff"}) {
				baseError("sniffer.sniff 只支持 HTTP / TLS / QUIC，「" + k + "」无法识别，Mihomo 会拒绝加载")
			}
			continue
		}
		if !changed([]string{"sniffer", "sniff", k}) {
			continue
		}
		var ports any
		itemObj, isObj := item.(*jsonx.Object)
		if isObj {
			ports = itemObj.Value("ports")
		}
		list, isList := ports.([]any)
		bad := !isList || len(list) == 0
		if !bad {
			for _, port := range list {
				if sniffPortError(port) {
					bad = true
					break
				}
			}
		}
		if bad {
			baseError("sniffer.sniff." + k + ".ports：" + SniffPortsHint)
		}
		override := missing
		if isObj {
			if v, present := itemObj.Get("override-destination"); present {
				override = v
			}
		}
		if _, isBool := override.(bool); override != missing && !isBool {
			baseError("sniffer.sniff." + k + ".override-destination 只能填写 true 或 false，不要加引号")
		}
	}
}

func checkController(document *jsonx.Object, changed func([]string) bool) {
	controllerPath := []string{"external-controller"}
	secretPath := []string{"secret"}
	controller := getPath(document, controllerPath)
	exposed := false
	if controller != missing && controller != nil {
		host, _, parsed := splitHostPort(controller)
		if changed(controllerPath) && !parsed {
			baseError("external-controller 需写成「地址:端口」，例如 0.0.0.0:9090 或 127.0.0.1:9090，端口 1–65535")
		}
		scope := "other"
		if parsed {
			scope = ControllerScope(host)
		}
		if changed(controllerPath) && scope == "other" {
			baseError("external-controller 只能监听 0.0.0.0 / ::（局域网可访问）或 127.0.0.1 / ::1（仅本机）：" +
				"Agent 经本机回环地址读取连接、流量并热重载配置，监听其他地址时这些功能不可用")
		}
		exposed = scope != "local"
	}
	secret := getPath(document, secretPath)
	if _, isString := secret.(string); secret != missing && secret != nil && !isString {
		if changed(secretPath) {
			baseError("控制面板密码（secret）必须是文本，纯数字等请加引号")
		}
		secret = missing
	}
	if !(changed(secretPath) || changed(controllerPath)) {
		return
	}
	empty := secret == missing || secret == nil || secret == ""
	if exposed {
		if empty {
			baseError("控制面板对局域网开放（external-controller 监听所有地址）时必须设置控制面板密码（secret）")
		}
		if message := controllerSecretError(secret); message != "" {
			baseError(message)
		}
	} else if changed(secretPath) && !empty {
		if message := controllerSecretError(secret); message != "" {
			baseError(message)
		}
	}
}

func checkAccess(document *jsonx.Object, changed func([]string) bool) {
	urlPath := []string{"external-ui-url"}
	url := getPath(document, urlPath)
	if changed(urlPath) && url != missing {
		valid := false
		if text, ok := url.(string); ok {
			if parts, err := weburl.Parse(text); err == nil {
				valid = (parts.Scheme == "http" || parts.Scheme == "https") && parts.Hostname() != ""
			}
		}
		if !valid {
			baseError("external-ui-url 需为 http(s) 下载地址，例如 https://github.com/…/gh-pages.zip")
		}
	}
	bindPath := []string{"bind-address"}
	bind := getPath(document, bindPath)
	if changed(bindPath) && bind != missing {
		text := ""
		if s, ok := bind.(string); ok {
			text = strings.Trim(pystr.Strip(s), "[]")
		}
		valid := text == "*"
		if !valid {
			_, valid = pyIPAddress(text)
		}
		if !valid {
			baseError("bind-address 只能填写 *（所有地址）或本机的一个 IP 地址")
		}
	}
}

func checkPorts(document *jsonx.Object, changed func([]string) bool, reserved map[int]string) {
	paths := [][]string{}
	for _, k := range BaseInboundPortKeys {
		paths = append(paths, []string{k})
	}
	paths = append(paths, []string{"external-controller"}, []string{"dns", "listen"}, []string{"dns", "enable"}, []string{"listeners"})
	pathChanged := false
	for _, p := range paths {
		if changed(p) {
			pathChanged = true
			break
		}
	}
	if !pathChanged {
		return
	}
	seen := map[string]string{}
	for _, item := range portOwners(document) {
		if owner, ok := seen[item.port]; ok {
			baseError("端口 " + item.port + " 同时被 " + owner + " 和 " + item.owner + " 使用，请改用不同端口")
		}
		seen[item.port] = item.owner
		n, _ := jsonx.ParseNumber(item.port)
		if v, ok := n.Int64(); ok {
			if description, ok := reserved[int(v)]; ok {
				baseError(item.owner + " 使用端口 " + item.port + "，与" + description + "冲突，请改用其他端口")
			}
		}
	}
}

func checkDNS(document *jsonx.Object, changed func([]string) bool, hostAddresses []netip.Addr) {
	dns, ok := document.Value("dns").(*jsonx.Object)
	if !ok {
		return
	}
	upstreamChanged := false
	for _, p := range DNSUpstreamPaths {
		if changed(p) {
			upstreamChanged = true
			break
		}
	}
	if upstreamChanged {
		hook := currentHooks().MihomoDNSLoop
		if hook == nil {
			raise(&HookMissingError{Hook: "mihomo_dns_loop", Provider: onboardingProvide})
		}
		k, upstream, found, err := hook(dns, hostAddresses)
		must(err)
		if found {
			baseError("dns." + k + " 中的 " + fstr(upstream) + " 指向本机或系统 DNS，会形成解析循环；" +
				"请改用外部 DNS，例如 https://223.5.5.5/dns-query")
		}
	}
	if changed([]string{"dns", "nameserver"}) && dns.Value("enable") == true && !jsonx.Truthy(dns.Value("nameserver")) {
		baseError("dns.nameserver 至少填写一个上游")
	}
	if (changed([]string{"dns", "respect-rules"}) || changed([]string{"dns", "proxy-server-nameserver"})) &&
		dns.Value("respect-rules") == true && !jsonx.Truthy(dns.Value("proxy-server-nameserver")) {
		baseError("开启 dns.respect-rules 时必须填写 dns.proxy-server-nameserver，否则 Mihomo 无法启动")
	}
}

// ValidateBaseSettings is validate_base_settings(document, previous,
// reserved_ports=, local_addresses=): *BaseSettingsError for a value this
// save changes that the form would reject. previous nil (or not a dict)
// checks every value. reserved maps Agent ports to their description;
// localAddresses are values passed to ipaddress.ip_address (invalid ones are
// skipped).
func ValidateBaseSettings(document, previous any, reserved map[int]string, localAddresses []any) (err error) {
	defer catch(&err)
	doc, ok := document.(*jsonx.Object)
	if !ok || doc == nil {
		return nil
	}
	prev, _ := previous.(*jsonx.Object)
	changed := func(path []string) bool {
		return prev == nil || !same(getPath(doc, path), getPath(prev, path))
	}
	var hosts []netip.Addr
	seen := map[netip.Addr]bool{}
	for _, value := range localAddresses {
		if addr, ok := pyIPAddressValue(value); ok && !seen[addr] {
			seen[addr] = true
			hosts = append(hosts, addr)
		}
	}
	checkTypes(doc, changed)
	checkFakeRange(doc, changed)
	checkSniff(doc, changed)
	checkController(doc, changed)
	checkAccess(doc, changed)
	if reserved == nil {
		reserved = map[int]string{}
	}
	checkPorts(doc, changed, reserved)
	checkDNS(doc, changed, hosts)
	return nil
}

// TakeoverMode is takeover_mode(config): who rewrites the TUN on publish —
// "linux_gateway", "full_tun" (router split) or "" (None). A malformed
// gateway setting is ignored (generation reports it); a missing hook fails
// closed.
func TakeoverMode(config *jsonx.Object) (string, error) {
	enabled, err := gatewayEnabled(config)
	if _, missingHook := err.(*HookMissingError); missingHook {
		return "", err
	}
	if err == nil && enabled {
		return "linux_gateway", nil
	}
	full, err := isFullTun(config)
	if err != nil {
		return "", err
	}
	if full {
		return "full_tun", nil
	}
	return "", nil
}

// AgentSource is what agent_context reads: the repository's profile Agent
// bindings and the Agent manager's records.
type AgentSource interface {
	// ProfileAgentIDs is [item['id'] for item in
	// repository.get_profile_agent_bindings(profile_id)['agents']]; an error
	// makes agent_context return nothing (Python catches every exception).
	ProfileAgentIDs(profileID string) ([]any, error)
	// AgentByID is manager.get_agent_by_id(id); nil for None.
	AgentByID(id any) *jsonx.Object
}

// AgentContext is agent_context(repository, manager, profile_id): the ports
// (port → description) and host addresses of the Agents next to this
// profile's Mihomo. Docker Agents are skipped. Port keys are the int ports
// that fit an int; hosts keep Python's values (strings).
func AgentContext(source AgentSource, profileID string) (reserved map[int]string, order []int, hosts []any) {
	reserved = map[int]string{}
	ids, err := source.ProfileAgentIDs(profileID)
	if err != nil {
		return map[int]string{}, nil, []any{}
	}
	var agents []*jsonx.Object
	for _, id := range ids {
		agent := source.AgentByID(id)
		if agent == nil || agent.Len() == 0 {
			agent = jsonx.NewObject()
		}
		if jsonx.Equal(agent.Value("deployment_method"), "docker") {
			continue
		}
		agents = append(agents, agent)
	}
	serviceOf := func(agent *jsonx.Object) any {
		if s := agent.Value("service_type"); jsonx.Truthy(s) {
			return s
		}
		return "mihomo"
	}
	var mihomoHosts []any
	for _, agent := range agents {
		if jsonx.Equal(serviceOf(agent), "mihomo") {
			mihomoHosts = append(mihomoHosts, agent.Value("host"))
		}
	}
	inList := func(list []any, v any) bool {
		for _, item := range list {
			if jsonx.Equal(item, v) {
				return true
			}
		}
		return false
	}
	hosts = []any{}
	for _, agent := range agents {
		service := serviceOf(agent)
		if !jsonx.Equal(service, "mihomo") && !inList(mihomoHosts, agent.Value("host")) {
			continue
		}
		if n, ok := isExactInt(agent.Value("port")); ok {
			name := agent.Value("name")
			if !jsonx.Truthy(name) {
				name = agent.Value("id")
			}
			kind := "OxiDNS"
			if jsonx.Equal(service, "mihomo") {
				kind = "Mihomo"
			}
			if v, fits := n.Int64(); fits {
				if _, exists := reserved[int(v)]; !exists {
					reserved[int(v)] = kind + " Agent「" + fstr(name) + "」的端口"
					order = append(order, int(v))
				}
			}
		}
		if jsonx.Equal(service, "mihomo") && jsonx.Truthy(agent.Value("host")) && !inList(hosts, agent.Value("host")) {
			hosts = append(hosts, agent.Value("host"))
		}
	}
	return reserved, order, hosts
}

// templateSecret is setup_flow.TEMPLATE_SECRET.
const templateSecret = "123456"

// controllerSecretError is setup_flow.controller_secret_error (C36).
// TODO(port): replace with the C36 setup_flow export when it lands.
func controllerSecretError(value any) string {
	if value == templateSecret {
		return "不能使用初始模板的默认密码 123456，请自己设置一个"
	}
	text, ok := value.(string)
	if ok {
		runes := []rune(text)
		ok = len(runes) >= 8 && len(runes) <= 64
		for _, r := range runes {
			if r < '!' || r > '~' {
				ok = false
				break
			}
		}
	}
	if !ok {
		return "控制面板密码需为 8–64 个字母、数字或符号，不含空格"
	}
	return ""
}
