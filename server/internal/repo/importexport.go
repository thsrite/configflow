package repo

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"configflow/internal/convert"
	"configflow/internal/ctxx"
	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/rules"
)

var sensitiveSystemSettings = map[string]bool{"config_token": true, "rule_proxy_token": true, "retired_rule_proxy_tokens": true}

// ExportAll is export_all(desensitize): a copy of the whole document. With
// desensitize the tokens, backup settings, Agents, subscription URLs, node
// credentials, custom configurations and restore points are removed and the
// result goes through the registered redaction hooks; without registered
// hooks it fails instead of exporting secrets.
func (r *Repository) ExportAll(ctx context.Context, desensitize bool) (result *jsonx.Object, err error) {
	if desensitize {
		err = r.WithReadSnapshot(WithRepository(ctx, r), func(held context.Context) error {
			result, err = r.exportAll(held, true)
			return err
		})
		return result, err
	}
	return r.exportAll(ctx, false)
}

func (r *Repository) exportAll(ctx context.Context, desensitize bool) (result *jsonx.Object, err error) {
	err = r.view(ctx, func(document *jsonx.Object) error {
		result = document.DeepCopy()
		return nil
	})
	if err != nil || !desensitize {
		return result, err
	}
	h := currentHooks()
	if h.SanitizeExternalPayload == nil || h.RedactOxidnsSettings == nil || h.RedactRouters == nil || h.RedactEasytierSettings == nil {
		return nil, repositoryError("脱敏导出尚未接入（脱敏 hooks 未注册），已拒绝导出")
	}
	system := result.GetObject("system")
	systemConfig := system.GetObject("system_config").DeepCopy()
	settings := jsonx.NewObject()
	for key, value := range systemConfig.All() {
		if !sensitiveSystemSettings[key] {
			settings.Set(key, value)
		}
	}
	system.Set("system_config", settings)
	system.Set("backup", jsonx.NewObject())
	system.Set("agents", []any{})
	shared := result.GetObject("shared")
	for _, item := range shared.GetList("subscriptions") {
		item.(*jsonx.Object).Set("url", "[REDACTED]")
	}
	for _, item := range shared.GetList("nodes") {
		node := item.(*jsonx.Object)
		for _, key := range []string{"server", "password", "uuid", "private-key", "proxy_string"} {
			if node.Has(key) {
				node.Set(key, "[REDACTED]")
			}
		}
		node.Set("params", jsonx.NewObject())
	}
	for _, value := range result.GetObject("profiles").All() {
		profile := value.(*jsonx.Object)
		if oxidns, ok := profile.Get("oxidns"); ok {
			profile.Set("oxidns", h.RedactOxidnsSettings(oxidns))
		}
		if routers, ok := profile.Get("routers"); ok {
			profile.Set("routers", h.RedactRouters(routers))
		}
		for _, engine := range []string{"mihomo", "surge", "mosdns"} {
			profile.GetObject(engine).Set("custom_config", "")
		}
		// Recovery snapshots contain the original opaque YAML and DNS
		// credentials. Human-shareable exports must omit them entirely.
		mihomo := profile.GetObject("mihomo")
		mihomo.Delete("linux_gateway_restore_point")
		if easytier, ok := mihomo.Get("easytier"); ok {
			mihomo.Set("easytier", h.RedactEasytierSettings(easytier))
		}
		if loon, ok := jsonx.AsObject(profile.Value("loon")); ok {
			loon.Set("custom_config", "")
		}
	}
	return h.SanitizeExternalPayload(ctx, result, systemConfig)
}

// prepareImport is _prepare_import: the imported data as a validated typed
// document with its rule proxy token maintained.
func (r *Repository) prepareImport(data *jsonx.Object) (*model.Document, error) {
	converted, err := r.convertImport(data, nil)
	if err != nil {
		return nil, err
	}
	document, err := modelFromLegacy(converted, nil)
	if err != nil {
		return nil, err
	}
	if _, err := ensureSystemTokens(&document.System); err != nil {
		return nil, err
	}
	if err := validateDocument(document); err != nil {
		return nil, err
	}
	return document, nil
}

