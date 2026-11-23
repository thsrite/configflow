package routes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

type ipv6ClientRequestResult struct {
	Family     string                 `json:"family"`
	Address    string                 `json:"address"`
	SourceIP   string                 `json:"source_ip,omitempty"`
	SourcePort int                    `json:"source_port,omitempty"`
	Status     string                 `json:"status"`
	StatusCode int                    `json:"status_code,omitempty"`
	Route      *ipv6ClientTargetRoute `json:"route,omitempty"`
}
type ipv6ClientTargetRoute struct {
	Gateway   string `json:"gateway"`
	Interface string `json:"interface"`
	Source    string `json:"source"`
	Table     string `json:"table,omitempty"`
	Scope     string `json:"scope"`
}
type ipv6ClientRoute struct {
	Family    string `json:"family"`
	Gateway   string `json:"gateway"`
	Interface string `json:"interface"`
}
type ipv6ClientDNS struct {
	Servers []string `json:"servers"`
	A       []string `json:"a"`
	AAAA    []string `json:"aaaa"`
}
type ipv6ClientReport struct {
	ipv6VerifyReport
	RequestID     string                    `json:"request_id"`
	DNS           ipv6ClientDNS             `json:"dns"`
	Requests      []ipv6ClientRequestResult `json:"requests"`
	DefaultRoutes []ipv6ClientRoute         `json:"default_routes"`
}

var ipv6ClientLookup = func(ctx context.Context, family, host string) ([]net.IP, error) {
	return (&net.Resolver{PreferGo: true}).LookupIP(ctx, family, strings.TrimSuffix(host, ".")+".")
}
var ipv6ClientDial = func(ctx context.Context, network, address string) (net.Conn, error) {
	return (&net.Dialer{}).DialContext(ctx, network, address)
}
var ipv6ClientRouteRead = func(ctx context.Context, family string) ([]byte, error) {
	step, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	return exec.CommandContext(step, "ip", "-j", family, "route", "show", "table", "main", "default").Output()
}
var ipv6ClientResolverFile = "/etc/resolv.conf"
var ipv6ClientHold = 2 * time.Second
var ipv6ClientTargetRouteRead = func(ctx context.Context, family, address, source string, sourcePort, targetPort int) ([]byte, error) {
	args := []string{"-j", "-4", "route", "get", address}
	if family == "ipv6" {
		args[1] = "-6"
	}
	if net.ParseIP(source) != nil {
		args = append(args, "from", source)
	}
	if sourcePort > 0 && targetPort > 0 {
		args = append(args, "ipproto", "tcp", "sport", strconv.Itoa(sourcePort), "dport", strconv.Itoa(targetPort))
	}
	step, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	return exec.CommandContext(step, "ip", args...).Output()
}

func ipv6ClientTargetRouting(ctx context.Context, result ipv6ClientRequestResult, port string) *ipv6ClientTargetRoute {
	n, _ := strconv.Atoi(port)
	data, err := ipv6ClientTargetRouteRead(ctx, result.Family, result.Address, result.SourceIP, result.SourcePort, n)
	if err != nil || len(data) > 65536 {
		return nil
	}
	var rows []struct {
		Gateway    string          `json:"gateway"`
		Dev        string          `json:"dev"`
		PrefSource string          `json:"prefsrc"`
		Source     string          `json:"src"`
		Table      json.RawMessage `json:"table"`
	}
	if json.Unmarshal(data, &rows) != nil || len(rows) != 1 {
		return nil
	}
	row := rows[0]
	if row.Dev == "" {
		return nil
	}
	source := row.Source
	if source == "" {
		source = row.PrefSource
	}
	table := strings.Trim(string(row.Table), "\"")
	return &ipv6ClientTargetRoute{Gateway: row.Gateway, Interface: row.Dev, Source: source, Table: table, Scope: "kernel_route_lookup"}
}

func ipv6VerifySupported(cfg *Config) bool {
	return cfg != nil && gatewayOS == "linux" && (cfg.ServiceManager == "systemd" || cfg.ServiceManager == "openrc")
}

func ipv6ClientResolvers() []string {
	servers := []string{}
	data, err := os.ReadFile(ipv6ClientResolverFile)
	if err != nil || len(data) > 65536 {
		return servers
	}
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.Fields(line)
		if len(parts) >= 2 && parts[0] == "nameserver" {
			host := strings.Split(parts[1], "%")[0]
			if net.ParseIP(host) != nil {
				servers = append(servers, parts[1])
				if len(servers) == 8 {
					break
				}
			}
		}
	}
	return servers
}

