package convert

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"configflow/internal/model"
	"configflow/internal/weburl"
	"configflow/internal/yamlx"

	"gopkg.in/yaml.v3"
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

func baseFailure(message string) error { return &BaseSettingsError{Message: message} }
func basePath(document *yamlx.Node, path []string) *yamlx.Node {
	node := document
	for _, key := range path {
		node = yamlx.Get(node, key)
		if node == nil {
			return nil
		}
	}
	return yamlx.Resolve(node)
}
func textNode(node *yamlx.Node) (string, bool) {
	node = yamlx.Resolve(node)
	if node == nil || node.Kind != yaml.ScalarNode || node.ShortTag() != "!!str" {
		return "", false
	}
	return node.Value, true
}
func boolNode(node *yamlx.Node) (bool, bool) {
	node = yamlx.Resolve(node)
	if node == nil || node.Kind != yaml.ScalarNode || node.ShortTag() != "!!bool" {
		return false, false
	}
	var value bool
	if yamlx.Decode(node, &value) != nil {
		return false, false
	}
	return value, true
}
func sequenceNode(node *yamlx.Node) ([]*yamlx.Node, bool) {
	node = yamlx.Resolve(node)
	if node == nil || node.Kind != yaml.SequenceNode {
		return nil, false
	}
	return node.Content, true
}
func nullNode(node *yamlx.Node) bool {
	node = yamlx.Resolve(node)
	return node != nil && node.ShortTag() == "!!null"
}

// ParseListener parses an explicitly textual host:port using native addresses.
func ParseListener(value string) (string, int, bool) {
	host, port, err := net.SplitHostPort(strings.TrimSpace(value))
	if err != nil || port == "" {
		return "", 0, false
	}
	for _, digit := range port {
		if digit < '0' || digit > '9' {
			return "", 0, false
		}
	}
	number, err := strconv.ParseUint(port, 10, 16)
	if err != nil || number == 0 {
		return "", 0, false
	}
	return host, int(number), true
}

// ControllerScope classifies wildcard, loopback and other listener addresses.
func ControllerScope(host string) string {
	if host == "" || host == "0.0.0.0" || host == "::" {
		return "lan"
	}
	if strings.EqualFold(host, "localhost") {
		return "local"
	}
	address, err := netip.ParseAddr(host)
	if err != nil {
		return "other"
	}
	if address.WithZone("").Unmap().IsLoopback() {
		return "local"
	}
	return "other"
}

