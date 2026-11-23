package routes

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type ipv6ConnectionMatch struct {
	ID                    string   `json:"id"`
	SourceIP              string   `json:"source_ip"`
	SourcePort            int      `json:"source_port"`
	Host                  string   `json:"host"`
	DestinationIP         string   `json:"destination_ip"`
	DestinationPort       int      `json:"destination_port"`
	Chains                []string `json:"chains"`
	Rule                  string   `json:"rule"`
	Start                 string   `json:"start"`
	RemoteDestination     string   `json:"remote_destination,omitempty"`
	ProxyName             string   `json:"proxy_name,omitempty"`
	ProxyType             string   `json:"proxy_type,omitempty"`
	IPv6TransportVerified bool     `json:"ipv6_transport_verified"`
	Network               string   `json:"network"`
	InboundType           string   `json:"inbound_type,omitempty"`
}
type ipv6ConnectionsReport struct {
	ipv6VerifyReport
	RequestID string                `json:"request_id,omitempty"`
	Matches   []ipv6ConnectionMatch `json:"matches"`
}
type ipv6RuntimeConnection struct {
	ID       string `json:"id"`
	Metadata struct {
		SourceIP          string          `json:"sourceIP"`
		SourcePort        json.RawMessage `json:"sourcePort"`
		Host              string          `json:"host"`
		DestinationIP     string          `json:"destinationIP"`
		DestinationPort   json.RawMessage `json:"destinationPort"`
		RemoteDestination string          `json:"remoteDestination"`
		Network           string          `json:"network"`
		Type              string          `json:"type"`
	} `json:"metadata"`
	Chains []string `json:"chains"`
	Rule   string   `json:"rule"`
	Start  string   `json:"start"`
}

func ipv6JSONPort(value json.RawMessage) int {
	var n int
	if json.Unmarshal(value, &n) != nil {
		var text string
		if json.Unmarshal(value, &text) != nil {
			return 0
		}
		n, _ = strconv.Atoi(text)
	}
	if n < 1 || n > 65535 {
		return 0
	}
	return n
}