func ipv6ClientDefaultRoutes(ctx context.Context) []ipv6ClientRoute {
	routes := []ipv6ClientRoute{}
	for _, family := range []string{"-4", "-6"} {
		data, err := ipv6ClientRouteRead(ctx, family)
		if err != nil || len(data) > 65536 {
			continue
		}
		var values []struct {
			Gateway string `json:"gateway"`
			Dev     string `json:"dev"`
		}
		if json.Unmarshal(data, &values) != nil {
			continue
		}
		for _, row := range values {
			if row.Dev == "" {
				continue
			}
			kind := "ipv4"
			if family == "-6" {
				kind = "ipv6"
			}
			routes = append(routes, ipv6ClientRoute{Family: kind, Gateway: row.Gateway, Interface: row.Dev})
		}
	}
	return routes
}

func ipv6ClientFetch(ctx context.Context, target *url.URL, address net.IP, family string) ipv6ClientRequestResult {
	result := ipv6ClientRequestResult{Family: family, Address: address.String(), Status: "error"}
	if address.IsLoopback() || address.IsUnspecified() || address.IsMulticast() || address.IsLinkLocalUnicast() {
		return result
	}
	port := target.Port()
	if port == "" {
		port = "80"
		if target.Scheme == "https" {
			port = "443"
		}
	}
	step, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	var connection net.Conn
	var localIP string
	var localPort int
	var connectionMu sync.Mutex
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: false, ForceAttemptHTTP2: false, TLSHandshakeTimeout: 3 * time.Second, ResponseHeaderTimeout: 5 * time.Second, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		network := "tcp4"
		if family == "ipv6" {
			network = "tcp6"
		}
		conn, err := ipv6ClientDial(ctx, network, net.JoinHostPort(address.String(), port))
		if err == nil {
			connectionMu.Lock()
			connection = conn
			if local, ok := conn.LocalAddr().(*net.TCPAddr); ok {
				localIP = local.IP.String()
				localPort = local.Port
			}
			connectionMu.Unlock()
		}
		return conn, err
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request, err := http.NewRequestWithContext(step, http.MethodGet, target.String(), nil)
	if err != nil {
		return result
	}
	request.Header.Set("User-Agent", "ConfigFlow-IPv6-Verification/1")
	response, err := client.Do(request)
	connectionMu.Lock()
	result.SourceIP, result.SourcePort = localIP, localPort
	connectionMu.Unlock()
	result.Route = ipv6ClientTargetRouting(step, result, port)
	if err != nil {
		return result
	}
	defer response.Body.Close()
	result.StatusCode = response.StatusCode
	// Keep the response/connection open briefly for the independent Mihomo-side
	// observer. Servers may still close it; absence of evidence stays unknown.
	timer := time.NewTimer(ipv6ClientHold)
	select {
	case <-timer.C:
	case <-step.Done():
	}
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	_, err = io.Copy(io.Discard, io.LimitReader(response.Body, 65537))
	if err == nil && step.Err() == nil && response.StatusCode >= 200 && response.StatusCode < 300 {
		result.Status = "ok"
	}
	connectionMu.Lock()
	if connection != nil {
		_ = connection.Close()
	}
	connectionMu.Unlock()
	return result
}

func ipv6ClientRange(raw string, v6 bool) (*net.IPNet, error) {
	if raw == "" {
		return nil, nil
	}
	ip, network, err := net.ParseCIDR(raw)
	if err != nil || (ip.To4() == nil) != v6 {
		return nil, fmt.Errorf("Fake-IP 网段无效")
	}
	return network, nil
}

