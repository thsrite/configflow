// Package health implements repository-backed node latency probing through the
// standalone node-probe worker, global node-test DNS settings and validation,
// and per-Agent probe/region store wiring.
//
// Pure health-check validation, region-check verdicts, and domain-probe scoring
// live in internal/health/check; HTTP route handlers live in internal/api/nodes.
package health

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"configflow/internal/jsonresp"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/rules"
	"configflow/internal/weburl"
)

// ConfigKey is the SystemConfig field key storing structured node-test DNS settings.
const ConfigKey = "node_test_dns_config"

// DirectDoH lists fallback IP-based DoH endpoints that need no bootstrap lookup.
var DirectDoH = []string{"https://223.5.5.5/dns-query", "https://1.12.12.12/dns-query"}

// CheckMessages maps worker DNS check error codes to user-facing Chinese messages.
var CheckMessages = map[string]string{
	"config":        msgCheckConfig,
	"dns_bootstrap": msgCheckDNSBootstrap,
	"dns_fake_ip":   msgCheckDNSFakeIP,
	"timeout":       msgCheckTimeout,
	"tls":           msgCheckTLS,
	"dns":           msgCheckDNS,
}

func valueError(message string) error { return &ValueError{Message: message} }

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

func ipv4ServerString(text, message string) (string, error) {
	host, port, separator := strings.Cut(strings.TrimSpace(text), ":")
	address, ok := parseIPv4(host)
	if !ok || !usableIPv4(address) {
		return "", valueError(message)
	}
	var portValue uint64
	if separator {
		if !isASCIIDecimal(port) {
			return "", valueError(message)
		}
		var err error
		portValue, err = strconv.ParseUint(port, 10, 16)
		if err != nil || portValue == 0 {
			return "", valueError(message)
		}
	}
	out := address.String()
	if separator && portValue != 53 {
		out += ":" + strconv.FormatUint(portValue, 10)
	}
	return out, nil
}

var (
	dnsLabel     = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
	invalidChars = "?#\\"
)

func serverString(raw string) (string, error) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return "", valueError(DNSError)
	}
	if !strings.Contains(text, "://") {
		return ipv4ServerString(text, DNSError)
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
	if _, err := url.Parse(text); err != nil {
		return "", false
	}
	for _, r := range text {
		if unicode.IsSpace(r) || r < 32 || r == 127 {
			return "", false
		}
	}
	parsed, err := weburl.Parse(text)
	if err != nil {
		return "", false
	}
	if (parsed.Scheme != "https" && parsed.Scheme != "tls") || parsed.Hostname() == "" || parsed.User != nil {
		return "", false
	}
	encoded, ok := rules.IDNAEncode(weburl.Hostname(parsed))
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
	rawPath := weburl.RawPath(parsed)
	port, hasPort, err := weburl.Port(parsed)
	if err != nil || (hasPort && port < 1) || strings.HasSuffix(parsed.Host, ":") {
		return "", false
	}
	if parsed.Scheme == "tls" && rawPath != "" {
		return "", false
	}
	defaultPort := 853
	if parsed.Scheme == "https" {
		defaultPort = 443
	}
	authority := host
	if hasPort && port != defaultPort {
		authority += ":" + strconv.Itoa(port)
	}
	path := ""
	if parsed.Scheme == "https" {
		path = rawPath
		if path == "" {
			path = "/dns-query"
		}
	}
	return parsed.Scheme + "://" + authority + path, true
}

// NormalizeServerList validates and deduplicates up to 4 DNS or bootstrap server strings.
func NormalizeServerList(servers []string, bootstrap bool) ([]string, error) {
	message := DNSError
	if bootstrap {
		message = BootstrapError
	}
	if len(servers) > 4 {
		return nil, valueError(message)
	}
	result := []string{}
	for _, entry := range servers {
		var normalized string
		var err error
		if bootstrap {
			normalized, err = ipv4ServerString(entry, message)
		} else {
			normalized, err = serverString(entry)
		}
		if err != nil {
			return nil, err
		}
		if !slices.Contains(result, normalized) {
			result = append(result, normalized)
		}
	}
	return result, nil
}

// NormalizeDNSServersJSON validates a raw JSON array of DNS or bootstrap server strings.
func NormalizeDNSServersJSON(raw json.RawMessage, bootstrap bool) ([]string, error) {
	message := DNSError
	if bootstrap {
		message = BootstrapError
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return nil, valueError(message)
	}
	var items []strictString
	if err := json.Unmarshal(trimmed, &items); err != nil {
		return nil, valueError(message)
	}
	plain := make([]string, len(items))
	for i, item := range items {
		plain[i] = string(item)
	}
	return NormalizeServerList(plain, bootstrap)
}

