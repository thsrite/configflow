package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"agent/routes"
)

// Mihomo 面板：把服务端的面板请求转发到本机 Mihomo 控制接口，替代独立的 Web 面板。
// 只放行面板用到的接口与方法，控制接口的 secret 由 Agent 自己从配置读取，不经过服务端。

const (
	mihomoPanelPrefix         = "/api/mihomo/"
	mihomoPanelBodyLimit      = 16 * 1024
	mihomoPanelReplyLimit     = 16 << 20
	mihomoPanelTimeout        = 15 * time.Second
	mihomoPanelDelayTimeout   = 35 * time.Second
	mihomoPanelReloadTimeout  = 60 * time.Second
	mihomoPanelUpgradeTimeout = 3 * time.Minute
	// 重载只允许从内核当前使用的配置文件重新读取，不接受外部传入的路径或内容
	mihomoPanelReloadBody = `{"path":"","payload":""}`
)

type mihomoPanelRoute struct {
	pattern *regexp.Regexp
	methods string
	query   []string
}

// 名称段为 URL 编码后的单段路径（策略组、节点与 provider 名称可含中文和空格）
const panelSegment = `[^/]+`

var mihomoPanelRoutes = []mihomoPanelRoute{
	{regexp.MustCompile(`^version$`), "GET", nil},
	{regexp.MustCompile(`^configs$`), "GET PATCH PUT", nil},
	{regexp.MustCompile(`^proxies$`), "GET", nil},
	{regexp.MustCompile(`^proxies/` + panelSegment + `$`), "GET PUT DELETE", nil},
	{regexp.MustCompile(`^proxies/` + panelSegment + `/delay$`), "GET", []string{"url", "timeout"}},
	{regexp.MustCompile(`^group/` + panelSegment + `/delay$`), "GET", []string{"url", "timeout"}},
	{regexp.MustCompile(`^connections$`), "GET DELETE", nil},
	// 由 Agent 自己聚合，不转发到控制接口
	{regexp.MustCompile(`^flow$`), "GET", nil},
	// 实时日志：Agent 按需订阅 /logs 并缓存，面板增量轮询
	{regexp.MustCompile(`^logs$`), "GET", nil},
	{regexp.MustCompile(`^connections/` + panelSegment + `$`), "DELETE", nil},
	{regexp.MustCompile(`^rules$`), "GET", nil},
	{regexp.MustCompile(`^providers/proxies$`), "GET", nil},
	{regexp.MustCompile(`^providers/proxies/` + panelSegment + `$`), "PUT", nil},
	{regexp.MustCompile(`^providers/proxies/` + panelSegment + `/healthcheck$`), "GET", nil},
	{regexp.MustCompile(`^providers/proxies/` + panelSegment + `/` + panelSegment + `/healthcheck$`), "GET", []string{"url", "timeout"}},
	{regexp.MustCompile(`^providers/rules$`), "GET", nil},
	{regexp.MustCompile(`^providers/rules/` + panelSegment + `$`), "PUT", nil},
	{regexp.MustCompile(`^cache/fakeip/flush$`), "POST", nil},
	{regexp.MustCompile(`^cache/dns/flush$`), "POST", nil},
	{regexp.MustCompile(`^upgrade$`), "POST", nil},
}

func matchMihomoPanelRoute(path, method string) (*mihomoPanelRoute, bool) {
	for i := range mihomoPanelRoutes {
		route := &mihomoPanelRoutes[i]
		if route.pattern.MatchString(path) {
			for _, allowed := range strings.Fields(route.methods) {
				if allowed == method {
					return route, true
				}
			}
			return route, false
		}
	}
	return nil, false
}

func mihomoPanelFail(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Cache-Control", "no-store")
	routes.JsonResponse(w, status, map[string]interface{}{"success": false, "code": code, "message": message})
}