// SniffPortInvalid validates a scalar port or textual inclusive port range.
func SniffPortInvalid(node *yamlx.Node) bool {
	if value, ok := integerPort(node); ok {
		return value < 1 || value > 65535
	}
	text, ok := textNode(node)
	if !ok {
		return true
	}
	low, high, found := strings.Cut(strings.TrimSpace(text), "-")
	if !found {
		high = low
	}
	parse := func(text string) (uint64, error) {
		text = strings.TrimSpace(text)
		if text == "" {
			return 0, fmt.Errorf("empty port")
		}
		for _, r := range text {
			if r < '0' || r > '9' {
				return 0, fmt.Errorf("invalid port")
			}
		}
		return strconv.ParseUint(text, 10, 16)
	}
	first, err := parse(low)
	if err != nil {
		return true
	}
	last, err := parse(high)
	return err != nil || first < 1 || last < first
}
func checkBaseTypes(doc *yamlx.Node, changed func([]string) bool) error {
	for _, setting := range baseBooleans {
		value := basePath(doc, setting.path)
		if value != nil && changed(setting.path) {
			if _, ok := boolNode(value); !ok {
				return baseFailure(fmt.Sprintf(msgBaseBoolean, setting.label))
			}
		}
	}
	for _, setting := range baseIntegers {
		value := basePath(doc, setting.path)
		if value != nil && changed(setting.path) {
			n, ok := integerPort(value)
			if !ok || n < setting.low || n > setting.high {
				return baseFailure(fmt.Sprintf(msgBaseInteger, setting.label, setting.low, setting.high))
			}
		}
	}
	path := []string{"global-client-fingerprint"}
	if changed(path) && basePath(doc, path) != nil {
		return baseFailure(RemovedFingerprint)
	}
	for _, setting := range baseEnums {
		value := basePath(doc, setting.path)
		if !changed(setting.path) || value == nil {
			continue
		}
		if setting.path[0] == "find-process-mode" {
			if _, ok := boolNode(value); ok {
				continue
			}
		}
		text, ok := textNode(value)
		if !ok || !slices.Contains(setting.options, strings.ToLower(strings.TrimSpace(text))) {
			return baseFailure(fmt.Sprintf(msgBaseEnum, setting.label, strings.Join(setting.options, " / ")))
		}
	}
	for _, setting := range baseStringLists {
		value := basePath(doc, setting.path)
		if !changed(setting.path) || value == nil || nullNode(value) {
			continue
		}
		items, ok := sequenceNode(value)
		if !ok {
			return baseFailure(fmt.Sprintf(msgBaseList, setting.label))
		}
		for _, item := range items {
			text, ok := textNode(item)
			if !ok || strings.TrimSpace(text) == "" {
				return baseFailure(fmt.Sprintf(msgBaseList, setting.label))
			}
		}
	}
	for _, setting := range baseTexts {
		value := basePath(doc, setting.path)
		if changed(setting.path) && value != nil {
			text, ok := textNode(value)
			if !ok || strings.TrimSpace(text) == "" {
				return baseFailure(fmt.Sprintf(msgBaseText, setting.label))
			}
		}
	}
	return nil
}
func checkBaseSniff(doc *yamlx.Node, changed func([]string) bool) error {
	path := []string{"sniffer", "sniff"}
	node := basePath(doc, path)
	if node == nil || nullNode(node) {
		return nil
	}
	if node.Kind != yaml.MappingNode {
		if changed(path) {
			return baseFailure(msgBaseSniffObject)
		}
		return nil
	}
	for key, item := range yamlx.Pairs(node) {
		if !slices.Contains(SniffProtocols, strings.ToUpper(key)) {
			if changed(path) {
				return baseFailure(fmt.Sprintf(msgBaseSniffProtocol, key))
			}
			continue
		}
		if !changed([]string{"sniffer", "sniff", key}) {
			continue
		}
		ports, ok := sequenceNode(yamlx.Get(item, "ports"))
		if !ok || len(ports) == 0 {
			return baseFailure(fmt.Sprintf(msgBaseSniffPorts, key, SniffPortsHint))
		}
		for _, port := range ports {
			if SniffPortInvalid(port) {
				return baseFailure(fmt.Sprintf(msgBaseSniffPorts, key, SniffPortsHint))
			}
		}
		if value := yamlx.Get(item, "override-destination"); value != nil {
			if _, ok := boolNode(value); !ok {
				return baseFailure(fmt.Sprintf(msgBaseSniffOverride, key))
			}
		}
	}
	return nil
}
func controllerSecretMessage(secret string) string {
	if secret == "123456" {
		return msgTemplateSecret
	}
	valid := utf8.RuneCountInString(secret) >= 8 && utf8.RuneCountInString(secret) <= 64
	for _, r := range secret {
		if r < '!' || r > '~' {
			valid = false
			break
		}
	}
	if !valid {
		return msgControllerSecret
	}
	return ""
}
func checkBaseController(doc *yamlx.Node, changed func([]string) bool) error {
	cp, sp := []string{"external-controller"}, []string{"secret"}
	controller := basePath(doc, cp)
	secret := basePath(doc, sp)
	exposed := false
	if controller != nil && !nullNode(controller) {
		text, _ := textNode(controller)
		host, _, ok := ParseListener(text)
		if changed(cp) && !ok {
			return baseFailure(msgBaseController)
		}
		scope := "other"
		if ok {
			scope = ControllerScope(host)
		}
		if changed(cp) && scope == "other" {
			return baseFailure(msgBaseControllerScope)
		}
		exposed = scope != "local"
	}
	text, isText := textNode(secret)
	if secret != nil && !nullNode(secret) && !isText {
		if changed(sp) {
			return baseFailure(msgBaseSecretType)
		}
		secret = nil
	}
	if !changed(sp) && !changed(cp) {
		return nil
	}
	empty := secret == nil || nullNode(secret) || text == ""
	if exposed && empty {
		return baseFailure(msgBaseSecretRequired)
	}
	if exposed || (changed(sp) && !empty) {
		if message := controllerSecretMessage(text); message != "" {
			return baseFailure(message)
		}
	}
	return nil
}
func checkBaseAccess(doc *yamlx.Node, changed func([]string) bool) error {
	path := []string{"external-ui-url"}
	node := basePath(doc, path)
	if changed(path) && node != nil {
		value, ok := textNode(node)
		if !ok {
			return baseFailure(msgBaseUIURL)
		}
		url, err := weburl.Parse(value)
		if err != nil || (url.Scheme != "http" && url.Scheme != "https") || url.Hostname() == "" {
			return baseFailure(msgBaseUIURL)
		}
	}
	path = []string{"bind-address"}
	node = basePath(doc, path)
	if changed(path) && node != nil {
		value, ok := textNode(node)
		value = strings.Trim(strings.TrimSpace(value), "[]")
		_, err := netip.ParseAddr(value)
		if !ok || (value != "*" && err != nil) {
			return baseFailure(msgBaseBind)
		}
	}
	return nil
}

