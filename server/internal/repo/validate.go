package repo

import (
	"errors"
	"math"
	"strings"

	"configflow/internal/jsonx"
)

// SchemaVersion is ProfileRepository.SCHEMA_VERSION.
const SchemaVersion = 5

// DefaultProfileID is ProfileRepository.DEFAULT_PROFILE_ID.
const DefaultProfileID = "default"

var systemFields = map[string]bool{"system_config": true, "backup": true, "agents": true, "_revision": true}

// isSchemaVersion is value == SCHEMA_VERSION (5 and 5.0, nothing else).
func isSchemaVersion(value any) bool {
	number, ok := value.(jsonx.Number)
	return ok && jsonx.Equal(number, jsonx.Int(SchemaVersion))
}

// revisionOf is int(section.get('_revision', 0)) for validated sections.
func revisionOf(section *jsonx.Object) int64 {
	switch value := section.Value("_revision").(type) {
	case bool:
		if value {
			return 1
		}
	case jsonx.Number:
		if number, ok := value.Int64(); ok {
			return number
		}
		return int64(value.Float64())
	}
	return 0
}

func bumpRevision(section *jsonx.Object) {
	section.Set("_revision", jsonx.Int(revisionOf(section)+1))
}

// enabledOr is obj.get(key, True) as a truth value.
func truthyOr(obj *jsonx.Object, key string, fallback bool) bool {
	value, ok := obj.Get(key)
	if !ok {
		return fallback
	}
	return jsonx.Truthy(value)
}

// itemsByID is _items_by_id: a list of objects with unique non-empty string ids.
func itemsByID(items any, label string) (*objectMap, error) {
	values, ok := jsonx.AsList(items)
	if !ok {
		return nil, validationError(label + " 必须是列表")
	}
	result := newObjectMap()
	for _, item := range values {
		object, _ := jsonx.AsObject(item)
		id, isString := object.Value("id").(string)
		if object == nil || !isString || id == "" {
			return nil, validationError(label + " 中每一项必须有 ID")
		}
		if _, duplicate := result.values[id]; duplicate {
			return nil, validationError(label + " 存在重复 ID：" + id)
		}
		result.set(id, object)
	}
	return result, nil
}

func (m *objectMap) has(key any) bool {
	text, ok := key.(string)
	if !ok {
		return false
	}
	_, found := m.values[text]
	return found
}

