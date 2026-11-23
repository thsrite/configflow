package routes

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

const (
	oxMonitorJSONLimit         = 4 << 20
	oxMonitorMetricsLimit      = 2 << 20
	oxMonitorTimeout           = 5 * time.Second
	oxMonitorClientSampleLimit = 10000
	oxMonitorExactIPPages      = 10
)

type oxMonitorError struct {
	status  int
	code    string
	message string
}

func oxMonitorFail(status int, code, message string) *oxMonitorError {
	return &oxMonitorError{status, code, message}
}

func oxMonitorWriteError(w http.ResponseWriter, err *oxMonitorError) {
	w.Header().Set("Cache-Control", "no-store")
	JsonResponse(w, err.status, CodedErrorResponse{Code: err.code, Message: err.message, Success: false})
}

// This wrapper preserves the monitoring error contract without changing legacy
// endpoints. Neither the Agent token nor incoming headers reach the core API.
func oxidnsMonitorAuth(cfg *Config, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, valid := parseBearerToken(r.Header.Get("Authorization"))
		if len(r.Header.Values("Authorization")) != 1 || !valid || subtle.ConstantTimeCompare(token, []byte(cfg.Token)) != 1 {
			oxMonitorWriteError(w, oxMonitorFail(http.StatusUnauthorized, "UNAUTHORIZED", "Agent authentication required"))
			return
		}
		next(w, r)
	}
}

type oxMonitorCapabilities struct {
	QueryRecorders []string `json:"query_recorders"`
	Caches         []string `json:"caches"`
}

type oxMonitorConfig struct {
	endpoint       string
	username       string
	password       string
	basicAuth      bool
	capabilities   oxMonitorCapabilities
	sourceRevision string
	rankings       oxRankingConfig
}

func oxMonitorConfigError() *oxMonitorError {
	return oxMonitorFail(http.StatusConflict, "CONFIG_UNSUPPORTED", "OxiDNS API configuration is not supported")
}

