package repo

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"configflow/internal/convert"
	"configflow/internal/devicenotes"
	"configflow/internal/health/check"
	"configflow/internal/model"
	"configflow/internal/routerdoc"
)

// Write validation (Python's _validate_document): the business rules every
// committed document satisfies. It runs on the typed document a transaction
// is about to commit (N8-5c); loading checks only structure (load.go).
//
// Typed fields read a member of the wrong JSON type as their zero value (R5),
// so the rules that are about a member's shape (a list, an object, a string
// id) look at the member as stored: memberView gives a record's members as
// JSON. Everything else reads the typed values.

// memberView is a record's members as stored JSON, in order.
type memberView struct{ fields model.Fields }

func viewOf(record any) (memberView, error) {
	fields, err := recordMembers(record)
	return memberView{fields}, err
}

func viewOfJSON(raw json.RawMessage) memberView {
	var fields model.Fields
	if jsonKind(raw) == '{' && fields.UnmarshalJSON(raw) != nil {
		return memberView{}
	}
	return memberView{fields}
}

func (v memberView) get(name string) (json.RawMessage, bool) { return v.fields.Get(name) }
func (v memberView) has(name string) bool                    { return v.fields.Has(name) }
func (v memberView) kind(name string) byte {
	raw, _ := v.fields.Get(name)
	return jsonKind(raw)
}

// jsonKind is the kind of a JSON value: '{', '[', '"', 'n' (null), 'b'
// (boolean) or '0' (number); 0 for an absent value.
func jsonKind(raw json.RawMessage) byte {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return 0
	}
	switch raw[0] {
	case '{', '[', '"', 'n':
		return raw[0]
	case 't', 'f':
		return 'b'
	}
	return '0'
}

func jsonString(raw json.RawMessage) (string, bool) {
	var text string
	if jsonKind(raw) != '"' || json.Unmarshal(raw, &text) != nil {
		return "", false
	}
	return text, true
}

// jsonTruthy evaluates a stored JSON value's truthiness with default false.
func jsonTruthy(raw json.RawMessage) bool {
	return model.Switch(raw, false)
}

// displayJSON is a stored scalar as message text: a string as itself,
// anything else as its JSON text.
func displayJSON(raw json.RawMessage) string {
	if text, ok := jsonString(raw); ok {
		return text
	}
	return string(bytes.TrimSpace(raw))
}

// items is a list of records by id: each item must be an object with a
// non-empty string id, ids unique (Python: _items_by_id).
type items struct {
	ids  []string
	byID map[string]memberView
}

func (c items) has(id string) bool { _, ok := c.byID[id]; return ok }

func itemsOf(raw json.RawMessage, label string) (items, error) {
	var list []json.RawMessage
	if jsonKind(raw) != '[' || json.Unmarshal(raw, &list) != nil {
		return items{}, validationError(label + " 必须是列表")
	}
	result := items{byID: map[string]memberView{}}
	for _, item := range list {
		if jsonKind(item) != '{' {
			return items{}, validationError(label + " 中每一项必须有 ID")
		}
		view := viewOfJSON(item)
		idRaw, _ := view.get("id")
		id, ok := jsonString(idRaw)
		if !ok || id == "" {
			return items{}, validationError(label + " 中每一项必须有 ID")
		}
		if result.has(id) {
			return items{}, validationError(label + " 存在重复 ID：" + id)
		}
		result.ids = append(result.ids, id)
		result.byID[id] = view
	}
	return result, nil
}

// idsOf is _id_list: a list of distinct string ids; absent is empty.
func idsOf(raw json.RawMessage, present bool, label string) ([]string, error) {
	if !present {
		return nil, nil
	}
	var list []json.RawMessage
	if jsonKind(raw) != '[' || json.Unmarshal(raw, &list) != nil {
		return nil, validationError(label + " 只能包含资源 ID，不能设置资源覆盖")
	}
	result := make([]string, 0, len(list))
	seen := map[string]bool{}
	for _, item := range list {
		text, ok := jsonString(item)
		if !ok {
			return nil, validationError(label + " 只能包含资源 ID，不能设置资源覆盖")
		}
		result = append(result, text)
		seen[text] = true
	}
	if len(seen) != len(result) {
		return nil, validationError(label + " 含有重复引用")
	}
	return result, nil
}

