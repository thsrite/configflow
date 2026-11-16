package monitor

// Port of backend/agents/mihomo_panel.py (A07): view and switch policies,
// connections and rules through a registered Mihomo Agent. The Agent
// forwards to its local Mihomo controller with the controller secret it
// reads itself; neither the secret nor arbitrary controller paths pass
// through here: only the (resource, method) pairs of PanelRoutes are
// relayed, names are encoded as single path segments, query parameters and
// bodies are rebuilt from validated values, and node listings are slimmed
// to the fields the panel shows.

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"configflow/internal/agents"
	"configflow/internal/jsonx"
	"configflow/internal/repo"
	"configflow/internal/rulefetch"
	"configflow/internal/weburl"
)

// Versions and limits of mihomo_panel.py.
const (
	PanelMinAgentVersion    = "1.8.15-go"
	PanelMaxResponseBytes   = 16 * 1024 * 1024
	PanelMaxNameLength      = 512
	PanelDefaultTestURL     = "https://www.gstatic.com/generate_204"
	CoreCheckTTL            = 86400.0
	CoreCheckFailureTTL     = 300.0
	coreVersionMaxBodyBytes = 256
)

type panelRoute struct{ resource, method string }

// PanelRoutes is ROUTES: (resource, method) → Agent path template. Names
// are independently encoded as single path segments, including a provider
// name for its individual nodes.
var PanelRoutes = map[panelRoute]string{
	{"version", "GET"}:                     "version",
	{"configs", "GET"}:                     "configs",
	{"configs", "PATCH"}:                   "configs",
	{"proxies", "GET"}:                     "proxies",
	{"proxy", "PUT"}:                       "proxies/{name}",
	{"proxy", "DELETE"}:                    "proxies/{name}",
	{"proxy/delay", "GET"}:                 "proxies/{name}/delay",
	{"group/delay", "GET"}:                 "group/{name}/delay",
	{"connections", "GET"}:                 "connections",
	{"connections", "DELETE"}:              "connections",
	{"connection", "DELETE"}:               "connections/{name}",
	{"flow", "GET"}:                        "flow",
	{"logs", "GET"}:                        "logs",
	{"rules", "GET"}:                       "rules",
	{"providers/proxies", "GET"}:           "providers/proxies",
	{"providers/proxy", "PUT"}:             "providers/proxies/{name}",
	{"providers/proxy/healthcheck", "GET"}: "providers/proxies/{name}/healthcheck",
	{"providers/proxy/delay", "GET"}:       "providers/proxies/{provider}/{name}/healthcheck",
	{"providers/rules", "GET"}:             "providers/rules",
	{"providers/rule", "PUT"}:              "providers/rules/{name}",
	{"cache/fakeip/flush", "POST"}:         "cache/fakeip/flush",
	{"cache/dns/flush", "POST"}:            "cache/dns/flush",
	// The Agent reloads the core's current config file and ignores any body.
	{"configs/reload", "PUT"}: "configs",
	{"upgrade", "POST"}:       "upgrade",
}

// PanelResourceMinVersion is RESOURCE_MIN_VERSION: core operations added
// after the first panel release need a newer Agent.
var PanelResourceMinVersion = map[string]string{
	"configs/reload": "1.8.16-go", "upgrade": "1.8.16-go",
	"providers/proxy/delay": "1.8.18-go",
	"flow":                  "1.8.26-go",
	"logs":                  "1.8.56-go",
}

var panelResourceFeature = map[string]string{"providers/proxy/delay": "订阅节点测速", "flow": "实时流量", "logs": "实时日志"}

var (
	panelLogLevels       = []string{"debug", "info", "warning", "error"}
	panelResourceTimeout = map[string]float64{"configs/reload": 70, "upgrade": 200}
	panelModes           = []string{"rule", "global", "direct"}
	// panelProxyFields is PROXY_FIELDS: what the panel reads from a node or group.
	panelProxyFields = []string{"name", "type", "udp", "alive", "now", "all", "fixed", "testUrl", "hidden", "icon"}
)

