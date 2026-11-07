package routes

import (
	"agent/preflight"
	"log"
	"net/netip"
	"os"

	"gopkg.in/yaml.v3"
)

// newlyExcludedOxiDNSUpstreams lists the same-host OxiDNS upstreams that the
// previous Mihomo TUN hijacked and the current one now routes around.
func newlyExcludedOxiDNSUpstreams(current, previous, oxidns map[string]interface{}) []netip.Addr {
	tun, _ := current["tun"].(map[string]interface{})
	before, _ := previous["tun"].(map[string]interface{})
	if tun["enable"] != true || before["enable"] != true {
		return nil
	}
	excluded, _ := tun["route-exclude-address"].([]interface{})
	excludedBefore, _ := before["route-exclude-address"].([]interface{})
	var result []netip.Addr
	for _, address := range preflight.TUNHijackedOxiDNSUpstreams(oxidns, before) {
		if preflight.PrefixesCover(excluded, address) && !preflight.PrefixesCover(excludedBefore, address) {
			result = append(result, address)
		}
	}
	return result
}

// refreshAIOPeerUpstreams reloads the same-host OxiDNS after Mihomo starts
// excluding upstreams it used to hijack. OxiDNS keeps one UDP socket per
// upstream, and every query refreshes the conntrack entry that DNATed that
// socket to Mihomo's DNS, so the new exclusion never applies and OxiDNS keeps
// getting fake-ip (lab: 45 of 45 queries over 25 s). A reload opens new
// sockets. It also clears the OxiDNS cache, so it runs only for a newly
// excluded upstream, and it is best effort: Mihomo's release already succeeded.
func refreshAIOPeerUpstreams(cfg *Config, previous []byte) {
	if dnsPeerService(cfg) != "oxidns" {
		return
	}
	var before, current map[string]interface{}
	if yaml.Unmarshal(previous, &before) != nil {
		return
	}
	data, err := os.ReadFile(cfg.ConfigPath)
	if err != nil || yaml.Unmarshal(data, &current) != nil {
		return
	}
	peer, _ := deploymentDNSPeer(cfg, readAIOPeerDNS)
	if peer == nil {
		return
	}
	var oxidns map[string]interface{}
	if yaml.Unmarshal(peer.Content, &oxidns) != nil {
		return
	}
	upstreams := newlyExcludedOxiDNSUpstreams(current, before, oxidns)
	if len(upstreams) == 0 {
		return
	}
	skip := func(reason string) {
		log.Printf("Mihomo now excludes OxiDNS upstreams %v; OxiDNS was not reloaded (%s), publish OxiDNS once so it stops getting fake-ip", upstreams, reason)
	}
	peerCfg := &Config{ServiceType: "oxidns", ConfigPath: peer.Config.ConfigPath}
	api, apiErr := loadOxiDNSMonitorConfig(peerCfg)
	if apiErr != nil {
		skip("no usable local HTTP API")
		return
	}
	reloader := &oxidnsReloader{endpoint: api.endpoint, client: oxMonitorClient()}
	if api.basicAuth {
		reloader.username, reloader.password = api.username, api.password
	}
	status, err := reloader.status()
	if err != nil {
		skip(err.Error())
		return
	}
	if status.Pending || status.InProgress {
		skip("another reload is in progress")
		return
	}
	// Reload only the configuration OxiDNS already runs; never apply an
	// on-disk file that its own Agent has not activated.
	live, err := os.ReadFile(peerCfg.ConfigPath)
	if err != nil || status.RunningVersion == nil || *status.RunningVersion != sha256Hex(live) {
		skip("running version differs from the on-disk configuration")
		return
	}
	if err = reloader.reload(peerCfg); err != nil {
		skip(err.Error())
		return
	}
	log.Printf("Mihomo now excludes OxiDNS upstreams %v; reloaded OxiDNS so its upstream sockets bypass the TUN", upstreams)
}