func ipv6VerifyConnectionsHandler(cfg *Config, w http.ResponseWriter, r *http.Request) {
	var request struct {
		SourceIP  string `json:"source_ip"`
		Host      string `json:"host"`
		RequestID string `json:"request_id"`
	}
	if decodeIPv6Request(w, r, &request) != nil || (request.SourceIP != "" && net.ParseIP(request.SourceIP) == nil) || len(request.Host) > 253 || !ipv6VerifyDomain.MatchString(request.Host) || (request.RequestID != "" && !ipv6VerifyRequestID.MatchString(request.RequestID)) || !ipv6VerifySupported(cfg) || cfg.ServiceType != "mihomo" {
		JsonResponse(w, 400, MessageResponse{Success: false, Message: "连接观察参数无效"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	report := ipv6ConnectionsReport{ipv6VerifyReport: ipv6VerifyReport{Success: true, Scope: "mihomo_client_connections", CheckedAt: time.Now().UTC().Format(time.RFC3339Nano), TargetHost: request.Host, Checks: []ipv6VerifyCheck{}}, RequestID: request.RequestID, Matches: []ipv6ConnectionMatch{}}
	finish := func() { JsonResponse(w, 200, report) }
	doc, _, err := readYAMLDocument(cfg.ConfigPath)
	if err != nil {
		report.add("client_mihomo_path", "客户端经过 Mihomo", "unknown", "无法读取本机控制接口设置", "mihomo_connections")
		finish()
		return
	}
	controller, err := newMihomoReloader(doc)
	if err != nil {
		report.add("client_mihomo_path", "客户端经过 Mihomo", "unknown", "无法确认本机 Mihomo 控制接口", "mihomo_connections")
		finish()
		return
	}
	defer controller.client.CloseIdleConnections()
	var snapshot struct {
		Connections []ipv6RuntimeConnection `json:"connections"`
	}
	if ipv6VerifyAPIStep(ctx, controller, "/connections", &snapshot) != nil {
		report.add("client_mihomo_path", "客户端经过 Mihomo", "unknown", "当前核心不提供可读取的连接记录", "mihomo_connections")
		finish()
		return
	}
	known := map[string]bool{}
	for _, connection := range snapshot.Connections {
		known[connection.ID] = true
	}
	deadline := time.Now().Add(8 * time.Second)
	var firstMatch time.Time
	observed := map[string]bool{}
	for time.Now().Before(deadline) && ctx.Err() == nil {
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
		if ctx.Err() != nil {
			break
		}
		if ipv6VerifyAPIStep(ctx, controller, "/connections", &snapshot) != nil {
			continue
		}
		for _, connection := range snapshot.Connections {
			if connection.ID == "" || known[connection.ID] || observed[connection.ID] || len(report.Matches) >= 16 {
				continue
			}
			source := net.ParseIP(connection.Metadata.SourceIP)
			if source == nil || (request.SourceIP != "" && !source.Equal(net.ParseIP(request.SourceIP))) || !strings.EqualFold(strings.TrimSuffix(connection.Metadata.Host, "."), strings.TrimSuffix(request.Host, ".")) {
				continue
			}
			observed[connection.ID] = true
			report.Matches = append(report.Matches, ipv6ConnectionMatch{ID: connection.ID, SourceIP: source.String(), SourcePort: ipv6JSONPort(connection.Metadata.SourcePort), Host: connection.Metadata.Host, DestinationIP: connection.Metadata.DestinationIP, DestinationPort: ipv6JSONPort(connection.Metadata.DestinationPort), RemoteDestination: connection.Metadata.RemoteDestination, Network: connection.Metadata.Network, InboundType: connection.Metadata.Type, Chains: connection.Chains, Rule: connection.Rule, Start: connection.Start})
			if firstMatch.IsZero() {
				firstMatch = time.Now()
			}
		}
		// The source port in the client result must disambiguate simultaneous
		// requests; the core does not provide the HTTP nonce as match evidence.
		if !firstMatch.IsZero() && time.Since(firstMatch) >= 500*time.Millisecond {
			break
		}
	}
	if len(report.Matches) == 0 {
		report.add("client_mihomo_path", "客户端经过 Mihomo", "unknown", "观察窗口内未捕获匹配客户端和目标域名的新连接", "mihomo_connections", "请先开始连接观察，再由独立客户端发起请求；短连接也可能来不及捕获。")
	} else {
		var proxies struct {
			Proxies map[string]ipv6RuntimeProxy `json:"proxies"`
		}
		identified := false
		transportVerified := false
		if ipv6VerifyAPIStep(ctx, controller, "/proxies", &proxies) == nil {
			for i := range report.Matches {
				match := &report.Matches[i]
				for _, name := range match.Chains {
					if p, exists := proxies.Proxies[name]; exists && ipv6ActualProxy(p) {
						identified = true
						match.ProxyName, match.ProxyType = name, p.Type
						_, node := ipv6SelectVerifyNode(doc, proxies.Proxies, name)
						match.IPv6TransportVerified = ipv6ConnectionTransport(ctx, cfg, *match, node, p)
						transportVerified = transportVerified || match.IPv6TransportVerified
						break
					}
				}
			}
		}
		if identified {
			report.add("client_mihomo_path", "客户端经过 Mihomo", "ok", "捕获到访问目标域名的新连接，连接链包含实际代理节点", "mihomo_connections", "请与客户端结果的来源 IP 和端口关联。观察只筛选目标域名、新连接和可选来源 IP；request_id 不是核心提供的匹配字段。")
			if transportVerified {
				report.add("client_ipv6_transport", "客户端请求的 IPv6 节点连接", "ok", "新连接的实际远端与 IPv6 节点一致，且已观察到 Mihomo 的对应 IPv6 连接", "mihomo_connection_transport", "仍须与客户端本次请求的来源地址、端口和 HTTP 结果一致，才能确认完整路径。")
			} else {
				report.add("client_ipv6_transport", "客户端请求的 IPv6 节点连接", "unknown", "没有取得足以归属到本次客户端连接的 IPv6 节点证据", "mihomo_connection_transport")
			}
		} else {
			report.add("client_mihomo_path", "客户端经过 Mihomo", "warning", "捕获到经过 Mihomo 的新连接，但没有确认其连接链使用实际代理节点", "mihomo_connections", "可能为 DIRECT 或节点类型无法确认；不能标为代理链路通过。")
		}
	}
	finish()
}

func ipv6ConnectionTransport(ctx context.Context, cfg *Config, match ipv6ConnectionMatch, node map[string]any, proxy ipv6RuntimeProxy) bool {
	// For these TCP adapters RemoteDestination comes from the connected socket.
	// Other adapters can expose a synthetic address (QUIC/multiplexing/tunnels),
	// so leave them unknown until an equally strong correlation is implemented.
	if match.Network != "tcp" || (!strings.EqualFold(proxy.Type, "socks5") && !strings.EqualFold(proxy.Type, "http")) {
		return false
	}
	if proxy.DialerProxy != "" || node["dialer-proxy"] != nil && node["dialer-proxy"] != "" {
		return false
	}
	server, _ := node["server"].(string)
	ip := net.ParseIP(server)
	remote := net.ParseIP(strings.Trim(match.RemoteDestination, "[]"))
	if remote == nil {
		host, _, err := net.SplitHostPort(match.RemoteDestination)
		if err == nil {
			remote = net.ParseIP(host)
		}
	}
	if ip == nil || ip.To4() != nil || remote == nil || !remote.Equal(ip) || match.SourcePort == 0 {
		return false
	}
	stop := ipv6ObserveNodeSockets(ctx, cfg, node, proxy.DialerProxy)
	timer := time.NewTimer(100 * time.Millisecond)
	select {
	case <-ctx.Done():
		timer.Stop()
	case <-timer.C:
	}
	return stop().Status == "ok"
}