func ipv6VerifyClientHandler(cfg *Config, w http.ResponseWriter, r *http.Request) {
	var request struct {
		TestURL       string `json:"test_url"`
		FakeIPv4Range string `json:"fake_ipv4_range"`
		FakeIPv6Range string `json:"fake_ipv6_range"`
		RequestID     string `json:"request_id"`
	}
	if err := decodeIPv6Request(w, r, &request); err != nil {
		JsonResponse(w, 400, map[string]any{"success": false, "message": "客户端验收参数无效"})
		return
	}
	target, err := ipv6VerificationURL(request.TestURL)
	range4, e4 := ipv6ClientRange(request.FakeIPv4Range, false)
	range6, e6 := ipv6ClientRange(request.FakeIPv6Range, true)
	if err != nil || e4 != nil || e6 != nil || !ipv6VerifyRequestID.MatchString(request.RequestID) || !ipv6VerifySupported(cfg) || target != nil && net.ParseIP(target.Hostname()) != nil {
		JsonResponse(w, 400, map[string]any{"success": false, "message": "需要原生 Linux Agent、域名形式的已保存验收地址及有效请求编号/网段"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 28*time.Second)
	defer cancel()
	report := ipv6ClientReport{ipv6VerifyReport: ipv6VerifyReport{Success: true, Scope: "client_default_route", CheckedAt: time.Now().UTC().Format(time.RFC3339Nano), TargetHost: target.Hostname(), Checks: []ipv6VerifyCheck{}}, RequestID: request.RequestID, DNS: ipv6ClientDNS{Servers: ipv6ClientResolvers(), A: []string{}, AAAA: []string{}}, Requests: []ipv6ClientRequestResult{}, DefaultRoutes: ipv6ClientDefaultRoutes(ctx)}
	query := target.Query()
	query.Set("__cf_ipv6_verify", request.RequestID)
	target.RawQuery = query.Encode()
	var addresses [2][]net.IP
	var lookupErrors [2]error
	var wg sync.WaitGroup
	for i, family := range []string{"ip4", "ip6"} {
		wg.Add(1)
		go func(i int, family string) {
			defer wg.Done()
			step, cancel := context.WithTimeout(ctx, 4*time.Second)
			defer cancel()
			addresses[i], lookupErrors[i] = ipv6ClientLookup(step, family, target.Hostname())
		}(i, family)
	}
	wg.Wait()
	for i, kind := range []string{"A", "AAAA"} {
		values := []string{}
		for _, ip := range addresses[i] {
			if (ip.To4() == nil) != (i == 1) {
				continue
			}
			values = append(values, ip.String())
			if len(values) == 16 {
				break
			}
		}
		if i == 0 {
			report.DNS.A = values
		} else {
			report.DNS.AAAA = values
		}
		id, label := "client_dns_a", "客户端 DNS IPv4"
		expected := range4
		if i == 1 {
			id, label = "client_dns_aaaa", "客户端 DNS IPv6"
			expected = range6
		}
		if len(values) == 0 {
			var dnsErr *net.DNSError
			empty := lookupErrors[i] == nil || errors.As(lookupErrors[i], &dnsErr) && dnsErr.IsNotFound
			if i == 1 && range6 == nil && empty && len(report.DNS.A) > 0 {
				report.add(id, label, "ok", "当前 DNS 没有返回 AAAA；IPv4 分流模式下可以保持这样", "client_dns")
			} else {
				report.add(id, label, "warning", "客户端当前 DNS 未返回可用的 "+kind+" 地址", "client_dns")
			}
			continue
		}
		status, message := "ok", "通过客户端实际系统 DNS 取得地址"
		if expected != nil {
			matched := false
			for _, ip := range addresses[i] {
				matched = matched || expected.Contains(ip)
			}
			if !matched {
				status = "warning"
				message = "已取得地址，但本次没有获得预期 Fake-IP 网段中的地址"
			}
		}
		report.add(id, label, status, message, "client_dns", strings.Join(values, ", "))
	}
	var attempts [2][]ipv6ClientRequestResult
	for i, family := range []string{"ipv4", "ipv6"} {
		wg.Add(1)
		go func(i int, family string) {
			defer wg.Done()
			count := 0
			for _, ip := range addresses[i] {
				if ctx.Err() != nil || count >= 3 {
					break
				}
				if (ip.To4() == nil) != (i == 1) {
					continue
				}
				count++
				result := ipv6ClientFetch(ctx, target, ip, family)
				attempts[i] = append(attempts[i], result)
				if result.Status == "ok" {
					break
				}
			}
		}(i, family)
	}
	wg.Wait()
	for i, family := range []string{"IPv4", "IPv6"} {
		id := "client_http_" + strings.ToLower(family)
		report.Requests = append(report.Requests, attempts[i]...)
		if len(attempts[i]) == 0 {
			if i == 0 || range6 != nil {
				report.add(id, "客户端 "+family+" 访问", "unknown", "没有可用于本次访问的 DNS 地址", "client_http")
			}
			continue
		}
		ok := false
		for _, attempt := range attempts[i] {
			ok = ok || attempt.Status == "ok"
		}
		if ok {
			report.add(id, "客户端 "+family+" 访问", "ok", "按客户端正常路由访问测试域名并取得成功 HTTP 响应", "client_http", "未绑定接口、未设置代理、未修改路由；是否经过目标 Mihomo 需要另行关联连接记录。")
		} else {
			report.add(id, "客户端 "+family+" 访问", "error", "已按 DNS 返回地址发起访问，但未取得成功 HTTP 响应", "client_http")
		}
	}
	if len(report.DefaultRoutes) == 0 {
		report.add("client_default_route", "客户端默认网关", "unknown", "未能读取客户端默认路由", "client_route")
	} else {
		details := []string{}
		for _, route := range report.DefaultRoutes {
			details = append(details, route.Family+": "+route.Gateway+" / "+route.Interface)
		}
		report.add("client_default_route", "客户端默认网关", "ok", "已读取客户端当前默认路由；未更改网络设置", "client_route", details...)
	}
	JsonResponse(w, 200, report)
}
