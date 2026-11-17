package repo

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"configflow/internal/ctxx"
	"configflow/internal/jsonx"
)

var metadataKeys = []string{"id", "name", "description", "created_at", "updated_at"}

// metadataOf is _metadata(profile).
func metadataOf(profile *jsonx.Object) *jsonx.Object {
	result := jsonx.NewObject()
	for _, key := range metadataKeys {
		result.Set(key, jsonx.DeepCopy(profile.GetOr(key, "")))
	}
	return result
}

// DefaultProfileID is default_profile_id(): system_config.default_profile_id,
// "default" when it was never switched.
func (r *Repository) DefaultProfileID(ctx context.Context) (result string, err error) {
	err = r.view(ctx, func(document *jsonx.Object) error {
		result = defaultProfileIDOf(document)
		return nil
	})
	return result, err
}

// AgentBindingSummary is agent_binding_summary: device identity only, never
// credentials or deployment payloads.
func AgentBindingSummary(agent *jsonx.Object) *jsonx.Object {
	name := agent.Value("name")
	if !jsonx.Truthy(name) {
		name = agent.Value("id")
	}
	serviceType := agent.Value("service_type")
	if !jsonx.Truthy(serviceType) {
		serviceType = "mihomo"
	}
	return jsonx.Obj("id", agent.Value("id"), "name", name, "host", agent.GetOr("host", ""),
		"port", agent.Value("port"), "service_type", serviceType)
}

func agentProfileID(agent *jsonx.Object, defaultID string) any {
	if value := agent.Value("profile_id"); jsonx.Truthy(value) {
		return value
	}
	return defaultID
}

// ListProfiles is list_profiles(): metadata of every profile plus revision
// (the generated-config revision, see ConfigRevision), is_default and
// bound_agents.
func (r *Repository) ListProfiles(ctx context.Context) (result []any, err error) {
	err = r.view(ctx, func(document *jsonx.Object) error {
		sharedRevision := revisionOf(document.GetObject("shared"))
		defaultID := defaultProfileIDOf(document)
		bindings := map[string][]any{}
		for _, item := range document.GetObject("system").GetList("agents") {
			agent, _ := jsonx.AsObject(item)
			if profileID, ok := agentProfileID(agent, defaultID).(string); ok {
				bindings[profileID] = append(bindings[profileID], AgentBindingSummary(agent))
			}
		}
		result = []any{}
		for _, value := range document.GetObject("profiles").All() {
			profile, _ := jsonx.AsObject(value)
			entry := metadataOf(profile)
			entry.Set("revision", jsonx.Int(revisionOf(profile)+sharedRevision))
			entry.Set("is_default", profile.Value("id") == defaultID)
			bound := bindings[profile.GetString("id")]
			if bound == nil {
				bound = []any{}
			}
			entry.Set("bound_agents", bound)
			result = append(result, entry)
		}
		return nil
	})
	return result, err
}

// GetProfileAgentBindings is get_profile_agent_bindings.
func (r *Repository) GetProfileAgentBindings(ctx context.Context, profileID string) (result *jsonx.Object, err error) {
	err = r.view(ctx, func(document *jsonx.Object) error {
		profile, err := profileOf(document, profileID)
		if err != nil {
			return err
		}
		defaultID := defaultProfileIDOf(document)
		agents := []any{}
		for _, item := range document.GetObject("system").GetList("agents") {
			agent, _ := jsonx.AsObject(item)
			if agentProfileID(agent, defaultID) == profileID {
				agents = append(agents, AgentBindingSummary(agent))
			}
		}
		result = jsonx.Obj("profile", metadataOf(profile), "agents", agents)
		return nil
	})
	return result, err
}

