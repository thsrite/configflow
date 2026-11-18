package dnsha

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"net/url"
	"strings"

	"configflow/internal/deploy"
	"configflow/internal/model"
	"configflow/internal/oxidns"
	"configflow/internal/yamlx"
	"gopkg.in/yaml.v3"
)

type normalized struct {
	intent   model.HAEnable
	previous map[string]*model.HASettings
}

func unicast(v string) (netip.Addr, bool) {
	a, e := netip.ParseAddr(v)
	return a, e == nil && a.Is4() && !a.IsUnspecified() && !a.IsLoopback() && !a.IsMulticast() && a.String() != "255.255.255.255"
}
func normalize(body json.RawMessage, a, b *agent, list []*agent) (normalized, error) {
	var out normalized
	var fields object
	if e := json.Unmarshal(body, &fields); e != nil || fields == nil {
		return out, refuse(msgInvalid)
	}
	for k := range fields {
		if k != "peer_id" && k != "vip" && k != "vrid" {
			return out, refuse(msgInvalid)
		}
	}
	if a == nil || a.ServiceType != "oxidns" {
		return out, refuse(msgOnlyOxi)
	}
	if fields.text("peer_id") == "" {
		return out, refuse(msgSelectPeer)
	}
	if b == nil || b.ServiceType != "oxidns" {
		return out, refuse(msgPeerMissing)
	}
	if a.ID == b.ID {
		return out, refuse(msgSelf)
	}
	for _, m := range []*agent{a, b} {
		if !m.enabled {
			return out, refuse(fmt.Sprintf(msgDisabled, name(m)))
		}
	}
	if !equal(parseObject(rawObject(a.identity))["profile_id"], parseObject(rawObject(b.identity))["profile_id"]) {
		return out, refuse(msgProfile)
	}
	ha, ok := unicast(a.Host)
	hb, okb := unicast(b.Host)
	if !ok || !okb {
		return out, refuse(msgHosts)
	}
	if ha == hb {
		return out, refuse(msgSameHost)
	}
	vip, ok := unicast(fields.text("vip"))
	if !ok {
		return out, refuse(msgVIP)
	}
	if vip == ha || vip == hb {
		return out, refuse(msgVIPHost)
	}
	vrid := int(vip.As4()[3])
	if vrid == 0 || vrid == 255 {
		vrid = 53
	}
	if raw := fields["vrid"]; len(raw) > 0 && string(raw) != "null" {
		var n int
		if json.Unmarshal(raw, &n) != nil || n < 1 || n > 255 {
			return out, refuse(msgVRID)
		}
		vrid = n
	}
	if haEnabled(a) || haEnabled(b) {
		if !paired(a, b) {
			busy := a
			if !haEnabled(a) {
				busy = b
			}
			return out, refuse(fmt.Sprintf(msgOtherGroup, name(busy)))
		}
		out.previous = map[string]*model.HASettings{a.ID: a.OxiDNSHA, b.ID: b.OxiDNSHA}
	}
	for _, other := range list {
		if other.ID == a.ID || other.ID == b.ID {
			continue
		}
		if other.Host == vip.String() {
			return out, refuse(fmt.Sprintf(msgOtherHost, vip, name(other)))
		}
		if haEnabled(other) {
			h := other.OxiDNSHA
			if h.PeerID == a.ID || h.PeerID == b.ID {
				return out, refuse(fmt.Sprintf(msgOtherPeer, name(other)))
			}
			if h.VIP == vip.String() {
				return out, refuse(fmt.Sprintf(msgOtherVIP, vip))
			}
		}
	}
	out.intent = model.HAEnable{PeerID: b.ID, VIP: vip.String(), VRID: model.Int(vrid)}
	return out, nil
}
func text(n *yamlx.Node) string {
	n = yamlx.Resolve(n)
	if n != nil && n.Kind == yaml.ScalarNode && n.Tag == "!!str" {
		return n.Value
	}
	return ""
}
func elements(n *yamlx.Node) []*yamlx.Node {
	n = yamlx.Resolve(n)
	if n != nil && n.Kind == yaml.SequenceNode {
		return n.Content
	}
	return nil
}
func loopback(s string) bool {
	if s == "" {
		return false
	}
	if !strings.Contains(s, "://") {
		s = "udp://" + s
	}
	u, e := url.Parse(s)
	if e != nil {
		return false
	}
	h := strings.ToLower(u.Hostname())
	if h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return true
	}
	a, e := netip.ParseAddr(h)
	return e == nil && a.IsLoopback()
}
func (s *Service) upstreamFindings(ctx context.Context, a, b *agent) ([]string, []string) {
	blockers, warnings := []string{}, []string{}
	for _, m := range []*agent{a, b} {
		if m.DNSFollow != nil && m.DNSFollow.Connection == "auto" {
			blockers = append(blockers, fmt.Sprintf(msgFollowLoopback, name(m)))
		}
	}
	src, _, e := s.source(ctx, a, false)
	if e != nil {
		return blockers, append(warnings, msgDraftUnknown)
	}
	raw, e := src.Raw()
	if e != nil {
		return blockers, append(warnings, msgDraftUnknown)
	}
	p, e := oxidns.ProfileFromJSON(raw)
	if e != nil {
		return blockers, append(warnings, msgDraftUnknown)
	}
	base := ""
	if a.AppliedContent != nil {
		base = a.AppliedContent.BaseURL
	}
	settings, e := oxidns.PrepareEditor(p.Settings(), p, base)
	if e != nil {
		return blockers, append(warnings, msgDraftUnknown)
	}
	doc, e := oxidns.LoadNativeYAML(text(yamlx.Get(settings, "native_config")), "")
	if e != nil {
		return blockers, append(warnings, msgDraftUnknown)
	}
	loops := []string{}
	cleanup := false
	for _, plugin := range elements(yamlx.Get(doc, "plugins")) {
		args := yamlx.Get(plugin, "args")
		switch text(yamlx.Get(plugin, "type")) {
		case "forward":
			for _, entry := range elements(yamlx.Get(args, "upstreams")) {
				addr, dial := text(entry), ""
				if yamlx.Get(entry, "addr") != nil {
					addr = text(yamlx.Get(entry, "addr"))
					dial = text(yamlx.Get(entry, "dial_addr"))
				}
				if loopback(addr) || loopback(dial) {
					found := false
					for _, v := range loops {
						if v == addr {
							found = true
						}
					}
					if !found {
						loops = append(loops, s.safe(ctx, addr))
					}
				}
			}
		case "ros_address_list", "ros_route":
			v := yamlx.Resolve(yamlx.Get(args, "cleanup_on_shutdown"))
			if v != nil && v.Tag == "!!bool" && v.Value == "true" {
				cleanup = true
			}
		}
	}
	if len(loops) > 0 {
		blockers = append(blockers, fmt.Sprintf(msgLoopback, strings.Join(loops, "、")))
	}
	if cleanup {
		warnings = append(warnings, msgCleanup)
	}
	return blockers, warnings
}
func (s *Service) preview(ctx context.Context, a, b *agent, n normalized) error {
	blockers := []string{}
	networks := []netip.Prefix{}
	for i, m := range []*agent{a, b} {
		other, role := b, "primary"
		if i == 1 {
			other, role = a, "secondary"
		}
		o, e := s.call(ctx, m, "POST", "/preview", rawObject(spec(n.intent.VIP, int(n.intent.VRID), role, m, other)), 30)
		if e != nil {
			return e
		}
		items := []string{}
		for _, item := range o.strings("blockers") {
			if item != "" {
				items = append(items, s.safe(ctx, item))
			}
		}
		if !o.isTrue("ok") && len(items) == 0 {
			items = append(items, msgPreflight)
		}
		for _, item := range items {
			blockers = append(blockers, fmt.Sprintf(msgDeviceError, name(m), item))
		}
		p, e := netip.ParsePrefix(o.text("cidr"))
		if e == nil && p.Addr().Is4() {
			networks = append(networks, p.Masked())
		}
	}
	vip, _ := netip.ParseAddr(n.intent.VIP)
	if len(networks) == 2 && networks[0] != networks[1] {
		blockers = append(blockers, fmt.Sprintf(msgNetworks, networks[0], networks[1]))
	} else if len(networks) > 0 && !networks[0].Contains(vip) {
		blockers = append(blockers, fmt.Sprintf(msgVIPNetwork, vip, networks[0]))
	}
	if len(blockers) > 0 {
		return &Error{Message: fmt.Sprintf(msgCannotEnable, strings.Join(blockers, "；")), Blockers: blockers}
	}
	return nil
}
func requireListener(b *deploy.Bundle) error {
	files, e := b.Files(b.Manifest().ConfigPath)
	if e != nil {
		return e
	}
	doc, e := yamlx.Parse(files[b.Manifest().ConfigPath])
	if e != nil {
		return e
	}
	for _, plugin := range elements(yamlx.Get(doc, "plugins")) {
		if text(yamlx.Get(plugin, "type")) != "udp_server" {
			continue
		}
		listen := strings.TrimSpace(text(yamlx.Get(yamlx.Get(plugin, "args"), "listen")))
		i := strings.LastIndex(listen, ":")
		if i < 0 || listen[i+1:] != "53" {
			continue
		}
		h := listen[:i]
		if h == "" || h == "*" {
			return nil
		}
		a, e := netip.ParseAddr(strings.Trim(h, "[]"))
		if e == nil && a.IsUnspecified() {
			return nil
		}
	}
	return refuse(msgListener)
}