// PanelMessages is MESSAGES of mihomo_panel.py.
var PanelMessages = map[string]string{
	"CONTROLLER_UNAVAILABLE": "Agent 未找到 Mihomo 控制接口，请确认配置里启用了 external-controller。",
	"CORE_UNREACHABLE":       "暂时无法连接 Mihomo 控制接口，请检查 Mihomo 运行状态。",
	"CORE_AUTH_FAILED":       "Mihomo 控制接口拒绝了 Agent 的 secret，请检查配置。",
	"AGENT_UNREACHABLE":      "暂时无法连接 Agent，请检查网络和 Agent 运行状态。",
	"AGENT_AUTH_FAILED":      "Agent 认证失败，请检查注册状态。",
	"INVALID_RESPONSE":       "Mihomo 返回的数据格式异常。",
	"INVALID_REQUEST":        "请求参数无效。",
}

// panelFail is _fail(code, status, message).
func panelFail(code string, status int, message string) *Error {
	if status == 0 {
		status = 502
	}
	if message == "" {
		var ok bool
		if message, ok = PanelMessages[code]; !ok {
			message = PanelMessages["INVALID_RESPONSE"]
		}
	}
	return &Error{Code: code, Status: status, Message: message}
}

// panelAgent is _agent(manager, agent_id, min_version, feature).
func panelAgent(ctx context.Context, m Manager, agentID, minVersion, feature string) (*jsonx.Object, error) {
	agent, err := m.GetAgentByID(ctx, agentID, false)
	if err != nil {
		return nil, err
	}
	if agent == nil || agent.Len() == 0 {
		return nil, panelFail("AGENT_NOT_FOUND", 404, "未找到这台 Agent。")
	}
	service, present := agent.Get("service_type")
	if !present {
		service = "mihomo"
	}
	if !pyEqualStr(service, "mihomo") {
		return nil, panelFail("WRONG_SERVICE", 400, "Mihomo 面板仅适用于 Mihomo Agent。")
	}
	if !versionAtLeast(agent, minVersion) {
		if feature == "" {
			feature = "这个内核操作"
			if minVersion == PanelMinAgentVersion {
				feature = "Mihomo 面板"
			}
		}
		return nil, panelFail("UPGRADE_REQUIRED", 409, "请将此 Agent 升级到 "+minVersion+" 或更新版本后使用"+feature+"。")
	}
	return agent, nil
}

func argGet(args [][2]string, key, def string) string {
	for _, pair := range args {
		if pair[0] == key {
			return pair[1]
		}
	}
	return def
}

func argPresent(args [][2]string, key string) bool {
	for _, pair := range args {
		if pair[0] == key {
			return true
		}
	}
	return false
}

// panelName is _name(args, key).
func panelName(args [][2]string, key string) (string, error) {
	name := argGet(args, key, "")
	if name == "" || runeLen(name) > PanelMaxNameLength {
		return "", panelFail("INVALID_REQUEST", 400, "")
	}
	for _, r := range name {
		if r < 32 {
			return "", panelFail("INVALID_REQUEST", 400, "")
		}
	}
	return name, nil
}

// pyIsDigit is str.isdigit() of a non-empty str (CPython 3.11).
func pyIsDigit(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if pyDecimalValue(r) >= 0 {
			continue
		}
		i := sort.Search(len(pyDigitExtra), func(i int) bool { return pyDigitExtra[i] >= r })
		if i == len(pyDigitExtra) || pyDigitExtra[i] != r {
			return false
		}
	}
	return true
}

