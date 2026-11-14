package main

import (
	"log"
	"net"
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
	value := strings.TrimSpace(c.AgentIP)
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
		return strings.TrimSpace(c.AgentIP)
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