func memberIDs(view memberView, member, label string) ([]string, error) {
	raw, present := view.get(member)
	return idsOf(raw, present, label)
}

// revisionValid is the write rule for _revision: a non-negative integer (R8:
// an integral JSON number in any notation, e.g. 60.0) or, as Python's
// isinstance(x, int) accepted, a boolean.
func revisionValid(raw json.RawMessage, present bool) bool {
	raw = bytes.TrimSpace(raw)
	switch jsonKind(raw) {
	case 'b':
		return true
	case '0':
		var number json.Number
		if json.Unmarshal(raw, &number) != nil || strings.HasPrefix(number.String(), "-") {
			return false
		}
		if _, err := number.Int64(); err == nil {
			return true
		}
		if value, err := strconv.ParseFloat(number.String(), 64); err == nil {
			return value == float64(int64(value)) || !strings.ContainsAny(number.String(), ".eE")
		}
		return !strings.ContainsAny(number.String(), ".eE")
	}
	return false
}

// refreshIntervalValid is validate_refresh_interval: an integer (written as
// one) that is 0 or 60 … maxRefreshInterval.
func refreshIntervalValid(raw json.RawMessage) bool {
	text := string(bytes.TrimSpace(raw))
	if jsonKind(raw) != '0' || strings.ContainsAny(text, ".eE") {
		return false
	}
	value, err := strconv.ParseInt(text, 10, 64)
	return err == nil && (value == 0 || (60 <= value && value <= maxRefreshInterval))
}

const maxRefreshInterval = 604800

var msgRefreshInterval = "订阅源更新间隔必须为 0（关闭自动更新）或 60 ~ " + strconv.Itoa(maxRefreshInterval) + " 秒的整数"