// pyIntStr is int(text) for a str in CPython 3.11: non-ASCII decimal
// digits become ASCII digits and non-ASCII whitespace a space
// (_PyUnicode_TransformDecimalAndSpaceToASCII; any other non-ASCII
// character fails), then ASCII whitespace around an optional sign and
// digits with single underscores between them; at most 4300 digits.
func pyIntStr(text string) (*big.Int, bool) {
	var b strings.Builder
	for _, r := range text {
		switch {
		case r < 127:
			b.WriteRune(r)
		case pyIsSpace(r):
			b.WriteByte(' ')
		default:
			d := pyDecimalValue(r)
			if d < 0 {
				return nil, false
			}
			b.WriteByte(byte('0' + d))
		}
	}
	s := strings.Trim(b.String(), " \t\n\v\f\r")
	negative := false
	if s != "" && (s[0] == '+' || s[0] == '-') {
		negative = s[0] == '-'
		s = s[1:]
	}
	if s == "" {
		return nil, false
	}
	var digits strings.Builder
	previousUnderscore := true
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '_':
			if previousUnderscore {
				return nil, false
			}
			previousUnderscore = true
		case c >= '0' && c <= '9':
			digits.WriteByte(c)
			previousUnderscore = false
		default:
			return nil, false
		}
	}
	if previousUnderscore || digits.Len() > maxIntDigits {
		return nil, false
	}
	value, ok := new(big.Int).SetString(digits.String(), 10)
	if !ok {
		return nil, false
	}
	if negative {
		value.Neg(value)
	}
	return value, true
}

// maxIntDigits is sys.get_int_max_str_digits() of CPython 3.11.
const maxIntDigits = 4300

// panelLogsQuery is _logs_query(args).
func panelLogsQuery(args [][2]string) (query, error) {
	level := argGet(args, "level", "info")
	if !contains(panelLogLevels, level) {
		return nil, panelFail("INVALID_REQUEST", 400, "日志等级只能是 debug、info、warning 或 error。")
	}
	after := argGet(args, "after", "0")
	if !pyIsDigit(after) || runeLen(after) > 18 {
		return nil, panelFail("INVALID_REQUEST", 400, "")
	}
	return query{{"level", level}, {"after", after}}, nil
}

// panelQuery is _query(resource, args).
func panelQuery(resource string, args [][2]string) (query, error) {
	if resource == "logs" {
		return panelLogsQuery(args)
	}
	if !strings.HasSuffix(resource, "/delay") {
		return nil, nil
	}
	testURL := argGet(args, "url", "")
	if testURL == "" {
		testURL = PanelDefaultTestURL
	}
	if !(strings.HasPrefix(testURL, "http://") || strings.HasPrefix(testURL, "https://")) || runeLen(testURL) > 2048 {
		return nil, panelFail("INVALID_REQUEST", 400, "测速地址必须是 http(s) 链接。")
	}
	timeout := big.NewInt(5000)
	if text, ok := lookupArg(args, "timeout"); ok {
		value, ok := pyIntStr(text)
		if !ok {
			return nil, panelFail("INVALID_REQUEST", 400, "")
		}
		timeout = value
	}
	if timeout.Cmp(big.NewInt(500)) < 0 || timeout.Cmp(big.NewInt(30000)) > 0 {
		return nil, panelFail("INVALID_REQUEST", 400, "测速超时需在 500–30000 毫秒之间。")
	}
	return query{{"url", testURL}, {"timeout", timeout.String()}}, nil
}

func lookupArg(args [][2]string, key string) (string, bool) {
	for _, pair := range args {
		if pair[0] == key {
			return pair[1], true
		}
	}
	return "", false
}

// hasExactKey is set(body) == {key}.
func hasExactKey(body any, key string) (*jsonx.Object, bool) {
	o, ok := body.(*jsonx.Object)
	if !ok || o == nil || o.Len() != 1 {
		return nil, false
	}
	_, present := o.Get(key)
	return o, present
}