// checkMigrationPath is _check_migration_path: path is inside the data
// directory and no component below it is a symbolic link.
func (r *Repository) checkMigrationPath(path string) error {
	for part := path; ; part = filepath.Dir(part) {
		if part == r.dataDir {
			return nil
		}
		if info, err := os.Lstat(part); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return validationError("旧配置路径不能是符号链接")
		}
		if part == filepath.Dir(part) {
			return validationError("旧配置路径不在数据目录内")
		}
	}
}

// createMigrationSnapshotDir is _create_migration_snapshot_dir:
// migrations/<UTC timestamp>-<uuid hex>.
func (r *Repository) createMigrationSnapshotDir() (string, error) {
	if err := os.MkdirAll(r.migrationsDir, 0o777); err != nil {
		return "", err
	}
	timestamp := time.Now().UTC().Format("20060102T150405Z")
	for range 16 {
		candidate := filepath.Join(r.migrationsDir, timestamp+"-"+randomHex(32))
		if err := os.Mkdir(candidate, 0o777); err == nil {
			return candidate, nil
		} else if !os.IsExist(err) {
			return "", err
		}
	}
	return "", repositoryError("Unable to allocate a unique migration snapshot directory")
}

// snapshot is _snapshot(reason) for the reasons this port still has
// ("import"): copy the database aside into migrations/<ts>-<uuid>/<reason>/.
// Callers hold .config.lock and no SQLite transaction.
func (r *Repository) snapshot(reason string) (string, error) {
	if err := r.checkMigrationPath(r.migrationsDir); err != nil {
		return "", err
	}
	parent, err := r.createMigrationSnapshotDir()
	if err != nil {
		return "", err
	}
	destination := filepath.Join(parent, reason)
	if err := os.Mkdir(destination, 0o777); err != nil {
		return "", err
	}
	if info, err := os.Stat(r.dbPath); err == nil && info.Mode().IsRegular() {
		if err := r.store.BackupTo(filepath.Join(destination, DatabaseFilename)); err != nil {
			return "", err
		}
	}
	var directories []string
	err = filepath.WalkDir(destination, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != destination {
				directories = append(directories, path)
			}
			return nil
		}
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
			return nil
		}
		handle, err := os.Open(path)
		if err != nil {
			return err
		}
		defer handle.Close()
		return handle.Sync()
	})
	if err != nil {
		return "", err
	}
	sort.SliceStable(directories, func(i, j int) bool {
		return strings.Count(directories[i], "/") > strings.Count(directories[j], "/")
	})
	for _, directory := range append(directories, destination, parent, r.migrationsDir, r.dataDir) {
		if err := fsyncDir(directory); err != nil {
			return "", err
		}
	}
	return destination, nil
}

// applyImport is _apply_import: replace the open transaction's document with
// an imported one. The database is copied aside first; the previous rule
// proxy tokens are retired and every revision advances.
func (r *Repository) applyImport(tx *transaction, document *model.Document, reason string) (string, error) {
	snapshot, err := r.snapshot(reason)
	if err != nil {
		return "", err
	}
	previous := tx.document
	settings := &document.System.SystemConfig
	oldView, err := viewOf(&previous.System.SystemConfig)
	if err != nil {
		return "", err
	}
	tokens := []string{previous.System.SystemConfig.RuleProxyToken}
	var oldRetired []json.RawMessage
	if raw, _ := oldView.get("retired_rule_proxy_tokens"); jsonKind(raw) == '[' {
		_ = json.Unmarshal(raw, &oldRetired)
	}
	for _, item := range oldRetired {
		if token, ok := jsonString(item); ok {
			tokens = append(tokens, token)
		}
	}
	for _, token := range tokens {
		if token == "" || token == settings.RuleProxyToken || slices.Contains(settings.RetiredRuleProxyTokens, token) {
			continue
		}
		settings.RetiredRuleProxyTokens = append(slices.Clone(settings.RetiredRuleProxyTokens), token)
	}
	if settings.RetiredRuleProxyTokens == nil {
		settings.RetiredRuleProxyTokens = []string{}
	}
	document.System.Revision = previous.System.Revision + 1
	document.Shared.Revision = previous.Shared.Revision + 1
	for _, profile := range document.Profiles {
		profile.Revision = 1
		if old := profileByKey(previous, profile.Key()); old != nil {
			profile.Revision = old.Revision + 1
		}
	}
	tx.document = document
	return snapshot, nil
}

