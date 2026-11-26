package routes

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

var ipv6VerifyCorePID = func(ctx context.Context, cfg *Config) (int, error) {
	if cfg.ServiceManager != "systemd" || !regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.@:-]*$`).MatchString(cfg.ServiceUnit) {
		return 0, fmt.Errorf("无法确认核心进程")
	}
	step, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	data, err := exec.CommandContext(step, "systemctl", "show", "--property=MainPID", "--value", "--", cfg.ServiceUnit).Output()
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 1 {
		return 0, fmt.Errorf("核心进程不可用")
	}
	executable, err := os.Readlink(filepath.Join("/proc", strconv.Itoa(pid), "exe"))
	if err != nil {
		return 0, err
	}
	name := strings.ToLower(filepath.Base(strings.TrimSuffix(executable, " (deleted)")))
	if !strings.Contains(name, "mihomo") && !strings.Contains(name, "clash") {
		return 0, fmt.Errorf("服务主进程不是可识别的 Mihomo")
	}
	return pid, nil
}

var ipv6VerifySocketRead = ipv6ProcessHasSocket

func ipv6ProcAddress(raw string) (net.IP, int, error) {
	parts := strings.Split(raw, ":")
	if len(parts) != 2 || len(parts[0]) != 32 {
		return nil, 0, fmt.Errorf("socket 地址无效")
	}
	data, err := hex.DecodeString(parts[0])
	if err != nil {
		return nil, 0, err
	}
	// Linux /proc exposes each network-order IPv6 word in host byte order.
	for i := 0; i < 16; i += 4 {
		binary.NativeEndian.PutUint32(data[i:i+4], binary.BigEndian.Uint32(data[i:i+4]))
	}
	port, err := strconv.ParseUint(parts[1], 16, 16)
	if err != nil {
		return nil, 0, err
	}
	return net.IP(data), int(port), nil
}

func ipv6ProcessHasSocket(pid int, address net.IP, port int) bool {
	root := filepath.Join("/proc", strconv.Itoa(pid))
	fds, err := os.ReadDir(filepath.Join(root, "fd"))
	if err != nil {
		return false
	}
	inodes := map[string]bool{}
	for _, fd := range fds {
		if link, err := os.Readlink(filepath.Join(root, "fd", fd.Name())); err == nil && strings.HasPrefix(link, "socket:[") && strings.HasSuffix(link, "]") {
			inodes[strings.TrimSuffix(strings.TrimPrefix(link, "socket:["), "]")] = true
		}
	}
	for _, name := range []string{"tcp6", "udp6"} {
		data, err := os.ReadFile(filepath.Join(root, "net", name))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			cols := strings.Fields(line)
			if len(cols) < 10 || !inodes[cols[9]] {
				continue
			}
			if name == "tcp6" && cols[3] != "01" {
				continue
			}
			ip, p, err := ipv6ProcAddress(cols[2])
			if err == nil && ip.Equal(address) && p == port {
				return true
			}
		}
	}
	return false
}

// Observation is intentionally an independent check: even a matching live
// socket does not attribute one HTTP request to that socket or prove a LAN path.
func ipv6ObserveNodeSockets(parent context.Context, cfg *Config, node map[string]any, dialerProxy string) func() ipv6VerifyCheck {
	result := ipv6VerifyCheck{ID: "node_ipv6_transport", Label: "Mihomo IPv6 连接", Status: "unknown", Message: "尚未观察到能确认由 Mihomo 发起的节点 IPv6 连接", Scope: "mihomo_ipv6_socket"}
	server, _ := node["server"].(string)
	address := net.ParseIP(server)
	port := 0
	switch value := node["port"].(type) {
	case int:
		port = value
	case string:
		port, _ = strconv.Atoi(value)
	}
	if address == nil || address.To4() != nil || !address.IsGlobalUnicast() || port < 1 || port > 65535 || dialerProxy != "" || node["dialer-proxy"] != nil && node["dialer-proxy"] != "" {
		return func() ipv6VerifyCheck { return result }
	}
	pid, err := ipv6VerifyCorePID(parent, cfg)
	if err != nil {
		return func() ipv6VerifyCheck { return result }
	}
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	found := false
	var once sync.Once
	go func() {
		defer close(done)
		tick := time.NewTicker(50 * time.Millisecond)
		defer tick.Stop()
		for {
			if ipv6VerifySocketRead(pid, address, port) {
				found = true
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
		}
	}()
	return func() ipv6VerifyCheck {
		once.Do(cancel)
		<-done
		if found {
			result.Status = "ok"
			result.Message = "已观察到 Mihomo 连接配置中节点的 IPv6 地址"
			result.Details = []string{"连接属于正在运行的 Mihomo；这一项只证明节点 IPv6 连接存在，网站访问和客户端完整路径另行验证。"}
		}
		return result
	}
}