// validateDocument is _validate_document on the typed document.
func validateDocument(d *model.Document) error {
	if d.SchemaVersion != SchemaVersion {
		return validationError(msgSchemaMismatch)
	}
	system, err := viewOf(&d.System)
	if err != nil {
		return err
	}
	if system.kind("system_config") != '{' {
		return validationError(msgSystemSectionsObject)
	}
	settings, err := viewOf(&d.System.SystemConfig)
	if err != nil {
		return err
	}
	if kind := settings.kind("default_profile_id"); kind != 0 && kind != 'n' && kind != '"' {
		return validationError(msgDefaultProfileIDText)
	}
	if profileByKey(d, defaultProfileIn(d)) == nil {
		return validationError(msgDefaultProfileRequired)
	}
	for _, key := range system.fields.Keys() {
		if !systemFields[key] {
			return validationError(msgSystemMembers)
		}
	}
	if system.kind("backup") != '{' {
		return validationError(msgSystemSectionsObject)
	}
	agentsRaw, _ := system.get("agents")
	agents, err := itemsOf(agentsRaw, "Agent")
	if err != nil {
		return err
	}
	sharedView, err := viewOf(&d.Shared)
	if err != nil {
		return err
	}
	catalogs := map[string]items{}
	for _, field := range SharedFields {
		raw, _ := sharedView.get(field)
		if catalogs[field], err = itemsOf(raw, field); err != nil {
			return err
		}
	}
	for _, id := range catalogs[kindSubscriptions].ids {
		if raw, ok := catalogs[kindSubscriptions].byID[id].get("interval"); ok && !refreshIntervalValid(raw) {
			return validationError(msgRefreshInterval)
		}
	}
	for _, kind := range []string{kindSubscriptions, kindAggregations} {
		for _, id := range catalogs[kind].ids {
			if raw, ok := catalogs[kind].byID[id].get("health_check"); ok {
				if err := checkHealth(raw, true); err != nil {
					return err
				}
			}
		}
	}
	nodesRaw, _ := sharedView.get(kindNodes)
	if err := checkNodeShapes(nodesRaw); err != nil {
		return err
	}
	for _, id := range catalogs[kindNodes].ids {
		node := catalogs[kindNodes].byID[id]
		if node.has("dialer_ref") {
			return validationError(msgNodeDialerRef)
		}
		if raw, _ := node.get("subscription_id"); jsonTruthy(raw) {
			if subscription, ok := jsonString(raw); !ok || !catalogs[kindSubscriptions].has(subscription) {
				return validationError(msgNodeSubscriptionMissing)
			}
		}
	}
	library := catalogs[kindRuleLibrary]
	for _, id := range library.ids {
		if library.byID[id].has("policy") || library.byID[id].has("target") {
			return validationError(msgLibraryPolicy)
		}
	}
	names := map[string]bool{}
	for _, id := range library.ids {
		raw, _ := library.byID[id].get("name")
		name, ok := jsonString(raw)
		if !ok || name == "" || names[name] {
			return validationError(msgLibraryNames)
		}
		names[name] = true
	}
	for _, id := range catalogs[kindAggregations].ids {
		aggregation := catalogs[kindAggregations].byID[id]
		nameRaw, _ := aggregation.get("name")
		name := displayJSON(nameRaw)
		for _, field := range []string{kindSubscriptions, kindNodes} {
			values, err := memberIDs(aggregation, field, "聚合 "+name+" 的 "+field)
			if err != nil {
				return err
			}
			for _, value := range values {
				if !catalogs[field].has(value) && !(field == kindNodes && builtinPolicy(value)) {
					return validationError("聚合 " + name + " 引用了不存在的资源：" + value)
				}
			}
		}
	}
	for _, profile := range d.Profiles {
		key := profile.Key()
		if err := ValidateProfileID(key); err != nil {
			return err
		}
		if profile.ID != key {
			return validationError(msgProfileIDMismatch)
		}
		if err := validateProfileIn(d, profile, catalogs); err != nil {
			var failure *Error
			if errors.As(err, &failure) && errors.Is(failure, ErrInUse) {
				usages := failure.Usages
				if len(usages) == 0 {
					usages = []any{profileUsageRecord(profile)}
				}
				return &Error{Kind: ErrInUse, Message: "配置 " + profile.Name + ": " + failure.Error(), Usages: usages, Cause: err}
			}
			return err
		}
	}
	for _, id := range agents.ids {
		agent := agents.byID[id]
		raw, _ := agent.get("profile_id")
		profileID, ok := jsonString(raw)
		if !ok || profileByKey(d, profileID) == nil {
			name := id
			if nameRaw, present := agent.get("name"); present {
				name = displayJSON(nameRaw)
			}
			return validationError("Agent " + name + " 绑定的配置不存在")
		}
	}
	revisions := []memberView{system, sharedView}
	for _, profile := range d.Profiles {
		view, err := viewOf(profile)
		if err != nil {
			return err
		}
		revisions = append(revisions, view)
	}
	for _, view := range revisions {
		if raw, present := view.get("_revision"); !revisionValid(raw, present) {
			return validationError(msgRevisionInvalid)
		}
	}
	return nil
}

// validateProfileIn is _validate_profile plus the dialer check of the
// resolved profile.
func validateProfileIn(d *model.Document, profile *model.Profile, catalogs map[string]items) error {
	if err := validateProfileSettings(d, profile, catalogs); err != nil {
		return err
	}
	// The resolved view only feeds the read-only dialer check and never
	// leaves this function, so it may borrow the document being validated.
	config, err := resolveConfig(d, profile.Key(), nil, agentCopyNone, true)
	if err != nil {
		return err
	}
	if err := checkDialers(config); err != nil {
		return err
	}
	return nil
}

var groupMemberFields = [][2]string{{"subscriptions", kindSubscriptions}, {"manual_nodes", kindNodes}, {"aggregations", kindAggregations}}