func needsBootstrap(server string) bool {
	if !strings.Contains(server, "://") {
		return false
	}
	parsed, err := weburl.Parse(server)
	if err != nil {
		return true
	}
	_, ok := parseIPv4(weburl.Hostname(parsed))
	return !ok
}

// DNSConfig holds normalized node-test DNS settings.
type DNSConfig struct {
	Mode      string   `json:"mode"`
	Servers   []string `json:"servers"`
	Bootstrap []string `json:"bootstrap"`
}

// EffectiveDNS holds the active DNS server and bootstrap lists passed to node-probe.
type EffectiveDNS struct {
	DNSServers   []string `json:"dns_servers"`
	DNSBootstrap []string `json:"dns_bootstrap"`
}

// DNSSettingsView combines normalized DNSConfig with its effective server lists.
type DNSSettingsView struct {
	DNS          DNSConfig `json:"dns"`
	DNSServers   []string  `json:"dns_servers"`
	DNSBootstrap []string  `json:"dns_bootstrap"`
}

// NormalizeDNSConfig validates and canonicalizes a typed DNSConfig.
func NormalizeDNSConfig(cfg DNSConfig) (DNSConfig, error) {
	if cfg.Mode != "system" && cfg.Mode != "custom" {
		return DNSConfig{}, valueError(msgSelectDNSMode)
	}
	out := DNSConfig{
		Mode:      cfg.Mode,
		Servers:   []string{},
		Bootstrap: []string{},
	}
	if cfg.Mode == "custom" {
		servers, err := NormalizeServerList(cfg.Servers, false)
		if err != nil {
			return DNSConfig{}, err
		}
		bootstrap, err := NormalizeServerList(cfg.Bootstrap, true)
		if err != nil {
			return DNSConfig{}, err
		}
		if len(servers) == 0 {
			return DNSConfig{}, valueError(msgCustomDNSMinOne)
		}
		if slices.ContainsFunc(servers, needsBootstrap) && len(bootstrap) == 0 {
			return DNSConfig{}, valueError(msgCustomDNSNeedsBoot)
		}
		out.Servers = servers
		out.Bootstrap = bootstrap
	}
	return out, nil
}

// NormalizeDNSConfigJSON validates and canonicalizes a raw JSON DNS settings object.
func NormalizeDNSConfigJSON(raw json.RawMessage) (DNSConfig, error) {
	var object rawObject
	if err := json.Unmarshal(raw, &object); err != nil {
		return DNSConfig{}, valueError(msgSelectDNSMode)
	}
	rawMode, ok := object.Fields.Get("mode")
	if !ok {
		return DNSConfig{}, valueError(msgSelectDNSMode)
	}
	var mode strictString
	if err := json.Unmarshal(rawMode, &mode); err != nil || (mode != "system" && mode != "custom") {
		return DNSConfig{}, valueError(msgSelectDNSMode)
	}
	if mode == "system" {
		return DNSConfig{
			Mode:      "system",
			Servers:   []string{},
			Bootstrap: []string{},
		}, nil
	}
	servers := []string{}
	if rawServers, ok := object.Fields.Get("servers"); ok {
		var err error
		servers, err = NormalizeDNSServersJSON(rawServers, false)
		if err != nil {
			return DNSConfig{}, err
		}
	}
	bootstrap := []string{}
	if rawBootstrap, ok := object.Fields.Get("bootstrap"); ok {
		var err error
		bootstrap, err = NormalizeDNSServersJSON(rawBootstrap, true)
		if err != nil {
			return DNSConfig{}, err
		}
	}
	return NormalizeDNSConfig(DNSConfig{
		Mode:      "custom",
		Servers:   servers,
		Bootstrap: bootstrap,
	})
}

func checkSystemConfigRoot(system *model.System) error {
	for _, issue := range model.Issues(system) {
		if issue.Path == "system_config" {
			return valueError(msgStoredSystemCfgInvalid)
		}
	}
	return nil
}