type basePortOwner struct {
	port  int64
	owner string
}

func basePorts(doc *yamlx.Node) []basePortOwner {
	var result []basePortOwner
	for _, key := range BaseInboundPortKeys {
		if value, ok := integerPort(yamlx.Get(doc, key)); ok && value >= 1 && value <= 65535 {
			result = append(result, basePortOwner{value, key})
		}
	}
	appendListener := func(node *yamlx.Node, owner string) {
		text, _ := textNode(node)
		if _, port, ok := ParseListener(text); ok {
			result = append(result, basePortOwner{int64(port), owner})
		}
	}
	appendListener(yamlx.Get(doc, "external-controller"), "external-controller")
	dns := yamlx.Get(doc, "dns")
	enable, valid := boolNode(yamlx.Get(dns, "enable"))
	if !valid || enable {
		appendListener(yamlx.Get(dns, "listen"), "dns.listen")
	}
	if listeners, ok := sequenceNode(yamlx.Get(doc, "listeners")); ok {
		for _, listener := range listeners {
			name, _ := textNode(yamlx.Get(listener, "name"))
			if name == RegionProbeListener {
				continue
			}
			if port, ok := integerPort(yamlx.Get(listener, "port")); ok {
				if name == "" {
					name = msgBaseUnnamed
				}
				result = append(result, basePortOwner{port, fmt.Sprintf(msgBaseListener, name)})
			}
		}
	}
	return result
}
func checkBasePorts(doc *yamlx.Node, changed func([]string) bool, reserved map[int]string) error {
	paths := [][]string{{"external-controller"}, {"dns", "listen"}, {"dns", "enable"}, {"listeners"}}
	for _, key := range BaseInboundPortKeys {
		paths = append(paths, []string{key})
	}
	edited := false
	for _, path := range paths {
		if changed(path) {
			edited = true
			break
		}
	}
	if !edited {
		return nil
	}
	seen := map[int64]string{}
	for _, item := range basePorts(doc) {
		if owner, ok := seen[item.port]; ok {
			return baseFailure(fmt.Sprintf(msgBasePortDuplicate, item.port, owner, item.owner))
		}
		seen[item.port] = item.owner
		if item.port >= 0 && uint64(item.port) <= uint64(^uint(0)>>1) {
			if description, ok := reserved[int(item.port)]; ok {
				return baseFailure(fmt.Sprintf(msgBasePortReserved, item.owner, item.port, description))
			}
		}
	}
	return nil
}
func checkBaseDNS(doc *yamlx.Node, changed func([]string) bool, hosts []netip.Addr) error {
	dns := yamlx.Resolve(yamlx.Get(doc, "dns"))
	if dns == nil || dns.Kind != yaml.MappingNode {
		return nil
	}
	upstreamChanged := false
	for _, path := range DNSUpstreamPaths {
		if changed(path) {
			upstreamChanged = true
			break
		}
	}
	if upstreamChanged {
		hook := currentIntegrations().DNSLoop
		if hook == nil {
			return &HookMissingError{Hook: "mihomo_dns_loop", Provider: onboardingProvide}
		}
		key, upstream, found, err := hook(dns, hosts)
		if err != nil {
			return err
		}
		if found {
			return baseFailure(fmt.Sprintf(msgBaseDNSLoop, key, upstream))
		}
	}
	isEmpty := func(node *yamlx.Node) bool {
		if node == nil || nullNode(node) {
			return true
		}
		node = yamlx.Resolve(node)
		if node.Kind == yaml.SequenceNode || node.Kind == yaml.MappingNode {
			return len(node.Content) == 0
		}
		text, ok := textNode(node)
		return ok && text == ""
	}
	if changed([]string{"dns", "nameserver"}) {
		if enable, ok := boolNode(yamlx.Get(dns, "enable")); ok && enable && isEmpty(yamlx.Get(dns, "nameserver")) {
			return baseFailure(msgBaseDNSRequired)
		}
	}
	if changed([]string{"dns", "respect-rules"}) || changed([]string{"dns", "proxy-server-nameserver"}) {
		if respect, ok := boolNode(yamlx.Get(dns, "respect-rules")); ok && respect && isEmpty(yamlx.Get(dns, "proxy-server-nameserver")) {
			return baseFailure(msgBaseProxyDNSRequired)
		}
	}
	return nil
}