// Read the active configuration for every request, so a pushed configuration
// cannot leave stale credentials or plugin instances in the monitoring bridge.
func loadOxiDNSMonitorConfig(cfg *Config) (*oxMonitorConfig, *oxMonitorError) {
	if cfg.ServiceType != "oxidns" {
		return nil, oxMonitorFail(http.StatusConflict, "CONFIG_UNSUPPORTED", "Monitoring requires an OxiDNS agent")
	}
	file, err := os.Open(cfg.ConfigPath)
	if err != nil {
		return nil, oxMonitorFail(http.StatusConflict, "CONFIG_UNSUPPORTED", "Cannot read the active OxiDNS configuration")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, oxMonitorJSONLimit+1))
	if err != nil || len(data) > oxMonitorJSONLimit {
		return nil, oxMonitorFail(http.StatusConflict, "CONFIG_UNSUPPORTED", "Cannot read the active OxiDNS configuration")
	}
	var doc map[string]any
	if yaml.Unmarshal(data, &doc) != nil {
		return nil, oxMonitorConfigError()
	}
	if includes, ok := doc["include"].([]any); ok && len(includes) > 0 {
		// Managed deployments inline includes; do not guess the core loader's
		// filesystem/environment expansion rules for other configurations.
		return nil, oxMonitorConfigError()
	}
	api, _ := doc["api"].(map[string]any)
	if api == nil || api["http"] == nil {
		return nil, oxMonitorFail(http.StatusConflict, "API_NOT_CONFIGURED", "Enable the OxiDNS HTTP API to use monitoring")
	}
	config := &oxMonitorConfig{capabilities: oxMonitorCapabilities{QueryRecorders: []string{}, Caches: []string{}}}
	revision := sha256.Sum256(data)
	config.sourceRevision = hex.EncodeToString(revision[:])
	var listen string
	switch httpConfig := api["http"].(type) {
	case string:
		listen = httpConfig
	case map[string]any:
		listen, _ = httpConfig["listen"].(string)
		if ssl := httpConfig["ssl"]; ssl != nil {
			return nil, oxMonitorFail(http.StatusConflict, "TLS_UNSUPPORTED", "OxiDNS API TLS is not supported by this monitoring bridge")
		}
		if rawAuth := httpConfig["auth"]; rawAuth != nil {
			auth, ok := rawAuth.(map[string]any)
			if !ok || auth["type"] != "basic" {
				return nil, oxMonitorConfigError()
			}
			config.username, ok = auth["username"].(string)
			if !ok || config.username == "" || strings.Contains(config.username, ":") {
				return nil, oxMonitorConfigError()
			}
			config.password, ok = auth["password"].(string)
			if !ok || config.password == "" || !oxMonitorLiteral(config.username) || !oxMonitorLiteral(config.password) {
				return nil, oxMonitorConfigError()
			}
			config.basicAuth = true
		}
	default:
		return nil, oxMonitorConfigError()
	}
	if strings.TrimSpace(listen) == "" {
		return nil, oxMonitorFail(http.StatusConflict, "API_NOT_CONFIGURED", "Enable the OxiDNS HTTP API to use monitoring")
	}
	address, err := oxMonitorLocalAddress(listen)
	if err != nil {
		return nil, oxMonitorFail(http.StatusConflict, "CONFIG_UNSUPPORTED", "The OxiDNS API must listen on a local IP address")
	}
	config.endpoint = "http://" + address
	plugins, _ := doc["plugins"].([]any)
	seen := make(map[string]bool)
	for _, item := range plugins {
		plugin, _ := item.(map[string]any)
		kind, _ := plugin["type"].(string)
		if kind != "query_recorder" && kind != "cache" {
			continue
		}
		tag, _ := plugin["tag"].(string)
		// Untagged plugins have no addressable API instance.
		if tag == "" {
			continue
		}
		if !oxMonitorSafeTag(tag) || seen[tag] {
			return nil, oxMonitorConfigError()
		}
		seen[tag] = true
		if kind == "query_recorder" {
			config.capabilities.QueryRecorders = append(config.capabilities.QueryRecorders, tag)
		} else {
			config.capabilities.Caches = append(config.capabilities.Caches, tag)
		}
	}
	sort.Strings(config.capabilities.QueryRecorders)
	sort.Strings(config.capabilities.Caches)
	config.rankings = oxMonitorRankingConfig(plugins)
	return config, nil
}

func oxMonitorLiteral(value string) bool {
	return !strings.Contains(value, "${") && utf8.ValidString(value) && strings.IndexFunc(value, unicode.IsControl) < 0
}

// OxiDNS v1.6.1 configured tags are ASCII, at most 64 bytes, with each
// dot-separated segment beginning and ending with an alphanumeric character.
func oxMonitorSafeTag(tag string) bool {
	if len(tag) == 0 || len(tag) > 64 {
		return false
	}
	alphanumeric := func(char byte) bool {
		return char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9'
	}
	for i := 0; i < len(tag); i++ {
		if !alphanumeric(tag[i]) && tag[i] != '_' && tag[i] != '-' && tag[i] != '.' {
			return false
		}
	}
	for _, segment := range strings.Split(tag, ".") {
		if segment == "" || !alphanumeric(segment[0]) || !alphanumeric(segment[len(segment)-1]) {
			return false
		}
	}
	return true
}

func oxMonitorLocalAddress(listen string) (string, error) {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return "", errors.New("invalid listen address")
	}
	portNumber, err := strconv.ParseUint(port, 10, 16)
	if err != nil || portNumber == 0 {
		return "", errors.New("invalid listen port")
	}
	// OxiDNS treats an empty host (e.g. :9199) as IPv6 wildcard.
	if host == "" {
		host = "::"
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return "", errors.New("listen must use an IP literal")
	}
	if ip.IsUnspecified() {
		if ip.To4() != nil {
			ip = net.ParseIP("127.0.0.1")
		} else {
			ip = net.IPv6loopback
		}
	}
	local := ip.IsLoopback()
	if !local {
		addresses, err := net.InterfaceAddrs()
		if err != nil {
			return "", err
		}
		for _, address := range addresses {
			addressIP, _, err := net.ParseCIDR(address.String())
			if err == nil && addressIP.Equal(ip) {
				local = true
				break
			}
		}
	}
	if !local {
		return "", errors.New("listen IP is not local")
	}
	return net.JoinHostPort(ip.String(), strconv.FormatUint(portNumber, 10)), nil
}

