package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"agent/routes"
)

// 域名探测：借本机 Mihomo 的延迟测试，分别经直连与代理策略访问目标，
// 与真实流量走同一条路径（开启 tun 时 Agent 自己发请求会被 Mihomo 截走）。

const (
	probeMaxTargets     = 20
	probeMaxPaths       = 3
	probeMaxAttempts    = 5
	probeMinTimeoutMS   = 1000
	probeMaxTimeoutMS   = 10000
	probeMaxBody        = 64 * 1024
	probeMaxConcurrency = 6
)

type probeTarget struct {
	Host string `json:"host"`
	URL  string `json:"url"`
}

type probeRequest struct {
	Targets   []probeTarget `json:"targets"`
	Paths     []string      `json:"paths"`
	Attempts  int           `json:"attempts"`
	TimeoutMS int           `json:"timeout_ms"`
}

type probePathResult struct {
	OK     int            `json:"ok"`
	Total  int            `json:"total"`
	Delays []int          `json:"delays"`
	Errors map[string]int `json:"errors"`
}

type probeTargetResult struct {
	Host  string                      `json:"host"`
	URL   string                      `json:"url"`
	Paths map[string]*probePathResult `json:"paths"`
}

func validateProbeRequest(req *probeRequest) error {
	if len(req.Targets) == 0 || len(req.Targets) > probeMaxTargets {
		return fmt.Errorf("targets must contain 1-%d items", probeMaxTargets)
	}
	if len(req.Paths) == 0 || len(req.Paths) > probeMaxPaths {
		return fmt.Errorf("paths must contain 1-%d items", probeMaxPaths)
	}
	for _, path := range req.Paths {
		if path == "" || len(path) > 128 {
			return errors.New("invalid path name")
		}
	}
	if req.Attempts < 1 || req.Attempts > probeMaxAttempts {
		return fmt.Errorf("attempts must be 1-%d", probeMaxAttempts)
	}
	if req.TimeoutMS < probeMinTimeoutMS || req.TimeoutMS > probeMaxTimeoutMS {
		return fmt.Errorf("timeout_ms must be %d-%d", probeMinTimeoutMS, probeMaxTimeoutMS)
	}
	for _, target := range req.Targets {
		host := normalizeDiscoveryHost(target.Host)
		if host == "" || host != target.Host {
			return fmt.Errorf("invalid host %q", target.Host)
		}
		parsed, err := url.Parse(target.URL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() != host {
			return fmt.Errorf("url must be http(s) on host %q", target.Host)
		}
		if port := parsed.Port(); port != "" {
			if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
				return fmt.Errorf("invalid port in %q", target.URL)
			}
		}
	}
	return nil
}

// probeOnce 调用一次 Mihomo 延迟测试，返回延迟（毫秒）或错误类别。
func probeOnce(ctx context.Context, client *http.Client, controller *mihomoController, path, target string, timeoutMS int) (int, string) {
	query := url.Values{"url": {target}, "timeout": {strconv.Itoa(timeoutMS)}}
	req, err := controller.request(ctx, http.MethodGet, "/proxies/"+url.PathEscape(path)+"/delay?"+query.Encode())
	if err != nil {
		return 0, "error"
	}
	resp, err := client.Do(req)
	if err != nil {
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return 0, "timeout"
		}
		return 0, "error"
	}
	defer resp.Body.Close()
	var body struct {
		Delay int `json:"delay"`
	}
	switch {
	case resp.StatusCode == http.StatusOK && json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&body) == nil && body.Delay > 0:
		return body.Delay, ""
	case resp.StatusCode == http.StatusGatewayTimeout:
		return 0, "timeout"
	case resp.StatusCode == http.StatusNotFound:
		return 0, "no_path"
	default:
		return 0, "error"
	}
}

// runProbes 对每个「目标 × 路径」依次探测；前两次结果一致时提前结束。
func runProbes(ctx context.Context, controller *mihomoController, req probeRequest) []probeTargetResult {
	client := &http.Client{Timeout: time.Duration(req.TimeoutMS)*time.Millisecond + 3*time.Second}
	results := make([]probeTargetResult, len(req.Targets))
	type job struct{ target, path int }
	jobs := make(chan job)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i, target := range req.Targets {
		results[i] = probeTargetResult{Host: target.Host, URL: target.URL, Paths: map[string]*probePathResult{}}
		for _, path := range req.Paths {
			results[i].Paths[path] = &probePathResult{Delays: []int{}, Errors: map[string]int{}}
		}
	}
	workers := probeMaxConcurrency
	if total := len(req.Targets) * len(req.Paths); total < workers {
		workers = total
	}
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				target := req.Targets[j.target]
				path := req.Paths[j.path]
				outcomes := []bool{}
				for attempt := 0; attempt < req.Attempts && ctx.Err() == nil; attempt++ {
					delay, kind := probeOnce(ctx, client, controller, path, target.URL, req.TimeoutMS)
					mu.Lock()
					stats := results[j.target].Paths[path]
					stats.Total++
					if kind == "" {
						stats.OK++
						stats.Delays = append(stats.Delays, delay)
					} else {
						stats.Errors[kind]++
					}
					mu.Unlock()
					outcomes = append(outcomes, kind == "")
					if kind == "no_path" || (len(outcomes) == 2 && outcomes[0] == outcomes[1]) {
						break
					}
				}
			}
		}()
	}
	for t := range req.Targets {
		for p := range req.Paths {
			select {
			case jobs <- job{t, p}:
			case <-ctx.Done():
			}
		}
	}
	close(jobs)
	wg.Wait()
	return results
}

func domainProbeHandler(cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if cfg.ServiceType != "mihomo" {
			routes.JsonResponse(w, http.StatusBadRequest, map[string]interface{}{"success": false, "message": "domain probe requires a mihomo agent"})
			return
		}
		var req probeRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, probeMaxBody)).Decode(&req); err != nil {
			routes.JsonResponse(w, http.StatusBadRequest, map[string]interface{}{"success": false, "message": "invalid probe request"})
			return
		}
		if err := validateProbeRequest(&req); err != nil {
			routes.JsonResponse(w, http.StatusBadRequest, map[string]interface{}{"success": false, "message": err.Error()})
			return
		}
		controller, err := resolveMihomoController(cfg.ConfigPath)
		if err != nil {
			routes.JsonResponse(w, http.StatusServiceUnavailable, map[string]interface{}{"success": false, "message": "mihomo controller unavailable: " + err.Error()})
			return
		}
		results := runProbes(r.Context(), controller, req)
		routes.JsonResponse(w, http.StatusOK, map[string]interface{}{"success": true, "results": results})
	}
}