// ValidateBaseNode checks changed editable settings, preserving existing invalid
// fields until the user actually edits that field. All diagnostics are ordinary errors.
func ValidateBaseNode(document, previous *yamlx.Node, reserved map[int]string, hosts []netip.Addr) error {
	doc := yamlx.Resolve(document)
	if doc == nil || doc.Kind != yaml.MappingNode {
		return nil
	}
	if _, err := yamlx.Encode(doc); err != nil {
		return err
	}
	if previous != nil {
		if _, err := yamlx.Encode(previous); err != nil {
			return err
		}
	}
	changed := func(path []string) bool {
		return previous == nil || !yamlx.Equal(basePath(doc, path), basePath(previous, path))
	}
	if err := checkBaseTypes(doc, changed); err != nil {
		return err
	}
	path := []string{"dns", "fake-ip-range"}
	if value := basePath(doc, path); value != nil && changed(path) {
		text, ok := textNode(value)
		if !ok {
			return baseFailure(msgBaseFakeRange)
		}
		hook := currentIntegrations().FakeIPNetworkError
		if hook == nil {
			return &HookMissingError{Hook: "fake_ip_network", Provider: gatewayProvider}
		}
		if hook(text) != nil {
			return baseFailure(msgBaseFakeRange)
		}
	}
	if err := checkBaseSniff(doc, changed); err != nil {
		return err
	}
	if err := checkBaseController(doc, changed); err != nil {
		return err
	}
	if err := checkBaseAccess(doc, changed); err != nil {
		return err
	}
	if err := checkBasePorts(doc, changed, reserved); err != nil {
		return err
	}
	return checkBaseDNS(doc, changed, hosts)
}

// GatewayMode identifies gateway/full-TUN ownership of base settings.
func GatewayMode(config *model.Config) (string, error) {
	on, err := gatewayActive(config)
	var missing *HookMissingError
	if errors.As(err, &missing) {
		return "", err
	}
	if err == nil && on {
		return "linux_gateway", nil
	}
	full, err := fullTunActive(config)
	if err != nil {
		return "", err
	}
	if full {
		return "full_tun", nil
	}
	return "", nil
}

// AgentPorts projects colocated non-Docker Agent listener ports and hosts.
func AgentPorts(agents []model.Agent) (map[int]string, []int, []string) {
	reserved := map[int]string{}
	var order []int
	hosts := []string{}
	local := map[string]bool{}
	for _, agent := range agents {
		if agent.DeploymentMethod != "docker" && (agent.ServiceType == "" || agent.ServiceType == "mihomo") {
			local[agent.Host] = true
		}
	}
	for _, agent := range agents {
		if agent.DeploymentMethod == "docker" {
			continue
		}
		service := agent.ServiceType
		if service == "" {
			service = "mihomo"
		}
		if service != "mihomo" && !local[agent.Host] {
			continue
		}
		port := int(agent.Port)
		if _, ok := reserved[port]; !ok && port > 0 {
			kind := "OxiDNS"
			if service == "mihomo" {
				kind = "Mihomo"
			}
			name := agent.Name
			if name == "" {
				name = agent.ID
			}
			reserved[port] = fmt.Sprintf(msgBaseAgentPort, kind, name)
			order = append(order, port)
		}
		if service == "mihomo" && agent.Host != "" && !slices.Contains(hosts, agent.Host) {
			hosts = append(hosts, agent.Host)
		}
	}
	return reserved, order, hosts
}