func oxMonitorBadQuery() *oxMonitorError {
	return oxMonitorFail(http.StatusBadRequest, "INVALID_QUERY", "Invalid or unsupported monitoring query parameters")
}

func oxMonitorParseQuery(raw string, detail bool) (string, url.Values, *oxMonitorError) {
	if len(raw) > 8192 {
		return "", nil, oxMonitorBadQuery()
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return "", nil, oxMonitorBadQuery()
	}
	for key, items := range values {
		if len(items) != 1 || !utf8.ValidString(items[0]) || strings.IndexFunc(items[0], unicode.IsControl) >= 0 {
			return "", nil, oxMonitorBadQuery()
		}
		if key == "recorder" {
			if !oxMonitorSafeTag(items[0]) {
				return "", nil, oxMonitorBadQuery()
			}
			continue
		}
		if detail {
			return "", nil, oxMonitorBadQuery()
		}
		value := items[0]
		switch key {
		case "limit":
			n, err := strconv.ParseUint(value, 10, 16)
			if err != nil || n < 1 || n > 100 {
				return "", nil, oxMonitorBadQuery()
			}
			values.Set(key, strconv.FormatUint(n, 10))
		case "cursor":
			parts := strings.Split(value, ":")
			if len(parts) != 2 {
				return "", nil, oxMonitorBadQuery()
			}
			for _, part := range parts {
				if _, err := strconv.ParseInt(part, 10, 64); err != nil {
					return "", nil, oxMonitorBadQuery()
				}
			}
		case "since_ms", "until_ms":
			if _, err := strconv.ParseUint(value, 10, 64); err != nil {
				return "", nil, oxMonitorBadQuery()
			}
		case "status":
			value = strings.TrimSpace(value)
			if value != "" && value != "all" && value != "error" && value != "has_response" && value != "no_response" {
				return "", nil, oxMonitorBadQuery()
			}
			values.Set(key, value)
		case "rcode":
			if len(value) > 64 {
				return "", nil, oxMonitorBadQuery()
			}
			for _, char := range value {
				if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == ' ' || char == '_' || char == '-') {
					return "", nil, oxMonitorBadQuery()
				}
			}
			// OxiDNS records use display text such as "No Error". Keep its
			// spelling and internal spaces; the server handles UI aliases.
			values.Set(key, strings.TrimSpace(value))
		case "qname", "client_ip", "matcher_tag", "qtype":
			if len(value) > 512 {
				return "", nil, oxMonitorBadQuery()
			}
			value = strings.TrimSpace(value)
			if key == "client_ip" && value != "" && net.ParseIP(value) == nil {
				return "", nil, oxMonitorBadQuery()
			}
			if key == "qtype" {
				value = strings.ToUpper(value)
			}
			values.Set(key, value)
		default:
			return "", nil, oxMonitorBadQuery()
		}
	}
	if values.Has("since_ms") && values.Has("until_ms") {
		since, _ := strconv.ParseUint(values.Get("since_ms"), 10, 64)
		until, _ := strconv.ParseUint(values.Get("until_ms"), 10, 64)
		if since > until {
			return "", nil, oxMonitorBadQuery()
		}
	}
	recorder := values.Get("recorder")
	values.Del("recorder")
	if !detail && !values.Has("limit") {
		values.Set("limit", "100")
	}
	return recorder, values, nil
}