// LoadDNSConfig loads and normalizes the saved node-test DNS configuration from
// the repository, falling back to the legacy node_test_dns list when needed.
func LoadDNSConfig(ctx context.Context) (DNSConfig, error) {
	repository, err := repo.GetRepository(ctx)
	if err != nil {
		return DNSConfig{}, err
	}
	var rawSaved, rawSettings []byte
	hasSaved := false
	if err := repository.View(ctx, func(doc *model.Document) error {
		if err := checkSystemConfigRoot(&doc.System); err != nil {
			return err
		}
		if saved := doc.System.SystemConfig.NodeTestDNSConfig; saved != nil && saved.Mode != "managed" {
			var err error
			rawSaved, err = model.Marshal(saved)
			hasSaved = true
			return err
		}
		var err error
		rawSettings, err = model.Marshal(&doc.System.SystemConfig)
		return err
	}); err != nil {
		return DNSConfig{}, err
	}
	if hasSaved {
		return NormalizeDNSConfigJSON(rawSaved)
	}
	var settingsObj rawObject
	if err := json.Unmarshal(rawSettings, &settingsObj); err != nil {
		return DNSConfig{}, err
	}
	servers := []string{}
	if rawLegacy, ok := settingsObj.Fields.Get("node_test_dns"); ok {
		servers, err = NormalizeDNSServersJSON(rawLegacy, false)
		if err != nil {
			return DNSConfig{}, err
		}
	}
	mode := "system"
	if len(servers) > 0 {
		mode = "custom"
	}
	return NormalizeDNSConfig(DNSConfig{Mode: mode, Servers: servers, Bootstrap: []string{}})
}

func resolveConfigJSON(ctx context.Context, raw json.RawMessage) (DNSConfig, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return LoadDNSConfig(ctx)
	}
	return NormalizeDNSConfigJSON(trimmed)
}

// EffectiveDNSSettings resolves effective DNS server and bootstrap lists from
// override (or loads saved settings when override is nil).
func EffectiveDNSSettings(ctx context.Context, override *DNSConfig) (EffectiveDNS, error) {
	var cfg DNSConfig
	var err error
	if override == nil {
		cfg, err = LoadDNSConfig(ctx)
	} else {
		cfg, err = NormalizeDNSConfig(*override)
	}
	if err != nil {
		return EffectiveDNS{}, err
	}
	return EffectiveDNS{
		DNSServers:   slices.Clone(cfg.Servers),
		DNSBootstrap: slices.Clone(cfg.Bootstrap),
	}, nil
}

// EffectiveDNSSettingsJSON resolves effective DNS lists from raw JSON (loading
// saved settings when raw is empty or null).
func EffectiveDNSSettingsJSON(ctx context.Context, raw json.RawMessage) (EffectiveDNS, error) {
	cfg, err := resolveConfigJSON(ctx, raw)
	if err != nil {
		return EffectiveDNS{}, err
	}
	return EffectiveDNS{
		DNSServers:   slices.Clone(cfg.Servers),
		DNSBootstrap: slices.Clone(cfg.Bootstrap),
	}, nil
}

// DescribeDNSSettingsJSON returns normalized DNSConfig alongside effective lists.
func DescribeDNSSettingsJSON(ctx context.Context, raw json.RawMessage) (DNSSettingsView, error) {
	cfg, err := resolveConfigJSON(ctx, raw)
	if err != nil {
		return DNSSettingsView{}, err
	}
	return DNSSettingsView{
		DNS:          cfg,
		DNSServers:   slices.Clone(cfg.Servers),
		DNSBootstrap: slices.Clone(cfg.Bootstrap),
	}, nil
}

// SaveDNSConfigJSON validates and persists raw DNS settings and the legacy
// node_test_dns mirror into system_config.
func SaveDNSConfigJSON(ctx context.Context, raw json.RawMessage) (DNSSettingsView, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return DNSSettingsView{}, err
	}
	cfg, err := NormalizeDNSConfigJSON(raw)
	if err != nil {
		return DNSSettingsView{}, err
	}
	repository, err := repo.GetRepository(ctx)
	if err != nil {
		return DNSSettingsView{}, err
	}
	cfgJSON, err := jsonresp.Encode(cfg)
	if err != nil {
		return DNSSettingsView{}, err
	}
	serversJSON, err := jsonresp.Encode(cfg.Servers)
	if err != nil {
		return DNSSettingsView{}, err
	}
	err = repository.UpdateSystem(ctx, func(_ context.Context, system *model.System) error {
		if err := checkSystemConfigRoot(system); err != nil {
			return err
		}
		if err := system.SystemConfig.SetJSON(ConfigKey, cfgJSON); err != nil {
			return err
		}
		return system.SystemConfig.SetJSON("node_test_dns", serversJSON)
	})
	if err != nil {
		return DNSSettingsView{}, err
	}
	repo.ResetConfigContext(ctx)
	return DNSSettingsView{
		DNS:          cfg,
		DNSServers:   slices.Clone(cfg.Servers),
		DNSBootstrap: slices.Clone(cfg.Bootstrap),
	}, nil
}

