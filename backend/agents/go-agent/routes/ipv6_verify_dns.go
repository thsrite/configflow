package routes

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

type ipv6DNSAnswer struct {
	Addresses []net.IP
	RCode     int
}

var ipv6VerifyDNSQuery = ipv6DNSQuery

func ipv6DNSName(packet []byte, offset, depth int) (string, int, error) {
	if depth > 16 {
		return "", 0, fmt.Errorf("DNS 名称指针无效")
	}
	labels := []string{}
	length := 0
	for {
		if offset >= len(packet) {
			return "", 0, fmt.Errorf("DNS 名称不完整")
		}
		n := int(packet[offset])
		offset++
		if n == 0 {
			return strings.Join(labels, "."), offset, nil
		}
		if n&0xc0 == 0xc0 {
			if offset >= len(packet) {
				return "", 0, fmt.Errorf("DNS 指针不完整")
			}
			suffix, _, err := ipv6DNSName(packet, (n&63)<<8|int(packet[offset]), depth+1)
			if err != nil {
				return "", 0, err
			}
			if length+len(suffix) > 253 {
				return "", 0, fmt.Errorf("DNS 名称过长")
			}
			labels = append(labels, suffix)
			return strings.Join(labels, "."), offset + 1, nil
		}
		if n > 63 || offset+n > len(packet) || length+n+1 > 254 {
			return "", 0, fmt.Errorf("DNS 名称无效")
		}
		labels = append(labels, string(packet[offset:offset+n]))
		length += n + 1
		offset += n
	}
}

func ipv6ParseDNS(packet, question []byte, name string, qtype uint16) (ipv6DNSAnswer, error) {
	answer := ipv6DNSAnswer{Addresses: []net.IP{}}
	bad := fmt.Errorf("DNS 应答格式或问题不匹配")
	if len(packet) < 12 || len(question) < 12 || packet[0] != question[0] || packet[1] != question[1] || packet[2]&0xfa != 0x80 || binary.BigEndian.Uint16(packet[4:6]) != 1 {
		return answer, bad
	}
	got, next, err := ipv6DNSName(packet, 12, 0)
	if err != nil || !strings.EqualFold(got, strings.TrimSuffix(name, ".")) || next+4 > len(packet) || binary.BigEndian.Uint16(packet[next:next+2]) != qtype || binary.BigEndian.Uint16(packet[next+2:next+4]) != 1 {
		return answer, bad
	}
	answer.RCode = int(packet[3] & 15)
	offset := next + 4
	for i := 0; i < int(binary.BigEndian.Uint16(packet[6:8])); i++ {
		_, next, err = ipv6DNSName(packet, offset, 0)
		if err != nil || next+10 > len(packet) {
			return answer, bad
		}
		kind, class, size := binary.BigEndian.Uint16(packet[next:next+2]), binary.BigEndian.Uint16(packet[next+2:next+4]), int(binary.BigEndian.Uint16(packet[next+8:next+10]))
		offset = next + 10 + size
		if offset > len(packet) {
			return answer, bad
		}
		if class == 1 && kind == qtype && ((qtype == 1 && size == 4) || (qtype == 28 && size == 16)) {
			answer.Addresses = append(answer.Addresses, net.IP(append([]byte(nil), packet[next+10:offset]...)))
		}
	}
	return answer, nil
}

func ipv6DNSQuery(parent context.Context, address, name string, qtype uint16) (ipv6DNSAnswer, error) {
	if !ipv6VerifyDomain.MatchString(name) || len(name) > 253 || (qtype != 1 && qtype != 28) {
		return ipv6DNSAnswer{}, fmt.Errorf("DNS 查询参数无效")
	}
	var id [2]byte
	if _, err := rand.Read(id[:]); err != nil {
		return ipv6DNSAnswer{}, fmt.Errorf("无法创建 DNS 查询")
	}
	query := buildDNSQuery(binary.BigEndian.Uint16(id[:]), strings.TrimSuffix(name, "."))
	binary.BigEndian.PutUint16(query[len(query)-4:len(query)-2], qtype)
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	for _, protocol := range []string{"udp", "tcp"} {
		conn, err := (&net.Dialer{}).DialContext(ctx, protocol, address)
		if err != nil {
			return ipv6DNSAnswer{}, fmt.Errorf("DNS 监听无法连接")
		}
		deadline, _ := ctx.Deadline()
		_ = conn.SetDeadline(deadline)
		payload := query
		if protocol == "tcp" {
			payload = append([]byte{byte(len(query) >> 8), byte(len(query))}, query...)
		}
		_, err = conn.Write(payload)
		if err != nil {
			conn.Close()
			return ipv6DNSAnswer{}, fmt.Errorf("DNS 查询发送失败")
		}
		var packet []byte
		if protocol == "udp" {
			packet = make([]byte, 4096)
			var n int
			n, err = conn.Read(packet)
			packet = packet[:n]
		} else {
			var size [2]byte
			_, err = io.ReadFull(conn, size[:])
			if err == nil {
				n := int(binary.BigEndian.Uint16(size[:]))
				if n < 12 || n > 16384 {
					err = fmt.Errorf("DNS 响应长度无效")
				} else {
					packet = make([]byte, n)
					_, err = io.ReadFull(conn, packet)
				}
			}
		}
		conn.Close()
		if err != nil {
			return ipv6DNSAnswer{}, fmt.Errorf("DNS 未在期限内返回有效应答")
		}
		if protocol == "udp" && len(packet) >= 12 && packet[0] == query[0] && packet[1] == query[1] && packet[2]&2 != 0 {
			continue
		}
		return ipv6ParseDNS(packet, query, name, qtype)
	}
	return ipv6DNSAnswer{}, fmt.Errorf("DNS 应答被截断")
}

