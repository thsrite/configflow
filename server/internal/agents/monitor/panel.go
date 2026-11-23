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
	"bytes"
	"configflow/internal/textx"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strings"
	"sync"

	"configflow/internal/agents"
	"configflow/internal/jsondecode"
	"configflow/internal/jsonresp"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/redact"
	"configflow/internal/repo"
	"configflow/internal/rulefetch"
	"configflow/internal/taskstatus"
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

var (
	panelLogLevels       = []string{"debug", "info", "warning", "error"}
	panelResourceTimeout = map[string]float64{"configs/reload": 70, "upgrade": 200}
	panelModes           = []string{"rule", "global", "direct"}
	// panelProxyFields is PROXY_FIELDS: what the panel reads from a node or group.
	panelProxyFields = []string{"name", "type", "udp", "alive", "now", "all", "fixed", "testUrl", "hidden", "icon"}
)

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

func readPanelAgent(ctx context.Context, m Registry, id, minimum, feature string) (*model.Agent, error) {
	a, err := m.ReadAgent(ctx, id, false)
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, panelFail("AGENT_NOT_FOUND", 404, msgAgentMissing)
	}
	if a.ServiceType != "" && a.ServiceType != "mihomo" {
		return nil, panelFail("WRONG_SERVICE", 400, msgPanelWrongService)
	}
	if !modelVersionAtLeast(a, minimum) {
		if feature == "" {
			feature = msgCoreOperation
			if minimum == PanelMinAgentVersion {
				feature = msgMihomoPanel
			}
		}
		return nil, panelFail("UPGRADE_REQUIRED", 409, fmt.Sprintf(msgPanelUpgrade, minimum, feature))
	}
	return a, nil
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

var decimalIntegerRE = regexp.MustCompile(`^[+-]?[0-9]+(?:_[0-9]+)*$`)

// decimalInteger accepts bounded decimal input without interpreting Unicode digits.
func decimalInteger(text string) (*big.Int, bool) {
	text = strings.TrimSpace(text)
	if !decimalIntegerRE.MatchString(text) {
		return nil, false
	}
	digits := strings.ReplaceAll(text, "_", "")
	if len(strings.TrimLeft(digits, "+-")) > 4300 {
		return nil, false
	}
	return new(big.Int).SetString(digits, 10)
}