// panelBody is _body(resource, method, body); nil is None (no body).
func panelBody(resource, method string, body any) (*jsonx.Object, error) {
	if (method != "PUT" && method != "PATCH") || resource == "configs/reload" {
		return nil, nil
	}
	switch resource {
	case "proxy":
		// Switching a selector group: {"name": "<member>"}
		o, ok := hasExactKey(body, "name")
		name, isStr := o.Value("name").(string)
		if !ok || !isStr || name == "" || runeLen(name) > PanelMaxNameLength {
			return nil, panelFail("INVALID_REQUEST", 400, "请选择要切换的节点。")
		}
		return jsonx.Obj("name", name), nil
	case "configs":
		// Only the outbound mode is editable here.
		o, ok := hasExactKey(body, "mode")
		mode, isStr := o.Value("mode").(string)
		if !ok || !isStr || !contains(panelModes, mode) {
			return nil, panelFail("INVALID_REQUEST", 400, "代理模式只能是 rule、global 或 direct。")
		}
		return jsonx.Obj("mode", mode), nil
	}
	return jsonx.NewObject(), nil
}

// slimProxy is _slim_proxy: the panel's fields of a node or group, with
// only the newest delay result per test URL.
func slimProxy(proxy any) any {
	o, ok := proxy.(*jsonx.Object)
	if !ok || o == nil {
		return proxy
	}
	slim := jsonx.NewObject()
	for _, key := range panelProxyFields {
		if value, present := o.Get(key); present {
			slim.Set(key, value)
		}
	}
	if history, ok := o.Value("history").([]any); ok {
		slim.Set("history", lastOne(history))
	}
	if extra, ok := o.Value("extra").(*jsonx.Object); ok && extra != nil {
		out := jsonx.NewObject()
		for url, value := range extra.All() {
			entry, ok := value.(*jsonx.Object)
			if !ok || entry == nil {
				continue
			}
			item := jsonx.NewObject()
			if alive, present := entry.Get("alive"); present {
				item.Set("alive", alive)
			}
			if history, ok := entry.Value("history").([]any); ok {
				item.Set("history", lastOne(history))
			} else {
				item.Set("history", []any{})
			}
			out.Set(url, item)
		}
		slim.Set("extra", out)
	}
	return slim
}

// lastOne is list[-1:].
func lastOne(list []any) []any {
	if len(list) == 0 {
		return []any{}
	}
	return []any{list[len(list)-1]}
}

// slim is _slim(resource, data): drop what the panel never shows from the
// node listings before they reach the browser.
func slim(resource string, data any) any {
	o, ok := data.(*jsonx.Object)
	if !ok || o == nil {
		return data
	}
	if proxies, ok := o.Value("proxies").(*jsonx.Object); resource == "proxies" && ok && proxies != nil {
		slimmed := jsonx.NewObject()
		for name, proxy := range proxies.All() {
			slimmed.Set(name, slimProxy(proxy))
		}
		result := o.Clone()
		result.Set("proxies", slimmed)
		return result
	}
	if providers, ok := o.Value("providers").(*jsonx.Object); resource == "providers/proxies" && ok && providers != nil {
		out := jsonx.NewObject()
		for name, value := range providers.All() {
			if provider, ok := value.(*jsonx.Object); ok && provider != nil {
				if list, ok := provider.Value("proxies").([]any); ok {
					// A group's Compatible provider holds config nodes and groups
					// that /proxies already lists in full; the panel needs only
					// its names.
					compatible := pyEqualStr(provider.Value("vehicleType"), "Compatible")
					proxies := make([]any, len(list))
					for i, proxy := range list {
						if p, isDict := proxy.(*jsonx.Object); compatible && isDict && p != nil {
							proxies[i] = jsonx.Obj("name", p.Value("name"))
						} else {
							proxies[i] = slimProxy(proxy)
						}
					}
					copied := provider.Clone()
					copied.Set("proxies", proxies)
					value = copied
				}
			}
			out.Set(name, value)
		}
		result := o.Clone()
		result.Set("providers", out)
		return result
	}
	return data
}