// DNSCheckResult holds the outcome of a live DNS check via node-probe.
type DNSCheckResult struct {
	Success    bool
	Addresses  []string
	LatencyMS  *json.Number
	ErrorCode  string
	Message    string
	hasLatency bool
}

// MarshalJSON encodes DNSCheckResult, omitting latency_ms on subprocess timeout
// and keeping explicit null when the worker returned no latency_ms.
func (r DNSCheckResult) MarshalJSON() ([]byte, error) {
	addresses := r.Addresses
	if addresses == nil {
		addresses = []string{}
	}
	if !r.hasLatency {
		type timeoutWire struct {
			Success   bool     `json:"success"`
			Addresses []string `json:"addresses"`
			ErrorCode string   `json:"error_code,omitempty"`
			Message   string   `json:"message"`
		}
		return jsonresp.Encode(timeoutWire{
			Success:   r.Success,
			Addresses: addresses,
			ErrorCode: r.ErrorCode,
			Message:   r.Message,
		})
	}
	type workerWire struct {
		Success   bool         `json:"success"`
		Addresses []string     `json:"addresses"`
		LatencyMS *json.Number `json:"latency_ms"`
		ErrorCode string       `json:"error_code,omitempty"`
		Message   string       `json:"message"`
	}
	return jsonresp.Encode(workerWire{
		Success:   r.Success,
		Addresses: addresses,
		LatencyMS: r.LatencyMS,
		ErrorCode: r.ErrorCode,
		Message:   r.Message,
	})
}

type dnsCheckPayload struct {
	Operation    string   `json:"operation"`
	TimeoutMS    int64    `json:"timeout_ms"`
	DNSServers   []string `json:"dns_servers"`
	DNSBootstrap []string `json:"dns_bootstrap"`
}

// CheckDNSSettingsJSON executes a live DNS check against the worker using raw
// DNS settings (or saved settings when raw is empty or null).
func CheckDNSSettingsJSON(ctx context.Context, raw json.RawMessage) (DNSCheckResult, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return DNSCheckResult{}, err
	}
	env := CurrentProbeEnv()
	options, err := EffectiveDNSSettingsJSON(ctx, raw)
	if err != nil {
		return DNSCheckResult{}, err
	}
	data, err := jsonresp.EncodeUTF8(dnsCheckPayload{
		Operation:    "dns_check",
		TimeoutMS:    5000,
		DNSServers:   options.DNSServers,
		DNSBootstrap: options.DNSBootstrap,
	})
	if err != nil {
		return DNSCheckResult{}, err
	}
	binary, err := env.probeBinary()
	if err != nil {
		return DNSCheckResult{}, err
	}
	process, err := env.Runner.Run(ctx, Command{
		Argv:    []string{binary},
		Stdin:   data,
		Timeout: checkTimeout,
		Env:     env.workerEnv(),
	})
	if ctxErr := ctx.Err(); ctxErr != nil {
		return DNSCheckResult{}, ctxErr
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return DNSCheckResult{}, err
	}
	if errors.Is(err, ErrTimeout) {
		return DNSCheckResult{
			Success:   false,
			Addresses: []string{},
			ErrorCode: "timeout",
			Message:   CheckMessages["timeout"],
		}, nil
	}
	if err != nil {
		return DNSCheckResult{}, &ProbeUnavailable{Message: msgProbeStartFailed}
	}
	stdout, err := decodeText(process.Stdout)
	if err != nil {
		return DNSCheckResult{}, err
	}
	if _, err := decodeText(process.Stderr); err != nil {
		return DNSCheckResult{}, err
	}
	if process.ReturnCode != 0 {
		return DNSCheckResult{}, &ProbeUnavailable{Message: msgDNSCheckExitFailed}
	}
	result, err := parseCheckResult(stdout)
	if err != nil {
		var unavailable *ProbeUnavailable
		if errors.As(err, &unavailable) {
			return DNSCheckResult{}, err
		}
		return DNSCheckResult{}, &ProbeUnavailable{Message: msgDNSCheckInvalidOutput}
	}
	return result, nil
}