func ipv6LocalDNSListener(raw string) (string, error) {
	host, port, err := net.SplitHostPort(raw)
	if err != nil {
		return "", fmt.Errorf("DNS 监听地址无效")
	}
	n, e := strconv.Atoi(port)
	if e != nil || n < 1 || n > 65535 {
		return "", fmt.Errorf("DNS 监听端口无效")
	}
	switch host {
	case "", "*", "0.0.0.0", "localhost":
		host = "127.0.0.1"
	case "::":
		host = "::1"
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return "", fmt.Errorf("DNS 监听不是本机 IP")
	}
	local := ip.IsLoopback()
	if !local {
		if addrs, e := net.InterfaceAddrs(); e == nil {
			for _, addr := range addrs {
				candidate, _, e := net.ParseCIDR(addr.String())
				if e == nil && candidate.Equal(ip) {
					local = true
				}
			}
		}
	}
	if !local {
		return "", fmt.Errorf("DNS 监听地址不属于本机")
	}
	return net.JoinHostPort(host, port), nil
}

func ipv6VerifyDNS(ctx context.Context, report *ipv6VerifyReport, doc map[string]any, host string) {
	dns, _ := doc["dns"].(map[string]any)
	listen, _ := dns["listen"].(string)
	address, err := ipv6LocalDNSListener(listen)
	if dns["enable"] != true || err != nil || net.ParseIP(host) != nil {
		report.add("dns_answers", "DNS 实际应答", "unknown", "未能确认本机 DNS 监听或用于查询的测试域名", "mihomo_dns")
		return
	}
	for _, kind := range []struct {
		q         uint16
		id, label string
	}{{1, "dns_a", "DNS 返回 IPv4 地址"}, {28, "dns_aaaa", "DNS 返回 IPv6 地址"}} {
		answer, err := ipv6VerifyDNSQuery(ctx, address, host, kind.q)
		if err != nil {
			report.add(kind.id, kind.label, "error", "实际 DNS 查询未取得有效应答", "mihomo_dns")
			continue
		}
		if answer.RCode != 0 {
			report.add(kind.id, kind.label, "warning", fmt.Sprintf("DNS 已响应，但本次查询返回错误码 %d", answer.RCode), "mihomo_dns")
			continue
		}
		if len(answer.Addresses) == 0 {
			if kind.q == 28 && dns["ipv6"] != true {
				report.add(kind.id, kind.label, "ok", "实际查询未返回 AAAA，符合当前关闭 DNS IPv6 的设置", "mihomo_dns", "这不影响仅通过 IPv6 节点代理的用途。")
			} else {
				report.add(kind.id, kind.label, "warning", "DNS 已响应，但本次域名没有返回对应地址", "mihomo_dns")
			}
			continue
		}
		values := []string{}
		for i, ip := range answer.Addresses {
			if i >= 8 {
				break
			}
			values = append(values, ip.String())
		}
		status := "ok"
		message := "已向配置文件记录的 DNS 监听实际查询并收到地址"
		if kind.q == 28 && dns["ipv6"] != true {
			status = "warning"
			message = "实际 DNS 仍返回 AAAA，与文件中关闭 DNS IPv6 的设置不同"
		}
		report.add(kind.id, kind.label, status, message, "mihomo_dns", strings.Join(values, ", "), "本项没有经过客户端的 OxiDNS 或路由器 DNS。")
	}
}