// Core update check (Mihomo's own updater reads one of these files and
// upgrades whenever the text differs from the running version).
var (
	CoreVersionURLs = map[string]string{
		"release": "https://github.com/MetaCubeX/mihomo/releases/latest/download/version.txt",
		"alpha":   "https://github.com/MetaCubeX/mihomo/releases/download/Prerelease-Alpha/version.txt",
	}
	coreVersionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$`)
	coreReleasePages   = map[string]string{
		"release": "https://github.com/MetaCubeX/mihomo/releases/tag/{version}",
		"alpha":   "https://github.com/MetaCubeX/mihomo/releases/tag/Prerelease-Alpha",
	}
)

// coreCached is one _core_latest entry: (finished_at, latest, error, settings).
type coreCached struct {
	finishedAt float64
	latest     string
	err        string
	settings   *jsonx.Object
}

var (
	coreLatestMu sync.Mutex
	coreLatest   = map[string]*coreCached{}
	// coreLocks: one GitHub fetch per channel at a time.
	coreLocks = map[string]*sync.Mutex{"release": {}, "alpha": {}}
)

// ResetCoreLatest empties _core_latest (tests).
func ResetCoreLatest() {
	coreLatestMu.Lock()
	coreLatest = map[string]*coreCached{}
	coreLatestMu.Unlock()
}

func coreCachedOf(channel string) *coreCached {
	coreLatestMu.Lock()
	defer coreLatestMu.Unlock()
	return coreLatest[channel]
}

// downloadSettings is _download_settings: only the download proxy and
// mirror settings, not the whole system section with the Agents.
var downloadSettings = func(ctx context.Context) (*jsonx.Object, error) {
	repository, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	settings, err := repository.GetSystemSettings(ctx)
	if err != nil {
		return nil, err
	}
	result := jsonx.NewObject()
	for _, key := range []string{"rule_fetch_proxy", "github_proxy_domain", "server_domain"} {
		result.Set(key, settings.GetOr(key, ""))
	}
	return result, nil
}

type hostURLKey struct{}

// WithHostURL carries flask.request.host_url to the core update check:
// request_rule compares the version.txt source and its GitHub mirror with
// the panel's own origin (a mirror under this panel's /api/ is dispatched
// in-process and never goes through the download proxy).
func WithHostURL(ctx context.Context, hostURL string) context.Context {
	return context.WithValue(ctx, hostURLKey{}, hostURL)
}

func hostURLOf(ctx context.Context) string {
	hostURL, _ := ctx.Value(hostURLKey{}).(string)
	return hostURL
}

// requestRule is backend.utils.rule_fetch.request_rule (a variable for tests).
var requestRule = rulefetch.RequestRule

// latestCoreVersion is _latest_core_version: the channel's version.txt read
// with the rule download proxy and GitHub mirror (a variable for tests).
var latestCoreVersion = func(ctx context.Context, channel string, settings *jsonx.Object) (string, error) {
	config := jsonx.Obj("system_config", settings.Clone())
	response, err := requestRule(ctx, CoreVersionURLs[channel], rulefetch.Request{Config: config,
		ConnectTimeout: 5e9, ReadTimeout: 10e9, Stream: true, MaxBytes: coreVersionMaxBodyBytes, HostURL: hostURLOf(ctx)})
	if err != nil {
		return "", err
	}
	content, err := response.Content()
	response.Close()
	if err != nil {
		return "", err
	}
	version := pyStrip(agents.DecodeUTF8Replace(content))
	if !coreVersionPattern.MatchString(version) {
		return "", errNotAVersion
	}
	return version, nil
}

var errNotAVersion = errors.New("version.txt is not a version")

// checkFailure is _check_failure: a short reason without URLs, proxy
// credentials or response bodies.
func checkFailure(err error) string {
	var fetchErr *rulefetch.RuleFetchError
	if errors.As(err, &fetchErr) && fetchErr.LastError != nil {
		err = fetchErr.LastError // source and mirror both failed: report the last try
	}
	if rulefetch.IsTimeout(err) {
		return "连接 GitHub 超时"
	}
	var f *rulefetch.FetchError
	if errors.As(err, &f) && f.Class == "HTTPError" && f.Status != 0 {
		return "GitHub 返回 HTTP " + strconv.Itoa(f.Status)
	}
	if rulefetch.IsRequestException(err) {
		return "无法连接 GitHub"
	}
	if class := rulefetch.PythonClass(err); class != "ValueError" {
		return class
	}
	return "version.txt 内容不是版本号"
}

// releaseParts is _RELEASE.fullmatch: v?(\d+)\.(\d+)\.(\d+) with Unicode
// decimal digits; the parts are the digit texts (ASCII).
func releaseParts(version string) ([]string, bool) {
	version = strings.TrimPrefix(version, "v")
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return nil, false
	}
	out := make([]string, 3)
	for i, part := range parts {
		if part == "" {
			return nil, false
		}
		var digits strings.Builder
		for _, r := range part {
			d := pyDecimalValue(r)
			if d < 0 {
				return nil, false
			}
			digits.WriteByte(byte('0' + d))
		}
		out[i] = digits.String()
	}
	return out, true
}

// releaseInts is tuple(map(int, match.groups())): int() refuses a text of
// more than 4300 digits (ValueError, not caught by the panel: 500).
func releaseInts(parts []string) ([]*big.Int, error) {
	out := make([]*big.Int, len(parts))
	for i, part := range parts {
		if len(part) > maxIntDigits {
			return nil, fmt.Errorf("ValueError: Exceeds the limit (%d digits) for integer string conversion: value has %d digits; use sys.set_int_max_str_digits() to increase the limit", maxIntDigits, len(part))
		}
		out[i], _ = new(big.Int).SetString(part, 10)
	}
	return out, nil
}

// newerRelease is _newer_release(current, latest).
func newerRelease(current, latest string) (bool, error) {
	partsA, okA := releaseParts(current)
	partsB, okB := releaseParts(latest)
	if !okA || !okB {
		return false, nil
	}
	a, err := releaseInts(partsA)
	if err != nil {
		return false, err
	}
	b, err := releaseInts(partsB)
	if err != nil {
		return false, err
	}
	for i := range a {
		if c := a[i].Cmp(b[i]); c != 0 {
			return c > 0, nil
		}
	}
	return false, nil
}

// usable is _usable(cached, current, now, settings).
func usable(cached *coreCached, current string, now float64, settings *jsonx.Object) (bool, error) {
	if !jsonx.Equal(cached.settings, settings) {
		return false, nil
	}
	if cached.err != "" {
		return now-cached.finishedAt < CoreCheckFailureTTL, nil
	}
	ttl := CoreCheckTTL
	newer, err := newerRelease(current, cached.latest)
	if err != nil {
		return false, err
	}
	if newer {
		ttl = CoreCheckFailureTTL
	}
	return now-cached.finishedAt < ttl, nil
}

// cachedLatest is _cached_latest(channel, current, force): (version,
// error) from the cache unless forced or no longer usable.
func cachedLatest(ctx context.Context, channel, current string, force bool) (string, string, error) {
	requested := monotonic()
	settings, err := downloadSettings(ctx)
	if err != nil {
		return "", "", err
	}
	cached := coreCachedOf(channel)
	if cached != nil && !force {
		ok, err := usable(cached, current, requested, settings)
		if err != nil {
			return "", "", err
		}
		if ok {
			return cached.latest, cached.err, nil
		}
	}
	lock := coreLocks[channel]
	// An automatic check with something to show does not wait for another
	// request's fetch.
	if force || cached == nil {
		lock.Lock()
	} else if !lock.TryLock() {
		return cached.latest, cached.err, nil
	}
	defer lock.Unlock()
	cached = coreCachedOf(channel)
	// Fetched by another request after this one started: reuse it.
	if cached != nil && jsonx.Equal(cached.settings, settings) {
		reuse := cached.finishedAt >= requested
		if !reuse && !force {
			var err error
			if reuse, err = usable(cached, current, monotonic(), settings); err != nil {
				return "", "", err
			}
		}
		if reuse {
			return cached.latest, cached.err, nil
		}
	}
	latest, failure := "", ""
	if version, err := latestCoreVersion(ctx, channel, settings); err != nil {
		failure = "无法获取最新版本（" + checkFailure(err) + "），可检查设置里的「规则下载代理」和「GitHub 代理域名」。"
	} else {
		latest = version
	}
	entry := &coreCached{finishedAt: monotonic(), latest: latest, err: failure, settings: settings}
	coreLatestMu.Lock()
	coreLatest[channel] = entry
	coreLatestMu.Unlock()
	return latest, failure, nil
}

// CheckCoreUpdate is check_core_update(manager, agent_id, force): compare
// the running core with the version its own updater would install.
func CheckCoreUpdate(ctx context.Context, m Manager, agentID string, force bool) (*jsonx.Object, error) {
	result, err := PanelCall(ctx, m, agentID, "version", "GET", nil, nil, PanelResourceMinVersion["upgrade"])
	if err != nil {
		return nil, err
	}
	var current any
	if data, ok := result.Value("data").(*jsonx.Object); ok && data != nil {
		current = data.Value("version")
	}
	currentText, ok := current.(string)
	if !ok || currentText == "" {
		return nil, panelFail("INVALID_RESPONSE", 0, "")
	}
	channel := "release"
	if strings.HasPrefix(currentText, "alpha") {
		channel = "alpha"
	}
	latest, failure, err := cachedLatest(ctx, channel, currentText, force)
	if err != nil {
		return nil, err
	}
	hasUpdate := latest != "" && latest != currentText
	if hasUpdate {
		newer, err := newerRelease(currentText, latest)
		if err != nil {
			return nil, err
		}
		hasUpdate = !newer
	}
	data := jsonx.Obj("current", currentText, "channel", channel, "latest", latest,
		"has_update", hasUpdate, "error", failure)
	if latest != "" {
		data.Set("release_url", strings.ReplaceAll(coreReleasePages[channel], "{version}", weburl.Quote(latest)))
	}
	return jsonx.Obj("success", true, "data", data), nil
}

// PanelCall is call(manager, agent_id, resource, method, args, body,
// min_version): one whitelisted panel request through the Agent. body nil
// is None; minVersion "" is the resource's own minimum.
func PanelCall(ctx context.Context, m Manager, agentID, resource, method string, args [][2]string, body any, minVersion string) (*jsonx.Object, error) {
	if resource == "core/update" && method == "GET" {
		return CheckCoreUpdate(ctx, m, agentID, argGet(args, "force", "") == "1")
	}
	template, ok := PanelRoutes[panelRoute{resource, method}]
	if !ok {
		return nil, panelFail("NOT_ALLOWED", 404, "面板不支持这个操作。")
	}
	if minVersion == "" {
		if minVersion, ok = PanelResourceMinVersion[resource]; !ok {
			minVersion = PanelMinAgentVersion
		}
	}
	agent, err := panelAgent(ctx, m, agentID, minVersion, panelResourceFeature[resource])
	if err != nil {
		return nil, err
	}
	path := template
	for _, key := range []string{"provider", "name"} {
		if strings.Contains(template, "{"+key+"}") {
			name, err := panelName(args, key)
			if err != nil {
				return nil, err
			}
			path = strings.ReplaceAll(path, "{"+key+"}", weburl.Quote(name))
		}
	}
	params, err := panelQuery(resource, args)
	if err != nil {
		return nil, err
	}
	payload, err := panelBody(resource, method, body)
	if err != nil {
		return nil, err
	}
	origin, err := AgentOrigin(agent)
	if err != nil {
		return nil, err
	}
	url := origin + "/api/mihomo/" + path
	timeout, ok := panelResourceTimeout[resource]
	if !ok {
		timeout = 20
		if strings.HasSuffix(resource, "delay") || strings.HasSuffix(resource, "healthcheck") || strings.HasPrefix(resource, "providers/") {
			timeout = 40
		}
	}
	token, ok := agent.Value("token").(string)
	if !ok {
		return nil, panelFail("AGENT_UNREACHABLE", 0, "") // KeyError / TypeError
	}
	encodedToken, ok := latin1(token)
	if !ok {
		return nil, panelFail("AGENT_UNREACHABLE", 0, "") // UnicodeEncodeError
	}
	if len(params) > 0 {
		url += "?" + params.encode()
	}
	header := http.Header{}
	header.Set("Authorization", "Bearer "+encodedToken)
	header.Set("Accept", "application/json")
	var data []byte
	if payload != nil {
		header.Set("Content-Type", "application/json")
		if data, err = jsonx.Default(payload); err != nil {
			return nil, err
		}
	}
	response, err := m.Client().Do(ctx, agents.Request{Method: method, URL: url, Header: header, Body: data,
		Timeout: agents.Pair(3, timeout), NoProxy: true, NoRedirects: true, MaxBody: PanelMaxResponseBytes})
	if err != nil {
		return nil, panelFail("AGENT_UNREACHABLE", 0, "")
	}
	if len(response.Body) > PanelMaxResponseBytes {
		return nil, panelFail("INVALID_RESPONSE", 0, "")
	}
	if response.BodyError != nil {
		return nil, panelFail("AGENT_UNREACHABLE", 0, "")
	}
	status := response.StatusCode
	if status == 401 || status == 403 {
		return nil, panelFail("AGENT_AUTH_FAILED", 0, "")
	}
	var value any
	if len(strings.Trim(string(response.Body), " \t\n\r\v\f")) > 0 {
		parsed, ok := loadsBytes(response.Body)
		if !ok {
			if status == 404 {
				return nil, panelFail("UPGRADE_REQUIRED", 409, "请将此 Agent 升级到 "+PanelMinAgentVersion+" 或更新版本后使用 Mihomo 面板。")
			}
			if status < 400 {
				return nil, panelFail("INVALID_RESPONSE", 0, "")
			}
		} else {
			value = parsed
		}
	}
	if status >= 400 {
		answer, isDict := value.(*jsonx.Object)
		if isDict && answer != nil && answer.Value("success") == false {
			code := answer.Value("code")
			if unhashable(code) {
				return nil, typeErrorUnhashable(code)
			}
			if text, ok := code.(string); ok {
				if _, known := PanelMessages[text]; known {
					errStatus := 502
					if text == "CONTROLLER_UNAVAILABLE" {
						errStatus = 409
					}
					return nil, panelFail(text, errStatus, "")
				}
			}
		}
		// Mihomo's own errors (delay timeout, unknown proxy …) are short
		// and safe to show.
		var message string
		if isDict && answer != nil {
			message, _ = answer.Value("message").(string)
		}
		if message == "" {
			message = "Mihomo 返回 HTTP " + strconv.Itoa(status)
		}
		errStatus := 502
		switch {
		case status == 408 || status == 504:
			errStatus = 504
		case status < 500:
			errStatus = 400
		}
		return nil, panelFail("CORE_ERROR", errStatus, pySlice(message, 300))
	}
	if method == "GET" {
		value = slim(resource, value)
	}
	return jsonx.Obj("success", true, "data", value), nil
}

// typeErrorUnhashable is the TypeError of a list or dict in a set or dict
// membership test (Python answers 500).
func typeErrorUnhashable(value any) error {
	return errors.New("TypeError: unhashable type: " + pyTypeName(value))
}
