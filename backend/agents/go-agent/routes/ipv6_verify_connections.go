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
	// Transport is the address family of the node connection: ipv6, ipv4 or unknown.
	Transport string `json:"transport,omitempty"`
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
		proxies, err := ipv6RuntimeProxies(ctx, controller)
		identified := false
		transport := ""
		if err == nil {
			for i := range report.Matches {
				match := &report.Matches[i]
				for _, name := range match.Chains {
					if p, exists := proxies[name]; exists && ipv6ActualProxy(p) {
						identified = true
						match.ProxyName, match.ProxyType = name, p.Type
						match.Transport = ipv6ConnectionTransport(*match, p)
						match.IPv6TransportVerified = match.Transport == "ipv6"
						if transport != "ipv6" && match.Transport != "unknown" {
							transport = match.Transport
						}
						break
					}
				}
			}
		}
		if identified {
			report.add("client_mihomo_path", "客户端经过 Mihomo", "ok", "捕获到访问目标域名的新连接，连接链包含实际代理节点", "mihomo_connections", "请与客户端结果的来源 IP 和端口关联。观察只筛选目标域名、新连接和可选来源 IP；request_id 不是核心提供的匹配字段。")
			switch transport {
			case "ipv6":
				report.add("client_ipv6_transport", "客户端请求的 IPv6 节点连接", "ok", "新连接的实际节点远端是 IPv6 地址", "mihomo_connection_transport", "仍须与客户端本次请求的来源地址、端口和 HTTP 结果一致，才能确认完整路径。")
			case "ipv4":
				report.add("client_ipv6_transport", "客户端请求的 IPv6 节点连接", "unknown", "新连接经由的节点使用 IPv4 连接", "mihomo_connection_transport")
			default:
				report.add("client_ipv6_transport", "客户端请求的 IPv6 节点连接", "unknown", "节点类型或连接方式不提供可靠的实际远端地址", "mihomo_connection_transport")
			}
		} else {
			report.add("client_mihomo_path", "客户端经过 Mihomo", "warning", "捕获到经过 Mihomo 的新连接，但没有确认其连接链使用实际代理节点", "mihomo_connections", "可能为 DIRECT 或节点类型无法确认；不能标为代理链路通过。")
		}
	}
	finish()
}

// ipv6RuntimeProxies returns the running proxies by name, including the nodes
// of proxy providers, which /proxies does not list.
func ipv6RuntimeProxies(ctx context.Context, controller *mihomoReloader) (map[string]ipv6RuntimeProxy, error) {
	var top struct {
		Proxies map[string]ipv6RuntimeProxy `json:"proxies"`
	}
	if err := ipv6VerifyAPIStep(ctx, controller, "/proxies", &top); err != nil {
		return nil, err
	}
	proxies := top.Proxies
	if proxies == nil {
		proxies = map[string]ipv6RuntimeProxy{}
	}
	var providers struct {
		Providers map[string]struct {
			Proxies []ipv6RuntimeProxy `json:"proxies"`
		} `json:"providers"`
	}
	// An older core without provider listing still identifies top-level nodes.
	if ipv6VerifyAPIStep(ctx, controller, "/providers/proxies", &providers) == nil {
		for _, provider := range providers.Providers {
			for _, p := range provider.Proxies {
				if _, exists := proxies[p.Name]; !exists && p.Name != "" {
					proxies[p.Name] = p
				}
			}
		}
	}
	return proxies, nil
}

// ipv6ConnectionTransport reports the address family Mihomo used to reach the
// node: "ipv6", "ipv4" or "unknown". Mihomo's remoteDestination is the node
// socket address for plain adapters (lab-checked on 1.19.34 for SS, VMess,
// VLESS, Trojan, AnyTLS, TUIC and provider nodes, including reused sessions).
// With smux it is the target address, and a chained dialer hides the node
// socket, so those stay unknown, as does a remote reported as a domain name.
func ipv6ConnectionTransport(match ipv6ConnectionMatch, proxy ipv6RuntimeProxy) string {
	if proxy.Smux || proxy.DialerProxy != "" {
		return "unknown"
	}
	remote := net.ParseIP(strings.Trim(match.RemoteDestination, "[]"))
	if remote == nil {
		if host, _, err := net.SplitHostPort(match.RemoteDestination); err == nil {
			remote = net.ParseIP(host)
		}
	}
	if remote == nil || remote.IsUnspecified() || remote.Equal(net.ParseIP(match.DestinationIP)) {
		return "unknown"
	}
	if remote.To4() != nil {
		return "ipv4"
	}
	return "ipv6"
}