var fakeIPNet = netip.MustParsePrefix("198.18.0.0/15")

func checkWorkerVersion(top rawObject) error {
	rawVersion, ok := top.Fields.Get("version")
	if !ok {
		return errors.New("missing worker version")
	}
	trimmed := bytes.TrimSpace(rawVersion)
	if bytes.Equal(trimmed, []byte("null")) {
		return &ProbeUnavailable{Message: msgVersionMismatch}
	}
	var version model.Int
	if len(trimmed) == 0 || trimmed[0] == '"' {
		return &ProbeUnavailable{Message: msgVersionMismatch}
	}
	if err := json.Unmarshal(trimmed, &version); err != nil || version != 4 {
		return &ProbeUnavailable{Message: msgVersionMismatch}
	}
	return nil
}

func parseCheckResult(stdout string) (DNSCheckResult, error) {
	var top rawObject
	if err := json.Unmarshal([]byte(stdout), &top); err != nil {
		return DNSCheckResult{}, err
	}
	if err := checkWorkerVersion(top); err != nil {
		return DNSCheckResult{}, err
	}
	rawCheck, ok := top.Fields.Get("dns_check")
	if !ok {
		return DNSCheckResult{}, errors.New("missing dns_check")
	}
	var checkObj rawObject
	if err := json.Unmarshal(rawCheck, &checkObj); err != nil {
		return DNSCheckResult{}, err
	}
	rawSuccess, ok := checkObj.Fields.Get("success")
	if !ok {
		return DNSCheckResult{}, errors.New("missing success")
	}
	trimmedSuccess := bytes.TrimSpace(rawSuccess)
	if !bytes.Equal(trimmedSuccess, []byte("true")) && !bytes.Equal(trimmedSuccess, []byte("false")) {
		return DNSCheckResult{}, errors.New("invalid success")
	}
	success := bytes.Equal(trimmedSuccess, []byte("true"))

	rawAddresses, ok := checkObj.Fields.Get("addresses")
	if !ok || len(bytes.TrimSpace(rawAddresses)) == 0 || bytes.TrimSpace(rawAddresses)[0] != '[' {
		return DNSCheckResult{}, errors.New("invalid addresses")
	}
	var rawAddrList []strictString
	if err := json.Unmarshal(rawAddresses, &rawAddrList); err != nil {
		return DNSCheckResult{}, err
	}
	addresses := []string{}
	for _, rawAddr := range rawAddrList {
		addr, scope, valid := ipAddress(string(rawAddr))
		if !valid {
			return DNSCheckResult{}, errors.New("invalid ip address")
		}
		text := ipString(addr, scope)
		if !slices.Contains(addresses, text) {
			addresses = append(addresses, text)
		}
	}

	var latencyMS *json.Number
	if rawLatency, ok := checkObj.Fields.Get("latency_ms"); ok && !bytes.Equal(bytes.TrimSpace(rawLatency), []byte("null")) {
		parsedLatency, valid := parseNonNegativeJSONInt(rawLatency)
		if !valid {
			return DNSCheckResult{}, errors.New("invalid latency_ms")
		}
		latencyMS = &parsedLatency
	}

	if success {
		if len(addresses) == 0 {
			return DNSCheckResult{}, errors.New("empty addresses on success")
		}
		for _, text := range addresses {
			addr, _, _ := ipAddress(text)
			if addr.Is4() && fakeIPNet.Contains(addr) {
				return DNSCheckResult{}, errors.New("fake ip address")
			}
		}
	}

	codeText := "dns"
	if rawCode, ok := checkObj.Fields.Get("error_code"); ok {
		trimmedCode := bytes.TrimSpace(rawCode)
		if len(trimmedCode) > 0 && (trimmedCode[0] == '[' || trimmedCode[0] == '{') {
			return DNSCheckResult{}, errors.New("invalid error_code container")
		}
		var parsedCode strictString
		if err := json.Unmarshal(trimmedCode, &parsedCode); err == nil {
			if _, known := CheckMessages[string(parsedCode)]; known {
				codeText = string(parsedCode)
			}
		}
	}

	out := DNSCheckResult{
		Success:    success,
		Addresses:  addresses,
		LatencyMS:  latencyMS,
		hasLatency: true,
	}
	if success {
		out.Message = msgCheckSuccess
	} else {
		out.ErrorCode = codeText
		out.Message = CheckMessages[codeText]
	}
	return out, nil
}
