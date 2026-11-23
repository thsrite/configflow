package convert

import (
	"encoding/json"
	"strings"

	"configflow/internal/model"
	"configflow/internal/yamlx"
)

// ClientNodeReplayIssue classifies stored-type inputs outside the normal Python
// byte contract. It uses the production model/options decoders, not a second
// parser. The immutable 2195-config byte gate still executes every source row.
func ClientNodeReplayIssue(value any, target string) string {
	node, err := DecodeJ[model.Node](value)
	object, ok := value.(*JObj)
	if !ok || object == nil {
		return "node object"
	}
	if err != nil {
		return "node record"
	}
	for _, field := range []string{"name", "type", "server", "port", "params"} {
		if raw, present := object.Get(field); present && raw == nil {
			return "node." + field + " null absent"
		}
	}
	if !object.Has("name") {
		return "node.name absent"
	}
	params, err := node.Params.Node()
	if err != nil {
		return "node.params schema"
	}
	if node.ProxyString != nil {
		parse := CurrentMihomoEnvironment().Clients.ParseNodes
		if parse == nil {
			return ""
		}
		nodes, err := parse(*node.ProxyString)
		if err != nil || len(nodes) == 0 {
			return ""
		}
		node, params = nodes[0].Node, nodes[0].Params
	}
	return clientOptionsReplayIssue(node.Type, params, target)
}

func clientOptionsReplayIssue(kind string, params *yamlx.Node, target string) string {
	options, err := decodeClientOptions(params)
	if err != nil {
		return "node.params schema"
	}
	for key, value := range yamlx.Pairs(params) {
		if nullNode(value) {
			return "options." + key + " null absent"
		}
	}
	for _, field := range []string{"sni", "uuid"} {
		if value := yamlx.Get(params, field); value != nil && (value.ShortTag() == "!!int" || value.ShortTag() == "!!float") && value.Value == "0" {
			return "numeric zero optional text"
		}
	}
	kind = strings.ToLower(kind)
	if target == "surge" && (kind == "tuic" || kind == "tuic-v5") {
		if n := yamlx.Get(params, "alpn"); n != nil && n.ShortTag() == "!!seq" && len(n.Content) > 0 {
			return "Surge TUIC ALPN list bug"
		}
	}
	if target == "surge" && (kind == "wg" || kind == "wireguard") {
		if n := yamlx.Get(params, "reserved"); n != nil && n.ShortTag() == "!!seq" && len(n.Content) > 0 {
			return "Surge WireGuard numeric reserved bug"
		}
	}
	if target == "loon" && kind == "hysteria2" && options.Down != nil {
		text := string(*options.Down)
		if text != "" && text[0] >= 128 {
			return "native non-ASCII down"
		}
	}
	return ""
}

// ClientGroupReplayIssue identifies malformed policy records and the approved
// missing-health fallback. Ordinary empty/missing optional fields stay normal.
func ClientGroupReplayIssue(value any, target string) string {
	group, err := DecodeJ[model.ProxyGroup](value)
	if err != nil {
		return "group record"
	}
	object, ok := value.(*JObj)
	if !ok || object == nil {
		return "group object"
	}
	if !object.Has("name") {
		return "group.name absent"
	}
	for _, field := range []string{"name", "type", "follow_group"} {
		if raw, present := object.Get(field); present && raw == nil {
			return "group." + field + " null absent"
		}
	}
	if target == "surge" {
		for _, field := range []string{"url", "interval"} {
			if raw, present := object.Get(field); present && raw == nil {
				return "group." + field + " null default"
			}
		}
		if stringValue(group.FollowGroup) != "" {
			return "group.follow health defaults"
		}
	}
	return ""
}

// ClientProfileReplayIssue audits only fields owned by this client migration.
func ClientProfileReplayIssue(config *JObj, target string) string {
	// The profile is stored data: lenient decoding reports the members it
	// read as zero values (O18 T1a; the plain record keeps no issues).
	data, err := json.Marshal(config)
	if err != nil {
		return "profile record"
	}
	var profile model.Profile
	issues, err := model.DecodeStored(data, &profile, "")
	if err != nil {
		return "profile record"
	}
	// A stored null settings object reads as empty settings without an issue
	// (Python failed on it).
	if value, present := config.Get(target); present && value == nil {
		return "profile." + target + " R5"
	}
	for _, key := range []string{"rules", "rule_sets"} {
		if raw, present := profile.LegacyRuleList(key); present {
			var items []model.RuleConfig
			if json.Unmarshal(raw, &items) != nil {
				return "profile." + key + " array"
			}
		}
	}
	for _, node := range config.GetList("nodes") {
		if issue := ClientNodeReplayIssue(node, target); issue != "" {
			return issue
		}
	}
	for _, group := range config.GetList("proxy_groups") {
		if issue := ClientGroupReplayIssue(group, target); issue != "" {
			return issue
		}
	}
	for _, issue := range issues {
		if issue.Kind != model.IssueUnknown && (model.IssueUnder([]model.FieldIssue{issue}, "rule_configs") || model.IssueUnder([]model.FieldIssue{issue}, "proxy_groups") || model.IssueUnder([]model.FieldIssue{issue}, target)) {
			return "profile." + issue.Path + " R5"
		}
	}
	return ""
}
