package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"agent/routes"

	"gopkg.in/yaml.v3"
)

// 区域检测：ConfigFlow 在推送给 Agent 的 Mihomo 配置里注入一个只监听本机的入口，
// 它的流量全部交给隐藏的选择组。Agent 切换这个组到目标节点 / 策略组，再经入口发请求，
// 从而拿到「经某个节点访问」的完整响应。服务的判定逻辑在服务端，这里只负责取页面和匹配标记。

const (
	regionProbeGroup       = "ConfigFlow-Region-Probe"
	regionProbeListener    = "configflow-region-probe"
	regionMaxRequests      = 12
	regionMaxMarkers       = 8
	regionMaxMarkerLength  = 256
	regionMaxBody          = 2 << 20
	regionMaxRedirects     = 5
	regionMaxConcurrency   = 4
	regionMaxCaptureLength = 200
	regionMinTimeoutMS     = 1000
	regionMaxTimeoutMS     = 15000
	regionUserAgent        = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/129.0 Safari/537.36"
)

var errRegionProbeMissing = errors.New("region probe listener is not deployed")

// 服务端能指定的请求头；其余一律忽略，避免被当成任意请求转发器
var regionAllowedHeaders = map[string]bool{
	"Authorization": true, "Accept-Language": true, "Cookie": true, "Referer": true, "Origin": true,
}

// 同一时间只允许一个检测切换探测组
var regionCheckMu sync.Mutex