// ProfileMetadata is _profile_metadata: id, name, description, created_at
// and updated_at of a profile, without copying the profile. It is the cheap
// existence check (ErrNotFound, or ErrValidation for a malformed id).
func (r *Repository) ProfileMetadata(ctx context.Context, profileID string) (result *jsonx.Object, err error) {
	r.metadataReads.Add(1)
	err = r.view(ctx, func(document *jsonx.Object) error {
		profile, err := profileOf(document, profileID)
		if err != nil {
			return err
		}
		result = metadataOf(profile)
		return nil
	})
	return result, err
}

// GetProfile is get_profile(): a copy of the stored profile (references
// only, no shared resources).
func (r *Repository) GetProfile(ctx context.Context, profileID string) (result *jsonx.Object, err error) {
	err = r.view(ctx, func(document *jsonx.Object) error {
		profile, err := profileOf(document, profileID)
		if err != nil {
			return err
		}
		result = profile.DeepCopy()
		return nil
	})
	return result, err
}

// ExportProfile is export_profile().
func (r *Repository) ExportProfile(ctx context.Context, profileID string) (*jsonx.Object, error) {
	return r.GetProfile(ctx, profileID)
}

// ConfigRevision is config_revision(): the revision of the generated
// configuration; it grows when the profile or the shared resources change
// and tells whether an Agent is out of date.
func (r *Repository) ConfigRevision(ctx context.Context, profileID string) (result int64, err error) {
	err = r.view(ctx, func(document *jsonx.Object) error {
		profile, err := profileOf(document, profileID)
		if err != nil {
			return err
		}
		result = revisionOf(profile) + revisionOf(document.GetObject("shared"))
		return nil
	})
	return result, err
}