// Client options deliberately ignore the selected query's other filters. Only
// the time range and an independent client search narrow this bounded sample.
func oxMonitorParseClients(raw string) (string, url.Values, *oxMonitorError) {
	if len(raw) > 8192 {
		return "", nil, oxMonitorBadQuery()
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return "", nil, oxMonitorBadQuery()
	}
	for key, items := range values {
		if key != "recorder" && key != "since_ms" && key != "until_ms" && key != "search" || len(items) != 1 {
			return "", nil, oxMonitorBadQuery()
		}
		if key == "search" {
			if len(items[0]) > 64 || !utf8.ValidString(items[0]) || strings.IndexFunc(items[0], unicode.IsControl) >= 0 {
				return "", nil, oxMonitorBadQuery()
			}
		}
	}
	search := strings.TrimSpace(values.Get("search"))
	values.Del("search")
	recorder, query, queryErr := oxMonitorParseQuery(values.Encode(), false)
	if queryErr != nil {
		return "", nil, queryErr
	}
	query.Set("limit", strconv.Itoa(oxMonitorClientSampleLimit))
	if search != "" {
		query.Set("client_ip", search)
	}
	return recorder, query, nil
}

func (config *oxMonitorConfig) recorder(requested string) (string, *oxMonitorError) {
	available := config.capabilities.QueryRecorders
	if len(available) == 0 {
		return "", oxMonitorFail(http.StatusConflict, "RECORDER_NOT_CONFIGURED", "Enable a tagged OxiDNS query_recorder to view queries")
	}
	if requested == "" {
		if len(available) == 1 {
			return available[0], nil
		}
		return "", oxMonitorFail(http.StatusBadRequest, "INVALID_QUERY", "Select a query recorder")
	}
	for _, tag := range available {
		if tag == requested {
			return tag, nil
		}
	}
	return "", oxMonitorFail(http.StatusBadRequest, "INVALID_QUERY", "The selected query recorder is not in the active configuration")
}

