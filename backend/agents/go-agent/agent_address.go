package main

import (
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

	"agent/routes"
)

// Set once the server's heartbeat reply says it reads "host". An older server
// rejects a heartbeat that carries a field it does not know.
var serverReadsHeartbeatHost atomic.Bool

var localInterfaceAddrs = net.InterfaceAddrs

var staleAgentIP struct {
	sync.Mutex
	logged string
}

func hostHasAddress(value string) (present, known bool) {
	addrs, err := localInterfaceAddrs()
	if err != nil {
		return false, false
	}
	want := net.ParseIP(value)
	if want == nil {
		return false, true
	}
	for _, addr := range addrs {
		var ip net.IP
		switch item := addr.(type) {
		case *net.IPNet:
			ip = item.IP
		case *net.IPAddr:
			ip = item.IP
		}
		if ip != nil && ip.Equal(want) {
			return true, true
		}
	}
	return false, true
}

// effectiveAgentIP is the configured agent_ip while this host still has it.
//
// The installer may have pinned the address the host had at install time.
// Once the host is renumbered that value names nothing here; treating it as
// unset (automatic detection, as for an install that never pinned one) keeps
// publishing and registration working without editing the config file by hand.
// A container's AGENT_IP is its operator's statement and is never second-guessed.
func (c *Config) effectiveAgentIP() string {
	value := c.configuredAgentIP()
	if value == "" || c.ServiceManager == "supervisor" {
		return value
	}
	present, known := hostHasAddress(value)
	if present || !known {
		return value
	}
	staleAgentIP.Lock()
	if staleAgentIP.logged != value {
		staleAgentIP.logged = value
		log.Printf("Configured agent_ip %s is no longer on any interface of this host; detecting the address automatically instead. Update agent_ip in the Agent config to silence this.", value)
	}
	staleAgentIP.Unlock()
	return ""
}

// heartbeatHost is the address the server should use to reach this Agent, or
// "" to leave the server's record alone.
func (c *Config) heartbeatHost() string {
	if c.ServiceManager == "supervisor" {
		// Inside a bridge container the local address is not reachable from
		// the LAN; only an explicit AGENT_IP is worth reporting.
		return c.configuredAgentIP()
	}
	if value := c.effectiveAgentIP(); value != "" {
		return value
	}
	vips := routes.OxiDNSHAVirtualIPs(&routes.Config{AgentConfigFile: c.filePath, ServiceType: c.ServiceType, NativeAIO: c.NativeAIO})
	value, err := detectLocalIPv4Except(vips)
	if err != nil {
		return ""
	}
	return value
}

func (c *Config) configuredAgentIP() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return strings.TrimSpace(c.AgentIP)
}

// setAgentIP stores agent_ip in the config file; memory keeps the old value
// when the file could not be written.
func (c *Config) setAgentIP(value string) error {
	c.mu.Lock()
	previous := c.AgentIP
	c.AgentIP = value
	c.mu.Unlock()
	if err := c.Save(); err != nil {
		c.mu.Lock()
		c.AgentIP = previous
		c.mu.Unlock()
		return err
	}
	return nil
}

// adoptConfirmedHost replaces a pinned agent_ip this host no longer has with
// the address the server has just confirmed it reaches this Agent at.
//
// It writes only when all of these hold, so a confirmed address is written
// once and never again: the file pins an address, that address differs from
// the confirmed one, the confirmed one is what this Agent reports, and this
// host has it. An install that never pinned an address stays automatic.
func (c *Config) adoptConfirmedHost(confirmed string) {
	confirmed = strings.TrimSpace(confirmed)
	if confirmed == "" || c.ServiceManager == "supervisor" {
		return
	}
	configured := c.configuredAgentIP()
	if configured == "" || configured == confirmed || c.heartbeatHost() != confirmed {
		return
	}
	if present, known := hostHasAddress(confirmed); !present || !known {
		return
	}
	if err := c.setAgentIP(confirmed); err != nil {
		log.Printf("Could not write the confirmed address %s to the Agent config: %v", confirmed, err)
		return
	}
	log.Printf("agent_ip in the Agent config changed from %s to %s: the server confirmed it reaches this Agent at the new address.", configured, confirmed)
}

// localAgentAddresses lists the LAN IPv4 addresses this Agent may be reached
// at; a floating DNS high-availability address is not one of them.
func (c *Config) localAgentAddresses() ([]string, error) {
	values, err := detectAllLocalIPv4()
	if err != nil {
		return nil, err
	}
	vips := routes.OxiDNSHAVirtualIPs(&routes.Config{AgentConfigFile: c.filePath, ServiceType: c.ServiceType, NativeAIO: c.NativeAIO})
	result := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		skip := value == ""
		for _, vip := range vips {
			skip = skip || value == vip
		}
		for _, seen := range result {
			skip = skip || value == seen
		}
		if !skip {
			result = append(result, value)
		}
	}
	return result, nil
}

// agentAddressHandler reports the addresses of this host and lets the server
// pin one of them. A container's address comes from AGENT_IP on every start,
// so it is reported but cannot be written here.
func agentAddressHandler(cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		supervised := cfg.ServiceManager == "supervisor"
		switch r.Method {
		case http.MethodGet:
			addresses, err := cfg.localAgentAddresses()
			if err != nil {
				addresses = []string{}
			}
			routes.JsonResponse(w, http.StatusOK, map[string]interface{}{
				"success": true, "protocol": 1, "configured": cfg.configuredAgentIP(), "reported": cfg.heartbeatHost(),
				"addresses": addresses, "writable": !supervised,
			})
		case http.MethodPost:
			var body struct {
				Host string `json:"host"`
			}
			data, err := io.ReadAll(io.LimitReader(r.Body, 4096))
			if err != nil || json.Unmarshal(data, &body) != nil {
				routes.JsonResponse(w, http.StatusBadRequest, map[string]interface{}{"success": false, "message": "请求内容无效"})
				return
			}
			if supervised {
				routes.JsonResponse(w, http.StatusConflict, map[string]interface{}{"success": false, "message": "容器的地址由 AGENT_IP 环境变量决定，请修改后重建容器"})
				return
			}
			host := strings.TrimSpace(body.Host)
			addresses, err := cfg.localAgentAddresses()
			if err != nil {
				routes.JsonResponse(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "message": "读取本机网络地址失败：" + err.Error()})
				return
			}
			found := false
			for _, value := range addresses {
				found = found || value == host
			}
			if !found {
				routes.JsonResponse(w, http.StatusBadRequest, map[string]interface{}{"success": false, "message": "这台设备没有地址 " + host + "，只能选择它现有的局域网地址", "addresses": addresses})
				return
			}
			if cfg.configuredAgentIP() != host {
				previous := cfg.configuredAgentIP()
				if err := cfg.setAgentIP(host); err != nil {
					routes.JsonResponse(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "message": "写入 Agent 配置失败：" + err.Error()})
					return
				}
				log.Printf("agent_ip in the Agent config changed from %q to %s at the server's request.", previous, host)
			}
			routes.JsonResponse(w, http.StatusOK, map[string]interface{}{"success": true, "host": host})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}
}
