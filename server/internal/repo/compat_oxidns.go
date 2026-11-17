// TODO(port): replace with package #11 oxidns editor, rule DNS
// (backend/common/oxidns_device_policy.py::normalize_client_range, backend/common/rule_dns.py::normalize_action,
// hydrate_ruleset) and
// package #38 clients (client_fixed_ip.validate_bindings now delegates to internal/routerdoc).
// These packages import repo, so replacements must live in a leaf package.
// Device notes now delegate to internal/devicenotes (C31).

package repo

import (
	"encoding/json"
	"fmt"
	"strings"

	"configflow/internal/clientrange"
	"configflow/internal/devicenotes"
	"configflow/internal/jsonx"
	"configflow/internal/routerdoc"
)

func oxidnsError(message string) error { return helperError(ClassOxidnsSettings, message) }

// ---- oxidns_device_policy.py ----

const clientRangeMessage = "请输入有效的客户端 IPv4、IPv6 或 CIDR 网段，例如 192.168.50.0/24。"

// normalizeClientRange is normalize_client_range: hosts and CIDRs in
// canonical text, IPv4-mapped peers as IPv4.
func normalizeClientRange(raw any) (string, error) {
	invalid := oxidnsError(clientRangeMessage)
	text, ok := raw.(string)
	if !ok {
		return "", invalid
	}
	value, ok := clientrange.Normalize(pyStrip(text))
	if !ok {
		return "", invalid
	}
	return value, nil
}

// normalizeNotes is the legacy storage boundary to the shared notes validator.
func normalizeNotes(value any) (*jsonx.Object, error) {
	raw, err := jsonx.Compact(value)
	if err != nil {
		return nil, oxidnsError(devicenotes.NotesMessage)
	}
	names, err := devicenotes.Parse(raw)
	if err != nil {
		return nil, oxidnsError(err.Error())
	}
	encoded, err := json.Marshal(names)
	if err != nil {
		return nil, fmt.Errorf("encode normalized device notes: %w", err)
	}
	value, err = jsonx.Parse(encoded)
	if err != nil {
		return nil, err
	}
	return value.(*jsonx.Object), nil
}

// ---- client_fixed_ip.py ----

// validateBindings is validate_bindings (internal/routerdoc holds the check).
func validateBindings(value any) error {
	document, err := jsonx.Compact(value)
	if err != nil {
		return oxidnsError(routerdoc.BindingsShapeMessage)
	}
	if message := routerdoc.ValidateBindings(document); message != "" {
		return oxidnsError(message)
	}
	return nil
}

// ---- rule_dns.py ----

var (
	ruleDNSActions     = []string{"refused", "nxdomain", "nodata", "blackhole", "upstream"}
	ruleDNSListRoutes  = []string{"direct", "proxy"}
	ruleDNSSources     = []string{"local", "remote", "fallback"}
	rulesetSourceField = []string{"name", "url", "behavior", "content", "source_type", "format"}

	// routeLabels is ROUTE_LABELS.
	routeLabels = map[string]string{"direct": "直连 DNS", "proxy": "代理 DNS", "refused": "拒绝", "nxdomain": "不存在",
		"nodata": "空应答", "blackhole": "黑洞", "upstream": "指定上游"}
)

func containsString(list []string, value any) bool {
	text, ok := value.(string)
	if !ok {
		return false
	}
	for _, item := range list {
		if item == text {
			return true
		}
	}
	return false
}

// hasExactKeys is set(obj) == {keys...}.
func hasExactKeys(obj *jsonx.Object, keys ...string) bool {
	if obj.Len() != len(keys) {
		return false
	}
	for _, key := range keys {
		if !obj.Has(key) {
			return false
		}
	}
	return true
}

// normalizeAction is normalize_action: the validated canonical copy of one
// rule_actions (or, with parked, rule_dns_parked) value.
func normalizeAction(value any, parked bool) (*jsonx.Object, error) {
	allowed := ruleDNSActions
	if parked {
		allowed = append(append([]string{}, ruleDNSListRoutes...), ruleDNSActions...)
	}
	object, ok := value.(*jsonx.Object)
	if !ok || object == nil || !containsString(allowed, object.Value("action")) {
		return nil, oxidnsError("规则 DNS 处理无效：" + strings.Join(allowed, "、"))
	}
	action := object.GetString("action")
	if action != "upstream" {
		if !hasExactKeys(object, "action") {
			return nil, oxidnsError("规则 DNS 处理「" + routeLabels[action] + "」不能包含其他参数")
		}
		return jsonx.Obj("action", action), nil
	}
	if !hasExactKeys(object, "action", "source", "upstream") || !containsString(ruleDNSSources, object.Value("source")) {
		return nil, oxidnsError("指定上游需要 source（local、remote 或 fallback）和 upstream")
	}
	upstream, ok := object.Value("upstream").(*jsonx.Object)
	if !ok || upstream == nil || !nonBlankString(upstream.Value("addr")) {
		return nil, oxidnsError("指定上游需要非空的 addr 地址")
	}
	return jsonx.Obj("action", "upstream", "source", object.Value("source"), "upstream", upstream.DeepCopy()), nil
}

// hydrateRuleset is hydrate_ruleset: it copies a shared rule-library source
// onto a profile ruleset reference in place.
func hydrateRuleset(item, libraryEntry *jsonx.Object, profileID string) error {
	for _, key := range rulesetSourceField {
		item.Delete(key)
	}
	item.Delete("library_enabled")
	for _, key := range rulesetSourceField {
		if value, ok := libraryEntry.Get(key); ok {
			item.Set(key, jsonx.DeepCopy(value))
		}
	}
	item.SetDefault("behavior", "domain")
	item.Set("library_enabled", jsonx.DeepCopy(libraryEntry.GetOr("enabled", true)))
	if jsonx.Equal(libraryEntry.Value("source_type"), "content") {
		id, ok := libraryEntry.Get("id")
		if !ok {
			return helperError(ClassKeyError, "'id'")
		}
		if profileID == "" {
			profileID = "default"
		}
		item.Set("url", "/api/profiles/"+profileID+"/rule-library/content/"+jsonx.Str(id))
	}
	return nil
}