// panelLogsQuery is _logs_query(args).
func panelLogsQuery(args [][2]string) (query, error) {
	level := argGet(args, "level", "info")
	if !contains(panelLogLevels, level) {
		return nil, panelFail("INVALID_REQUEST", 400, msgLogLevel)
	}
	after := argGet(args, "after", "0")
	if !textx.ASCIIDigits(after) || runeLen(after) > 18 {
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
		return nil, panelFail("INVALID_REQUEST", 400, msgTestURL)
	}
	timeout := big.NewInt(5000)
	if text, ok := lookupArg(args, "timeout"); ok {
		value, ok := decimalInteger(text)
		if !ok {
			return nil, panelFail("INVALID_REQUEST", 400, "")
		}
		timeout = value
	}
	if timeout.Cmp(big.NewInt(500)) < 0 || timeout.Cmp(big.NewInt(30000)) > 0 {
		return nil, panelFail("INVALID_REQUEST", 400, msgTestTimeout)
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

func panelPayload(resource, method string, body json.RawMessage) (json.RawMessage, error) {
	if method != "PUT" && method != "PATCH" || resource == "configs/reload" {
		return nil, nil
	}
	key, message := "", ""
	switch resource {
	case "proxy":
		key, message = "name", msgSelectNode
	case "configs":
		key, message = "mode", msgPanelMode
	default:
		return []byte("{}"), nil
	}
	var fields model.Fields
	if jsondecode.Unmarshal(body, &fields) != nil || fields.Len() != 1 || !fields.Has(key) {
		return nil, panelFail("INVALID_REQUEST", 400, message)
	}
	value := fieldText(fields, key)
	if key == "name" && (value == "" || runeLen(value) > PanelMaxNameLength) || key == "mode" && !contains(panelModes, value) {
		return nil, panelFail("INVALID_REQUEST", 400, message)
	}
	return jsonresp.Encode(fields)
}

func slimProxyJSON(raw json.RawMessage) json.RawMessage {
	var fields model.Fields
	if jsondecode.Unmarshal(raw, &fields) != nil {
		return raw
	}
	var result model.Fields
	for _, key := range panelProxyFields {
		if value, present := fields.Get(key); present {
			_ = result.Set(key, value)
		}
	}
	if raw, present := fields.Get("history"); present {
		var history []json.RawMessage
		if jsondecode.Unmarshal(raw, &history) == nil && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			_ = result.Set("history", lastHistory(history))
		}
	}
	if raw, present := fields.Get("extra"); present {
		var extra model.Fields
		if jsondecode.Unmarshal(raw, &extra) == nil {
			var out model.Fields
			for name, raw := range extra.All() {
				var entry model.Fields
				if jsondecode.Unmarshal(raw, &entry) != nil {
					continue
				}
				var item model.Fields
				if value, present := entry.Get("alive"); present {
					_ = item.Set("alive", value)
				}
				historyRaw, _ := entry.Get("history")
				var history []json.RawMessage
				_ = jsondecode.Unmarshal(historyRaw, &history)
				_ = item.Set("history", lastHistory(history))
				encoded, _ := json.Marshal(item)
				_ = out.Set(name, encoded)
			}
			encoded, _ := json.Marshal(out)
			_ = result.Set("extra", encoded)
		}
	}
	encoded, _ := json.Marshal(result)
	return encoded
}
func lastHistory(history []json.RawMessage) json.RawMessage {
	if len(history) == 0 {
		return []byte("[]")
	}
	encoded, _ := json.Marshal(history[len(history)-1:])
	return encoded
}
func slimPanel(resource string, raw json.RawMessage) json.RawMessage {
	key := ""
	switch resource {
	case "proxies":
		key = "proxies"
	case "providers/proxies":
		key = "providers"
	default:
		return raw
	}
	var fields model.Fields
	if jsondecode.Unmarshal(raw, &fields) != nil {
		return raw
	}
	values, _ := fields.Get(key)
	var entries model.Fields
	if jsondecode.Unmarshal(values, &entries) != nil {
		return raw
	}
	var output model.Fields
	for name, value := range entries.All() {
		if resource == "proxies" {
			value = slimProxyJSON(value)
		} else {
			var provider model.Fields
			if jsondecode.Unmarshal(value, &provider) == nil {
				rawList, _ := provider.Get("proxies")
				var list []json.RawMessage
				if jsondecode.Unmarshal(rawList, &list) == nil && !bytes.Equal(bytes.TrimSpace(rawList), []byte("null")) {
					compatible := fieldText(provider, "vehicleType") == "Compatible"
					for i, item := range list {
						var proxy model.Fields
						if compatible && jsondecode.Unmarshal(item, &proxy) == nil {
							name, present := proxy.Get("name")
							if !present {
								name = []byte("null")
							}
							list[i], _ = json.Marshal(struct {
								Name json.RawMessage `json:"name"`
							}{name})
						} else {
							list[i] = slimProxyJSON(item)
						}
					}
					encoded, _ := json.Marshal(list)
					_ = provider.Set("proxies", encoded)
					value, _ = json.Marshal(provider)
				}
			}
		}
		_ = output.Set(name, value)
	}
	encoded, _ := json.Marshal(output)
	_ = fields.Set(key, encoded)
	encoded, _ = json.Marshal(fields)
	return encoded
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
	settings   *model.SystemConfig
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
var downloadSettings = func(ctx context.Context) (*model.SystemConfig, error) {
	repository, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	var settings model.SystemConfig
	err = repository.View(ctx, func(document *model.Document) error {
		source := &document.System.SystemConfig
		settings.RuleFetchProxy, settings.GitHubProxyDomain, settings.ServerDomain = source.RuleFetchProxy, source.GitHubProxyDomain, source.ServerDomain
		return nil
	})
	return &settings, err
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
var requestRule = rulefetch.Download

// latestCoreVersion is _latest_core_version: the channel's version.txt read
// with the rule download proxy and GitHub mirror (a variable for tests).
var latestCoreVersion = func(ctx context.Context, channel string, settings *model.SystemConfig) (string, error) {
	config := &rulefetch.SourceConfig{Settings: *settings}
	response, err := requestRule(ctx, CoreVersionURLs[channel], rulefetch.DownloadRequest{Config: config,
		ConnectTimeout: 5e9, ReadTimeout: 10e9, Stream: true, MaxBytes: coreVersionMaxBodyBytes, HostURL: hostURLOf(ctx)})
	if err != nil {
		return "", err
	}
	content, err := response.Content()
	response.Close()
	if err != nil {
		return "", err
	}
	version := trimText(agents.DecodeUTF8Replace(content))
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
		return msgGithubTimeout
	}
	var f *rulefetch.FetchError
	if errors.As(err, &f) && f.Class == "HTTPError" && f.Status != 0 {
		return fmt.Sprintf(msgGithubHTTP, f.Status)
	}
	if rulefetch.IsRequestException(err) {
		return msgGithubUnreachable
	}
	return msgVersionContent
}

// releaseParts recognizes an ASCII dotted core release.
func releaseParts(version string) ([]string, bool) {
	parts := strings.Split(strings.TrimPrefix(version, "v"), ".")
	if len(parts) != 3 {
		return nil, false
	}
	for _, part := range parts {
		if part == "" {
			return nil, false
		}
		for _, r := range part {
			if r < '0' || r > '9' {
				return nil, false
			}
		}
	}
	return parts, true
}

func releaseInts(parts []string) ([]*big.Int, error) {
	result := make([]*big.Int, len(parts))
	for i, part := range parts {
		raw, _ := json.Marshal(part)
		value, err := taskstatus.Count(raw)
		if err != nil {
			return nil, fmt.Errorf("parse core release component: %w", err)
		}
		result[i] = value
	}
	return result, nil
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
func usable(cached *coreCached, current string, now float64, settings *model.SystemConfig) (bool, error) {
	if !sameDownloadSettings(cached.settings, settings) {
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
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
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
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	cached = coreCachedOf(channel)
	// Fetched by another request after this one started: reuse it.
	if cached != nil && sameDownloadSettings(cached.settings, settings) {
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
		failure = fmt.Sprintf(msgCoreFetchFailure, checkFailure(err))
	} else {
		latest = version
	}
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	entry := &coreCached{finishedAt: monotonic(), latest: latest, err: failure, settings: settings}
	coreLatestMu.Lock()
	if err := ctx.Err(); err != nil {
		coreLatestMu.Unlock()
		return "", "", err
	}
	coreLatest[channel] = entry
	coreLatestMu.Unlock()
	return latest, failure, nil
}

func sameDownloadSettings(a, b *model.SystemConfig) bool {
	return a != nil && b != nil && a.RuleFetchProxy == b.RuleFetchProxy && a.GitHubProxyDomain == b.GitHubProxyDomain && a.ServerDomain == b.ServerDomain
}

// PanelResult carries the whitelisted core response as opaque, validated JSON.
type PanelResult struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
}

// CoreUpdate is the bounded, credential-free result of a version check.
type CoreUpdate struct {
	Current    string `json:"current"`
	Channel    string `json:"channel"`
	Latest     string `json:"latest"`
	HasUpdate  bool   `json:"has_update"`
	Error      string `json:"error"`
	ReleaseURL string `json:"release_url,omitempty"`
}

// CoreUpdateCheck compares the running version with its core update channel.
func CoreUpdateCheck(ctx context.Context, m Registry, id string, force bool) (*PanelResult, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result, err := CallPanel(ctx, m, id, "version", "GET", nil, nil, PanelResourceMinVersion["upgrade"])
	if err != nil {
		return nil, err
	}
	var fields model.Fields
	if jsondecode.Unmarshal(result.Data, &fields) != nil {
		return nil, panelFail("INVALID_RESPONSE", 0, "")
	}
	current := fieldText(fields, "version")
	// Machine versions are ASCII, including alpha revisions. A Unicode lookalike
	// cannot authorize an upgrade or suppress one through numeric coercion.
	if !coreVersionPattern.MatchString(current) {
		return nil, panelFail("INVALID_RESPONSE", 0, "")
	}
	channel := "release"
	if strings.HasPrefix(current, "alpha") {
		channel = "alpha"
	}
	latest, failure, err := cachedLatest(ctx, channel, current, force)
	if err != nil {
		return nil, err
	}
	update := latest != "" && latest != current
	if update {
		newer, err := newerRelease(current, latest)
		if err != nil {
			return nil, err
		}
		update = !newer
	}
	data := CoreUpdate{Current: current, Channel: channel, Latest: latest, HasUpdate: update, Error: failure}
	if latest != "" {
		data.ReleaseURL = strings.ReplaceAll(coreReleasePages[channel], "{version}", weburl.Quote(latest))
	}
	raw, err := json.Marshal(data)
	return &PanelResult{Success: true, Data: raw}, err
}

// CallPanel validates a whitelisted panel operation and sends only its rebuilt
// query/body to the registered Agent. GET/DELETE requests carry no JSON body.
func CallPanel(ctx context.Context, m Registry, id, resource, method string, args [][2]string, body json.RawMessage, minimum string) (*PanelResult, error) {
	if resource == "core/update" && method == "GET" {
		return CoreUpdateCheck(ctx, m, id, argGet(args, "force", "") == "1")
	}
	template, allowed := PanelRoutes[panelRoute{resource, method}]
	if !allowed {
		return nil, panelFail("NOT_ALLOWED", 404, msgPanelNotAllowed)
	}
	if method != "GET" {
		ctx = lifecycle.Detach(ctx)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if minimum == "" {
		minimum = PanelResourceMinVersion[resource]
		if minimum == "" {
			minimum = PanelMinAgentVersion
		}
	}
	a, err := readPanelAgent(ctx, m, id, minimum, panelResourceFeature[resource])
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
	payload, err := panelPayload(resource, method, body)
	if err != nil {
		return nil, err
	}
	origin, err := Origin(a)
	if err != nil {
		return nil, err
	}
	timeout, ok := panelResourceTimeout[resource]
	if !ok {
		timeout = 20
		if strings.HasSuffix(resource, "delay") || strings.HasSuffix(resource, "healthcheck") || strings.HasPrefix(resource, "providers/") {
			timeout = 40
		}
	}
	token := a.Token
	valid := token != ""
	if !valid {
		return nil, panelFail("AGENT_UNREACHABLE", 0, "")
	}
	header, err := monitorHeaders(token)
	if err != nil {
		return nil, panelFail("AGENT_UNREACHABLE", 0, "")
	}
	url := origin + "/api/mihomo/" + path
	if len(params) > 0 {
		url += "?" + params.encode()
	}
	if payload != nil {
		header.Set("Content-Type", "application/json")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	response, err := m.Client().Do(ctx, agents.Request{Method: method, URL: url, Header: header, Body: payload, Timeout: agents.Pair(3, timeout), NoProxy: true, NoRedirects: true, MaxBody: PanelMaxResponseBytes})
	if err != nil {
		return nil, panelFail("AGENT_UNREACHABLE", 0, "")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if response == nil || len(response.Body) > PanelMaxResponseBytes {
		return nil, panelFail("INVALID_RESPONSE", 0, "")
	}
	if response.BodyError != nil {
		return nil, panelFail("AGENT_UNREACHABLE", 0, "")
	}
	status := response.StatusCode
	if status == 401 || status == 403 {
		return nil, panelFail("AGENT_AUTH_FAILED", 0, "")
	}
	value := json.RawMessage("null")
	if len(bytes.Trim(response.Body, " \t\n\r\v\f")) > 0 {
		text, valid := agents.DecodeJSONBytes(response.Body)
		normalized, _ := jsonresp.NormalizeNonFinite([]byte(text))
		var parsed model.Dynamic
		if !valid || jsondecode.Unmarshal(normalized, &parsed) != nil {
			if status == 404 {
				return nil, panelFail("UPGRADE_REQUIRED", 409, msgPanelEndpointUpgrade)
			}
			if status < 400 {
				return nil, panelFail("INVALID_RESPONSE", 0, "")
			}
		} else {
			value = normalized
		}
	}
	if status >= 400 {
		var fields model.Fields
		isObject := jsondecode.Unmarshal(value, &fields) == nil
		success, present, valid := answerBool(fields, "success")
		if isObject && present && valid && !success {
			rawCode, _ := fields.Get("code")
			if len(rawCode) > 0 && (rawCode[0] == '[' || rawCode[0] == '{') {
				return nil, errors.New(msgPanelErrorShape)
			}
			code := fieldText(fields, "code")
			if _, known := PanelMessages[code]; known {
				status := 502
				if code == "CONTROLLER_UNAVAILABLE" {
					status = 409
				}
				return nil, panelFail(code, status, "")
			}
		}
		message := fieldText(fields, "message")
		if message == "" {
			message = fmt.Sprintf(msgPanelHTTP, status)
		}
		message = redact.DiagnosticText(message, redact.Secrets{token}, 0)
		errStatus := 502
		switch {
		case status == 408 || status == 504:
			errStatus = 504
		case status < 500:
			errStatus = 400
		}
		return nil, panelFail("CORE_ERROR", errStatus, truncateText(message, 300))
	}
	if method == "GET" {
		value = slimPanel(resource, value)
	}
	return &PanelResult{Success: true, Data: value}, nil
}