type regionRequest struct {
	ID      string            `json:"id"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
	Markers map[string]string `json:"markers"`
}

type regionCheckRequest struct {
	Target    string          `json:"target"`
	TimeoutMS int             `json:"timeout_ms"`
	Requests  []regionRequest `json:"requests"`
}

type regionResult struct {
	ID        string            `json:"id"`
	Status    int               `json:"status"`
	FinalURL  string            `json:"final_url"`
	Redirects []string          `json:"redirects"`
	Markers   map[string]string `json:"markers"`
	Error     string            `json:"error"`
	ElapsedMS int64             `json:"elapsed_ms"`
}

// resolveRegionProbeListener 从 Mihomo 配置里找到注入的探测入口地址。
func resolveRegionProbeListener(configPath string) (string, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return "", err
	}
	var parsed struct {
		Listeners []struct {
			Name   string `yaml:"name"`
			Listen string `yaml:"listen"`
			Port   int    `yaml:"port"`
		} `yaml:"listeners"`
	}
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		return "", err
	}
	for _, listener := range parsed.Listeners {
		if listener.Name != regionProbeListener || listener.Port <= 0 {
			continue
		}
		host := listener.Listen
		if host == "" || host == "0.0.0.0" || host == "::" {
			host = "127.0.0.1"
		}
		return net.JoinHostPort(host, strconv.Itoa(listener.Port)), nil
	}
	return "", errRegionProbeMissing
}

func validateRegionCheck(req *regionCheckRequest) (map[string]map[string]*regexp.Regexp, error) {
	if req.Target == "" || len(req.Target) > 128 {
		return nil, errors.New("invalid target")
	}
	if req.TimeoutMS < regionMinTimeoutMS || req.TimeoutMS > regionMaxTimeoutMS {
		return nil, fmt.Errorf("timeout_ms must be %d-%d", regionMinTimeoutMS, regionMaxTimeoutMS)
	}
	if len(req.Requests) == 0 || len(req.Requests) > regionMaxRequests {
		return nil, fmt.Errorf("requests must contain 1-%d items", regionMaxRequests)
	}
	compiled := map[string]map[string]*regexp.Regexp{}
	for _, item := range req.Requests {
		if item.ID == "" || len(item.ID) > 64 || compiled[item.ID] != nil {
			return nil, fmt.Errorf("invalid or duplicate request id %q", item.ID)
		}
		parsed, err := url.Parse(item.URL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" {
			return nil, fmt.Errorf("request %s: url must be http(s)", item.ID)
		}
		if len(item.Markers) > regionMaxMarkers {
			return nil, fmt.Errorf("request %s: too many markers", item.ID)
		}
		patterns := map[string]*regexp.Regexp{}
		for name, pattern := range item.Markers {
			if name == "" || len(name) > 64 || len(pattern) > regionMaxMarkerLength {
				return nil, fmt.Errorf("request %s: invalid marker %q", item.ID, name)
			}
			re, err := regexp.Compile(pattern)
			if err != nil {
				return nil, fmt.Errorf("request %s: marker %q: %v", item.ID, name, err)
			}
			patterns[name] = re
		}
		compiled[item.ID] = patterns
	}
	return compiled, nil
}

func classifyFetchError(err error) string {
	var netErr net.Error
	var certErr *tls.CertificateVerificationError
	var unknownAuthority x509.UnknownAuthorityError
	switch {
	case errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()):
		return "timeout"
	case errors.As(err, &certErr) || errors.As(err, &unknownAuthority) || strings.Contains(err.Error(), "tls:"):
		return "tls"
	case strings.Contains(err.Error(), "connection refused") || strings.Contains(err.Error(), "connection reset") ||
		strings.Contains(err.Error(), "EOF") || strings.Contains(err.Error(), "proxyconnect"):
		return "connect"
	default:
		return "other"
	}
}

// fetchThroughProbe 经探测入口请求一次，记录跳转链并在正文里匹配标记。
func fetchThroughProbe(ctx context.Context, client *http.Client, item regionRequest, patterns map[string]*regexp.Regexp) regionResult {
	result := regionResult{ID: item.ID, Redirects: []string{}, Markers: map[string]string{}}
	started := time.Now()
	defer func() { result.ElapsedMS = time.Since(started).Milliseconds() }()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, item.URL, nil)
	if err != nil {
		result.Error = "other"
		return result
	}
	req.Header.Set("User-Agent", regionUserAgent)
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	for name, value := range item.Headers {
		canonical := http.CanonicalHeaderKey(name)
		if regionAllowedHeaders[canonical] && len(value) <= 1024 {
			req.Header.Set(canonical, value)
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		result.Error = classifyFetchError(err)
		return result
	}
	defer resp.Body.Close()
	result.Status = resp.StatusCode
	result.FinalURL = resp.Request.URL.String()
	// 每个跳转后的请求都挂着触发它的响应，沿着它回溯出完整的跳转链（不含最终地址）
	for r := resp.Request; r.Response != nil; r = r.Response.Request {
		result.Redirects = append([]string{r.Response.Request.URL.String()}, result.Redirects...)
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, regionMaxBody))
	for name, re := range patterns {
		match := re.FindSubmatch(body)
		if match == nil {
			continue
		}
		value := match[0]
		if len(match) > 1 {
			value = match[1]
		}
		if len(value) > regionMaxCaptureLength {
			value = value[:regionMaxCaptureLength]
		}
		result.Markers[name] = string(value)
	}
	return result
}

func newProbeClient(listener string, timeout time.Duration) (*http.Client, *http.Transport) {
	proxyURL := &url.URL{Scheme: "http", Host: listener}
	// 每次检测都用新的 Transport：切换节点后，连接池里的旧连接仍走上一个节点
	transport := &http.Transport{
		Proxy:               http.ProxyURL(proxyURL),
		TLSHandshakeTimeout: timeout,
		DisableKeepAlives:   true,
		MaxIdleConnsPerHost: -1,
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > regionMaxRedirects {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
	return client, transport
}

// selectProbeTarget 把探测组切到 target，返回切换前的选择。
func selectProbeTarget(ctx context.Context, controller *mihomoController, target string) (string, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	path := "/proxies/" + url.PathEscape(regionProbeGroup)
	req, err := controller.request(ctx, http.MethodGet, path)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	var group struct {
		Now string   `json:"now"`
		All []string `json:"all"`
	}
	decodeErr := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&group)
	resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", errRegionProbeMissing
	}
	if decodeErr != nil {
		return "", decodeErr
	}
	if err := putProbeSelection(ctx, controller, target); err != nil {
		return group.Now, err
	}
	return group.Now, nil
}

func putProbeSelection(ctx context.Context, controller *mihomoController, target string) error {
	body, _ := json.Marshal(map[string]string{"name": target})
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, controller.BaseURL+"/proxies/"+url.PathEscape(regionProbeGroup), bytes.NewReader(body))
	if err != nil {
		return err
	}
	if controller.Secret != "" {
		req.Header.Set("Authorization", "Bearer "+controller.Secret)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("target %q is not selectable (status %d)", target, resp.StatusCode)
	}
	return nil
}

func runRegionCheck(ctx context.Context, controller *mihomoController, listener string, req regionCheckRequest,
	patterns map[string]map[string]*regexp.Regexp) ([]regionResult, error) {
	regionCheckMu.Lock()
	defer regionCheckMu.Unlock()

	previous, err := selectProbeTarget(ctx, controller, req.Target)
	if err != nil {
		return nil, err
	}
	defer func() {
		if previous != "" && previous != req.Target {
			_ = putProbeSelection(context.Background(), controller, previous)
		}
	}()

	timeout := time.Duration(req.TimeoutMS) * time.Millisecond
	client, transport := newProbeClient(listener, timeout)
	defer transport.CloseIdleConnections()

	results := make([]regionResult, len(req.Requests))
	sem := make(chan struct{}, regionMaxConcurrency)
	var wg sync.WaitGroup
	for i, item := range req.Requests {
		wg.Add(1)
		go func(i int, item regionRequest) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i] = fetchThroughProbe(ctx, client, item, patterns[item.ID])
		}(i, item)
	}
	wg.Wait()
	return results, nil
}

type regionTarget struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Alive bool   `json:"alive"`
}

// listRegionTargets 返回探测组可以切换到的目标及其类型、存活状态。
func listRegionTargets(ctx context.Context, controller *mihomoController) ([]regionTarget, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	req, err := controller.request(ctx, http.MethodGet, "/proxies")
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var body struct {
		Proxies map[string]struct {
			Type  string   `json:"type"`
			Alive *bool    `json:"alive"`
			All   []string `json:"all"`
		} `json:"proxies"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(&body); err != nil {
		return nil, err
	}
	group, ok := body.Proxies[regionProbeGroup]
	if !ok {
		return nil, errRegionProbeMissing
	}
	targets := make([]regionTarget, 0, len(group.All))
	for _, name := range group.All {
		info := body.Proxies[name]
		alive := info.Alive == nil || *info.Alive
		targets = append(targets, regionTarget{Name: name, Type: info.Type, Alive: alive})
	}
	return targets, nil
}