// idList is _id_list: a list of distinct strings.
func idList(values any, label string) ([]string, error) {
	items, ok := jsonx.AsList(values)
	if !ok {
		return nil, validationError(label + " 只能包含资源 ID，不能设置资源覆盖")
	}
	result := make([]string, 0, len(items))
	seen := map[string]bool{}
	for _, item := range items {
		text, ok := item.(string)
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

// orderedSet is a set that remembers insertion order, so the first error is
// deterministic where Python iterates a set.
type orderedSet struct {
	keys []string
	set  map[string]bool
}

func newOrderedSet() *orderedSet { return &orderedSet{set: map[string]bool{}} }

func (s *orderedSet) add(key string) {
	if !s.set[key] {
		s.set[key] = true
		s.keys = append(s.keys, key)
	}
}

func (s *orderedSet) has(key string) bool { return s != nil && s.set[key] }

// ResourceRoots names shared resources to resolve in addition to those a
// profile's composition selects: kind → ids, a nil slice meaning every
// resource of that kind (Python: {kind: None}).
type ResourceRoots map[string][]string

var sourceKinds = map[string]string{"node": "nodes", "subscription": "subscriptions", "aggregation": "subscription_aggregations"}

var groupResourceFields = [][2]string{{"subscriptions", "subscriptions"}, {"manual_nodes", "nodes"},
	{"aggregations", "subscription_aggregations"}}

func sourceKind(value any) string {
	text, _ := value.(string)
	return sourceKinds[text]
}

// pruneRuleDNS is _prune_rule_dns: drop per-rule OxiDNS DNS entries of rules
// the profile no longer has. Rule ids and list entries compare like Python
// set members (a non-string id such as 5 or None keeps an equal entry); an
// unhashable one is Python's TypeError.
func pruneRuleDNS(profile *jsonx.Object) error {
	oxidns, ok := jsonx.AsObject(profile.Value("oxidns"))
	rules, isList := jsonx.AsList(profile.Value("rule_configs"))
	if !ok || !isList {
		return nil
	}
	ids := map[string]bool{}
	for _, rule := range rules {
		if object, ok := jsonx.AsObject(rule); ok {
			key, hashable := pyHashKey(object.Value("id"))
			if !hashable {
				return unhashable(object.Value("id"))
			}
			ids[key] = true
		}
	}
	for _, key := range RuleDNSFields {
		switch value := oxidns.Value(key).(type) {
		case *jsonx.Object:
			kept := jsonx.NewObject()
			for identity, item := range value.All() {
				if ids["S"+identity] {
					kept.Set(identity, item)
				}
			}
			oxidns.Set(key, kept)
		case []any:
			kept := []any{}
			for _, identity := range value {
				hashKey, hashable := pyHashKey(identity)
				if !hashable {
					return unhashable(identity)
				}
				if ids[hashKey] {
					kept = append(kept, identity)
				}
			}
			oxidns.Set(key, kept)
		}
	}
	return nil
}

// collectResourceRefs is _collect_resource_refs: the resource closure of a
// profile's composition.
func collectResourceRefs(profile *jsonx.Object, catalogs map[string]*objectMap, roots ResourceRoots) (map[string]*orderedSet, error) {
	selected := map[string]*orderedSet{}
	for _, field := range ResourceFields {
		selected[field] = newOrderedSet()
	}
	groups, err := itemsByID(profile.GetOr("proxy_groups", []any{}), "策略组")
	if err != nil {
		return nil, err
	}
	selectResource := func(kind string, value any, allowBuiltin bool) error {
		text, ok := value.(string)
		if !ok || pyStrip(text) == "" {
			return validationError("资源引用必须是非空 ID")
		}
		if allowBuiltin && kind == "nodes" && isBuiltinPolicy(text) {
			return nil
		}
		if !catalogs[kind].has(text) {
			return inUse("引用的共享资源不存在：" + kind + "/" + text)
		}
		selected[kind].add(text)
		return nil
	}

	for _, id := range groups.keys {
		group := groups.values[id]
		if group.Value("type") == "chain" {
			chain, ok := jsonx.AsObject(group.Value("chain"))
			if !ok || !hasExactKeys(chain, "entry", "exit") {
				return nil, validationError("代理链必须指定前置和落地引用")
			}
			for _, field := range []string{"entry", "exit"} {
				reference, ok := jsonx.AsObject(chain.Value(field))
				kind, _ := reference.Value("type").(string)
				target, isString := reference.Value("id").(string)
				if !ok || !hasExactKeys(reference, "type", "id") || (kind != "node" && kind != "group") ||
					!isString || pyStrip(target) == "" {
					return nil, validationError("代理链引用必须是 node/group type 与非空 id")
				}
				if kind == "node" {
					if err := selectResource("nodes", target, false); err != nil {
						return nil, err
					}
				} else if !groups.has(target) {
					return nil, inUse("代理链引用的策略组不存在：" + target)
				}
			}
			continue
		}
		for _, pair := range groupResourceFields {
			values, err := idList(group.GetOr(pair[0], []any{}), pair[0])
			if err != nil {
				return nil, err
			}
			for _, value := range values {
				if err := selectResource(pair[1], value, pair[0] == "manual_nodes"); err != nil {
					return nil, err
				}
			}
		}
		if source := group.Value("source"); source != nil {
			if _, ok := source.(string); !ok {
				return nil, validationError("策略组 source 必须是字符串")
			}
		}
		if legacyKind := sourceKind(group.Value("source")); legacyKind != "" {
			values, err := idList(group.GetOr("proxies", []any{}), "proxies")
			if err != nil {
				return nil, err
			}
			for _, value := range values {
				if err := selectResource(legacyKind, value, legacyKind == "nodes"); err != nil {
					return nil, err
				}
			}
		}
		order, ok := jsonx.AsList(group.GetOr("proxies_order", []any{}))
		if !ok {
			return nil, validationError("策略组排序必须是数组")
		}
		for _, item := range order {
			object, ok := jsonx.AsObject(item)
			if !ok {
				return nil, validationError("策略组排序项必须是 type/id 对象")
			}
			if kind := sourceKind(object.Value("type")); kind != "" {
				if err := selectResource(kind, object.Value("id"), kind == "nodes"); err != nil {
					return nil, err
				}
			}
		}
	}

	for kind, ids := range roots {
		catalog, ok := catalogs[kind]
		if !ok || selected[kind] == nil {
			return nil, helperError(ClassKeyError, jsonx.Repr(kind))
		}
		if ids == nil {
			ids = catalog.keys
		}
		for _, value := range ids {
			if catalog.has(value) {
				if err := selectResource(kind, value, false); err != nil {
					return nil, err
				}
			}
		}
	}

	// Python iterates the set while select() only adds to other kinds.
	for _, aggregationID := range append([]string(nil), selected["subscription_aggregations"].keys...) {
		aggregation := catalogs["subscription_aggregations"].values[aggregationID]
		for _, kind := range []string{"nodes", "subscriptions"} {
			values, err := idList(aggregation.GetOr(kind, []any{}), "聚合 "+aggregationID+" 的 "+kind)
			if err != nil {
				return nil, err
			}
			for _, value := range values {
				if err := selectResource(kind, value, false); err != nil {
					return nil, err
				}
			}
		}
	}

	nodesByName := map[string][]string{}
	for _, id := range catalogs["nodes"].keys {
		if name, ok := catalogs["nodes"].values[id].Value("name").(string); ok {
			nodesByName[name] = append(nodesByName[name], id)
		}
	}
	groupsByName := map[string]int{}
	for _, id := range groups.keys {
		if name, ok := groups.values[id].Value("name").(string); ok {
			groupsByName[name]++
		}
	}
	pending := append([]string(nil), selected["nodes"].keys...)
	visited := map[string]bool{}
	for len(pending) > 0 {
		nodeID := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if visited[nodeID] {
			continue
		}
		visited[nodeID] = true
		node := catalogs["nodes"].values[nodeID]
		if subscriptionID := node.Value("subscription_id"); jsonx.Truthy(subscriptionID) {
			if err := selectResource("subscriptions", subscriptionID, false); err != nil {
				return nil, err
			}
		}
		if !truthyOr(node, "enabled", true) {
			continue
		}
		target, err := rawDialer(node)
		if err != nil {
			return nil, err
		}
		if target == nil {
			continue
		}
		name, ok := target.(string)
		if !ok || pyStrip(name) == "" {
			return nil, validationError("原始 dialer-proxy 必须是非空名称")
		}
		if isBuiltinPolicy(name) {
			continue
		}
		nodeTargets := nodesByName[name]
		if len(nodeTargets)+groupsByName[name] != 1 {
			return nil, inUse("原始拨号代理目标不存在或名称不唯一：" + name)
		}
		if len(nodeTargets) > 0 {
			if err := selectResource("nodes", nodeTargets[0], false); err != nil {
				return nil, err
			}
			pending = append(pending, nodeTargets[0])
		}
	}
	return selected, nil
}

func resourceCatalogs(shared *jsonx.Object, fields []string) (map[string]*objectMap, error) {
	catalogs := map[string]*objectMap{}
	for _, field := range fields {
		catalog, err := itemsByID(shared.Value(field), field)
		if err != nil {
			return nil, err
		}
		catalogs[field] = catalog
	}
	return catalogs, nil
}

// profileOf is ProfileRepository._profile.
func profileOf(document *jsonx.Object, profileID string) (*jsonx.Object, error) {
	if err := ValidateProfileID(profileID); err != nil {
		return nil, err
	}
	profile, ok := jsonx.AsObject(document.GetObject("profiles").Value(profileID))
	if !ok {
		return nil, notFound(profileID)
	}
	return profile, nil
}

// defaultProfileIDOf is _default_profile_id(document).
func defaultProfileIDOf(document *jsonx.Object) string {
	value := document.GetObject("system").GetObject("system_config").Value("default_profile_id")
	if jsonx.Truthy(value) {
		if text, ok := value.(string); ok {
			return text
		}
	}
	return DefaultProfileID
}

// resolveProfile is _resolve_profile: the profile with the system sections
// and the shared resources its composition selects ("compat" view).
func resolveProfile(document *jsonx.Object, profileID string, roots ResourceRoots, agents bool) (*jsonx.Object, error) {
	profile, err := profileOf(document, profileID)
	if err != nil {
		return nil, err
	}
	shared := document.GetObject("shared")
	system := document.GetObject("system")
	result := profile.DeepCopy()
	result.Set("profile_id", profileID)
	for _, key := range systemSections {
		if key == "agents" && !agents {
			continue
		}
		value, ok := system.Get(key)
		if !ok {
			return nil, helperError(ClassKeyError, jsonx.Repr(key))
		}
		result.Set(key, jsonx.DeepCopy(value))
	}
	catalogs, err := resourceCatalogs(shared, ResourceFields)
	if err != nil {
		return nil, err
	}
	selected, err := collectResourceRefs(profile, catalogs, roots)
	if err != nil {
		return nil, err
	}
	for _, field := range ResourceFields {
		items := []any{}
		for _, id := range catalogs[field].keys {
			if selected[field].has(id) {
				items = append(items, catalogs[field].values[id].DeepCopy())
			}
		}
		result.Set(field, items)
	}
	result.Set("rule_library", jsonx.DeepCopy(shared.Value("rule_library")))
	library := map[string]*jsonx.Object{}
	for _, item := range shared.GetList("rule_library") {
		object, err := compatObject(item)
		if err != nil {
			return nil, helperError(ClassTypeError, "rule_library item is not an object")
		}
		if id, ok := object.Value("id").(string); ok {
			library[id] = object
		}
	}
	rules, err := compatList(result.Value("rule_configs"))
	if err != nil {
		return nil, err
	}
	for _, item := range rules {
		rule, err := compatObject(item)
		if err != nil {
			return nil, err
		}
		if rule.Value("itemType") != "ruleset" {
			continue
		}
		libraryID, _ := rule.Value("library_rule_id").(string)
		entry, ok := library[libraryID]
		if !ok {
			return nil, helperError(ClassKeyError, jsonx.Repr(rule.Value("library_rule_id")))
		}
		if err := hydrateRuleset(rule, entry, profileID); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func profileUsage(profile *jsonx.Object) *jsonx.Object {
	return jsonx.Obj("kind", "profile", "id", profile.Value("id"), "name", profile.Value("name"),
		"profile_id", profile.Value("id"), "profile_name", profile.Value("name"))
}

// resourceUsage is _resource_usage: who references a shared resource.
func resourceUsage(document *jsonx.Object, kind, resourceID string) ([]any, error) {
	usages := []any{}
	shared := document.GetObject("shared")
	if kind == "subscriptions" || kind == "nodes" {
		for _, item := range shared.GetList("subscription_aggregations") {
			aggregation, _ := jsonx.AsObject(item)
			for _, value := range aggregation.GetList(kind) {
				if value == resourceID {
					usages = append(usages, jsonx.Obj("kind", "aggregation", "id", aggregation.Value("id"),
						"name", aggregation.GetOr("name", aggregation.Value("id"))))
					break
				}
			}
		}
	}
	if kind == "subscriptions" {
		for _, item := range shared.GetList("nodes") {
			node, _ := jsonx.AsObject(item)
			if node.Value("subscription_id") == resourceID {
				usages = append(usages, jsonx.Obj("kind", "node", "id", node.Value("id"),
					"name", node.GetOr("name", node.Value("id"))))
			}
		}
	}
	catalogs, err := resourceCatalogs(shared, ResourceFields)
	if err != nil {
		return nil, err
	}
	for _, value := range document.GetObject("profiles").All() {
		profile, _ := jsonx.AsObject(value)
		selected, err := collectResourceRefs(profile, catalogs, nil)
		if err != nil {
			return nil, err
		}
		referenced := selected[kind].has(resourceID)
		if kind == "rule_library" {
			referenced = false
			for _, item := range profile.GetList("rule_configs") {
				if rule, ok := jsonx.AsObject(item); ok && rule.Value("library_rule_id") == resourceID {
					referenced = true
					break
				}
			}
		}
		if referenced {
			usages = append(usages, profileUsage(profile))
		}
	}
	return usages, nil
}

// routerKey is (profile_id, router_id).
type routerKey struct{ ProfileID, RouterID string }

type routerRecords struct {
	keys   []routerKey
	values map[routerKey]*jsonx.Object
}

// routerRecordsOf is _router_records.
func routerRecordsOf(document *jsonx.Object) (*routerRecords, error) {
	records := &routerRecords{values: map[routerKey]*jsonx.Object{}}
	for profileID, value := range document.GetObject("profiles").All() {
		profile, _ := jsonx.AsObject(value)
		var routers *jsonx.Object
		if raw := profile.Value("routers"); jsonx.Truthy(raw) {
			object, ok := jsonx.AsObject(raw)
			if !ok {
				return nil, validationError("路由器设置必须是对象")
			}
			routers = object
		}
		items, err := itemsByID(routers.GetOr("items", []any{}), "路由器")
		if err != nil {
			return nil, err
		}
		for _, routerID := range items.keys {
			key := routerKey{profileID, routerID}
			records.keys = append(records.keys, key)
			records.values[key] = items.values[routerID]
		}
	}
	return records, nil
}

// asValidation converts the helper exceptions Python catches at a call site
// into ProfileValidationError(str(error)).
func asValidation(err error, classes ...string) error {
	if err != nil && isHelperClass(err, classes...) {
		return &Error{Kind: ErrValidation, Message: err.Error(), Cause: err}
	}
	return err
}

// ValidateDocument is ProfileRepository._validate_document, run on every
// commit and on every load that misses the cache.
func ValidateDocument(document *jsonx.Object) error {
	if !isSchemaVersion(document.Value("schema_version")) {
		return validationError(msgSchemaMismatch)
	}
	for _, key := range []string{"system", "shared", "profiles"} {
		if _, ok := jsonx.AsObject(document.Value(key)); !ok {
			return validationError(msgSectionMissing + key)
		}
	}
	system := document.GetObject("system")
	settings, ok := jsonx.AsObject(system.Value("system_config"))
	if !ok {
		return validationError("系统设置和备份设置必须是对象")
	}
	if defaultID := settings.Value("default_profile_id"); defaultID != nil {
		if _, ok := defaultID.(string); !ok {
			return validationError("默认配置空间 ID 必须是字符串")
		}
	}
	profiles := document.GetObject("profiles")
	if !profiles.Has(defaultProfileIDOf(document)) {
		return validationError(msgDefaultProfileRequired)
	}
	for key := range system.All() {
		if !systemFields[key] {
			return validationError("system 节只接受全局设置、备份与 Agent")
		}
	}
	if _, ok := jsonx.AsObject(system.Value("backup")); !ok {
		return validationError("系统设置和备份设置必须是对象")
	}
	if _, err := itemsByID(system.Value("agents"), "Agent"); err != nil {
		return err
	}
	shared := document.GetObject("shared")
	catalogs, err := resourceCatalogs(shared, SharedFields)
	if err != nil {
		return err
	}
	for _, id := range catalogs["subscriptions"].keys {
		if interval, ok := catalogs["subscriptions"].values[id].Get("interval"); ok {
			if err := validateRefreshInterval(interval); err != nil {
				return asValidation(err, ClassValueError, ClassDialerReference, ClassOxidnsSettings)
			}
		}
	}
	for _, kind := range []string{"subscriptions", "subscription_aggregations"} {
		for _, id := range catalogs[kind].keys {
			if health, ok := catalogs[kind].values[id].Get("health_check"); ok {
				if err := validateHealth(health, true); err != nil {
					return asValidation(err, ClassValueError, ClassDialerReference, ClassOxidnsSettings)
				}
			}
		}
	}
	if err := validateShapes(jsonx.Obj("nodes", shared.Value("nodes")), true); err != nil {
		return asValidation(err, ClassDialerReference)
	}
	for _, id := range catalogs["nodes"].keys {
		node := catalogs["nodes"].values[id]
		if node.Has("dialer_ref") {
			return validationError("共享节点不能保存拨号引用，请在策略组中创建代理链")
		}
		if subscriptionID := node.Value("subscription_id"); jsonx.Truthy(subscriptionID) && !catalogs["subscriptions"].has(subscriptionID) {
			return validationError("节点引用的订阅不存在")
		}
	}
	libraryNames := map[string]bool{}
	validNames := true
	for _, id := range catalogs["rule_library"].keys {
		resource := catalogs["rule_library"].values[id]
		if resource.Has("policy") || resource.Has("target") {
			return validationError("规则仓库不包含目标策略，请在规则配置中设置")
		}
	}
	for _, id := range catalogs["rule_library"].keys {
		name, ok := catalogs["rule_library"].values[id].Value("name").(string)
		if !ok || name == "" {
			validNames = false
		}
		libraryNames[name] = true
	}
	if !validNames || len(libraryNames) != len(catalogs["rule_library"].keys) {
		return validationError("共享规则仓库名称必须非空且唯一")
	}
	for _, id := range catalogs["subscription_aggregations"].keys {
		aggregation := catalogs["subscription_aggregations"].values[id]
		name := jsonx.Str(aggregation.Value("name"))
		for _, field := range []string{"subscriptions", "nodes"} {
			values, err := idList(aggregation.GetOr(field, []any{}), "聚合 "+name+" 的 "+field)
			if err != nil {
				return err
			}
			for _, value := range values {
				if !catalogs[field].has(value) && !(field == "nodes" && isBuiltinPolicy(value)) {
					return validationError("聚合 " + name + " 引用了不存在的资源：" + value)
				}
			}
		}
	}
	for profileID, value := range profiles.All() {
		if err := ValidateProfileID(profileID); err != nil {
			return err
		}
		profile, ok := jsonx.AsObject(value)
		if !ok || profile.Value("id") != profileID {
			return validationError(msgProfileIDMismatch)
		}
		if err := validateProfileInDocument(document, profileID, profile, catalogs); err != nil {
			var failure *Error
			if errors.As(err, &failure) && errors.Is(failure, ErrInUse) {
				usages := failure.Usages
				if len(usages) == 0 {
					usages = []any{jsonx.Obj("kind", "profile", "id", profileID, "name", profile.Value("name"),
						"profile_id", profileID, "profile_name", profile.Value("name"))}
				}
				return &Error{Kind: ErrInUse, Message: "配置 " + jsonx.Str(profile.Value("name")) + ": " + failure.Error(),
					Usages: usages, Cause: err}
			}
			return err
		}
	}
	agents, err := itemsByID(system.Value("agents"), "Agent")
	if err != nil {
		return err
	}
	for _, id := range agents.keys {
		agent := agents.values[id]
		if profileID, ok := agent.Value("profile_id").(string); !ok || !profiles.Has(profileID) {
			return validationError("Agent " + jsonx.Str(agent.GetOr("name", id)) + " 绑定的配置不存在")
		}
	}
	sections := []*jsonx.Object{system, shared}
	for _, value := range profiles.All() {
		sections = append(sections, value.(*jsonx.Object))
	}
	for _, section := range sections {
		if !validRevision(section.Value("_revision")) {
			return validationError(msgRevisionInvalid)
		}
	}
	return nil
}

// validRevision is the write rule for a section's _revision: a non-negative
// integer (R8: an integral JSON number in any notation, e.g. 60.0) or, as
// Python's isinstance(x, int) accepted, a boolean.
func validRevision(value any) bool {
	switch revision := value.(type) {
	case bool:
		return true
	case jsonx.Number:
		if strings.HasPrefix(revision.String(), "-") {
			return false
		}
		if revision.IsInt() {
			return true
		}
		number := revision.Float64()
		return number == math.Trunc(number) && !math.IsInf(number, 0)
	}
	return false
}

func validateProfileInDocument(document *jsonx.Object, profileID string, profile *jsonx.Object, catalogs map[string]*objectMap) error {
	if err := validateProfile(profile, catalogs); err != nil {
		return err
	}
	resolved, err := resolveProfile(document, profileID, nil, true)
	if err != nil {
		return err
	}
	if err := validateDialers(resolved); err != nil {
		if isHelperClass(err, ClassDialerReference) {
			return &Error{Kind: ErrInUse, Message: err.Error(), Usages: []any{}, Cause: err}
		}
		return err
	}
	return nil
}

// validateProfile is ProfileRepository._validate_profile.
func validateProfile(profile *jsonx.Object, catalogs map[string]*objectMap) error {
	for _, key := range SharedFields {
		if profile.Has(key) {
			return validationError("独立配置不能保存共享资源副本")
		}
	}
	if name, ok := profile.Value("name").(string); !ok || pyStrip(name) == "" {
		return validationError("配置名称不能为空")
	}
	if profile.Has("resource_refs") || profile.Has("node_dialers") {
		return validationError("独立配置不接受资源白名单或节点拨号覆盖字段")
	}
	if err := validateShapes(profile, true); err != nil {
		return asValidation(err, ClassDialerReference)
	}
	if _, err := collectResourceRefs(profile, catalogs, nil); err != nil {
		return err
	}
	groups, err := itemsByID(profile.Value("proxy_groups"), "策略组")
	if err != nil {
		return err
	}
	groupNames := map[string]bool{}
	for _, id := range groups.keys {
		name, ok := groups.values[id].Value("name").(string)
		if !ok || name == "" {
			return validationError("策略组名称必须非空")
		}
		groupNames[name] = true
	}
	if len(groupNames) != len(groups.keys) || groupNames["DIRECT"] || groupNames["REJECT"] {
		return validationError("策略组名称必须唯一，且不能使用 DIRECT 或 REJECT")
	}
	for _, id := range groups.keys {
		group := groups.values[id]
		if err := validateHealth(group, false); err != nil {
			return asValidation(err, ClassValueError, ClassDialerReference, ClassOxidnsSettings)
		}
		label := "策略组“" + jsonx.Str(group.Value("name")) + "”"
		for _, pair := range groupResourceFields {
			values, err := idList(group.GetOr(pair[0], []any{}), label+pair[0])
			if err != nil {
				return err
			}
			for _, value := range values {
				if !catalogs[pair[1]].has(value) && !(pair[1] == "nodes" && isBuiltinPolicy(value)) {
					return inUse(label + " 引用了不存在的资源：" + value)
				}
			}
		}
		if legacyKind := sourceKind(group.Value("source")); legacyKind != "" {
			values, err := idList(group.GetOr("proxies", []any{}), label+"proxies")
			if err != nil {
				return err
			}
			for _, value := range values {
				if !catalogs[legacyKind].has(value) && !(legacyKind == "nodes" && isBuiltinPolicy(value)) {
					return inUse(label + " 引用了不存在的资源：" + value)
				}
			}
		}
		targets, err := compatList(group.GetOr("include_groups", []any{}))
		if err != nil {
			return err
		}
		targets = append([]any(nil), targets...)
		followGroup := group.Value("follow_group")
		if jsonx.Truthy(followGroup) {
			targets = append(targets, followGroup)
		}
		for _, target := range targets {
			if !groups.has(target) || target == id {
				return inUse(label + " 引用了无效的策略组：" + jsonx.Str(target))
			}
		}
		if jsonx.Truthy(followGroup) && groups.values[followGroup.(string)].Value("type") == "chain" {
			return validationError("代理链请通过引用策略使用，不能作为跟随目标")
		}
		order, err := compatList(group.GetOr("proxies_order", []any{}))
		if err != nil {
			return err
		}
		for _, entry := range order {
			item, err := compatObject(entry)
			if err != nil {
				return err
			}
			kind, _ := item.Value("type").(string)
			value := item.Value("id")
			valid := false
			switch kind {
			case "node":
				valid = catalogs["nodes"].has(value) || isBuiltinPolicy(value)
			case "strategy":
				valid = groups.has(value) && value != id
			case "aggregation":
				valid = catalogs["subscription_aggregations"].has(value)
			case "subscription":
				valid = catalogs["subscriptions"].has(value)
			}
			if !valid {
				return inUse(label + " 的排序列表包含无效引用：" + jsonx.Str(value))
			}
		}
	}
	rules, err := itemsByID(profile.Value("rule_configs"), "规则配置")
	if err != nil {
		return err
	}
	for _, id := range rules.keys {
		rule := rules.values[id]
		itemType := rule.Value("itemType")
		if itemType != "rule" && itemType != "ruleset" {
			return validationError("规则类型必须为 rule 或 ruleset")
		}
		if itemType == "ruleset" && !catalogs["rule_library"].has(rule.Value("library_rule_id")) {
			return validationError("规则集必须引用全局规则仓库，不能单独设置来源")
		}
	}
	for _, engine := range []string{"mihomo", "surge", "mosdns"} {
		if _, ok := jsonx.AsObject(profile.Value(engine)); !ok {
			return validationError(engine + " 参数必须是对象")
		}
	}
	emptyObject := jsonx.NewObject()
	if _, ok := jsonx.AsObject(profile.GetOr("loon", emptyObject)); !ok {
		return validationError("loon 参数必须是对象")
	}
	if _, ok := jsonx.AsObject(profile.GetOr("domain_discovery", emptyObject)); !ok {
		return validationError("domain_discovery 参数必须是对象")
	}
	if _, err := normalizeNotes(profile.GetOr("device_notes", emptyObject)); err != nil {
		return asValidation(err, ClassOxidnsSettings)
	}
	if err := validateBindings(profile.GetOr("client_fixed_ips", emptyObject)); err != nil {
		return asValidation(err, ClassOxidnsSettings)
	}
	routers, ok := jsonx.AsObject(profile.GetOr("routers", emptyObject))
	if _, isList := jsonx.AsList(routers.GetOr("items", []any{})); !ok || !isList {
		return validationError("routers 参数必须是包含 items 列表的对象")
	}
	if _, ok := jsonx.AsObject(profile.GetOr("oxidns", emptyObject)); !ok {
		return validationError("oxidns 参数必须是对象")
	}
	for _, engine := range [][2]string{{"mosdns", "MosDNS"}, {"oxidns", "OxiDNS"}} {
		settings, _ := jsonx.AsObject(profile.GetOr(engine[0], emptyObject))
		for _, key := range []string{"direct_rulesets", "proxy_rulesets", "direct_rules", "proxy_rules"} {
			expected := "rule"
			if strings.HasSuffix(key, "rulesets") {
				expected = "ruleset"
			}
			values, err := idList(settings.GetOr(key, []any{}), key)
			if err != nil {
				return err
			}
			for _, value := range values {
				if !rules.has(value) || rules.values[value].Value("itemType") != expected {
					return inUse(engine[1] + " " + key + " 仍引用不存在的规则 " + value + "，请先取消选择")
				}
			}
		}
	}
	oxidns, _ := jsonx.AsObject(profile.GetOr("oxidns", emptyObject))
	for _, key := range RuleDNSFields {
		value := oxidns.GetOr(key, []any{})
		var identities []any
		object, isObject := jsonx.AsObject(value)
		if isObject {
			identities = jsonx.ListOf(object.Keys())
		} else if items, ok := jsonx.AsList(value); ok {
			identities = items
		} else {
			return validationError("OxiDNS " + key + " 必须以规则 ID 为键")
		}
		for _, identity := range identities {
			if _, ok := identity.(string); !ok {
				return validationError("OxiDNS " + key + " 必须以规则 ID 为键")
			}
		}
		for _, identity := range identities {
			if !rules.has(identity) {
				return inUse("OxiDNS DNS 处理仍引用不存在的规则 " + identity.(string) + "，请先取消")
			}
		}
		if key == "rule_dns_follow" || !isObject {
			continue
		}
		for identity, action := range object.All() {
			if action == nil {
				continue
			}
			if _, err := normalizeAction(action, key == "rule_dns_parked"); err != nil {
				if isHelperClass(err, ClassOxidnsSettings) {
					return &Error{Kind: ErrValidation, Cause: err,
						Message: "OxiDNS " + key + " 中规则 " + identity + " 的 DNS 处理无效：" + err.Error()}
				}
				return err
			}
		}
	}
	smartGroups, err := compatList(profile.GetObject("surge").GetOr("smart_groups", []any{}))
	if err != nil {
		return err
	}
	for _, entry := range smartGroups {
		setting, err := compatObject(entry)
		if err != nil {
			return err
		}
		if !groups.has(setting.Value("group_id")) {
			return inUse("Surge Smart 设置引用的策略组不存在")
		}
	}
	return nil
}