func oxMonitorClient() *http.Client {
	return &http.Client{
		Timeout: oxMonitorTimeout,
		Transport: &http.Transport{
			Proxy:                  nil,
			DialContext:            (&net.Dialer{Timeout: oxMonitorTimeout}).DialContext,
			DisableKeepAlives:      true,
			MaxResponseHeaderBytes: 32 << 10,
		},
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func (config *oxMonitorConfig) fetch(ctx context.Context, client *http.Client, path string, query url.Values, limit int64, isJSON bool) ([]byte, *oxMonitorError) {
	target := config.endpoint + path
	if len(query) != 0 {
		target += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, oxMonitorConfigError()
	}
	if config.basicAuth {
		req.SetBasicAuth(config.username, config.password)
	}
	if isJSON {
		req.Header.Set("Accept", "application/json")
	} else {
		req.Header.Set("Accept", "text/plain")
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, oxMonitorRequestError(ctx, err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return nil, oxMonitorFail(http.StatusBadGateway, "CORE_AUTH_FAILED", "OxiDNS API authentication failed")
	}
	if response.StatusCode == http.StatusNotFound && strings.HasPrefix(path, "/api/plugins/") {
		// Only the official error code identifies an expired/missing record.
		// A missing plugin route must not look like an old Agent or an empty DB.
		data, readErr := io.ReadAll(io.LimitReader(response.Body, 8193))
		if readErr != nil {
			return nil, oxMonitorRequestError(ctx, readErr)
		}
		var failure struct {
			Code string `json:"code"`
		}
		if len(data) <= 8192 && json.Unmarshal(data, &failure) == nil && len(strings.Split(path, "/")) == 6 && strings.Split(path, "/")[4] == "records" && failure.Code == "record_not_found" {
			return nil, oxMonitorFail(http.StatusNotFound, "RECORD_NOT_FOUND", "The query record is no longer available")
		}
		return nil, oxMonitorFail(http.StatusConflict, "RECORDER_NOT_CONFIGURED", "The query recorder API is not available in the running OxiDNS instance")
	}
	if response.StatusCode != http.StatusOK {
		return nil, oxMonitorFail(http.StatusBadGateway, "INVALID_RESPONSE", "OxiDNS API returned an unexpected response")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, oxMonitorRequestError(ctx, err)
	}
	if int64(len(data)) > limit {
		return nil, oxMonitorFail(http.StatusBadGateway, "INVALID_RESPONSE", "OxiDNS API response exceeds the monitoring size limit")
	}
	if !utf8.Valid(data) {
		return nil, oxMonitorFail(http.StatusBadGateway, "INVALID_RESPONSE", "OxiDNS API returned an invalid response")
	}
	if isJSON && !oxMonitorValidJSON(path, data) {
		return nil, oxMonitorFail(http.StatusBadGateway, "INVALID_RESPONSE", "OxiDNS API returned an invalid JSON response")
	}
	return data, nil
}

// Check the documented envelope before forwarding data. Core error documents
// can contain filesystem paths; even HTTP 200 must not bypass error sanitizing.
func oxMonitorValidJSON(path string, data []byte) bool {
	var object map[string]json.RawMessage
	if json.Unmarshal(data, &object) != nil || object == nil {
		return false
	}
	if path == "/api/health" {
		for _, key := range []string{"status", "version", "instance_id"} {
			var value string
			if json.Unmarshal(object[key], &value) != nil || value == "" {
				return false
			}
		}
		var uptime uint64
		return json.Unmarshal(object["uptime_ms"], &uptime) == nil && string(object["uptime_ms"]) != "null"
	}
	var ok bool
	if json.Unmarshal(object["ok"], &ok) != nil || !ok {
		return false
	}
	if strings.HasSuffix(path, "/stats/top_clients") {
		var sample uint64
		var rows []struct {
			Key   string `json:"key"`
			Count uint64 `json:"count"`
		}
		if json.Unmarshal(object["sample_size"], &sample) != nil || string(object["sample_size"]) == "null" || sample > oxMonitorClientSampleLimit || json.Unmarshal(object["rows"], &rows) != nil || rows == nil || len(rows) > oxMonitorClientSampleLimit {
			return false
		}
		seen := make(map[string]bool)
		var total uint64
		for _, row := range rows {
			if net.ParseIP(row.Key) == nil || seen[row.Key] || row.Count == 0 || row.Count > sample {
				return false
			}
			seen[row.Key] = true
			total += row.Count
		}
		return total == sample
	}
	if strings.HasSuffix(path, "/records") {
		var records []json.RawMessage
		if json.Unmarshal(object["records"], &records) != nil || records == nil {
			return false
		}
		var cursor *string
		return json.Unmarshal(object["next_cursor"], &cursor) == nil
	}
	var record map[string]json.RawMessage
	if json.Unmarshal(object["record"], &record) != nil || record == nil {
		return false
	}
	var id int64
	return json.Unmarshal(record["id"], &id) == nil && id > 0
}

func oxMonitorRequestError(ctx context.Context, err error) *oxMonitorError {
	var timeout net.Error
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.As(err, &timeout) && timeout.Timeout() {
		return oxMonitorFail(http.StatusGatewayTimeout, "CORE_UNREACHABLE", "OxiDNS API request timed out")
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return oxMonitorFail(http.StatusBadGateway, "CORE_UNREACHABLE", "Monitoring request was canceled")
	}
	return oxMonitorFail(http.StatusBadGateway, "CORE_UNREACHABLE", "Cannot reach the OxiDNS API")
}

// The core's client_ip filter is a substring search. Apply numeric IP equality
// over successive core pages, retaining the last scanned cursor even when a
// sparse match reaches the scan bound. Request only the remaining row count so
// no unreturned matches can be skipped at the end of a core page.
func (config *oxMonitorConfig) exactIPRecords(ctx context.Context, client *http.Client, path string, query url.Values) ([]byte, *oxMonitorError) {
	selected := net.ParseIP(query.Get("client_ip"))
	if selected == nil {
		return nil, oxMonitorBadQuery()
	}
	wanted, _ := strconv.Atoi(query.Get("limit"))
	if wanted <= 0 || wanted > 100 {
		return nil, oxMonitorBadQuery()
	}
	if selected.To4() != nil {
		query.Set("client_ip", selected.String())
	} else {
		// Textual IPv6 compression/casing differs; a broad IPv6 prefilter
		// avoids dropping numerically equal addresses before comparison.
		query.Set("client_ip", ":")
	}
	records := make([]json.RawMessage, 0, wanted)
	var next *string
	seen := map[string]bool{}
	if cursor := query.Get("cursor"); cursor != "" {
		seen[cursor] = true
	}
	for page := 0; page < oxMonitorExactIPPages; page++ {
		remaining := wanted - len(records)
		query.Set("limit", strconv.Itoa(remaining))
		data, err := config.fetch(ctx, client, path, query, oxMonitorJSONLimit, true)
		if err != nil {
			return nil, err
		}
		var result struct {
			Records    []json.RawMessage `json:"records"`
			NextCursor *string           `json:"next_cursor"`
		}
		if json.Unmarshal(data, &result) != nil || len(result.Records) > remaining {
			return nil, oxMonitorFail(http.StatusBadGateway, "INVALID_RESPONSE", "OxiDNS API returned an invalid query page")
		}
		for _, row := range result.Records {
			var record struct {
				ClientIP string `json:"client_ip"`
			}
			if json.Unmarshal(row, &record) != nil {
				return nil, oxMonitorFail(http.StatusBadGateway, "INVALID_RESPONSE", "OxiDNS API returned an invalid query record")
			}
			if selected.Equal(net.ParseIP(record.ClientIP)) {
				records = append(records, row)
			}
		}
		next = result.NextCursor
		if next == nil {
			break
		}
		if _, _, err := oxMonitorParseQuery(url.Values{"cursor": {*next}}.Encode(), false); err != nil || seen[*next] {
			return nil, oxMonitorFail(http.StatusBadGateway, "INVALID_RESPONSE", "OxiDNS API returned an invalid query cursor")
		}
		seen[*next] = true
		if len(records) == wanted {
			break
		}
		query.Set("cursor", *next)
	}
	data, err := json.Marshal(map[string]any{"ok": true, "records": records, "next_cursor": next})
	if err != nil || len(data) > oxMonitorJSONLimit {
		return nil, oxMonitorFail(http.StatusBadGateway, "INVALID_RESPONSE", "OxiDNS API response exceeds the monitoring size limit")
	}
	return data, nil
}

// OxiDNSMonitorHandler exposes only the explicitly supported read APIs.
// It does not invoke lifecycle commands, deploy configuration, or open recorder
// databases; all query data comes from the running core's official HTTP API.
func OxiDNSMonitorHandler(cfg *Config) http.HandlerFunc {
	return oxMonitorHandlerWithSources(cfg, newOxSourceCache())
}

func oxMonitorHandlerWithSources(cfg *Config, sources *oxSourceCache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			oxMonitorWriteError(w, oxMonitorFail(http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Monitoring supports GET requests only"))
			return
		}
		path := r.URL.Path
		overview := path == "/api/oxidns/overview"
		rankings := path == "/api/oxidns/rankings"
		clients := path == "/api/oxidns/clients"
		detail := strings.HasPrefix(path, "/api/oxidns/queries/")
		id := ""
		if detail {
			id = strings.TrimPrefix(path, "/api/oxidns/queries/")
			n, err := strconv.ParseInt(id, 10, 64)
			if err != nil || n <= 0 || strconv.FormatInt(n, 10) != id {
				oxMonitorWriteError(w, oxMonitorFail(http.StatusBadRequest, "INVALID_QUERY", "A positive query record ID is required"))
				return
			}
		}
		if (!overview && !rankings && !clients && !detail && path != "/api/oxidns/queries") || r.URL.EscapedPath() != path {
			oxMonitorWriteError(w, oxMonitorFail(http.StatusNotFound, "NOT_FOUND", "Unknown monitoring endpoint"))
			return
		}
		var recorder string
		var query url.Values
		if overview {
			if r.URL.RawQuery != "" {
				oxMonitorWriteError(w, oxMonitorBadQuery())
				return
			}
		} else {
			var queryErr *oxMonitorError
			if rankings {
				recorder, query, queryErr = oxMonitorParseRankings(r.URL.RawQuery)
			} else if clients {
				recorder, query, queryErr = oxMonitorParseClients(r.URL.RawQuery)
			} else {
				recorder, query, queryErr = oxMonitorParseQuery(r.URL.RawQuery, detail)
			}
			if queryErr != nil {
				oxMonitorWriteError(w, queryErr)
				return
			}
		}
		config, configErr := loadOxiDNSMonitorConfig(cfg)
		wantSources := !overview && !rankings && !clients && !detail && r.Header.Get(oxSourceHeader) == "1"
		if configErr != nil {
			if wantSources {
				sources.reset()
			}
			oxMonitorWriteError(w, configErr)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), oxMonitorTimeout)
		defer cancel()
		client := oxMonitorClient()
		defer client.CloseIdleConnections()
		if rankings {
			config.writeRankings(w, ctx, client, cfg, recorder, query)
			return
		}
		if overview {
			health, err := config.fetch(ctx, client, "/api/health", nil, oxMonitorJSONLimit, true)
			if err != nil {
				oxMonitorWriteError(w, err)
				return
			}
			running, err := config.runtimeSnapshot(ctx, client)
			if err != nil {
				oxMonitorWriteError(w, err)
				return
			}
			metrics, err := config.fetch(ctx, client, "/api/metrics", nil, oxMonitorMetricsLimit, false)
			if err != nil {
				oxMonitorWriteError(w, err)
				return
			}
			collectedAt := time.Now().UnixMilli()
			if err := config.verifySnapshot(ctx, client, cfg, health, running); err != nil {
				oxMonitorWriteError(w, err)
				return
			}
			JsonResponse(w, http.StatusOK, OxiDNSOverviewResponse{
				Capabilities:    config.capabilities,
				CollectedAtMS:   collectedAt,
				Health:          json.RawMessage(health),
				Metrics:         string(metrics),
				RuntimeRevision: running.revision(),
				SourceRevision:  config.sourceRevision,
				Success:         true,
				UpstreamLabels:  config.rankings.upstreams,
			})
			return
		}
		tag, err := config.recorder(recorder)
		if err != nil {
			oxMonitorWriteError(w, err)
			return
		}
		var sourceStream *oxSourceStream
		if wantSources {
			healthCtx, healthCancel := context.WithTimeout(ctx, 500*time.Millisecond)
			health, healthErr := config.fetch(healthCtx, client, "/api/health", nil, oxMonitorJSONLimit, true)
			healthCancel()
			if healthErr == nil && oxSourceInstance(health) != "" {
				sourceStream = sources.subscribe(config, tag, oxSourceInstance(health))
			} else {
				sources.reset()
			}
		}
		corePath := "/api/plugins/" + tag + "/records"
		if detail {
			corePath += "/" + id
		} else if clients {
			corePath = "/api/plugins/" + tag + "/stats/top_clients"
		}
		var data []byte
		if !detail && !clients && query.Get("client_ip") != "" {
			data, err = config.exactIPRecords(ctx, client, corePath, query)
		} else {
			data, err = config.fetch(ctx, client, corePath, query, oxMonitorJSONLimit, true)
		}
		if err != nil {
			oxMonitorWriteError(w, err)
			return
		}
		if clients {
			var result map[string]json.RawMessage
			_ = json.Unmarshal(data, &result)
			selected, _ := json.Marshal(tag)
			result["recorder"] = selected
			JsonResponse(w, http.StatusOK, result)
			return
		}
		data = sourceStream.enrich(ctx, data)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	}
}
