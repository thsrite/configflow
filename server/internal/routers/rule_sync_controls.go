package routers

import (
	"configflow/internal/jsondecode"
	"configflow/internal/routers/device"
	"context"
	"encoding/json"
	"fmt"

	"configflow/internal/model"
)

func syncProjection(raw json.RawMessage, keys ...string) map[string]json.RawMessage {
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(raw, &fields)
	result := map[string]json.RawMessage{}
	for _, key := range keys {
		value := fields[key]
		if len(value) == 0 {
			value = json.RawMessage("null")
		}
		result[key] = value
	}
	return result
}

// SyncControls returns a Go-native signature of settings requiring a manual release.
// Credentials affect the signature, but only the SHA-256 digest is ever persisted.
func (s *RuleSyncService) SyncControls(ctx context.Context, config json.RawMessage, r *Router) (string, error) {
	sources, err := selectedSyncSources(config, r)
	if err != nil {
		return "", err
	}
	raw, err := releaseRouterRecord(r)
	if err != nil {
		return "", err
	}
	router := syncProjection(raw, "id", "type", "endpoint", "username", "password", "verify_tls", "proxy_gateway_agent_id", "dns_agent_id", "manage_return_routing", "ip_rule_sets", "ros_policy_rule_sets")
	if r.DNSGuard != nil && r.DNSGuard.Enabled {
		router["dns_guard"] = syncProjection(raw, "dns_guard")["dns_guard"]
	}
	if r.IPv6PDGuard != nil && r.IPv6PDGuard.Enabled {
		router["ipv6_pd_guard"] = syncProjection(raw, "ipv6_pd_guard")["ipv6_pd_guard"]
	}
	agents := []map[string]json.RawMessage{}
	var dns *GatewayAgent
	for _, id := range []string{r.ProxyGatewayAgentID, r.DNSAgentID} {
		var a *GatewayAgent
		if s.Agents != nil && id != "" {
			a, err = s.Agents.Agent(ctx, id)
			if err != nil {
				return "", err
			}
		}
		if id == r.DNSAgentID {
			dns = a
		}
		agents = append(agents, syncProjection(device.JSON(a), "id", "host", "service_type", "profile_id"))
	}
	selected := map[string]map[string]map[string]json.RawMessage{}
	for _, source := range sources {
		selected[source.Item.ID] = map[string]map[string]json.RawMessage{"item": syncProjection(source.Item.Raw, "id", "library_rule_id", "policy", "enabled", "behavior"), "library": syncProjection(source.Library.Raw, "id", "name", "source_type", "url", "behavior", "enabled")}
	}
	var doc struct {
		Mihomo struct {
			CustomConfig string `json:"custom_config"`
		} `json:"mihomo"`
		Oxidns json.RawMessage `json:"oxidns"`
		Groups json.RawMessage `json:"proxy_groups"`
	}
	if err = jsondecode.Unmarshal(config, &doc); err != nil {
		return "", err
	}
	fake, err := FakeIPNetwork(doc.Mihomo.CustomConfig)
	if err != nil {
		return "", err
	}
	value := map[string]json.RawMessage{"router": device.JSON(router), "agents": device.JSON(agents), "sources": device.JSON(selected), "fake_ip": device.JSON(fake.String()), "oxidns": doc.Oxidns, "proxy_groups": doc.Groups}
	for key, v := range value {
		if len(v) == 0 {
			value[key] = json.RawMessage("null")
		}
	}
	var profile model.Profile
	if err = json.Unmarshal(config, &profile); err != nil {
		return "", err
	}
	if r.Type == "routeros" && IPv6FakeIPRelevant(&profile, r) {
		value["ipv6_fakeip"] = device.JSON(struct {
			Settings IPv6Settings    `json:"settings"`
			State    json.RawMessage `json:"state"`
		}{IPv6FakeIPSettings(&profile), syncNull(r.State("ipv6_fakeip_state"))})
	}
	if dns != nil {
		var view struct {
			HA struct {
				Enabled bool            `json:"enabled"`
				GroupID json.RawMessage `json:"group_id"`
				VIP     json.RawMessage `json:"vip"`
				PeerID  string          `json:"peer_id"`
			} `json:"oxidns_ha"`
		}
		_ = jsondecode.Unmarshal(device.JSON(dns), &view)
		if view.HA.Enabled {
			var peer *GatewayAgent
			if s.Agents != nil {
				peer, err = s.Agents.Agent(ctx, view.HA.PeerID)
				if err != nil {
					return "", err
				}
			}
			value["dns_ha"] = device.JSON(struct {
				GroupID json.RawMessage            `json:"group_id"`
				VIP     json.RawMessage            `json:"vip"`
				Peer    map[string]json.RawMessage `json:"peer"`
			}{syncNull(view.HA.GroupID), syncNull(view.HA.VIP), syncProjection(device.JSON(peer), "id", "host")})
		}
	}
	encoded, err := canonicalReleaseJSON(value)
	if err != nil {
		return "", fmt.Errorf("encode sync controls: %w", err)
	}
	return syncVersion(string(encoded)), nil
}
func syncNull(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage("null")
	}
	return raw
}