// validateProfileSettings is _validate_profile.
func validateProfileSettings(d *model.Document, profile *model.Profile, catalogs map[string]items) error {
	view, err := viewOf(profile)
	if err != nil {
		return err
	}
	for _, key := range SharedFields {
		if view.has(key) {
			return validationError(msgProfileSharedCopies)
		}
	}
	if raw, _ := view.get("name"); !nonBlank(raw) {
		return validationError(msgProfileNameEmpty)
	}
	if view.has("resource_refs") || view.has("node_dialers") {
		return validationError(msgProfileImportOverrides)
	}
	if err := checkProfileShapes(profile); err != nil {
		return err
	}
	groupsRaw, _ := view.get("proxy_groups")
	if !view.has("proxy_groups") {
		groupsRaw = json.RawMessage("[]")
	}
	if err := checkGroupReferenceShapes(groupsRaw); err != nil {
		return err
	}
	if _, err := selectResources(profile, catalogsOf(&d.Shared), nil); err != nil {
		return err
	}
	groupsRaw, _ = view.get("proxy_groups")
	groups, err := itemsOf(groupsRaw, labelProxyGroups)
	if err != nil {
		return err
	}
	groupNames := map[string]bool{}
	for _, id := range groups.ids {
		raw, _ := groups.byID[id].get("name")
		name, ok := jsonString(raw)
		if !ok || name == "" {
			return validationError(msgGroupNameEmpty)
		}
		groupNames[name] = true
	}
	if len(groupNames) != len(groups.ids) || groupNames["DIRECT"] || groupNames["REJECT"] {
		return validationError("策略组名称必须唯一，且不能使用 DIRECT 或 REJECT")
	}
	for _, id := range groups.ids {
		group := groups.byID[id]
		groupJSON, err := group.fields.MarshalJSON()
		if err != nil {
			return err
		}
		if err := checkHealth(groupJSON, false); err != nil {
			return err
		}
		nameRaw, _ := group.get("name")
		label := "策略组“" + displayJSON(nameRaw) + "”"
		for _, pair := range groupMemberFields {
			values, err := memberIDs(group, pair[0], label+pair[0])
			if err != nil {
				return err
			}
			for _, value := range values {
				if !catalogs[pair[1]].has(value) && !(pair[1] == kindNodes && builtinPolicy(value)) {
					return inUse(label + " 引用了不存在的资源：" + value)
				}
			}
		}
		sourceRaw, _ := group.get("source")
		if source, _ := jsonString(sourceRaw); orderSourceKinds[source] != "" {
			kind := orderSourceKinds[source]
			values, err := memberIDs(group, "proxies", label+"proxies")
			if err != nil {
				return err
			}
			for _, value := range values {
				if !catalogs[kind].has(value) && !(kind == kindNodes && builtinPolicy(value)) {
					return inUse(label + " 引用了不存在的资源：" + value)
				}
			}
		}
		var targets []json.RawMessage
		if raw, ok := group.get("include_groups"); ok {
			if json.Unmarshal(raw, &targets) != nil || jsonKind(raw) != '[' {
				return validationError(label + " 引用了无效的策略组：" + displayJSON(raw))
			}
		}
		followRaw, _ := group.get("follow_group")
		if jsonTruthy(followRaw) {
			targets = append(targets, followRaw)
		}
		for _, target := range targets {
			text, ok := jsonString(target)
			if !ok || !groups.has(text) || text == id {
				return inUse(label + " 引用了无效的策略组：" + displayJSON(target))
			}
		}
		if follow, ok := jsonString(followRaw); ok && jsonTruthy(followRaw) {
			if kindRaw, _ := groups.byID[follow].get("type"); displayJSON(kindRaw) == "chain" && jsonKind(kindRaw) == '"' {
				return validationError("代理链请通过引用策略使用，不能作为跟随目标")
			}
		}
		if raw, ok := group.get("proxies_order"); ok {
			var entries []json.RawMessage
			if jsonKind(raw) != '[' || json.Unmarshal(raw, &entries) != nil {
				return validationError(msgGroupOrderArray)
			}
			for _, entry := range entries {
				if jsonKind(entry) != '{' {
					return validationError(msgGroupOrderEntry)
				}
				item := viewOfJSON(entry)
				kindRaw, _ := item.get("type")
				kind, _ := jsonString(kindRaw)
				valueRaw, _ := item.get("id")
				value, isString := jsonString(valueRaw)
				valid := false
				if isString {
					switch kind {
					case "node":
						valid = catalogs[kindNodes].has(value) || builtinPolicy(value)
					case "strategy":
						valid = groups.has(value) && value != id
					case "aggregation":
						valid = catalogs[kindAggregations].has(value)
					case "subscription":
						valid = catalogs[kindSubscriptions].has(value)
					}
				}
				if !valid {
					return inUse(label + " 的排序列表包含无效引用：" + displayJSON(valueRaw))
				}
			}
		}
	}
	rulesRaw, _ := view.get("rule_configs")
	rules, err := itemsOf(rulesRaw, labelRuleConfigs)
	if err != nil {
		return err
	}
	for _, id := range rules.ids {
		rule := rules.byID[id]
		kindRaw, _ := rule.get("itemType")
		itemType, _ := jsonString(kindRaw)
		if itemType != "rule" && itemType != "ruleset" {
			return validationError(msgRuleItemType)
		}
		if itemType == "ruleset" {
			libraryRaw, _ := rule.get("library_rule_id")
			libraryID, ok := jsonString(libraryRaw)
			if !ok || !catalogs[kindRuleLibrary].has(libraryID) {
				return validationError("规则集必须引用全局规则仓库，不能单独设置来源")
			}
		}
	}
	for _, engine := range []string{"mihomo", "surge", "mosdns"} {
		if view.kind(engine) != '{' {
			return validationError(fmt.Sprintf(msgMemberObject, engine))
		}
	}
	for _, member := range []string{"loon", "domain_discovery"} {
		if view.has(member) && view.kind(member) != '{' {
			return validationError(fmt.Sprintf(msgMemberObject, member))
		}
	}
	notes, ok := view.get("device_notes")
	if !ok {
		notes = json.RawMessage("{}")
	}
	if _, err := devicenotes.Parse(notes); err != nil {
		return validationError(err.Error())
	}
	bindings, ok := view.get("client_fixed_ips")
	if !ok {
		bindings = json.RawMessage("{}")
	}
	if message := routerdoc.ValidateBindings(bindings); message != "" {
		return validationError(message)
	}
	if view.has("routers") {
		routers := viewOfJSON(mustGet(view, "routers"))
		if view.kind("routers") != '{' || (routers.has("items") && routers.kind("items") != '[') {
			return validationError(msgRoutersShape)
		}
	}
	if view.has("oxidns") && view.kind("oxidns") != '{' {
		return validationError(fmt.Sprintf(msgMemberObject, "oxidns"))
	}
	for _, engine := range [][2]string{{"mosdns", "MosDNS"}, {"oxidns", "OxiDNS"}} {
		settings := viewOfJSON(mustGet(view, engine[0]))
		for _, key := range []string{"direct_rulesets", "proxy_rulesets", "direct_rules", "proxy_rules"} {
			expected := "rule"
			if strings.HasSuffix(key, "rulesets") {
				expected = "ruleset"
			}
			values, err := memberIDs(settings, key, key)
			if err != nil {
				return err
			}
			for _, value := range values {
				kindRaw, _ := rules.byID[value].get("itemType")
				if itemType, _ := jsonString(kindRaw); !rules.has(value) || itemType != expected {
					return inUse(engine[1] + " " + key + " 仍引用不存在的规则 " + value + "，请先取消选择")
				}
			}
		}
	}
	oxidns := viewOfJSON(mustGet(view, "oxidns"))
	for _, key := range RuleDNSFields {
		raw, present := oxidns.get(key)
		if !present {
			continue
		}
		var identities []json.RawMessage
		actions := map[string]json.RawMessage{}
		var actionOrder []string
		switch jsonKind(raw) {
		case '{':
			members := viewOfJSON(raw)
			for name, value := range members.fields.All() {
				encoded, _ := json.Marshal(name)
				identities = append(identities, encoded)
				actions[name] = value
				actionOrder = append(actionOrder, name)
			}
		case '[':
			if json.Unmarshal(raw, &identities) != nil {
				return validationError("OxiDNS " + key + " 必须以规则 ID 为键")
			}
		default:
			return validationError("OxiDNS " + key + " 必须以规则 ID 为键")
		}
		for _, identity := range identities {
			if _, ok := jsonString(identity); !ok {
				return validationError("OxiDNS " + key + " 必须以规则 ID 为键")
			}
		}
		for _, identity := range identities {
			if id, _ := jsonString(identity); !rules.has(id) {
				return inUse("OxiDNS DNS 处理仍引用不存在的规则 " + id + "，请先取消")
			}
		}
		if key == "rule_dns_follow" {
			continue
		}
		for _, id := range actionOrder {
			action := actions[id]
			if jsonKind(action) == 'n' {
				continue
			}
			if message := checkRuleDNSAction(action, key == "rule_dns_parked"); message != "" {
				return validationError("OxiDNS " + key + " 中规则 " + id + " 的 DNS 处理无效：" + message)
			}
		}
	}
	surge := viewOfJSON(mustGet(view, "surge"))
	if raw, ok := surge.get("smart_groups"); ok {
		var entries []json.RawMessage
		if jsonKind(raw) == '{' && viewOfJSON(raw).fields.Len() == 0 {
			entries = nil // an empty object has no entries
		} else if jsonKind(raw) != '[' || json.Unmarshal(raw, &entries) != nil {
			return validationError("Surge Smart 设置必须是列表")
		}
		for _, entry := range entries {
			groupRaw, _ := viewOfJSON(entry).get("group_id")
			if id, ok := jsonString(groupRaw); jsonKind(entry) != '{' || !ok || !groups.has(id) {
				return inUse("Surge Smart 设置引用的策略组不存在")
			}
		}
	}
	return nil
}

