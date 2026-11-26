package routes

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gopkg.in/yaml.v3"
)

type oxRuleUpdateTask struct {
	ID            string `json:"id"`
	Status        string `json:"status"`
	Phase         string `json:"phase"`
	Code          string `json:"code,omitempty"`
	Downloaded    int    `json:"downloaded"`
	DownloadTotal int    `json:"download_total"`
	Reloaded      int    `json:"reloaded"`
	ReloadTotal   int    `json:"reload_total"`
}

type oxRuleUpdatePlan struct {
	api           *oxMonitorConfig
	downloadTag   string
	downloadTotal int
	providers     []string
	// Configured target of each download index; "" when the core derives it.
	downloadPaths []string
	// Files read by each provider; nil when the provider's inputs are unknown.
	providerFiles map[string][]string
}

// A targeted update downloads only the selected indexes and reloads only the
// providers that read them. A nil selection updates every configured item.
type oxRuleSelection struct {
	indexes   []int
	providers []string
}

const oxRuleMaxPaths = 1000
const oxRuleRequestLimit = 128 * 1024

type oxRuleUpdater struct {
	mu             sync.Mutex
	task           oxRuleUpdateTask
	requestTimeout time.Duration
	roundTimeout   time.Duration
}

// oxRuleUpdatesRunning lets other lock users explain why the Agent is busy:
// a rule update holds the service lock for its whole download round.
var oxRuleUpdatesRunning atomic.Int32

func oxRuleUpdateRunning() bool { return oxRuleUpdatesRunning.Load() > 0 }

func oxRuleNotConfigured() *oxMonitorError {
	return oxMonitorFail(http.StatusConflict, "RULE_UPDATE_NOT_CONFIGURED", "Configure downloadable rules and republish the OxiDNS configuration, then retry")
}

// Discover an unconditional, two-step download -> reload_provider sequence.
// Never execute arbitrary cron jobs, sequences, or user-supplied plugin tags.
func oxRuleUpdateConfiguration(cfg *Config) (*oxRuleUpdatePlan, *oxMonitorError) {
	api, apiErr := loadOxiDNSMonitorConfig(cfg)
	if apiErr != nil {
		return nil, apiErr
	}
	data, err := os.ReadFile(cfg.ConfigPath)
	if err != nil || len(data) > oxMonitorJSONLimit || sha256Hex(data) != api.sourceRevision {
		return nil, oxMonitorFail(http.StatusConflict, "SNAPSHOT_CHANGED", "The active configuration changed; try again")
	}
	var doc map[string]any
	if yaml.Unmarshal(data, &doc) != nil {
		return nil, oxMonitorConfigError()
	}
	items, _ := doc["plugins"].([]any)
	plugins := make(map[string]map[string]any)
	for _, item := range items {
		plugin, _ := item.(map[string]any)
		tag, _ := plugin["tag"].(string)
		if tag == "" {
			continue
		}
		if !oxMonitorSafeTag(tag) || plugins[tag] != nil {
			return nil, oxMonitorConfigError()
		}
		plugins[tag] = plugin
	}
	var candidates []*oxRuleUpdatePlan
	for _, plugin := range plugins {
		if plugin["type"] != "sequence" {
			continue
		}
		steps, _ := plugin["args"].([]any)
		if len(steps) != 2 {
			continue
		}
		tags := make([]string, 2)
		for i, value := range steps {
			step, _ := value.(map[string]any)
			exec, _ := step["exec"].(string)
			if len(step) == 1 && strings.HasPrefix(exec, "$") && oxMonitorSafeTag(exec[1:]) {
				tags[i] = exec[1:]
			}
		}
		download, reload := plugins[tags[0]], plugins[tags[1]]
		if download["type"] != "download" || reload["type"] != "reload_provider" {
			continue
		}
		args, _ := download["args"].(map[string]any)
		downloads, _ := args["downloads"].([]any)
		if len(downloads) == 0 || len(downloads) > 1000 {
			continue
		}
		var references []any
		switch value := reload["args"].(type) {
		case string:
			references = []any{value}
		case []any:
			references = value
		}
		if len(references) == 0 || len(references) > 1000 {
			continue
		}
		plan := &oxRuleUpdatePlan{api: api, downloadTag: tags[0], downloadTotal: len(downloads), providerFiles: make(map[string][]string)}
		for _, value := range downloads {
			entry, _ := value.(map[string]any)
			dir, _ := entry["dir"].(string)
			filename, _ := entry["filename"].(string)
			target := ""
			if filename != "" {
				target = oxRuleCleanPath(path.Join(dir, filename))
			}
			plan.downloadPaths = append(plan.downloadPaths, target)
		}
		seen := make(map[string]bool)
		for _, value := range references {
			ref, _ := value.(string)
			if !strings.HasPrefix(ref, "$") || !oxMonitorSafeTag(ref[1:]) {
				break
			}
			tag := ref[1:]
			switch plugins[tag]["type"] {
			case "domain_set", "ip_set", "geosite", "geoip", "adguard_rule":
				if !seen[tag] {
					plan.providers = append(plan.providers, tag)
					plan.providerFiles[tag] = oxRuleProviderFiles(plugins[tag])
					seen[tag] = true
				}
			default:
				seen = nil
			}
			if seen == nil {
				break
			}
		}
		// Duplicated/malformed provider references are rejected rather than guessed.
		if len(plan.providers) != len(references) || seen == nil {
			continue
		}
		candidates = append(candidates, plan)
	}
	if len(candidates) > 1 {
		return nil, oxMonitorFail(http.StatusConflict, "RULE_UPDATE_AMBIGUOUS", "Multiple rule update chains are configured")
	}
	if len(candidates) == 0 {
		return nil, oxRuleNotConfigured()
	}
	return candidates[0], nil
}