func regionFailure(w http.ResponseWriter, err error) {
	status := http.StatusBadGateway
	if errors.Is(err, errRegionProbeMissing) {
		status = http.StatusConflict
	}
	routes.JsonResponse(w, status, map[string]interface{}{"success": false, "message": err.Error()})
}

func regionCheckPrecheck(w http.ResponseWriter, cfg *Config) (*mihomoController, string, bool) {
	if cfg.ServiceType != "mihomo" {
		routes.JsonResponse(w, http.StatusBadRequest, map[string]interface{}{"success": false, "message": "region check requires a mihomo agent"})
		return nil, "", false
	}
	controller, err := resolveMihomoController(cfg.ConfigPath)
	if err != nil {
		routes.JsonResponse(w, http.StatusServiceUnavailable, map[string]interface{}{"success": false, "message": "mihomo controller unavailable: " + err.Error()})
		return nil, "", false
	}
	listener, err := resolveRegionProbeListener(cfg.ConfigPath)
	if err != nil {
		regionFailure(w, err)
		return nil, "", false
	}
	return controller, listener, true
}

func regionTargetsHandler(cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		controller, _, ok := regionCheckPrecheck(w, cfg)
		if !ok {
			return
		}
		targets, err := listRegionTargets(r.Context(), controller)
		if err != nil {
			regionFailure(w, err)
			return
		}
		routes.JsonResponse(w, http.StatusOK, map[string]interface{}{"success": true, "targets": targets})
	}
}

func regionCheckHandler(cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		controller, listener, ok := regionCheckPrecheck(w, cfg)
		if !ok {
			return
		}
		var req regionCheckRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&req); err != nil {
			routes.JsonResponse(w, http.StatusBadRequest, map[string]interface{}{"success": false, "message": "invalid region check request"})
			return
		}
		patterns, err := validateRegionCheck(&req)
		if err != nil {
			routes.JsonResponse(w, http.StatusBadRequest, map[string]interface{}{"success": false, "message": err.Error()})
			return
		}
		results, err := runRegionCheck(r.Context(), controller, listener, req, patterns)
		if err != nil {
			regionFailure(w, err)
			return
		}
		routes.JsonResponse(w, http.StatusOK, map[string]interface{}{"success": true, "target": req.Target, "results": results})
	}
}