func mustGet(view memberView, name string) json.RawMessage {
	raw, _ := view.get(name)
	return raw
}

func nonBlank(raw json.RawMessage) bool {
	text, ok := jsonString(raw)
	return ok && trimText(text) != ""
}

// checkGroupReferenceShapes is the shape part of _collect_resource_refs:
// chain references, member id lists, the legacy source and proxies_order,
// with Python's messages; selectResources then checks what they reference.
func checkGroupReferenceShapes(raw json.RawMessage) error {
	groups, err := itemsOf(raw, labelProxyGroups)
	if err != nil {
		return err
	}
	for _, id := range groups.ids {
		group := groups.byID[id]
		kindRaw, _ := group.get("type")
		if kind, _ := jsonString(kindRaw); kind == "chain" {
			chain := viewOfJSON(mustGet(group, "chain"))
			if group.kind("chain") != '{' || chain.fields.Len() != 2 || !chain.has("entry") || !chain.has("exit") {
				return validationError("代理链必须指定前置和落地引用")
			}
			for _, field := range []string{"entry", "exit"} {
				reference := viewOfJSON(mustGet(chain, field))
				typeRaw, _ := reference.get("type")
				target, _ := reference.get("id")
				referenceType, _ := jsonString(typeRaw)
				if chain.kind(field) != '{' || reference.fields.Len() != 2 || !reference.has("type") || !reference.has("id") ||
					(referenceType != "node" && referenceType != "group") || !nonBlank(target) {
					return validationError("代理链引用必须是 node/group type 与非空 id")
				}
			}
			continue
		}
		for _, pair := range groupMemberFields {
			values, err := memberIDs(group, pair[0], pair[0])
			if err != nil {
				return err
			}
			for _, value := range values {
				if trimText(value) == "" {
					return validationError("资源引用必须是非空 ID")
				}
			}
		}
		if group.has("source") && group.kind("source") != '"' && group.kind("source") != 'n' {
			return validationError("策略组 source 必须是字符串")
		}
		if source, _ := jsonString(mustGet(group, "source")); orderSourceKinds[source] != "" {
			if _, err := memberIDs(group, "proxies", "proxies"); err != nil {
				return err
			}
		}
		if raw, ok := group.get("proxies_order"); ok {
			var entries []json.RawMessage
			if jsonKind(raw) != '[' || json.Unmarshal(raw, &entries) != nil {
				return validationError(msgGroupOrderArray)
			}
			for _, entry := range entries {
				if jsonKind(entry) != '{' {
					return validationError(msgGroupOrderEntry)
				}
				item := viewOfJSON(entry)
				typeRaw, _ := item.get("type")
				if kind, _ := jsonString(typeRaw); orderSourceKinds[kind] != "" && !nonBlank(mustGet(item, "id")) {
					return validationError("资源引用必须是非空 ID")
				}
			}
		}
	}
	return nil
}