func oxRuleCleanPath(value string) string {
	return strings.TrimPrefix(path.Clean(value), "./")
}

func oxRuleProviderFiles(plugin map[string]any) []string {
	args, _ := plugin["args"].(map[string]any)
	values, ok := args["files"].([]any)
	if !ok || len(values) == 0 {
		return nil
	}
	files := make([]string, 0, len(values))
	for _, value := range values {
		file, _ := value.(string)
		if file == "" {
			return nil
		}
		files = append(files, oxRuleCleanPath(file))
	}
	return files
}

// Resolve requested files against the deployed download list only; the
// request can never name a URL, an output path or a plugin tag.
func (plan *oxRuleUpdatePlan) selectPaths(paths []string) (*oxRuleSelection, *oxMonitorError) {
	selection := &oxRuleSelection{}
	wanted := make(map[string]bool)
	for _, requested := range paths {
		target := oxRuleCleanPath(requested)
		if wanted[target] {
			continue
		}
		wanted[target] = true
		matched := false
		for index, configured := range plan.downloadPaths {
			if configured != "" && configured == target {
				selection.indexes = append(selection.indexes, index)
				matched = true
			}
		}
		if !matched {
			return nil, oxMonitorFail(http.StatusConflict, "RULE_UPDATE_TARGET_UNKNOWN", "A requested rule file is not a configured download target")
		}
	}
	sort.Ints(selection.indexes)
	for _, tag := range plan.providers {
		files := plan.providerFiles[tag]
		affected := files == nil
		for _, file := range files {
			if wanted[file] {
				affected = true
				break
			}
		}
		if affected {
			selection.providers = append(selection.providers, tag)
		}
	}
	return selection, nil
}

func oxRuleRequest(ctx context.Context, client *http.Client, api *oxMonitorConfig, method, path string, result any) *oxMonitorError {
	return oxRuleRequestBody(ctx, client, api, method, path, "{}", result)
}