// ImportAll is import_all(data): replace the whole configuration with a
// current-format export, an original single-profile config or a schema-2
// multi-profile backup. The database is first copied to
// migrations/<ts>-<uuid>/import/. It returns ExportAll.
func (r *Repository) ImportAll(ctx context.Context, data *jsonx.Object) (*jsonx.Object, error) {
	ctx = ctxx.Detach(ctx)
	document, err := r.prepareImport(data)
	if err != nil {
		return nil, err
	}
	batch := false
	err = r.transact(ctx, writeOptions{batch: &batch}, func(ctx context.Context, tx *transaction) error {
		if err := beginCommitBatch(ctx, &batch); err != nil {
			return err
		}
		_, err := r.applyImport(tx, document, "import")
		return err
	})
	if err != nil {
		return nil, err
	}
	return r.ExportAll(ctx, false)
}

// ResetAll is reset_all(): ImportAll of the initial configuration.
func (r *Repository) ResetAll(ctx context.Context) (*jsonx.Object, error) {
	ctx = ctxx.Detach(ctx)
	return r.ImportAll(ctx, r.initialConfig())
}

// objectsOf is a list of objects of an old-format document.
func objectsOf(value any) ([]*jsonx.Object, error) {
	items, ok := jsonx.AsList(value)
	if !ok {
		return nil, validationError(msgLegacyShape)
	}
	result := make([]*jsonx.Object, len(items))
	for index, item := range items {
		object, ok := jsonx.AsObject(item)
		if !ok {
			return nil, validationError(msgLegacyShape)
		}
		result[index] = object
	}
	return result, nil
}

// valueKey identifies a stored value by its JSON text (object members
// sorted), so equal values of any type match.
func valueKey(value any) string {
	text, err := jsonx.Encode(value, jsonx.Options{SortKeys: true})
	if err != nil {
		return ""
	}
	return string(text)
}

func sortedJSON(value any) string {
	text, err := jsonx.Encode(value, jsonx.Options{SortKeys: true})
	if err != nil {
		return jsonx.Repr(value)
	}
	return string(text)
}