// checkRuleDNSAction is rule_dns.normalize_action's validation: "" when the
// action is valid, else the reason.
func checkRuleDNSAction(raw json.RawMessage, parked bool) string {
	allowed := ruleDNSActions
	if parked {
		allowed = append(append([]string{}, ruleDNSListRoutes...), ruleDNSActions...)
	}
	action := viewOfJSON(raw)
	name, _ := jsonString(mustGet(action, "action"))
	known := false
	for _, candidate := range allowed {
		known = known || candidate == name
	}
	if jsonKind(raw) != '{' || !known {
		return "规则 DNS 处理无效：" + strings.Join(allowed, "、")
	}
	if name != "upstream" {
		if action.fields.Len() != 1 {
			return "规则 DNS 处理「" + routeLabels[name] + "」不能包含其他参数"
		}
		return ""
	}
	source, _ := jsonString(mustGet(action, "source"))
	validSource := false
	for _, candidate := range ruleDNSSources {
		validSource = validSource || candidate == source
	}
	if action.fields.Len() != 3 || !action.has("source") || !action.has("upstream") || !validSource {
		return "指定上游需要 source（local、remote 或 fallback）和 upstream"
	}
	upstream := viewOfJSON(mustGet(action, "upstream"))
	if action.kind("upstream") != '{' || !nonBlank(mustGet(upstream, "addr")) {
		return "指定上游需要非空的 addr 地址"
	}
	return ""
}

