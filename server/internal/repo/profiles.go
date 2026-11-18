package repo

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"configflow/internal/ctxx"
	"configflow/internal/jsonx"
	"configflow/internal/model"
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
	err = r.read(ctx, func(document *model.Document) error {
		result = defaultProfileIn(document)
		return nil
	})
	return result, err
}

// defaultProfileIn is the default profile id of a typed document.
func defaultProfileIn(document *model.Document) string {
	if id := document.System.SystemConfig.DefaultProfileID; id != nil && *id != "" {
		return *id
	}
	return DefaultProfileID
}

// profileMetadata is _metadata(profile) of a typed profile.
func profileMetadata(profile *model.Profile) *jsonx.Object {
	return jsonx.Obj("id", profile.ID, "name", profile.Name, "description", profile.Description,
		"created_at", profile.CreatedAt, "updated_at", profile.UpdatedAt)
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

// agentBindings is the binding summary (AgentBindingSummary) of every stored
// Agent by the profile it is bound to (its profile_id, else the default).
func agentBindings(document *model.Document) (map[string][]any, error) {
	system, err := viewOf(&document.System)
	if err != nil {
		return nil, err
	}
	raw, _ := system.get("agents")
	var agents []json.RawMessage
	if jsonKind(raw) == '[' {
		if err := json.Unmarshal(raw, &agents); err != nil {
			return nil, err
		}
	}
	defaultID := defaultProfileIn(document)
	bindings := map[string][]any{}
	for _, item := range agents {
		if jsonKind(item) != '{' {
			continue
		}
		agent := viewOfJSON(item)
		profileID := defaultID
		if raw, _ := agent.get("profile_id"); jsonTruthy(raw) {
			text, ok := jsonString(raw)
			if !ok {
				continue
			}
			profileID = text
		}
		id := storedValue(agent, "id", nil)
		name := id
		if raw, _ := agent.get("name"); jsonTruthy(raw) {
			name = storedValue(agent, "name", nil)
		}
		serviceType := any("mihomo")
		if raw, _ := agent.get("service_type"); jsonTruthy(raw) {
			serviceType = storedValue(agent, "service_type", nil)
		}
		bindings[profileID] = append(bindings[profileID], jsonx.Obj("id", id, "name", name,
			"host", storedValue(agent, "host", ""), "port", storedValue(agent, "port", nil), "service_type", serviceType))
	}
	return bindings, nil
}

// ListProfiles is list_profiles(): metadata of every profile plus revision
// (the generated-config revision, see ConfigRevision), is_default and
// bound_agents.
func (r *Repository) ListProfiles(ctx context.Context) (result []any, err error) {
	err = r.read(ctx, func(document *model.Document) error {
		bindings, err := agentBindings(document)
		if err != nil {
			return err
		}
		defaultID := defaultProfileIn(document)
		result = []any{}
		for _, profile := range document.Profiles {
			entry := profileMetadata(profile)
			entry.Set("revision", jsonx.Int(int64(profile.Revision)+int64(document.Shared.Revision)))
			entry.Set("is_default", profile.Key() == defaultID)
			bound := bindings[profile.Key()]
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
	err = r.read(ctx, func(document *model.Document) error {
		profile, err := profileFor(document, profileID)
		if err != nil {
			return err
		}
		bindings, err := agentBindings(document)
		if err != nil {
			return err
		}
		agents := bindings[profileID]
		if agents == nil {
			agents = []any{}
		}
		result = jsonx.Obj("profile", profileMetadata(profile), "agents", agents)
		return nil
	})
	return result, err
}

// ProfileMetadata is _profile_metadata: id, name, description, created_at
// and updated_at of a profile, without copying the profile. It is the cheap
// existence check (ErrNotFound, or ErrValidation for a malformed id).
func (r *Repository) ProfileMetadata(ctx context.Context, profileID string) (result *jsonx.Object, err error) {
	r.metadataReads.Add(1)
	err = r.read(ctx, func(document *model.Document) error {
		profile, err := profileFor(document, profileID)
		if err != nil {
			return err
		}
		result = profileMetadata(profile)
		return nil
	})
	return result, err
}

// GetProfile is get_profile(): a copy of the stored profile (references
// only, no shared resources).
func (r *Repository) GetProfile(ctx context.Context, profileID string) (result *jsonx.Object, err error) {
	err = r.read(ctx, func(document *model.Document) error {
		profile, err := profileFor(document, profileID)
		if err != nil {
			return err
		}
		result, err = legacyObject(profile)
		return err
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
	err = r.read(ctx, func(document *model.Document) error {
		profile, err := profileFor(document, profileID)
		if err != nil {
			return err
		}
		result = int64(profile.Revision) + int64(document.Shared.Revision)
		return nil
	})
	return result, err
}

// GetPublishSnapshot is get_publish_snapshot(profile_id, agents=...): the
// compat view and its config revision from the same read snapshot.
// agents=false leaves the Agent records out.
func (r *Repository) GetPublishSnapshot(ctx context.Context, profileID string, agents bool) (config *jsonx.Object, revision int64, err error) {
	err = r.read(ctx, func(document *model.Document) error {
		profile, err := profileFor(document, profileID)
		if err != nil {
			return err
		}
		revision = int64(profile.Revision) + int64(document.Shared.Revision)
		config, err = legacyConfig(document, profileID, nil, agents)
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	return config, revision, nil
}

// PublishConfig is GetPublishSnapshot on typed values: the resolved view of
// a profile (Agents only when agents is set) and its config revision, from
// one generation. The value belongs to the caller.
func (r *Repository) PublishConfig(ctx context.Context, profileID string, agents bool) (config *model.Config, revision int64, err error) {
	err = r.View(ctx, func(document *model.Document) error {
		profile, err := profileFor(document, profileID)
		if err != nil {
			return err
		}
		revision = int64(profile.Revision) + int64(document.Shared.Revision)
		config, err = resolveConfig(document, profileID, nil, agents)
		return err
	})
	return config, revision, err
}

// Config returns the resolved view of a profile (Python's compat config):
// its own members, system_config, backup, the Agents, the shared resources
// its composition selects plus roots, the whole rule library, and rulesets
// hydrated from it. The value belongs to the caller.
func (r *Repository) Config(ctx context.Context, profileID string, roots ResourceRoots) (result *model.Config, err error) {
	err = r.View(ctx, func(document *model.Document) error {
		result, err = resolveConfig(document, profileID, roots, true)
		return err
	})
	return result, err
}

// GetCompatConfig is get_compat_config(profile_id, resource_roots=...):
// Config as a *jsonx.Object.
func (r *Repository) GetCompatConfig(ctx context.Context, profileID string, roots ResourceRoots) (result *jsonx.Object, err error) {
	err = r.read(ctx, func(document *model.Document) error {
		result, err = legacyConfig(document, profileID, roots, true)
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
	result, rebuild, err := r.compatConfigWithBaseline(ctx, profileID)
	if err != nil || rebuild == nil {
		return result, nil, err
	}
	entry, err := r.readEntry(ctx, false)
	if err != nil {
		return nil, nil, err
	}
	return result, entry.Document, nil
}

// compatConfigWithBaseline is the compat view and, when it was read from a
// committed generation, a function that resolves the same view again from
// that generation (the three-way merge baseline of SaveConfig).
func (r *Repository) compatConfigWithBaseline(ctx context.Context, profileID string) (*jsonx.Object, func() (*jsonx.Object, error), error) {
	if r.InTransaction(ctx) {
		document, _, err := r.workingModel(ctx)
		if err != nil {
			return nil, nil, err
		}
		result, err := legacyConfig(document, profileID, nil, true)
		return result, nil, err
	}
	entry, err := r.readEntry(ctx, false)
	if err != nil {
		return nil, nil, err
	}
	result, err := legacyConfig(entry.Model, profileID, nil, true)
	if err != nil {
		return nil, nil, err
	}
	return result, func() (*jsonx.Object, error) { return legacyConfig(entry.Model, profileID, nil, true) }, nil
}

// CompatConfigFrom is compat_config_from: resolve again from a document
// returned by GetCompatConfigWithSource (or a candidate built as jsonx); the
// result equals the view of then.
func (r *Repository) CompatConfigFrom(document *jsonx.Object, profileID string) (*jsonx.Object, error) {
	typed, err := modelFromLegacy(document, nil)
	if err != nil {
		return nil, err
	}
	return legacyConfig(typed, profileID, nil, true)
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
	if !ok || trimText(name) == "" {
		return nil, validationError("配置名称不能为空")
	}
	var profile *model.Profile
	err = r.Update(ctx, func(_ context.Context, document *model.Document) error {
		if profileByKey(document, profileID) != nil {
			return exists(profileID)
		}
		var err error
		if cloneFrom != "" {
			source, err := profileFor(document, cloneFrom)
			if err != nil {
				return err
			}
			if profile, err = copyRecord(source); err != nil {
				return err
			}
			// Router credentials, sync settings and ownership belong to the source devices.
			if err := profile.SetJSON("routers", []byte(`{"items":[]}`)); err != nil {
				return err
			}
			if err := profile.SetJSON("client_fixed_ips", []byte(`{}`)); err != nil {
				return err
			}
		} else if profile, err = r.newProfileRecord(r.emptyProfile(profileID, name, "")); err != nil {
			return err
		}
		now := nowISO()
		profile.ID, profile.Name, profile.Description = profileID, trimText(name), descriptionOf(metadata)
		profile.CreatedAt, profile.UpdatedAt, profile.Revision = now, now, 0
		document.Profiles = append(document.Profiles, profile)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return profileMetadata(profile), nil
}

// newProfileRecord decodes a profile built as jsonx (defaults, imports).
func (r *Repository) newProfileRecord(profile *jsonx.Object) (*model.Profile, error) {
	data, err := jsonx.Compact(profile)
	if err != nil {
		return nil, err
	}
	var result model.Profile
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return &result, nil
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
	if !ok || trimText(name) == "" {
		return nil, validationError("配置名称不能为空")
	}
	var result *jsonx.Object
	var profile *model.Profile
	err = r.Update(ctx, func(ctx context.Context, document *model.Document) error {
		if profileByKey(document, profileID) != nil {
			return exists(profileID)
		}
		shared, err := legacyObject(&document.Shared)
		if err != nil {
			return err
		}
		if result, err = build(ctx, shared); err != nil {
			return err
		}
		if result == nil {
			return validationError(msgImportResultShape)
		}
		additions, ok := jsonx.AsObject(result.GetOr("shared", jsonx.NewObject()))
		if !ok {
			return validationError(msgImportResultShape)
		}
		adds := false
		for _, field := range SharedFields {
			adds = adds || jsonx.Truthy(additions.Value(field))
		}
		if adds {
			current, err := legacyObject(&document.Shared)
			if err != nil {
				return err
			}
			for _, field := range SharedFields {
				extra, ok := jsonx.AsList(jsonx.DeepCopy(additions.GetOr(field, []any{})))
				if !ok {
					// The additions are not resources with ids.
					return validationError(field + " 中每一项必须有 ID")
				}
				if len(extra) == 0 {
					continue
				}
				combined := append(append([]any{}, current.GetList(field)...), extra...)
				if err := setLegacy(document.Shared.SetJSON, field, combined); err != nil {
					return err
				}
			}
			document.Shared.Revision++
		}
		built := r.emptyProfile(profileID, trimText(name), "")
		profileValue := result.GetOr("profile", jsonx.NewObject())
		fields, ok := profileValue.(*jsonx.Object)
		if !ok || fields == nil {
			return validationError(msgImportResultShape)
		}
		for key, value := range fields.All() {
			if !IsProfileField(key) {
				return validationError("导入结果包含未知字段：" + key)
			}
			if current, ok := jsonx.AsObject(built.Value(key)); ok {
				override, ok := jsonx.AsObject(value)
				if !ok {
					return validationError(msgImportResultShape)
				}
				built.Set(key, DeepMerge(current, override))
			} else {
				built.Set(key, jsonx.DeepCopy(value))
			}
		}
		built.Set("description", descriptionOf(metadata))
		if profile, err = r.newProfileRecord(built); err != nil {
			return err
		}
		document.Profiles = append(document.Profiles, profile)
		return nil
	})
	if err != nil {
		return nil, err
	}
	output := result.Clone()
	output.Set("profile", profileMetadata(profile))
	return output, nil
}

// UpdateProfileMetadata is update_profile(profile_id, updates): rename or describe a
// profile; updates may only hold "name" and "description".
func (r *Repository) UpdateProfileMetadata(ctx context.Context, profileID string, updates *jsonx.Object) (*jsonx.Object, error) {
	ctx = ctxx.Detach(ctx)
	if updates == nil {
		return nil, validationError("只能修改配置名称和说明")
	}
	for key := range updates.All() {
		if key != "name" && key != "description" {
			return nil, validationError("只能修改配置名称和说明")
		}
	}
	var profile *model.Profile
	err := r.Update(ctx, func(_ context.Context, document *model.Document) error {
		var err error
		if profile, err = profileFor(document, profileID); err != nil {
			return err
		}
		for key, value := range updates.All() {
			text, ok := value.(string)
			if !ok || (key == "name" && trimText(text) == "") {
				return validationError("配置名称和说明必须是字符串，名称不能为空")
			}
			if key == "name" {
				profile.Name = trimText(text)
			} else {
				profile.Description = text
			}
		}
		profile.Revision++
		profile.UpdatedAt = nowISO()
		return nil
	})
	if err != nil {
		return nil, err
	}
	return profileMetadata(profile), nil
}

// SetDefaultProfile is set_default_profile: make another profile the
// default; it reports whether anything changed. Stored Agents always name
// their profile, so no device moves.
func (r *Repository) SetDefaultProfile(ctx context.Context, profileID string) (changed bool, err error) {
	ctx = ctxx.Detach(ctx)
	err = r.Update(ctx, func(_ context.Context, document *model.Document) error {
		if _, err := profileFor(document, profileID); err != nil {
			return err
		}
		if defaultProfileIn(document) == profileID {
			return nil
		}
		id := profileID
		document.System.SystemConfig.DefaultProfileID = &id
		document.System.Revision++
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
		err = r.transact(ctx, writeOptions{lockedRouters: lockedRouters, batch: &batch}, func(ctx context.Context, tx *transaction) error {
			document := tx.document
			profile, err := profileFor(document, profileID)
			if err != nil {
				return err
			}
			isDefault := profileID == defaultProfileIn(document)
			if isDefault {
				if len(document.Profiles) < 2 {
					return inUse("只有一个配置空间时不能删除默认配置")
				}
				if newDefaultID == "" {
					return inUse("删除默认配置时需要指定新的默认配置空间")
				}
				if newDefaultID == profileID || profileByKey(document, newDefaultID) == nil {
					return validationError("新的默认配置空间不存在：" + newDefaultID)
				}
			}
			var usages []any
			var names []string
			systemView, err := viewOf(&document.System)
			if err != nil {
				return err
			}
			agentsRaw, _ := systemView.get("agents")
			agents, err := itemsOf(agentsRaw, "Agent")
			if err != nil {
				return err
			}
			for _, id := range agents.ids {
				agent := agents.byID[id]
				if bound, _ := jsonString(mustGet(agent, "profile_id")); bound != profileID {
					continue
				}
				var name any = id
				if raw, _ := agent.get("name"); jsonTruthy(raw) {
					name = storedValue(agent, "name", id)
				}
				usages = append(usages, jsonx.Obj("kind", "agent", "id", id, "name", name,
					"profile_id", profileID, "profile_name", profile.Name))
				names = append(names, jsonx.Str(name))
			}
			if len(usages) > 0 {
				return inUse("配置「"+profile.Name+"」仍被以下 Agent 使用："+strings.Join(names, "、"), usages...)
			}
			// Keep job locks until after the document commits, and take
			// them before moving profile-local artifacts out of the way.
			records, err := routerRecordsIn(document)
			if err != nil {
				return err
			}
			remaining := routerRecordSet{values: map[routerKey]memberView{}}
			for _, key := range records.keys {
				if key.ProfileID != profileID {
					remaining.keys = append(remaining.keys, key)
					remaining.values[key] = records.values[key]
				}
			}
			release, err := r.routerGuard(ctx, document, records, remaining, nil, nil)
			if err != nil {
				return err
			}
			releases = append(releases, release)
			for _, key := range records.keys {
				if key.ProfileID == profileID {
					lockedRouters[key] = true
				}
			}
			exportedProfile, err := legacyObject(profile)
			if err != nil {
				return err
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
			text, err := fileJSON(exportedProfile)
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
			document.Profiles = slices.DeleteFunc(document.Profiles, func(p *model.Profile) bool { return p.Key() == profileID })
			if isDefault {
				id := newDefaultID
				document.System.SystemConfig.DefaultProfileID = &id
				document.System.Revision++
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

// UpdateProfileTransaction is update_profile_transaction(profile_id,
// updater, include_config_revision=True): updater edits the compat view of
// the profile; its ProfileFields are stored. It returns a copy of the
// stored profile and the new config revision.
func (r *Repository) UpdateProfileTransaction(ctx context.Context, profileID string, updater Updater) (saved *jsonx.Object, revision int64, err error) {
	ctx = ctxx.Detach(ctx)
	var current *model.Profile
	err = r.Update(ctx, func(ctx context.Context, document *model.Document) error {
		var err error
		if current, err = updateProfileLegacy(ctx, document, profileID, updater); err != nil {
			return err
		}
		revision = int64(current.Revision) + int64(document.Shared.Revision)
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	saved, err = legacyObject(current)
	return saved, revision, err
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
				merged, err := threeWayMerge(current.Value(key), base, value)
				if err != nil {
					return nil, err
				}
				current.Set(key, merged)
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
		return "", false, validationError(msgRouterIDInvalid)
	}
	switch typed := value.(type) {
	case string:
		return typed, true, nil
	case *jsonx.Object, []any:
		return "", false, validationError(msgRouterIDInvalid)
	}
	return "", false, nil
}

// SaveRouterSettings is save_router_settings(profile_id, expected_router,
// replacement): commit a router PUT (replacement) or DELETE (nil) while the
// caller holds this router's job lock. The stored record must still equal
// expectedRouter.
func (r *Repository) SaveRouterSettings(ctx context.Context, profileID string, expectedRouter, replacement *jsonx.Object) error {
	expected, err := json.Marshal(expectedRouter)
	if err != nil {
		return err
	}
	var next json.RawMessage
	if replacement != nil {
		next, err = json.Marshal(replacement)
		if err != nil {
			return err
		}
	}
	return r.SaveRouterSettingsJSON(ctx, profileID, expected, next)
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
	return r.transact(ctx, options, func(_ context.Context, tx *transaction) error {
		profile, err := profileFor(tx.document, profileID)
		if err != nil {
			return err
		}
		records, err := routerRecordsIn(tx.document)
		if err != nil {
			return err
		}
		current, found := records.values[key]
		expected, err := viewOfLegacy(expectedRouter)
		if err != nil {
			return err
		}
		if !isString || !found || routerFieldsDiffer(current, &expected, RouterConnectionFields) {
			return inUse("路由器连接已变化，无法保存发布回读记录")
		}
		var router *model.Router
		for index := range profile.Routers.Items {
			if profile.Routers.Items[index].ID == routerID {
				router = &profile.Routers.Items[index]
			}
		}
		if router == nil {
			return inUse("路由器连接已变化，无法保存发布回读记录")
		}
		states := []Optional{publication.Manifest, publication.DNSGuard, publication.IPv6FakeIP, publication.IPv6Routes,
			publication.IPv6PDGuard}
		for index, field := range RouterManifestFields {
			state := states[index]
			if !state.Set {
				continue
			}
			// A set but falsy state removes the record (Python: pop).
			if !jsonx.Truthy(state.Value) {
				if err := router.Clear(field); err != nil {
					return err
				}
			} else if err := setLegacy(router.SetJSON, field, state.Value); err != nil {
				return err
			}
		}
		if publication.LastRelease.Set {
			if err := setLegacy(router.SetJSON, "last_release", publication.LastRelease.Value); err != nil {
				return err
			}
		}
		profile.Revision++
		profile.UpdatedAt = nowISO()
		return nil
	})
}

// JSONUpdate distinguishes an untouched publication field, replacement, and
// explicit null (clear). It is a boundary DTO; callers retain native models.
type JSONUpdate struct {
	Set   bool
	Value json.RawMessage
}

// RouterPublicationJSON is the native JSON boundary of RouterPublication.
type RouterPublicationJSON struct {
	Manifest, DNSGuard, IPv6FakeIP, IPv6Routes, IPv6PDGuard, LastRelease JSONUpdate
}

// SaveRouterPublicationJSON decodes only at the legacy repository boundary and
// delegates the single guarded publication transaction, including its revision.
func (r *Repository) SaveRouterPublicationJSON(ctx context.Context, profileID string, expected json.RawMessage, p RouterPublicationJSON) error {
	ctx = ctxx.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	var record jsonx.Object
	if err := json.Unmarshal(expected, &record); err != nil {
		return err
	}
	decode := func(v JSONUpdate) (Optional, error) {
		if !v.Set {
			return Optional{}, nil
		}
		if len(v.Value) == 0 {
			return Some(nil), nil
		}
		var value any
		value, err := jsonx.Parse(v.Value)
		if err != nil {
			return Optional{}, err
		}
		return Some(value), nil
	}
	fields := []JSONUpdate{p.Manifest, p.DNSGuard, p.IPv6FakeIP, p.IPv6Routes, p.IPv6PDGuard, p.LastRelease}
	out := make([]Optional, len(fields))
	for i, v := range fields {
		var err error
		out[i], err = decode(v)
		if err != nil {
			return err
		}
	}
	return r.SaveRouterPublication(ctx, profileID, &record, RouterPublication{Manifest: out[0], DNSGuard: out[1], IPv6FakeIP: out[2], IPv6Routes: out[3], IPv6PDGuard: out[4], LastRelease: out[5]})
}