func mihomoPanelHandler(cfg *Config) http.HandlerFunc {
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	flow := newMihomoFlowSampler()
	// 日志订阅是长连接，不能用带超时的面板客户端
	logs := newMihomoLogTail(&http.Client{CheckRedirect: client.CheckRedirect})
	return func(w http.ResponseWriter, r *http.Request) {
		if cfg.ServiceType != "mihomo" {
			mihomoPanelFail(w, http.StatusBadRequest, "WRONG_SERVICE", "mihomo panel requires a mihomo agent")
			return
		}
		// EscapedPath 保留名称里的 %2F 等编码，避免把一个名称拆成多段
		path := strings.TrimPrefix(r.URL.EscapedPath(), mihomoPanelPrefix)
		route, allowed := matchMihomoPanelRoute(path, r.Method)
		if route == nil {
			mihomoPanelFail(w, http.StatusNotFound, "NOT_ALLOWED", "endpoint is not available through the panel")
			return
		}
		if !allowed {
			mihomoPanelFail(w, http.StatusMethodNotAllowed, "NOT_ALLOWED", "method is not available through the panel")
			return
		}
		query := url.Values{}
		for _, key := range route.query {
			if value := r.URL.Query().Get(key); value != "" {
				query.Set(key, value)
			}
		}
		reload := path == "configs" && r.Method == http.MethodPut
		upgrading := path == "upgrade"
		providerUpdate := r.Method == http.MethodPut && (strings.HasPrefix(path, "providers/rules/") || strings.HasPrefix(path, "providers/proxies/"))
		var body []byte
		if reload {
			body = []byte(mihomoPanelReloadBody)
			query.Set("force", "true")
		} else if r.Method == http.MethodPut || r.Method == http.MethodPatch {
			data, err := io.ReadAll(io.LimitReader(r.Body, mihomoPanelBodyLimit+1))
			if err != nil || len(data) > mihomoPanelBodyLimit {
				mihomoPanelFail(w, http.StatusBadRequest, "INVALID_REQUEST", "request body is too large")
				return
			}
			body = data
		}
		controller, err := resolveMihomoController(cfg.ConfigPath)
		if err != nil {
			mihomoPanelFail(w, http.StatusConflict, "CONTROLLER_UNAVAILABLE", "mihomo controller unavailable: "+err.Error())
			return
		}
		if path == "flow" {
			flow.serve(r.Context(), w, client, controller)
			return
		}
		if path == "logs" {
			logs.serve(w, r, controller)
			return
		}
		if reload || upgrading || providerUpdate {
			// Native provider refreshes replace managed files, so they serialize
			// with deployments and rule snapshots as well as reloads/upgrades.
			release, err := routes.AcquireServiceOperation(toRoutesConfig(cfg))
			if err != nil {
				mihomoPanelFail(w, http.StatusConflict, "SERVICE_BUSY", err.Error())
				return
			}
			defer release()
		}
		timeout := mihomoPanelTimeout
		if reload {
			timeout = mihomoPanelReloadTimeout
		} else if upgrading {
			timeout = mihomoPanelUpgradeTimeout
		} else if strings.HasSuffix(path, "/delay") || strings.HasSuffix(path, "/healthcheck") || (strings.HasPrefix(path, "providers/") && r.Method == http.MethodPut) {
			timeout = mihomoPanelDelayTimeout
		}
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		target := "/" + path
		if len(query) > 0 {
			target += "?" + query.Encode()
		}
		req, err := controller.request(ctx, r.Method, target)
		if err != nil {
			mihomoPanelFail(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid panel request")
			return
		}
		if body != nil {
			req.Body = io.NopCloser(bytes.NewReader(body))
			req.ContentLength = int64(len(body))
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := client.Do(req)
		if err != nil {
			mihomoPanelFail(w, http.StatusBadGateway, "CORE_UNREACHABLE", "mihomo controller is unreachable")
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			mihomoPanelFail(w, http.StatusBadGateway, "CORE_AUTH_FAILED", "mihomo controller rejected the secret")
			return
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, mihomoPanelReplyLimit+1))
		if err != nil || len(data) > mihomoPanelReplyLimit {
			mihomoPanelFail(w, http.StatusBadGateway, "INVALID_RESPONSE", "mihomo controller response is too large")
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		if contentType := resp.Header.Get("Content-Type"); contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(data)
	}
}