var (
	ruleDNSActions    = []string{"refused", "nxdomain", "nodata", "blackhole", "upstream"}
	ruleDNSListRoutes = []string{"direct", "proxy"}
	ruleDNSSources    = []string{"local", "remote", "fallback"}
	routeLabels       = map[string]string{"direct": "直连 DNS", "proxy": "代理 DNS", "refused": "拒绝", "nxdomain": "不存在",
		"nodata": "空应答", "blackhole": "黑洞", "upstream": "指定上游"}
)

// trimText trims leading and trailing white space and control characters
// (including the information separators U+001C–U+001F) from names and ids.
func trimText(text string) string {
	return strings.TrimFunc(text, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) })
}

func validateResourceArray(raw json.RawMessage, name string) error {
	var values []json.RawMessage
	if jsonKind(raw) != '[' || json.Unmarshal(raw, &values) != nil {
		return validationError(fmt.Sprintf(msgResourceArray, name))
	}
	for _, raw := range values {
		if jsonKind(raw) != '{' {
			return validationError(msgResourceObject)
		}
	}
	return nil
}

// convertFailure maps the converter's input errors to validation errors.
func convertFailure(err error) error {
	var reference *convert.DialerReferenceError
	var yamlFailure *convert.ProxyYAMLError
	switch {
	case errors.As(err, &reference):
		return &Error{Kind: ErrValidation, Message: reference.Message, Cause: err}
	case errors.As(err, &yamlFailure):
		return &Error{Kind: ErrValidation, Message: err.Error(), Cause: err}
	}
	return err
}

// checkHealth is health_check.validate_health of a stored value: a
// ValueError is a validation error.
func checkHealth(raw json.RawMessage, provider bool) error {
	err := check.ValidateHealthJSON(raw, provider)
	var invalid *check.ValueError
	if errors.As(err, &invalid) {
		return validationError(invalid.Message)
	}
	return err
}

// checkNodeShapes is dialer_references.validate_shapes of the shared nodes.
func checkNodeShapes(raw json.RawMessage) error {
	if err := validateResourceArray(raw, "nodes"); err != nil {
		return err
	}
	var shared model.Shared
	if err := shared.SetJSON("nodes", raw); err != nil {
		return err
	}
	return dialerValidation(convert.DialerFailure(convert.ConfigShapes(&model.Config{Nodes: shared.Nodes}, true)))
}

// checkProfileShapes is dialer_references.validate_shapes of a profile.
func checkProfileShapes(profile *model.Profile) error {
	if raw, present, err := recordMemberJSON(profile, "proxy_groups"); err != nil {
		return err
	} else if present {
		if err := validateResourceArray(raw, "proxy_groups"); err != nil {
			return err
		}
	}
	return dialerValidation(convert.DialerFailure(convert.ConfigShapes(&model.Config{Profile: profile}, true)))
}

// checkDialers is dialer_references.validate_dialers of a resolved profile:
// a broken reference is ErrInUse.
func checkDialers(config *model.Config) error {
	err := convert.ConfigDialers(config)
	if err == nil {
		return nil
	}
	var yamlFailure *convert.ProxyYAMLError
	if errors.As(err, &yamlFailure) {
		return err
	}
	return &Error{Kind: ErrInUse, Message: err.Error(), Usages: []any{}, Cause: err}
}

func dialerValidation(err error) error {
	var reference *convert.DialerReferenceError
	if errors.As(err, &reference) {
		return &Error{Kind: ErrValidation, Message: reference.Message, Cause: err}
	}
	return err
}