// scopeLegacyDialers is _scope_legacy_dialers: bind raw dialers locally
// before the independent node catalogs of a schema-2 backup are merged.
func scopeLegacyDialers(source *jsonx.Object) (*jsonx.Object, error) {
	profiles := source.DeepCopy()
	type edge struct {
		node, destination *jsonx.Object
		kind, target      string
	}
	definitions := map[string]map[string]bool{}
	groupNames := map[string]bool{}
	usedNames := map[string]bool{"DIRECT": true, "REJECT": true}
	targets := map[string]bool{}
	var edges []edge
	for profileID, value := range profiles.All() {
		profile, ok := jsonx.AsObject(value)
		if !ok {
			return nil, validationError("旧配置内容必须是对象")
		}
		if err := dialerValidation(convert.ValidateShapes(profile, false)); err != nil {
			return nil, err
		}
		nodes, err := objectsOf(profile.GetOr("nodes", []any{}))
		if err != nil {
			return nil, err
		}
		groups, err := objectsOf(profile.GetOr("proxy_groups", []any{}))
		if err != nil {
			return nil, err
		}
		subscriptions, err := itemsByID(profile.GetOr("subscriptions", []any{}), "旧配置订阅")
		if err != nil {
			return nil, err
		}
		for _, node := range nodes {
			name := node.GetString("name")
			subscriptionID := node.Value("subscription_id")
			identity := sortedJSON(node) + "\x00"
			if jsonx.Truthy(subscriptionID) {
				if !subscriptions.has(subscriptionID) {
					return nil, validationError("旧配置 " + profileID + " 的节点引用的订阅不存在")
				}
				identity += sortedJSON(subscriptions.values[subscriptionID.(string)])
			}
			if definitions[name] == nil {
				definitions[name] = map[string]bool{}
			}
			definitions[name][identity] = true
			usedNames[name] = true
		}
		for _, group := range groups {
			groupNames[group.GetString("name")] = true
			usedNames[group.GetString("name")] = true
		}
		for _, node := range nodes {
			if !truthyOr(node, "enabled", true) {
				continue
			}
			raw, err := legacyRawDialer(node)
			if err != nil {
				return nil, err
			}
			if raw == nil {
				continue
			}
			target, ok := raw.(string)
			if !ok || trimText(target) == "" {
				return nil, validationError("原始 dialer-proxy 必须是非空名称")
			}
			if isBuiltinPolicy(target) {
				continue
			}
			var matches []edge
			for _, item := range nodes {
				if item.GetString("name") == target && truthyOr(item, "enabled", true) {
					matches = append(matches, edge{node, item, "node", target})
				}
			}
			for _, item := range groups {
				if item.GetString("name") == target && truthyOr(item, "enabled", true) {
					matches = append(matches, edge{node, item, "group", target})
				}
			}
			if len(matches) != 1 {
				return nil, validationError("旧配置 " + profileID + " 的原始拨号目标不存在或名称不唯一：" + target)
			}
			targets[target] = true
			edges = append(edges, matches[0])
		}
	}

	scoped := map[string]bool{}
	for name := range targets {
		if definitions[name] != nil && (len(definitions[name]) > 1 || groupNames[name]) {
			scoped[name] = true
		}
	}
	// A shared-looking B -> A also becomes profile-specific when A differs.
	// Propagate this through multi-hop raw chains before assigning any names.
	for grew := true; grew; {
		grew = false
		for _, item := range edges {
			name := item.node.GetString("name")
			if item.kind == "node" && scoped[item.target] && targets[name] && !scoped[name] {
				scoped[name] = true
				grew = true
			}
		}
	}
	for profileID, value := range profiles.All() {
		profile := value.(*jsonx.Object)
		renamed := map[string]string{}
		nodes, _ := objectsOf(profile.GetOr("nodes", []any{}))
		for _, node := range nodes {
			oldName := node.GetString("name")
			if !scoped[oldName] {
				continue
			}
			candidate := oldName + " (" + profileID + ")"
			for index := 2; usedNames[candidate]; index++ {
				candidate = oldName + " (" + profileID + "-" + strconv.Itoa(index) + ")"
			}
			usedNames[candidate] = true
			node.Set("name", candidate)
			if truthyOr(node, "enabled", true) {
				renamed[oldName] = candidate
			}
		}
		localGroups := map[string]bool{}
		groups, _ := objectsOf(profile.GetOr("proxy_groups", []any{}))
		for _, group := range groups {
			localGroups[group.GetString("name")] = true
		}
		rules, err := objectsOf(profile.GetOr("rule_configs", []any{}))
		if err != nil {
			return nil, err
		}
		for _, rule := range rules {
			if policy, ok := rule.Value("policy").(string); ok && renamed[policy] != "" && !localGroups[policy] {
				rule.Set("policy", renamed[policy])
			}
		}
	}
	for _, item := range edges {
		name := item.destination.GetString("name")
		if item.kind != "node" || name == item.target {
			continue
		}
		if proxyString := item.node.Value("proxy_string"); jsonx.Truthy(proxyString) {
			parsed, err := convert.InspectStructuredProxyString(proxyString.(string))
			if err != nil {
				return nil, convertFailure(err)
			}
			if parsed == nil {
				return nil, validationError(msgLegacyDialerProxyString)
			}
			parsed.Set("dialer-proxy", name)
			text, err := jsonx.Encode(parsed, jsonx.Options{})
			if err != nil {
				return nil, err
			}
			item.node.Set("proxy_string", string(text))
		} else {
			item.node.GetObject("params").Set("dialer-proxy", name)
		}
	}
	return profiles, nil
}

// sanitizeRuleNameOf is rule_utils.sanitize_rule_name for any value: a falsy
// value is "unnamed", a truthy non-string is Python's TypeError from re.sub.
func sanitizeRuleNameOf(name any) (string, error) {
	if text, ok := name.(string); ok {
		return rules.SanitizeRuleName(text), nil
	}
	if !jsonx.Truthy(name) {
		return "unnamed", nil
	}
	return "", validationError(msgLegacyRuleName)
}

// validateLegacyIndex is _validate_legacy_index (the system index of a
// schema-2 backup).
func validateLegacyIndex(system *jsonx.Object) (*objectMap, error) {
	if !jsonx.Equal(system.Value("schema_version"), jsonx.Int(2)) || system.Value("schema_version") == true {
		return nil, validationError("不支持的旧配置索引版本；请保留原数据，不要重置")
	}
	profiles, err := itemsByID(system.Value("profiles"), "旧配置索引")
	if err != nil {
		return nil, err
	}
	if !profiles.has("default") {
		return nil, validationError("旧配置索引缺少默认配置")
	}
	for _, profileID := range profiles.keys {
		if err := ValidateProfileID(profileID); err != nil {
			return nil, err
		}
	}
	return profiles, nil
}