// GetPublishSnapshot is get_publish_snapshot(profile_id, agents=...): the
// compat view and its config revision from the same read snapshot.
// agents=false leaves the Agent records out.
func (r *Repository) GetPublishSnapshot(ctx context.Context, profileID string, agents bool) (config *jsonx.Object, revision int64, err error) {
	err = r.view(ctx, func(document *jsonx.Object) error {
		profile, err := profileOf(document, profileID)
		if err != nil {
			return err
		}
		revision = revisionOf(profile) + revisionOf(document.GetObject("shared"))
		config, err = resolveProfile(document, profileID, nil, agents)
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	return config, revision, nil
}

// GetCompatConfig is get_compat_config(profile_id, resource_roots=...): the
// profile with system_config, backup, agents, the shared resources its
// composition selects (plus roots), the whole rule library, and rulesets
// hydrated from it.
func (r *Repository) GetCompatConfig(ctx context.Context, profileID string, roots ResourceRoots) (result *jsonx.Object, err error) {
	err = r.view(ctx, func(document *jsonx.Object) error {
		result, err = resolveProfile(document, profileID, roots, true)
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// GetCompatConfigWithSource is get_compat_config_with_source: the compat
// view and the committed document it was resolved from (nil inside a
// transaction). The source is shared and must never be modified.
func (r *Repository) GetCompatConfigWithSource(ctx context.Context, profileID string) (result, source *jsonx.Object, err error) {
	document, committed, err := r.committedSource(ctx)
	if err != nil {
		return nil, nil, err
	}
	result, err = resolveProfile(document, profileID, nil, true)
	if err != nil {
		return nil, nil, err
	}
	if committed {
		source = document
	}
	return result, source, nil
}

// CompatConfigFrom is compat_config_from: resolve again from a document
// returned by GetCompatConfigWithSource; the result equals the view of then.
func (r *Repository) CompatConfigFrom(document *jsonx.Object, profileID string) (*jsonx.Object, error) {
	return resolveProfile(document, profileID, nil, true)
}

func newProfileID(metadata *jsonx.Object) (string, error) {
	value := metadata.Value("id")
	if !jsonx.Truthy(value) {
		return "profile_" + randomHex(12), nil
	}
	profileID, ok := value.(string)
	if !ok {
		return "", validationError("Invalid profile id")
	}
	return profileID, ValidateProfileID(profileID)
}

func descriptionOf(metadata *jsonx.Object) string {
	if value := metadata.Value("description"); jsonx.Truthy(value) {
		return jsonx.Str(value)
	}
	return ""
}

// CreateProfile is create_profile(metadata, clone_from): metadata has id
// (optional, generated "profile_<12 hex>"), name (defaults to the id) and
// description. A non-empty cloneFrom copies that profile without its
// routers and client fixed IPs. It returns the new profile's metadata.
func (r *Repository) CreateProfile(ctx context.Context, metadata *jsonx.Object, cloneFrom string) (*jsonx.Object, error) {
	ctx = ctxx.Detach(ctx)
	if metadata == nil {
		return nil, validationError("Profile metadata must be an object")
	}
	profileID, err := newProfileID(metadata)
	if err != nil {
		return nil, err
	}
	var rawName any = profileID
	if value := metadata.Value("name"); jsonx.Truthy(value) {
		rawName = value
	}
	name, ok := rawName.(string)
	if !ok || pyStrip(name) == "" {
		return nil, validationError("配置名称不能为空")
	}
	var profile *jsonx.Object
	err = r.update(ctx, writeOptions{}, func(_ context.Context, document *jsonx.Object) error {
		profiles := document.GetObject("profiles")
		if profiles.Has(profileID) {
			return exists(profileID)
		}
		if cloneFrom != "" {
			source, err := profileOf(document, cloneFrom)
			if err != nil {
				return err
			}
			profile = source.DeepCopy()
			// Router credentials, sync settings and ownership belong to the source devices.
			profile.Set("routers", jsonx.Obj("items", []any{}))
			profile.Set("client_fixed_ips", jsonx.NewObject())
		} else {
			profile = r.emptyProfile(profileID, name, "")
		}
		profile.Set("id", profileID)
		profile.Set("name", pyStrip(name))
		profile.Set("description", descriptionOf(metadata))
		profile.Set("created_at", nowISO())
		profile.Set("updated_at", nowISO())
		profile.Set("_revision", jsonx.Int(0))
		profiles.Set(profileID, profile)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return metadataOf(profile), nil
}

// CloneProfile is clone_profile(source_id, metadata).
func (r *Repository) CloneProfile(ctx context.Context, sourceID string, metadata *jsonx.Object) (*jsonx.Object, error) {
	return r.CreateProfile(ctx, metadata, sourceID)
}

// ImportClientProfile is import_client_profile(metadata, build): create a
// profile and append shared resources in one transaction. build receives a
// copy of the shared section and returns {"shared": {field: [new resources]},
// "profile": {profile fields}, ...}; nothing is written when validation
// fails. The result is build's object with "profile" replaced by the new
// profile's metadata.
func (r *Repository) ImportClientProfile(ctx context.Context, metadata *jsonx.Object,
	build func(ctx context.Context, shared *jsonx.Object) (*jsonx.Object, error)) (*jsonx.Object, error) {
	ctx = ctxx.Detach(ctx)
	if metadata == nil {
		return nil, validationError("Profile metadata must be an object")
	}
	profileID, err := newProfileID(metadata)
	if err != nil {
		return nil, err
	}
	name, ok := metadata.Value("name").(string)
	if !ok || pyStrip(name) == "" {
		return nil, validationError("配置名称不能为空")
	}
	var result, profile *jsonx.Object
	err = r.update(ctx, writeOptions{}, func(ctx context.Context, document *jsonx.Object) error {
		profiles := document.GetObject("profiles")
		if profiles.Has(profileID) {
			return exists(profileID)
		}
		shared := document.GetObject("shared")
		var err error
		if result, err = build(ctx, shared.DeepCopy()); err != nil {
			return err
		}
		if result == nil {
			return helperError(ClassAttributeError, "'NoneType' object has no attribute 'get'")
		}
		additions, err := compatObject(result.GetOr("shared", jsonx.NewObject()))
		if err != nil {
			return err
		}
		adds := false
		for _, field := range SharedFields {
			adds = adds || jsonx.Truthy(additions.Value(field))
		}
		if adds {
			for _, field := range SharedFields {
				extra, err := compatList(jsonx.DeepCopy(additions.GetOr(field, []any{})))
				if err != nil {
					return err
				}
				shared.Set(field, append(append([]any{}, shared.GetList(field)...), extra...))
			}
			bumpRevision(shared)
		}
		profile = r.emptyProfile(profileID, pyStrip(name), "")
		profileValue := result.GetOr("profile", jsonx.NewObject())
		fields, ok := profileValue.(*jsonx.Object)
		if !ok || fields == nil {
			return helperError(ClassAttributeError, "'"+pyTypeName(profileValue)+"' object has no attribute 'items'")
		}
		for key, value := range fields.All() {
			if !IsProfileField(key) {
				return validationError("导入结果包含未知字段：" + key)
			}
			if current, ok := jsonx.AsObject(profile.Value(key)); ok {
				override, err := compatObject(value)
				if err != nil {
					return helperError(ClassAttributeError, "'"+pyTypeName(value)+"' object has no attribute 'items'")
				}
				profile.Set(key, DeepMerge(current, override))
			} else {
				profile.Set(key, jsonx.DeepCopy(value))
			}
		}
		profile.Set("description", descriptionOf(metadata))
		profiles.Set(profileID, profile)
		return nil
	})
	if err != nil {
		return nil, err
	}
	output := result.Clone()
	output.Set("profile", metadataOf(profile))
	return output, nil
}

// UpdateProfile is update_profile(profile_id, updates): rename or describe a
// profile; updates may only hold "name" and "description".
func (r *Repository) UpdateProfile(ctx context.Context, profileID string, updates *jsonx.Object) (*jsonx.Object, error) {
	ctx = ctxx.Detach(ctx)
	if updates == nil {
		return nil, validationError("只能修改配置名称和说明")
	}
	for key := range updates.All() {
		if key != "name" && key != "description" {
			return nil, validationError("只能修改配置名称和说明")
		}
	}
	var profile *jsonx.Object
	err := r.update(ctx, writeOptions{}, func(_ context.Context, document *jsonx.Object) error {
		var err error
		if profile, err = profileOf(document, profileID); err != nil {
			return err
		}
		for key, value := range updates.All() {
			text, ok := value.(string)
			if !ok || (key == "name" && pyStrip(text) == "") {
				return validationError("配置名称和说明必须是字符串，名称不能为空")
			}
			if key == "name" {
				text = pyStrip(text)
			}
			profile.Set(key, text)
		}
		bumpRevision(profile)
		profile.Set("updated_at", nowISO())
		return nil
	})
	if err != nil {
		return nil, err
	}
	return metadataOf(profile), nil
}

// SetDefaultProfile is set_default_profile: make another profile the
// default; it reports whether anything changed. Stored Agents always name
// their profile, so no device moves.
func (r *Repository) SetDefaultProfile(ctx context.Context, profileID string) (changed bool, err error) {
	err = r.update(ctx, writeOptions{}, func(_ context.Context, document *jsonx.Object) error {
		if _, err := profileOf(document, profileID); err != nil {
			return err
		}
		if defaultProfileIDOf(document) == profileID {
			return nil
		}
		system := document.GetObject("system")
		system.GetObject("system_config").Set("default_profile_id", profileID)
		bumpRevision(system)
		changed = true
		return nil
	})
	return changed && err == nil, err
}

// DeleteProfile is delete_profile(profile_id, new_default_id): deleting the
// default profile needs the profile that takes over (newDefaultID, "" for
// none). The profile's directory is moved to
// migrations/deleted-<id>-<uuid>/ together with deleted-profile.json (the
// export_profile shape, mode 0600), and moved back when the commit fails.
func (r *Repository) DeleteProfile(ctx context.Context, profileID, newDefaultID string) error {
	ctx = ctxx.Detach(ctx)
	return r.withProfileOperation(ctx, profileID, func(ctx context.Context) error {
		var releases []func()
		defer func() {
			for index := len(releases) - 1; index >= 0; index-- {
				releases[index]()
			}
		}()
		tombstone, exported := "", ""
		moved := false
		directory, err := r.ProfileDir(profileID)
		if err != nil {
			return err
		}
		lockedRouters := map[routerKey]bool{}
		batch := false
		err = r.update(ctx, writeOptions{lockedRouters: lockedRouters, batch: &batch}, func(ctx context.Context, document *jsonx.Object) error {
			profile, err := profileOf(document, profileID)
			if err != nil {
				return err
			}
			profiles := document.GetObject("profiles")
			isDefault := profileID == defaultProfileIDOf(document)
			if isDefault {
				if profiles.Len() < 2 {
					return inUse("只有一个配置空间时不能删除默认配置")
				}
				if newDefaultID == "" {
					return inUse("删除默认配置时需要指定新的默认配置空间")
				}
				if newDefaultID == profileID || !profiles.Has(newDefaultID) {
					return validationError("新的默认配置空间不存在：" + newDefaultID)
				}
			}
			var usages []any
			var names []string
			for _, item := range document.GetObject("system").GetList("agents") {
				agent, _ := jsonx.AsObject(item)
				if agent.Value("profile_id") != profileID {
					continue
				}
				name := agent.Value("name")
				if !jsonx.Truthy(name) {
					name = agent.Value("id")
				}
				usages = append(usages, jsonx.Obj("kind", "agent", "id", agent.Value("id"), "name", name,
					"profile_id", profileID, "profile_name", profile.Value("name")))
				names = append(names, jsonx.Str(name))
			}
			if len(usages) > 0 {
				return inUse("配置「"+jsonx.Str(profile.Value("name"))+"」仍被以下 Agent 使用："+strings.Join(names, "、"), usages...)
			}
			// Keep job locks until after the document commits, and take
			// them before moving profile-local artifacts out of the way.
			remaining := jsonx.NewObject()
			for key, value := range profiles.All() {
				if key != profileID {
					remaining.Set(key, value)
				}
			}
			release, err := r.routerCommitGuard(ctx, document, jsonx.Obj("profiles", remaining), nil, nil)
			if err != nil {
				return err
			}
			releases = append(releases, release)
			records, err := routerRecordsOf(document)
			if err != nil {
				return err
			}
			for _, key := range records.keys {
				if key.ProfileID == profileID {
					lockedRouters[key] = true
				}
			}
			if err := beginCommitBatch(ctx, &batch); err != nil {
				return err
			}
			if err := os.Mkdir(r.migrationsDir, 0o777); err != nil && !os.IsExist(err) {
				return err
			}
			tombstone = filepath.Join(r.migrationsDir, "deleted-"+profileID+"-"+randomHex(32))
			if pathExists(directory) {
				if err := os.Rename(directory, tombstone); err != nil {
					return err
				}
				moved = true
			} else if err := os.MkdirAll(tombstone, 0o777); err != nil {
				return err
			}
			// The database keeps no old generations: retain the deleted
			// profile itself (export_profile shape, 0600: it holds credentials).
			exported = filepath.Join(tombstone, "deleted-profile.json")
			if pathExists(exported) {
				exported = filepath.Join(tombstone, "deleted-profile-"+randomHex(32)+".json")
			}
			text, err := fileJSON(profile)
			if err != nil {
				return err
			}
			temporary, err := writeTemp(exported, text)
			if err != nil {
				return err
			}
			if err := os.Rename(temporary, exported); err != nil {
				os.Remove(temporary)
				return err
			}
			if err := fsyncDir(tombstone); err != nil {
				return err
			}
			profiles.Delete(profileID)
			if isDefault {
				system := document.GetObject("system")
				system.GetObject("system_config").Set("default_profile_id", newDefaultID)
				bumpRevision(system)
			}
			return nil
		})
		if err != nil {
			if exported != "" {
				os.Remove(exported)
			}
			if tombstone != "" && pathExists(tombstone) {
				if moved {
					os.Rename(tombstone, directory)
				} else {
					os.RemoveAll(tombstone)
				}
			}
			return err
		}
		return nil
	})
}

// nameSet collects group names for "was this policy removed" checks.
func nameSet(groups any) (map[string]bool, error) {
	items, err := compatList(groups)
	if err != nil {
		return nil, err
	}
	names := map[string]bool{}
	for _, item := range items {
		group, err := compatObject(item)
		if err != nil {
			return nil, err
		}
		names[jsonx.Repr(group.Value("name"))] = true
	}
	return names, nil
}

// updateProfileLocked is _update_profile_locked: apply one profile updater
// inside an open write transaction. The updater sees the compat view
// (hydrated resources) but only ProfileFields are stored.
func (r *Repository) updateProfileLocked(ctx context.Context, document *jsonx.Object, profileID string, updater Updater) (*jsonx.Object, error) {
	current, err := profileOf(document, profileID)
	if err != nil {
		return nil, err
	}
	previousNames, err := nameSet(current.Value("proxy_groups"))
	if err != nil {
		return nil, err
	}
	// Updaters may inspect hydrated resources but cannot persist copies.
	snapshot, err := resolveProfile(document, profileID, nil, true)
	if err != nil {
		return nil, err
	}
	updated, err := applyUpdater(ctx, snapshot, updater)
	if err != nil {
		return nil, err
	}
	for _, key := range ProfileFields {
		if value, ok := updated.Get(key); ok {
			current.Set(key, jsonx.DeepCopy(value))
		}
	}
	currentNames, err := nameSet(current.Value("proxy_groups"))
	if err != nil {
		return nil, err
	}
	rules, err := compatList(current.Value("rule_configs"))
	if err != nil {
		return nil, err
	}
	for _, item := range rules {
		rule, err := compatObject(item)
		if err != nil {
			return nil, err
		}
		policy := jsonx.Repr(rule.Value("policy"))
		if previousNames[policy] && !currentNames[policy] {
			return nil, inUse("配置 "+jsonx.Str(current.Value("name"))+" 的规则仍引用被删除策略组："+jsonx.Str(rule.Value("policy")),
				jsonx.Obj("kind", "profile", "id", profileID, "name", current.Value("name")))
		}
	}
	for _, item := range rules {
		rule := item.(*jsonx.Object)
		if rule.Value("itemType") == "ruleset" {
			for _, key := range RuleSourceFields {
				rule.Delete(key)
			}
			rule.Delete("library_enabled")
		}
	}
	bumpRevision(current)
	current.Set("updated_at", nowISO())
	return current, nil
}

// UpdateProfileTransaction is update_profile_transaction(profile_id,
// updater, include_config_revision=True): updater edits the compat view of
// the profile; its ProfileFields are stored. It returns a copy of the
// stored profile and the new config revision.
func (r *Repository) UpdateProfileTransaction(ctx context.Context, profileID string, updater Updater) (saved *jsonx.Object, revision int64, err error) {
	ctx = ctxx.Detach(ctx)
	var current *jsonx.Object
	err = r.update(ctx, writeOptions{}, func(ctx context.Context, document *jsonx.Object) error {
		var err error
		if current, err = r.updateProfileLocked(ctx, document, profileID, updater); err != nil {
			return err
		}
		revision = revisionOf(current) + revisionOf(document.GetObject("shared"))
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	return current.DeepCopy(), revision, nil
}

// UpdateProfileFields is update_profile_fields(profile_id, fields,
// baseline): store the ProfileFields found in fields. With a baseline each
// field present in it is merged three-way into the current value; without
// one a _revision in fields must equal the profile's.
func (r *Repository) UpdateProfileFields(ctx context.Context, profileID string, fields, baseline *jsonx.Object) (*jsonx.Object, int64, error) {
	ctx = ctxx.Detach(ctx)
	if fields == nil {
		return nil, 0, validationError("Profile fields and baseline must be objects")
	}
	if fields.Has("resource_refs") || fields.Has("node_dialers") {
		return nil, 0, validationError("资源由策略组直接引用，拨号组合请使用代理链类型")
	}
	return r.UpdateProfileTransaction(ctx, profileID, func(_ context.Context, current *jsonx.Object) (*jsonx.Object, error) {
		if revisionConflict(fields, current, baseline) {
			return nil, inUse("配置已更新，请刷新后重试")
		}
		for key, value := range fields.All() {
			if !IsProfileField(key) {
				continue
			}
			if base, ok := baseline.Get(key); ok {
				current.Set(key, ThreeWayMerge(current.Value(key), base, value))
			} else {
				current.Set(key, jsonx.DeepCopy(value))
			}
		}
		return nil, nil
	})
}

// SaveProfile is save_profile(profile_id, data, baseline).
func (r *Repository) SaveProfile(ctx context.Context, profileID string, data, baseline *jsonx.Object) (*jsonx.Object, int64, error) {
	ctx = ctxx.Detach(ctx)
	return r.UpdateProfileFields(ctx, profileID, data, baseline)
}

// ImportProfile is import_profile(profile_id, data): replace the profile's
// fields from an exported profile (or {"config": profile}); shared resource
// copies are refused.
func (r *Repository) ImportProfile(ctx context.Context, profileID string, data *jsonx.Object) (*jsonx.Object, error) {
	ctx = ctxx.Detach(ctx)
	if data == nil {
		return nil, validationError("Imported profile must be an object")
	}
	if inner, ok := data.Get("config"); ok {
		data, _ = jsonx.AsObject(inner)
	}
	if data == nil {
		return nil, validationError("独立配置导入只接受引用，不接受共享资源副本")
	}
	for _, key := range SharedFields {
		if data.Has(key) {
			return nil, validationError("独立配置导入只接受引用，不接受共享资源副本")
		}
	}
	if data.Has("resource_refs") || data.Has("node_dialers") {
		return nil, validationError("独立配置导入仅支持当前格式")
	}
	saved, _, err := r.UpdateProfileTransaction(ctx, profileID, func(_ context.Context, current *jsonx.Object) (*jsonx.Object, error) {
		if revision, ok := data.Get("_revision"); ok && !jsonx.Equal(revision, current.Value("_revision")) {
			return nil, inUse("配置已更新，请刷新后重试")
		}
		for key, value := range data.All() {
			if IsProfileField(key) {
				current.Set(key, jsonx.DeepCopy(value))
			}
		}
		// Imported rules replace the composition; per-rule DNS of rules it
		// no longer has would otherwise refuse the whole import.
		return nil, pruneRuleDNS(current)
	})
	return saved, err
}

// Optional is an argument that may be left out (Python: the _MISSING default).
type Optional struct {
	Set   bool
	Value any
}

// Some is an Optional that is set, also to nil.
func Some(value any) Optional { return Optional{Set: true, Value: value} }

// RouterPublication holds the keyword arguments of save_router_publication.
// A set but falsy state removes the field from the router record.
type RouterPublication struct {
	Manifest    Optional // ros_policy_state
	DNSGuard    Optional // dns_guard_state
	IPv6FakeIP  Optional // ipv6_fakeip_state
	IPv6Routes  Optional // ipv6_routes_state
	IPv6PDGuard Optional // ipv6_pd_guard_state
	LastRelease Optional // last_release (stored as given)
}

// expectedRouterID is expected_router['id'] of the save_router_* methods:
// KeyError when missing, TypeError (Python: the set of locked routers) when
// unhashable. A hashable non-string id matches no record (router ids are
// strings), so the caller reports the router as changed (isString=false).
func expectedRouterID(expectedRouter *jsonx.Object) (routerID string, isString bool, err error) {
	value, ok := expectedRouter.Get("id")
	if !ok {
		return "", false, helperError(ClassKeyError, "'id'")
	}
	switch typed := value.(type) {
	case string:
		return typed, true, nil
	case *jsonx.Object, []any:
		return "", false, helperError(ClassTypeError, "unhashable type: '"+pyTypeName(value)+"'")
	}
	return "", false, nil
}

// SaveRouterSettings is save_router_settings(profile_id, expected_router,
// replacement): commit a router PUT (replacement) or DELETE (nil) while the
// caller holds this router's job lock. The stored record must still equal
// expectedRouter.
func (r *Repository) SaveRouterSettings(ctx context.Context, profileID string, expectedRouter, replacement *jsonx.Object) error {
	ctx = ctxx.Detach(ctx)
	routerID, isString, err := expectedRouterID(expectedRouter)
	if err != nil {
		return err
	}
	key := routerKey{profileID, routerID}
	return r.update(ctx, writeOptions{lockedRouters: map[routerKey]bool{key: true}}, func(_ context.Context, document *jsonx.Object) error {
		profile, err := profileOf(document, profileID)
		if err != nil {
			return err
		}
		records, err := routerRecordsOf(document)
		if err != nil {
			return err
		}
		current, found := records.values[key]
		if !found || !isString || !jsonx.Equal(current, expectedRouter) {
			return inUse("路由器设置已变化，请刷新后重试")
		}
		if replacement != nil && replacement.Value("id") != routerID {
			return validationError("不能修改路由器 ID")
		}
		routers := profile.GetObject("routers")
		items := routers.GetList("items")
		updated := make([]any, 0, len(items))
		for _, item := range items {
			if item != any(current) {
				updated = append(updated, item)
			} else if replacement != nil {
				updated = append(updated, replacement.DeepCopy())
			}
		}
		routers.Set("items", updated)
		bumpRevision(profile)
		profile.Set("updated_at", nowISO())
		return nil
	})
}

// SaveRouterPublication is save_router_publication: persist verified
// readback or rollback under the caller's held job lock. It is the only
// write of an existing record allowed to replace or clear an active
// manifest; it cannot delete a router or alter its connection, and
// unrelated edits made during the device operation are retained.
func (r *Repository) SaveRouterPublication(ctx context.Context, profileID string, expectedRouter *jsonx.Object, publication RouterPublication) error {
	ctx = ctxx.Detach(ctx)
	routerID, isString, err := expectedRouterID(expectedRouter)
	if err != nil {
		return err
	}
	key := routerKey{profileID, routerID}
	options := writeOptions{lockedRouters: map[routerKey]bool{key: true}, policyRouter: &key}
	return r.update(ctx, options, func(_ context.Context, document *jsonx.Object) error {
		profile, err := profileOf(document, profileID)
		if err != nil {
			return err
		}
		records, err := routerRecordsOf(document)
		if err != nil {
			return err
		}
		current := records.values[key]
		if !isString {
			current = nil // router ids are strings: no record has this one
		}
		if current == nil || fieldsDiffer(current, expectedRouter, RouterConnectionFields) {
			return inUse("路由器连接已变化，无法保存发布回读记录")
		}
		states := []Optional{publication.Manifest, publication.DNSGuard, publication.IPv6FakeIP, publication.IPv6Routes,
			publication.IPv6PDGuard}
		for index, field := range RouterManifestFields {
			state := states[index]
			if !state.Set {
				continue
			}
			if jsonx.Truthy(state.Value) {
				current.Set(field, jsonx.DeepCopy(state.Value))
			} else {
				current.Delete(field)
			}
		}
		if publication.LastRelease.Set {
			current.Set("last_release", jsonx.DeepCopy(publication.LastRelease.Value))
		}
		bumpRevision(profile)
		profile.Set("updated_at", nowISO())
		return nil
	})
}