func oxRuleRequestBody(ctx context.Context, client *http.Client, api *oxMonitorConfig, method, path, payload string, result any) *oxMonitorError {
	var body io.Reader
	if method == http.MethodPost {
		body = strings.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, api.endpoint+path, body)
	if err != nil {
		return oxMonitorConfigError()
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if api.basicAuth {
		req.SetBasicAuth(api.username, api.password)
	}
	response, err := client.Do(req)
	if err != nil {
		var networkError net.Error
		// Client.Timeout does not wrap context.DeadlineExceeded on every supported Go version.
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) ||
			(errors.As(err, &networkError) && networkError.Timeout()) {
			return oxMonitorFail(http.StatusGatewayTimeout, "RULE_UPDATE_UNKNOWN", "The core request timed out; its outcome could not be confirmed")
		}
		return oxMonitorFail(http.StatusBadGateway, "CORE_UNREACHABLE", "Cannot reach the OxiDNS API")
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound, http.StatusMethodNotAllowed:
		return oxMonitorFail(http.StatusConflict, "CORE_UPDATE_UNSUPPORTED", "The running OxiDNS core does not support native rule update APIs")
	case http.StatusUnauthorized, http.StatusForbidden:
		return oxMonitorFail(http.StatusBadGateway, "CORE_AUTH_FAILED", "OxiDNS API authentication failed")
	case http.StatusConflict:
		return oxMonitorFail(http.StatusConflict, "CORE_BUSY", "An OxiDNS rule operation is already running")
	default:
		return oxMonitorFail(http.StatusBadGateway, "RULE_UPDATE_FAILED", "OxiDNS rejected the rule operation")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, oxMonitorJSONLimit+1))
	if err != nil {
		return oxMonitorFail(http.StatusBadGateway, "RULE_UPDATE_UNKNOWN", "The core response could not be confirmed")
	}
	if len(data) > oxMonitorJSONLimit || json.Unmarshal(data, result) != nil {
		return oxMonitorFail(http.StatusBadGateway, "INVALID_RESPONSE", "OxiDNS returned an invalid rule update response")
	}
	return nil
}

func oxRuleCheckVersion(ctx context.Context, client *http.Client, cfg *Config, plan *oxRuleUpdatePlan) *oxMonitorError {
	var status struct {
		Pending        *bool   `json:"pending"`
		InProgress     *bool   `json:"in_progress"`
		RunningVersion *string `json:"running_version"`
	}
	if err := oxRuleRequest(ctx, client, plan.api, http.MethodGet, "/api/reload/status", &status); err != nil {
		return err
	}
	data, err := os.ReadFile(cfg.ConfigPath)
	if err != nil || status.Pending == nil || status.InProgress == nil || status.RunningVersion == nil ||
		*status.Pending || *status.InProgress || *status.RunningVersion != plan.api.sourceRevision || sha256Hex(data) != plan.api.sourceRevision {
		return oxMonitorFail(http.StatusConflict, "SNAPSHOT_CHANGED", "The running configuration changed or has not been applied")
	}
	return nil
}

func oxRuleCheckDownload(ctx context.Context, client *http.Client, plan *oxRuleUpdatePlan, selection *oxRuleSelection) *oxMonitorError {
	var status struct {
		OK        bool  `json:"ok"`
		Running   *bool `json:"running"`
		Downloads []struct {
			Index *int   `json:"index"`
			Path  string `json:"path"`
		} `json:"downloads"`
	}
	if err := oxRuleRequest(ctx, client, plan.api, http.MethodGet, "/api/plugins/"+plan.downloadTag+"/downloads", &status); err != nil {
		return err
	}
	if !status.OK || status.Running == nil || len(status.Downloads) != plan.downloadTotal {
		return oxMonitorFail(http.StatusBadGateway, "INVALID_RESPONSE", "OxiDNS returned an invalid download status")
	}
	if *status.Running {
		return oxMonitorFail(http.StatusConflict, "CORE_BUSY", "An OxiDNS download is already running")
	}
	if selection != nil {
		// Runtime indexes follow the running configuration. Where the core
		// reports a target, it must still be the file this plan resolved.
		for _, index := range selection.indexes {
			entry := status.Downloads[index]
			if entry.Index != nil && *entry.Index != index {
				return oxMonitorFail(http.StatusConflict, "SNAPSHOT_CHANGED", "The running download list changed")
			}
			if reported := strings.ReplaceAll(entry.Path, "\\", "/"); reported != "" {
				expected := plan.downloadPaths[index]
				if cleaned := oxRuleCleanPath(reported); cleaned != expected && !strings.HasSuffix(cleaned, "/"+expected) {
					return oxMonitorFail(http.StatusConflict, "SNAPSHOT_CHANGED", "The running download list changed")
				}
			}
		}
	}
	return nil
}

func (updater *oxRuleUpdater) snapshot() oxRuleUpdateTask {
	updater.mu.Lock()
	defer updater.mu.Unlock()
	return updater.task
}

func (updater *oxRuleUpdater) change(edit func(*oxRuleUpdateTask)) {
	updater.mu.Lock()
	defer updater.mu.Unlock()
	edit(&updater.task)
}