func cutRunes(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit])
}

// ResourceRemaps records, per profile of an imported old-format document,
// old resource id → new shared id for each of SharedFields.
type ResourceRemaps map[string]map[string]map[string]string

// convertImport is _convert_import: turn a current-format document, an
// original single-profile config or a schema-2 multi-profile backup into a
// validated-shape current document. source is not modified.
func (r *Repository) convertImport(source *jsonx.Object, resourceRemaps ResourceRemaps) (*jsonx.Object, error) {
	if source == nil {
		return nil, validationError("导入内容必须是 JSON 对象")
	}
	version := source.Value("schema_version")
	if isSchemaVersion(version) {
		document := source.DeepCopy()
		if err := validateLegacyDocument(document); err != nil {
			return nil, err
		}
		return document, nil
	}
	var oldSystem, oldProfiles *jsonx.Object
	metadata := map[string]*jsonx.Object{}
	isVersion2 := false
	if number, ok := version.(jsonx.Number); ok && jsonx.Equal(number, jsonx.Int(2)) {
		isVersion2 = true
		system, isSystem := jsonx.AsObject(source.Value("system"))
		profiles, isProfiles := jsonx.AsObject(source.Value("profiles"))
		if !isSystem || !isProfiles {
			return nil, validationError("旧多配置备份必须包含 system 索引和全部 profiles 配置内容")
		}
		index, err := validateLegacyIndex(system)
		if err != nil {
			return nil, err
		}
		if len(index.keys) != profiles.Len() {
			return nil, validationError("旧多配置备份的索引与配置内容不一致")
		}
		for _, profileID := range index.keys {
			if !profiles.Has(profileID) {
				return nil, validationError("旧多配置备份的索引与配置内容不一致")
			}
			metadata[profileID] = index.values[profileID]
		}
		oldSystem = system
		if oldProfiles, err = scopeLegacyDialers(profiles); err != nil {
			return nil, err
		}
	} else if version == nil && !source.Has("profiles") && !source.Has("shared") && !source.Has("resource_refs") && !source.Has("node_dialers") {
		oldSystem = source
		oldProfiles = jsonx.Obj("default", source)
		metadata["default"] = jsonx.NewObject()
	} else {
		return nil, validationError("仅支持原版单配置、schema 2 完整多配置备份或当前格式；未修改原数据")
	}

	system := jsonx.NewObject()
	for _, key := range systemSections {
		var fallback any = jsonx.NewObject()
		if key == "agents" {
			fallback = []any{}
		}
		system.Set(key, jsonx.DeepCopy(oldSystem.GetOr(key, fallback)))
	}
	shared := jsonx.NewObject()
	for _, key := range SharedFields {
		shared.Set(key, []any{})
	}
	profiles := jsonx.NewObject()
	document := jsonx.Obj("schema_version", jsonx.Int(SchemaVersion), "system", system, "shared", shared, "profiles", profiles)
	system.Set("_revision", oldSystem.GetOr("_revision", jsonx.Int(0)))
	shared.Set("_revision", jsonx.Int(0))
	settings, ok := jsonx.AsObject(system.Value("system_config"))
	if !ok {
		return nil, validationError("系统设置必须是对象")
	}
	settings.SetDefault("server_domain", "")
	settings.SetDefault("github_proxy_domain", "")
	settings.SetDefault("rule_fetch_proxy", "")
	if _, ok := settings.Value("github_proxy_domain").(string); !ok {
		settings.Set("github_proxy_domain", "")
	}

	catalogs := map[string]map[string]*jsonx.Object{}
	identities := map[string]map[string]string{}
	names := map[string]map[string]bool{}
	for _, kind := range SharedFields {
		catalogs[kind] = map[string]*jsonx.Object{}
		identities[kind] = map[string]string{}
		names[kind] = map[string]bool{}
	}
	names["subscription_aggregations"] = names["subscriptions"]
	// Python keeps the names in sets: sanitized names for the rule library,
	// the names themselves (any hashable value) for the other kinds.
	nameKey := func(kind string, name any) (string, error) {
		if kind == "rule_library" {
			sanitized, err := sanitizeRuleNameOf(name)
			return "S" + sanitized, err
		}
		key := valueKey(name)
		return key, nil
	}
	mergeResource := func(kind string, resource *jsonx.Object, profileID string) (string, error) {
		oldID := resource.GetString("id")
		identity := oldID + "\x00" + sortedJSON(resource)
		if id, ok := identities[kind][identity]; ok {
			return id, nil
		}
		if _, taken := catalogs[kind][oldID]; taken {
			for {
				resource.Set("id", cutRunes(oldID, 40)+"_"+randomHex(12))
				if _, taken := catalogs[kind][resource.GetString("id")]; !taken {
					break
				}
			}
		}
		// Providers and rule libraries are emitted as name-keyed maps.
		// Nodes keep their names: raw dialer-proxy values refer to them.
		name := resource.Value("name")
		key, err := nameKey(kind, name)
		if err != nil {
			return "", err
		}
		if text, ok := name.(string); ok && kind != "nodes" && text != "" && names[kind][key] {
			suffix := profileID
			for index := 1; names[kind][key]; {
				limit := max(1, 196-len([]rune(suffix)))
				resource.Set("name", cutRunes(text, limit)+" ("+suffix+")")
				key, _ = nameKey(kind, resource.Value("name"))
				index++
				suffix = profileID + "-" + strconv.Itoa(index)
			}
		}
		id := resource.GetString("id")
		catalogs[kind][id] = resource
		identities[kind][identity] = id
		names[kind][key] = true
		shared.Set(kind, append(shared.GetList(kind), resource))
		return id, nil
	}

	for profileID, value := range oldProfiles.All() {
		if err := ValidateProfileID(profileID); err != nil {
			return nil, err
		}
		rawProfile, ok := jsonx.AsObject(value)
		if !ok {
			return nil, validationError("旧配置内容必须是对象")
		}
		for _, key := range []string{"resource_refs", "node_dialers", "schema_version", "shared", "profiles"} {
			if rawProfile.Has(key) {
				return nil, validationError("旧配置包含不支持的中间格式字段；未修改原数据")
			}
		}
		old := DeepMerge(r.legacyDefaults(), rawProfile)
		old.Update(metadata[profileID].DeepCopy())
		remaps := map[string]map[string]string{}
		for _, kind := range SharedFields {
			remaps[kind] = map[string]string{}
		}
		remapReference := func(kind string, value any) (string, error) {
			text, isString := value.(string)
			if kind == "nodes" && isString && isBuiltinPolicy(text) {
				return text, nil
			}
			id, ok := remaps[kind][text]
			if !isString || !ok {
				return "", validationError("旧配置 " + profileID + " 引用的本地资源不存在：" + kind + "/" + jsonx.Str(value))
			}
			return id, nil
		}
		remapList := func(kind string, values any, label string) ([]any, error) {
			ids, err := idList(values, label)
			if err != nil {
				return nil, err
			}
			result := make([]any, len(ids))
			for index, id := range ids {
				if result[index], err = remapReference(kind, id); err != nil {
					return nil, err
				}
			}
			return result, nil
		}

		for _, kind := range SharedFields {
			items, isList := jsonx.AsList(old.Value(kind))
			if !isList {
				return nil, validationError(kind + " 必须是资源对象数组")
			}
			for _, item := range items {
				if _, ok := jsonx.AsObject(item); !ok {
					return nil, validationError(kind + " 必须是资源对象数组")
				}
			}
			for _, item := range items {
				item.(*jsonx.Object).SetDefault("id", kind+"_"+randomHex(12))
			}
			if _, err := itemsByID(items, kind); err != nil {
				return nil, err
			}
			for _, item := range items {
				raw := item.(*jsonx.Object)
				resource := raw.DeepCopy()
				oldID := resource.GetString("id")
				if subscriptionID := resource.Value("subscription_id"); kind == "nodes" && jsonx.Truthy(subscriptionID) {
					id, err := remapReference("subscriptions", subscriptionID)
					if err != nil {
						return nil, err
					}
					resource.Set("subscription_id", id)
				}
				if kind == "subscription_aggregations" {
					for _, field := range []string{"subscriptions", "nodes"} {
						if values, ok := resource.Get(field); ok {
							remapped, err := remapList(field, values, field)
							if err != nil {
								return nil, err
							}
							resource.Set(field, remapped)
						}
					}
				}
				id, err := mergeResource(kind, resource, profileID)
				if err != nil {
					return nil, err
				}
				remaps[kind][oldID] = id
			}
		}
		if resourceRemaps != nil {
			resourceRemaps[profileID] = remaps
		}
		defaultName := profileID
		if profileID == "default" {
			defaultName = "默认配置"
		}
		profile := r.emptyProfile(profileID, old.GetOr("name", defaultName), old.GetOr("description", ""))
		for _, key := range append(append([]string{}, ProfileFields...), "created_at", "updated_at", "_revision") {
			if value, ok := old.Get(key); ok {
				profile.Set(key, jsonx.DeepCopy(value))
			}
		}
		groups, err := objectsOf(profile.Value("proxy_groups"))
		if err != nil {
			return nil, err
		}
		groupIDs := map[string]bool{}
		for _, group := range groups {
			for _, pair := range groupResourceFields {
				if values, ok := group.Get(pair[0]); ok {
					remapped, err := remapList(pair[1], values, pair[0])
					if err != nil {
						return nil, err
					}
					group.Set(pair[0], remapped)
				}
			}
			if legacyKind := sourceKind(group.Value("source")); legacyKind != "" && group.Has("proxies") {
				remapped, err := remapList(legacyKind, group.Value("proxies"), "proxies")
				if err != nil {
					return nil, err
				}
				group.Set("proxies", remapped)
			}
		}
		for _, group := range groups {
			key := valueKey(group.Value("id"))
			groupIDs[key] = true
		}
		// proxies_order is only an ordering hint. Old versions left entries
		// behind when the referenced group/resource was deleted, and the
		// converters skipped them; drop them instead of failing the upgrade.
		for _, group := range groups {
			entries, isList := jsonx.AsList(group.Value("proxies_order"))
			if !isList {
				continue
			}
			order := []any{}
			for _, entry := range entries {
				item, ok := jsonx.AsObject(entry)
				if !ok {
					continue
				}
				value, isString := item.Value("id").(string)
				idKey := valueKey(item.Value("id"))
				if item.Value("type") == "strategy" {
					if !groupIDs[idKey] || jsonx.Equal(item.Value("id"), group.Value("id")) {
						continue
					}
				} else {
					kind := sourceKind(item.Value("type"))
					if kind == "" {
						continue
					}
					if !(kind == "nodes" && isString && isBuiltinPolicy(value)) {
						id, ok := remaps[kind][value]
						if !isString || !ok {
							continue
						}
						item.Set("id", id)
					}
				}
				order = append(order, item)
			}
			group.Set("proxies_order", order)
		}
		if !jsonx.Truthy(profile.Value("rule_configs")) {
			ruleConfigs := []any{}
			for _, pair := range [][2]string{{"rules", "rule"}, {"rule_sets", "ruleset"}} {
				items, err := objectsOf(old.GetOr(pair[0], []any{}))
				if err != nil {
					return nil, err
				}
				for _, item := range items {
					rule := item.Clone()
					rule.Set("itemType", pair[1])
					ruleConfigs = append(ruleConfigs, rule)
				}
			}
			profile.Set("rule_configs", ruleConfigs)
		}
		// rule_names is a dict keyed by the old names (any hashable value).
		ruleNames := map[string]any{}
		for _, item := range old.GetList("rule_library") {
			entry := item.(*jsonx.Object)
			if !entry.Has("name") {
				return nil, validationError(msgLegacyRuleName)
			}
			key := valueKey(entry.Value("name"))
			ruleNames[key] = catalogs["rule_library"][remaps["rule_library"][entry.GetString("id")]].Value("name")
		}
		rules, err := objectsOf(profile.Value("rule_configs"))
		if err != nil {
			return nil, err
		}
		for _, rule := range rules {
			rule.SetDefault("id", "rule_"+randomHex(12))
			if rule.Value("itemType") != "ruleset" {
				continue
			}
			oldLibraryID := rule.Value("library_rule_id")
			libraryID := ""
			if jsonx.Truthy(oldLibraryID) {
				if libraryID, err = remapReference("rule_library", oldLibraryID); err != nil {
					return nil, err
				}
				// Old rules could customize their emitted name/behavior even
				// when the content came from a library. Preserve that variant.
				var original *jsonx.Object
				for _, item := range old.GetList("rule_library") {
					if entry := item.(*jsonx.Object); entry.Value("id") == oldLibraryID {
						original = entry
						break
					}
				}
				overrides := jsonx.NewObject()
				for _, key := range []string{"name", "behavior", "format"} {
					if value, ok := rule.Get(key); ok && !jsonx.Equal(value, original.Value(key)) {
						overrides.Set(key, value)
					}
				}
				if overrides.Len() > 0 {
					resource := original.DeepCopy()
					resource.Update(overrides)
					resource.Set("id", "migrated_rule_"+jsonx.Str(rule.Value("id")))
					if libraryID, err = mergeResource("rule_library", resource, profileID); err != nil {
						return nil, err
					}
				}
			} else {
				resource := jsonx.NewObject()
				for _, key := range RuleSourceFields {
					if value, ok := rule.Get(key); ok {
						resource.Set(key, jsonx.DeepCopy(value))
					}
				}
				resource.Set("id", "migrated_rule_"+jsonx.Str(rule.Value("id")))
				resource.Set("enabled", true)
				name := rule.Value("name")
				if !jsonx.Truthy(name) {
					name = resource.Value("id")
				}
				resource.SetDefault("name", name)
				sourceType := "url"
				if jsonx.Truthy(resource.Value("content")) {
					sourceType = "content"
				}
				resource.SetDefault("source_type", sourceType)
				if libraryID, err = mergeResource("rule_library", resource, profileID); err != nil {
					return nil, err
				}
			}
			if name := rule.Value("name"); jsonx.Truthy(name) {
				key := valueKey(name)
				ruleNames[key] = catalogs["rule_library"][libraryID].Value("name")
			}
			rule.Set("library_rule_id", libraryID)
			for _, key := range RuleSourceFields {
				rule.Delete(key)
			}
		}
		itemTypes := map[string]any{}
		for _, rule := range rules {
			if rule.Value("itemType") == "rule" && rule.Value("rule_type") == "RULE-SET" {
				// rule['value'] = rule_names.get(rule.get('value'), rule.get('value')):
				// a rule without a value gets "value": null.
				value := rule.Value("value")
				key := valueKey(value)
				if replacement, found := ruleNames[key]; found {
					value = replacement
				}
				rule.Set("value", value)
			}
			if id, ok := rule.Value("id").(string); ok {
				itemTypes[id] = rule.Value("itemType")
			}
		}
		// Same as proxies_order: generators skip selections whose rule or
		// group no longer exists, so drop them instead of failing the upgrade.
		for _, engine := range []string{"mosdns", "oxidns"} {
			dnsSettings, ok := jsonx.AsObject(profile.Value(engine))
			if !ok {
				continue
			}
			for _, key := range []string{"direct_rulesets", "proxy_rulesets", "direct_rules", "proxy_rules"} {
				values, isList := jsonx.AsList(dnsSettings.Value(key))
				if !isList {
					continue
				}
				expected := "rule"
				if strings.HasSuffix(key, "rulesets") {
					expected = "ruleset"
				}
				kept := []any{}
				for _, value := range values {
					if id, ok := value.(string); ok && itemTypes[id] == expected {
						kept = append(kept, value)
					}
				}
				dnsSettings.Set(key, kept)
			}
		}
		if surge, ok := jsonx.AsObject(profile.Value("surge")); ok {
			if smartGroups, isList := jsonx.AsList(surge.Value("smart_groups")); isList {
				kept := []any{}
				for _, entry := range smartGroups {
					setting, ok := jsonx.AsObject(entry)
					if !ok {
						continue
					}
					key := valueKey(setting.Value("group_id"))
					if groupIDs[key] {
						kept = append(kept, entry)
					}
				}
				surge.Set("smart_groups", kept)
			}
		}
		profiles.Set(profileID, profile)
	}
	agents, err := objectsOf(system.Value("agents"))
	if err != nil {
		return nil, err
	}
	for _, agent := range agents {
		if isVersion2 {
			agent.SetDefault("profile_id", "default")
		} else {
			agent.Set("profile_id", "default")
		}
	}
	return document, nil
}