func (updater *oxRuleUpdater) run(cfg *Config, plan *oxRuleUpdatePlan, selection *oxRuleSelection, release func()) {
	finalStatus, finalCode := "failed", "RULE_UPDATE_UNKNOWN"
	defer func() {
		// A terminal observation also means the Agent service lock was released.
		updater.mu.Lock()
		defer updater.mu.Unlock()
		oxRuleUpdatesRunning.Add(-1)
		release()
		updater.task.Status, updater.task.Phase, updater.task.Code = finalStatus, "done", finalCode
	}()
	ctx, cancel := context.WithTimeout(context.Background(), updater.roundTimeout)
	defer cancel()
	client := oxMonitorClient()
	client.Timeout = updater.requestTimeout
	fail := func(code string) {
		finalCode = code
	}
	download := func(payload string, total int) (int, bool, *oxMonitorError) {
		var result struct {
			OK        bool `json:"ok"`
			Total     *int `json:"total"`
			Succeeded *int `json:"succeeded"`
			Failed    *int `json:"failed"`
		}
		if err := oxRuleRequestBody(ctx, client, plan.api, http.MethodPost, "/api/plugins/"+plan.downloadTag+"/download", payload, &result); err != nil {
			return 0, false, err
		}
		if result.Total == nil || result.Succeeded == nil || result.Failed == nil || *result.Total != total ||
			*result.Succeeded < 0 || *result.Succeeded > total || *result.Failed < 0 || *result.Failed > total ||
			*result.Succeeded+*result.Failed != total {
			return 0, false, oxMonitorFail(http.StatusBadGateway, "INVALID_RESPONSE", "OxiDNS returned an invalid download result")
		}
		return *result.Succeeded, result.OK && *result.Failed == 0, nil
	}
	providers := plan.providers
	if selection == nil {
		succeeded, ok, err := download("{}", plan.downloadTotal)
		if err != nil {
			fail(err.code)
			return
		}
		updater.change(func(task *oxRuleUpdateTask) { task.Downloaded = succeeded })
		if !ok {
			fail("DOWNLOAD_FAILED")
			return
		}
	} else {
		// The core accepts one index per request and rejects overlapping
		// downloads, so selected items run in order. A failed item keeps its
		// old file; the remaining items still run before reporting failure.
		providers = selection.providers
		complete := true
		for _, index := range selection.indexes {
			succeeded, ok, err := download(fmt.Sprintf(`{"index":%d}`, index), 1)
			if err != nil {
				fail(err.code)
				return
			}
			updater.change(func(task *oxRuleUpdateTask) { task.Downloaded += succeeded })
			complete = complete && ok
		}
		if !complete {
			fail("DOWNLOAD_FAILED")
			return
		}
	}
	// External core reloads are outside the Agent lock. Recheck before touching
	// providers so a stale plan cannot target a replacement plugin instance.
	if err := oxRuleCheckVersion(ctx, client, cfg, plan); err != nil {
		fail(err.code)
		return
	}
	updater.change(func(task *oxRuleUpdateTask) { task.Phase = "reloading" })
	for _, tag := range providers {
		var result struct {
			OK bool `json:"ok"`
		}
		if err := oxRuleRequest(ctx, client, plan.api, http.MethodPost, "/api/plugins/"+tag+"/reload", &result); err != nil {
			if err.code == "RULE_UPDATE_FAILED" {
				fail("RELOAD_FAILED")
			} else {
				fail(err.code)
			}
			return
		}
		if !result.OK {
			fail("RELOAD_FAILED")
			return
		}
		updater.change(func(task *oxRuleUpdateTask) { task.Reloaded++ })
	}
	if ctx.Err() != nil {
		fail("RULE_UPDATE_UNKNOWN")
		return
	}
	if err := oxRuleCheckVersion(ctx, client, cfg, plan); err != nil {
		fail(err.code)
		return
	}
	finalStatus, finalCode = "succeeded", ""
}

func (updater *oxRuleUpdater) handler(cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			w.Header().Set("Allow", "GET, POST")
			oxMonitorWriteError(w, oxMonitorFail(http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Use GET or POST"))
			return
		}
		if r.URL.Path != "/api/oxidns/rules/update" || r.URL.EscapedPath() != r.URL.Path || r.URL.RawQuery != "" {
			oxMonitorWriteError(w, oxMonitorFail(http.StatusBadRequest, "INVALID_REQUEST", "Rule updates do not accept parameters"))
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, oxRuleRequestLimit+1))
		var paths []string
		if err != nil || len(body) > oxRuleRequestLimit || (r.Method == http.MethodGet && len(bytes.TrimSpace(body)) != 0) ||
			(r.Method == http.MethodPost && !oxRuleParseBody(body, &paths)) {
			oxMonitorWriteError(w, oxMonitorFail(http.StatusBadRequest, "INVALID_REQUEST", "POST requires {} or {\"paths\": [...]}"))
			return
		}
		if r.Method == http.MethodGet {
			JsonResponse(w, http.StatusOK, OxiDNSRuleUpdateResponse{Success: true, Task: updater.snapshot()})
			return
		}
		release, err := AcquireServiceOperation(cfg)
		if err != nil {
			code := "SERVICE_BUSY"
			if updater.snapshot().Status == "running" {
				code = "RULE_UPDATE_BUSY"
			}
			oxMonitorWriteError(w, oxMonitorFail(http.StatusConflict, code, "A service operation is already running"))
			return
		}
		accepted := false
		defer func() {
			if !accepted {
				release()
			}
		}()
		if deploymentBlocked(cfg) != nil {
			oxMonitorWriteError(w, oxMonitorFail(http.StatusConflict, "SERVICE_BUSY", "Complete deployment recovery before updating rules"))
			return
		}
		plan, planErr := oxRuleUpdateConfiguration(cfg)
		if planErr != nil {
			oxMonitorWriteError(w, planErr)
			return
		}
		var selection *oxRuleSelection
		if paths != nil {
			if selection, planErr = plan.selectPaths(paths); planErr != nil {
				oxMonitorWriteError(w, planErr)
				return
			}
		}
		client := oxMonitorClient()
		if checkErr := oxRuleCheckVersion(r.Context(), client, cfg, plan); checkErr != nil {
			oxMonitorWriteError(w, checkErr)
			return
		}
		if checkErr := oxRuleCheckDownload(r.Context(), client, plan, selection); checkErr != nil {
			oxMonitorWriteError(w, checkErr)
			return
		}
		var id [16]byte
		if _, err := rand.Read(id[:]); err != nil {
			oxMonitorWriteError(w, oxMonitorFail(http.StatusInternalServerError, "RULE_UPDATE_FAILED", "Cannot create a rule update task"))
			return
		}
		task := oxRuleUpdateTask{ID: hex.EncodeToString(id[:]), Status: "running", Phase: "downloading", DownloadTotal: plan.downloadTotal, ReloadTotal: len(plan.providers)}
		if selection != nil {
			task.DownloadTotal, task.ReloadTotal = len(selection.indexes), len(selection.providers)
		}
		updater.change(func(current *oxRuleUpdateTask) { *current = task })
		accepted = true
		oxRuleUpdatesRunning.Add(1)
		go updater.run(cfg, plan, selection, release)
		JsonResponse(w, http.StatusAccepted, OxiDNSRuleUpdateResponse{Success: true, Task: task})
	}
}

// Accept exactly {} (every configured download) or {"paths": [...]}, a
// non-empty list of relative files that must match configured targets.
func oxRuleParseBody(body []byte, paths *[]string) bool {
	var object map[string]json.RawMessage
	if json.Unmarshal(body, &object) != nil || object == nil {
		return false
	}
	if len(object) == 0 {
		return true
	}
	raw, ok := object["paths"]
	var values []string
	if !ok || len(object) != 1 || json.Unmarshal(raw, &values) != nil || len(values) == 0 || len(values) > oxRuleMaxPaths {
		return false
	}
	for _, value := range values {
		cleaned := path.Clean(value)
		if value == "" || len(value) > 512 || strings.ContainsAny(value, "\\\x00") || path.IsAbs(value) ||
			cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
			return false
		}
	}
	*paths = values
	return true
}

// State is kept across requests, not across an Agent process restart.
func OxiDNSRuleUpdateHandler(cfg *Config) http.HandlerFunc {
	updater := &oxRuleUpdater{task: oxRuleUpdateTask{Status: "idle", Phase: "idle"}, requestTimeout: 180 * time.Second, roundTimeout: 600 * time.Second}
	return updater.handler(cfg)
}
