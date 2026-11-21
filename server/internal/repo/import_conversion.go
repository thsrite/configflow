package repo

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"configflow/internal/convert"
	"configflow/internal/jsonresp"
	"configflow/internal/model"
	"configflow/internal/rules"
	"configflow/internal/yamlx"
)

// importProfile is an old single-profile record after its defaults are merged.
// Config owns the known resources and profile fields; Fields retains old rules
// and rule_sets until those lists are promoted into the current composition.
type importProfile struct {
	id     string
	config *model.Config
	fields model.Fields
}

func schemaNumber(raw json.RawMessage, want int64) bool {
	if jsonKind(raw) != '0' {
		return false
	}
	var number model.Int
	return json.Unmarshal(raw, &number) == nil && int64(number) == want
}

func (r *Repository) convertImportJSON(raw json.RawMessage, resourceRemaps ResourceRemaps) (*model.Document, error) {
	document, err := r.convertImportInput(raw, resourceRemaps)
	if err != nil {
		return nil, inputError(err)
	}
	return document, nil
}

func (r *Repository) convertImportInput(raw json.RawMessage, resourceRemaps ResourceRemaps) (*model.Document, error) {
	if jsonKind(raw) != '{' {
		return nil, validationError(msgImportObject)
	}
	var root model.Fields
	if err := root.UnmarshalJSON(raw); err != nil {
		return nil, validationError(msgImportObject)
	}
	version, hasVersion := root.Get("schema_version")
	if schemaNumber(version, SchemaVersion) {
		if err := checkDocumentInput(raw); err != nil {
			return nil, err
		}
		document, _, err := model.DecodeDocument(raw)
		if err != nil {
			return nil, inputError(err)
		}
		if err := document.System.Clear("agent_registration_sessions"); err != nil {
			return nil, err
		}
		if err := validateDocument(document); err != nil {
			return nil, err
		}
		return document, nil
	}
	var oldSystem model.Fields
	var oldProfiles model.Fields
	metadata := map[string]model.Fields{}
	version2 := schemaNumber(version, 2)
	if version2 {
		systemRaw, _ := root.Get("system")
		profilesRaw, _ := root.Get("profiles")
		if jsonKind(systemRaw) != '{' || jsonKind(profilesRaw) != '{' {
			return nil, validationError(msgLegacyBackupSections)
		}
		if err := oldSystem.UnmarshalJSON(systemRaw); err != nil {
			return nil, err
		}
		if err := oldProfiles.UnmarshalJSON(profilesRaw); err != nil {
			return nil, err
		}
		v, _ := oldSystem.Get("schema_version")
		if !schemaNumber(v, 2) {
			return nil, validationError(msgLegacyIndexVersion)
		}
		indexRaw, _ := oldSystem.Get("profiles")
		index, err := itemsOf(indexRaw, msgLegacyIndexLabel)
		if err != nil {
			return nil, err
		}
		if !index.has(DefaultProfileID) {
			return nil, validationError(msgLegacyIndexDefault)
		}
		if len(index.ids) != oldProfiles.Len() {
			return nil, validationError(msgLegacyIndexContents)
		}
		for _, id := range index.ids {
			if err := ValidateProfileID(id); err != nil {
				return nil, err
			}
			if !oldProfiles.Has(id) {
				return nil, validationError(msgLegacyIndexContents)
			}
			metadata[id] = index.byID[id].fields
		}
	} else if (!hasVersion || jsonKind(version) == 'n') && !root.Has("profiles") && !root.Has("shared") && !root.Has("resource_refs") && !root.Has("node_dialers") {
		oldSystem = root
		if err := oldProfiles.Set(DefaultProfileID, raw); err != nil {
			return nil, err
		}
	} else {
		return nil, validationError(msgImportFormats)
	}
	defaults, err := r.profileDefaultsJSON()
	if err != nil {
		return nil, err
	}
	profiles := make([]importProfile, 0, oldProfiles.Len())
	for id, raw := range oldProfiles.All() {
		if err := ValidateProfileID(id); err != nil {
			return nil, err
		}
		if jsonKind(raw) != '{' {
			return nil, validationError(msgLegacyProfileObject)
		}
		var fields model.Fields
		if err := fields.UnmarshalJSON(raw); err != nil {
			return nil, err
		}
		for _, key := range []string{"resource_refs", "node_dialers", "schema_version", "shared", "profiles"} {
			if fields.Has(key) {
				return nil, validationError(msgLegacyIntermediateFields)
			}
		}
		merged, err := mergeObjectsJSON(defaults, raw)
		if err != nil {
			return nil, err
		}
		if err := fields.UnmarshalJSON(merged); err != nil {
			return nil, err
		}
		for key, value := range metadata[id].All() {
			if err := fields.Set(key, value); err != nil {
				return nil, err
			}
		}
		merged, err = fields.MarshalJSON()
		if err != nil {
			return nil, err
		}
		config, err := model.DecodeConfig(merged)
		if err != nil {
			return nil, err
		}
		profiles = append(profiles, importProfile{id, config, fields})
	}
	if version2 {
		if err := scopeImportedDialers(profiles); err != nil {
			return nil, err
		}
	}
	var systemFields model.Fields
	for _, key := range systemSections {
		value, present := oldSystem.Get(key)
		if !present {
			value = []byte(`{}`)
			if key == "agents" {
				value = []byte(`[]`)
			}
		}
		if err := systemFields.Set(key, value); err != nil {
			return nil, err
		}
	}
	revision, present := oldSystem.Get("_revision")
	if !present {
		revision = []byte(`0`)
	}
	if err := systemFields.Set("_revision", revision); err != nil {
		return nil, err
	}
	settings, _ := systemFields.Get("system_config")
	if jsonKind(settings) != '{' {
		return nil, validationError(msgSystemSettingsObject)
	}
	var settingsFields model.Fields
	if err := settingsFields.UnmarshalJSON(settings); err != nil {
		return nil, err
	}
	for _, key := range []string{"server_domain", "github_proxy_domain", "rule_fetch_proxy"} {
		value, present := settingsFields.Get(key)
		if !present || (key == "github_proxy_domain" && jsonKind(value) != '"') {
			if err := settingsFields.Set(key, []byte(`""`)); err != nil {
				return nil, err
			}
		}
	}
	settings, err = settingsFields.MarshalJSON()
	if err != nil {
		return nil, err
	}
	if err := systemFields.Set("system_config", settings); err != nil {
		return nil, err
	}
	systemRaw, err := systemFields.MarshalJSON()
	if err != nil {
		return nil, err
	}
	// Validate the raw Agent list before decoding it.
	agentRaw, _ := systemFields.Get("agents")
	if err := requireObjectList(agentRaw); err != nil {
		return nil, err
	}
	var system model.System
	if err := json.Unmarshal(systemRaw, &system); err != nil {
		return nil, err
	}
	for i := range system.Agents {
		if _, present, err := recordMemberJSON(&system.Agents[i], "profile_id"); err != nil {
			return nil, err
		} else if !version2 || !present {
			system.Agents[i].ProfileID = DefaultProfileID
		}
	}
	document := &model.Document{SchemaVersion: SchemaVersion, System: system, Profiles: []*model.Profile{}}
	catalogs := newImportCatalogs(&document.Shared)
	for _, source := range profiles {
		if err := r.importOldProfile(document, source, catalogs, resourceRemaps); err != nil {
			return nil, err
		}
	}
	return document, nil
}

func requireObjectList(raw json.RawMessage) error {
	var items []json.RawMessage
	if jsonKind(raw) != '[' || json.Unmarshal(raw, &items) != nil {
		return validationError(msgLegacyShape)
	}
	for _, item := range items {
		if jsonKind(item) != '{' {
			return validationError(msgLegacyShape)
		}
	}
	return nil
}

// canonicalImportValue delegates transient identity encoding to the shared codec.
// Its inputs have already passed JSON decoding, so encoding cannot fail.
func canonicalImportValue(value any) string {
	raw, err := model.Marshal(value)
	if err != nil {
		return ""
	}
	raw, err = jsonresp.Canonical(raw)
	if err != nil {
		return ""
	}
	return string(raw)
}

type importCatalogs struct {
	shared     *model.Shared
	records    map[string]map[string]json.RawMessage
	identities map[string]map[string]string
	names      map[string]map[string]bool
}

func newImportCatalogs(shared *model.Shared) *importCatalogs {
	c := &importCatalogs{shared: shared, records: map[string]map[string]json.RawMessage{}, identities: map[string]map[string]string{}, names: map[string]map[string]bool{}}
	for _, kind := range SharedFields {
		c.records[kind] = map[string]json.RawMessage{}
		c.identities[kind] = map[string]string{}
		c.names[kind] = map[string]bool{}
	}
	c.names[kindAggregations] = c.names[kindSubscriptions]
	return c
}

type importResource interface {
	MarshalJSON() ([]byte, error)
}

func (c *importCatalogs) add(kind string, resource importResource, oldID, name, profileID string) (string, error) {
	if kind == kindRuleLibrary {
		raw, _, err := recordMemberJSON(resource, "name")
		if err != nil {
			return "", err
		}
		if jsonKind(raw) != '"' && jsonTruthy(raw) {
			return "", validationError(msgLegacyRuleName)
		}
	}
	encoded, err := model.Marshal(resource)
	if err != nil {
		return "", err
	}
	identityJSON, err := jsonresp.Canonical(encoded)
	if err != nil {
		return "", err
	}
	identity := oldID + "\x00" + string(identityJSON)
	if id, ok := c.identities[kind][identity]; ok {
		return id, nil
	}
	id := oldID
	if _, taken := c.records[kind][id]; taken {
		for {
			id = cutRunes(oldID, 40) + "_" + randomHex(12)
			if _, taken := c.records[kind][id]; !taken {
				break
			}
		}
		raw, _ := json.Marshal(id)
		if err := setRecordMemberJSON(resource, "id", raw); err != nil {
			return "", err
		}
	}
	nameKey := func(text string) string {
		if kind == kindRuleLibrary {
			return rules.SanitizeRuleName(text)
		}
		return text
	}
	key := nameKey(name)
	if kind != kindNodes && name != "" && c.names[kind][key] {
		suffix := profileID
		for index := 1; c.names[kind][key]; {
			next := cutRunes(name, max(1, 196-len([]rune(suffix)))) + " (" + suffix + ")"
			raw, _ := json.Marshal(next)
			if err := setRecordMemberJSON(resource, "name", raw); err != nil {
				return "", err
			}
			key = nameKey(next)
			index++
			suffix = profileID + "-" + strconv.Itoa(index)
		}
	}
	raw, err := model.Marshal(resource)
	if err != nil {
		return "", err
	}
	switch kind {
	case kindSubscriptions:
		var item model.Subscription
		if err := json.Unmarshal(raw, &item); err != nil {
			return "", err
		}
		c.shared.Subscriptions = append(c.shared.Subscriptions, item)
	case kindNodes:
		var item model.Node
		if err := json.Unmarshal(raw, &item); err != nil {
			return "", err
		}
		c.shared.Nodes = append(c.shared.Nodes, item)
	case kindAggregations:
		var item model.Aggregation
		if err := json.Unmarshal(raw, &item); err != nil {
			return "", err
		}
		c.shared.Aggregations = append(c.shared.Aggregations, item)
	case kindRuleLibrary:
		var item model.RuleLibraryItem
		if err := json.Unmarshal(raw, &item); err != nil {
			return "", err
		}
		c.shared.RuleLibrary = append(c.shared.RuleLibrary, item)
	}
	c.records[kind][id] = raw
	c.identities[kind][identity] = id
	c.names[kind][key] = true
	return id, nil
}

func (r *Repository) importOldProfile(document *model.Document, source importProfile, catalogs *importCatalogs, resourceRemaps ResourceRemaps) error {
	config := source.config
	remaps := map[string]map[string]string{}
	for _, kind := range SharedFields {
		remaps[kind] = map[string]string{}
	}
	remap := func(kind, id string) (string, error) {
		if kind == kindNodes && isBuiltinPolicy(id) {
			return id, nil
		}
		next, present := remaps[kind][id]
		if !present {
			return "", validationError(fmt.Sprintf(msgLegacyMissingResource, source.id, kind, id))
		}
		return next, nil
	}
	remapList := func(kind string, ids []string) ([]string, error) {
		next := make([]string, len(ids))
		for i, id := range ids {
			var err error
			next[i], err = remap(kind, id)
			if err != nil {
				return nil, err
			}
		}
		return next, nil
	}
	// Assign missing IDs through the model codec; null/wrong IDs are not repaired.
	prepare := func(kind string, records []importResource) error {
		for _, record := range records {
			if _, present, err := recordMemberJSON(record, "id"); err != nil {
				return err
			} else if !present {
				raw, _ := json.Marshal(kind + "_" + randomHex(12))
				if err := setRecordMemberJSON(record, "id", raw); err != nil {
					return err
				}
			}
		}
		raw, err := json.Marshal(records)
		if err != nil {
			return err
		}
		_, err = itemsOf(raw, kind)
		return err
	}
	for _, kind := range SharedFields {
		raw, _ := source.fields.Get(kind)
		if err := requireObjectList(raw); err != nil {
			return validationError(fmt.Sprintf(msgResourceObjects, kind))
		}
		records := []importResource{}
		switch kind {
		case kindSubscriptions:
			for i := range config.Subscriptions {
				records = append(records, &config.Subscriptions[i])
			}
		case kindNodes:
			for i := range config.Nodes {
				records = append(records, &config.Nodes[i])
			}
		case kindAggregations:
			for i := range config.Aggregations {
				records = append(records, &config.Aggregations[i])
			}
		case kindRuleLibrary:
			for i := range config.RuleLibrary {
				records = append(records, &config.RuleLibrary[i])
			}
		}
		if err := prepare(kind, records); err != nil {
			return err
		}
		for _, originalResource := range records {
			resource, err := copyImportResource(originalResource)
			if err != nil {
				return err
			}
			idRaw, _, err := recordMemberJSON(resource, "id")
			if err != nil {
				return err
			}
			oldID, _ := jsonString(idRaw)
			nameRaw, _, err := recordMemberJSON(resource, "name")
			if err != nil {
				return err
			}
			name, _ := jsonString(nameRaw)
			switch item := resource.(type) {
			case *model.Node:
				if raw, _, err := recordMemberJSON(item, "subscription_id"); err != nil {
					return err
				} else if jsonTruthy(raw) {
					if _, ok := jsonString(raw); !ok {
						return validationError(fmt.Sprintf(msgLegacyMissingResource, source.id, kindSubscriptions, displayJSON(raw)))
					}
					id, err := remap(kindSubscriptions, item.SubscriptionID)
					if err != nil {
						return err
					}
					item.SubscriptionID = id
				}
			case *model.Aggregation:
				for _, field := range []string{"subscriptions", "nodes"} {
					raw, present, err := recordMemberJSON(item, field)
					if err != nil {
						return err
					}
					if !present {
						continue
					}
					ids, err := idsOf(raw, true, field)
					if err != nil {
						return err
					}
					next, err := remapList(field, ids)
					if err != nil {
						return err
					}
					raw, _ = json.Marshal(next)
					if err := setRecordMemberJSON(item, field, raw); err != nil {
						return err
					}
				}
			}
			id, err := catalogs.add(kind, resource, oldID, name, source.id)
			if err != nil {
				return err
			}
			remaps[kind][oldID] = id
		}
	}
	if resourceRemaps != nil {
		resourceRemaps[source.id] = remaps
	}
	name := source.id
	if name == DefaultProfileID {
		name = defaultProfileName
	}
	if raw, present := source.fields.Get("name"); present {
		name = displayJSON(raw)
	}
	description := ""
	if raw, present := source.fields.Get("description"); present {
		description = displayJSON(raw)
	}
	profile, err := r.emptyProfileRecord(source.id, name, description)
	if err != nil {
		return err
	}
	for _, key := range []string{"name", "description"} {
		if raw, present := source.fields.Get(key); present {
			if err := profile.SetJSON(key, raw); err != nil {
				return err
			}
		}
	}
	profileRaw, err := model.Marshal(config.Profile)
	if err != nil {
		return err
	}
	var fields model.Fields
	if err := fields.UnmarshalJSON(profileRaw); err != nil {
		return err
	}
	for _, key := range ProfileFields {
		if raw, present := fields.Get(key); present {
			if err := profile.SetJSON(key, raw); err != nil {
				return err
			}
		}
	}
	// The decoded profile always has its metadata members; only metadata the
	// source carries replaces the new record's.
	for _, key := range []string{"created_at", "updated_at", "_revision"} {
		if raw, present := source.fields.Get(key); present {
			if err := profile.SetJSON(key, raw); err != nil {
				return err
			}
		}
	}
	// The source's own list: the decoded profile reads a wrong-typed list as
	// empty (O18 T1a).
	groupsRaw, present := source.fields.Get("proxy_groups")
	if !present {
		groupsRaw = json.RawMessage(`[]`)
	}
	if err := requireObjectList(groupsRaw); err != nil {
		return err
	}
	groupIDs := map[string]bool{}
	for i := range profile.ProxyGroups {
		group := &profile.ProxyGroups[i]
		for _, pair := range groupResourceFields {
			raw, present, err := recordMemberJSON(group, pair[0])
			if err != nil {
				return err
			}
			if !present {
				continue
			}
			ids, err := idsOf(raw, true, pair[0])
			if err != nil {
				return err
			}
			next, err := remapList(pair[1], ids)
			if err != nil {
				return err
			}
			raw, _ = json.Marshal(next)
			if err := group.SetJSON(pair[0], raw); err != nil {
				return err
			}
		}
		if kind := importGroupSource(group.Source); kind != "" {
			if raw, present, err := recordMemberJSON(group, "proxies"); err != nil {
				return err
			} else if present {
				ids, err := idsOf(raw, true, "proxies")
				if err != nil {
					return err
				}
				next, err := remapList(kind, ids)
				if err != nil {
					return err
				}
				raw, _ = json.Marshal(next)
				if err := group.SetJSON("proxies", raw); err != nil {
					return err
				}
			}
		}
		groupIDs[group.ID] = true
	}
	for i := range profile.ProxyGroups {
		group := &profile.ProxyGroups[i]
		raw, present, err := recordMemberJSON(group, "proxies_order")
		if err != nil {
			return err
		}
		if !present || jsonKind(raw) != '[' {
			continue
		}
		var order []json.RawMessage
		if err := json.Unmarshal(raw, &order); err != nil {
			return err
		}
		kept := []json.RawMessage{}
		for _, entry := range order {
			if jsonKind(entry) != '{' {
				continue
			}
			var ref model.ResourceRef
			if err := json.Unmarshal(entry, &ref); err != nil {
				return err
			}
			fields := viewOfJSON(entry)
			idRaw, _ := fields.get("id")
			id, isString := jsonString(idRaw)
			if ref.Type == "strategy" {
				if !groupIDs[ref.ID] || ref.ID == group.ID {
					continue
				}
			} else {
				kind := sourceKinds[ref.Type]
				if kind == "" {
					continue
				}
				if !(kind == kindNodes && isString && isBuiltinPolicy(id)) {
					next, present := remaps[kind][id]
					if !isString || !present {
						continue
					}
					ref.ID = next
					encoded, _ := json.Marshal(next)
					if err := fields.fields.Set("id", encoded); err != nil {
						return err
					}
					entry, err = fields.fields.MarshalJSON()
					if err != nil {
						return err
					}
				}
			}
			kept = append(kept, entry)
		}
		raw, err = json.Marshal(kept)
		if err != nil {
			return err
		}

		if err := group.SetJSON("proxies_order", raw); err != nil {
			return err
		}
	}
	ruleConfigsRaw, _, err := recordMemberJSON(profile, "rule_configs")
	if err != nil {
		return err
	}
	if jsonTruthy(ruleConfigsRaw) {
		if err := requireObjectList(ruleConfigsRaw); err != nil {
			return err
		}
	}
	if !jsonTruthy(ruleConfigsRaw) {
		for _, pair := range [][2]string{{"rules", "rule"}, {"rule_sets", "ruleset"}} {
			raw, present := source.fields.Get(pair[0])
			if !present {
				continue
			}
			if err := requireObjectList(raw); err != nil {
				return err
			}
			var entries []model.RuleConfig
			if err := json.Unmarshal(raw, &entries); err != nil {
				return err
			}
			for i := range entries {
				entries[i].ItemType = pair[1]
			}
			profile.RuleConfigs = append(profile.RuleConfigs, entries...)
		}
	}
	ruleNames := map[string]string{}
	for i := range config.RuleLibrary {
		original := &config.RuleLibrary[i]
		raw, present, err := recordMemberJSON(original, "name")
		if err != nil {
			return err
		}
		if !present {
			return validationError(msgLegacyRuleName)
		}
		var item model.RuleLibraryItem
		if err := json.Unmarshal(catalogs.records[kindRuleLibrary][remaps[kindRuleLibrary][original.ID]], &item); err != nil {
			return err
		}
		ruleNames[canonicalImportValue(raw)] = item.Name
	}
	for i := range profile.RuleConfigs {
		rule := &profile.RuleConfigs[i]
		if _, present, err := recordMemberJSON(rule, "id"); err != nil {
			return err
		} else if !present {
			rule.ID = "rule_" + randomHex(12)
		}
		if rule.ItemType != "ruleset" {
			continue
		}
		libraryID := ""
		if rule.LibraryRuleID != "" {
			libraryID, err = remap(kindRuleLibrary, rule.LibraryRuleID)
			if err != nil {
				return err
			}
			var original *model.RuleLibraryItem
			for j := range config.RuleLibrary {
				if config.RuleLibrary[j].ID == rule.LibraryRuleID {
					original = &config.RuleLibrary[j]
					break
				}
			}
			if original == nil {
				return validationError(msgLegacyRuleName)
			}
			variant, err := copyRecord(original)
			if err != nil {
				return err
			}
			changed := false
			for _, key := range []string{"name", "behavior", "format"} {
				raw, present, err := recordMemberJSON(rule, key)
				if err != nil {
					return err
				}
				previous, _, err := recordMemberJSON(original, key)
				if err != nil {
					return err
				}
				if present && canonicalImportValue(raw) != canonicalImportValue(previous) {
					changed = true
					if err := setRecordMemberJSON(variant, key, raw); err != nil {
						return err
					}
				}
			}
			if changed {
				variant.ID = "migrated_rule_" + rule.ID
				libraryID, err = catalogs.add(kindRuleLibrary, variant, variant.ID, variant.Name, source.id)
				if err != nil {
					return err
				}
			}
		} else {
			var item model.RuleLibraryItem
			for _, key := range RuleSourceFields {
				if raw, present, err := recordMemberJSON(rule, key); err != nil {
					return err
				} else if present {
					if err := setRecordMemberJSON(&item, key, raw); err != nil {
						return err
					}
				}
			}
			item.ID = "migrated_rule_" + rule.ID
			enabled := true
			item.Enabled = &enabled
			if raw, present, err := recordMemberJSON(item, "name"); err != nil {
				return err
			} else if !present {
				if jsonTruthy(raw) {
					item.Name = displayJSON(raw)
				} else {
					item.Name = item.ID
				}
			}
			if _, present, err := recordMemberJSON(item, "source_type"); err != nil {
				return err
			} else if !present {
				value := "url"
				if item.Content != nil && *item.Content != "" {
					value = "content"
				}
				item.SourceType = &value
			}
			libraryID, err = catalogs.add(kindRuleLibrary, &item, item.ID, item.Name, source.id)
			if err != nil {
				return err
			}
		}
		if raw, _, err := recordMemberJSON(rule, "name"); err != nil {
			return err
		} else if jsonTruthy(raw) {
			var item model.RuleLibraryItem
			if err := json.Unmarshal(catalogs.records[kindRuleLibrary][libraryID], &item); err != nil {
				return err
			}
			ruleNames[canonicalImportValue(raw)] = item.Name
		}
		rule.LibraryRuleID = libraryID
		for _, key := range RuleSourceFields {
			if err := rule.Clear(key); err != nil {
				return err
			}
		}
	}
	itemTypes := map[string]string{}
	for i := range profile.RuleConfigs {
		rule := &profile.RuleConfigs[i]
		if rule.ItemType == "rule" && rule.RuleType == "RULE-SET" {
			raw, _, err := recordMemberJSON(rule, "value")
			if err != nil {
				return err
			}
			if next, found := ruleNames[canonicalImportValue(raw)]; found {
				raw, _ = json.Marshal(next)
			}
			if len(raw) == 0 {
				raw = []byte(`null`)
			}
			if err := setRecordMemberJSON(rule, "value", raw); err != nil {
				return err
			}
		}
		itemTypes[rule.ID] = rule.ItemType
	}
	for _, engine := range []string{"mosdns", "oxidns"} {
		raw, _, err := recordMemberJSON(profile, engine)
		if err != nil {
			return err
		}
		if jsonKind(raw) != '{' {
			continue
		}
		var dns model.Fields
		if err := dns.UnmarshalJSON(raw); err != nil {
			return err
		}
		for _, key := range []string{"direct_rulesets", "proxy_rulesets", "direct_rules", "proxy_rules"} {
			raw, present := dns.Get(key)
			if !present || jsonKind(raw) != '[' {
				continue
			}
			var ids []json.RawMessage
			if err := json.Unmarshal(raw, &ids); err != nil {
				return err
			}
			expected := "rule"
			if strings.HasSuffix(key, "rulesets") {
				expected = "ruleset"
			}
			kept := []string{}
			for _, raw := range ids {
				if id, ok := jsonString(raw); ok && itemTypes[id] == expected {
					kept = append(kept, id)
				}
			}
			raw, _ = json.Marshal(kept)
			if err := dns.Set(key, raw); err != nil {
				return err
			}
		}
		raw, err = dns.MarshalJSON()
		if err != nil {
			return err
		}
		if err := profile.SetJSON(engine, raw); err != nil {
			return err
		}
	}
	if raw, present, err := recordMemberJSON(profile.Surge, "smart_groups"); err != nil {
		return err
	} else if present && jsonKind(raw) == '[' {
		var entries []json.RawMessage
		if err := json.Unmarshal(raw, &entries); err != nil {
			return err
		}
		kept := []json.RawMessage{}
		for _, entry := range entries {
			view := viewOfJSON(entry)
			id, _ := view.get("group_id")
			text, ok := jsonString(id)
			if ok && groupIDs[text] {
				kept = append(kept, entry)
			}
		}
		raw, _ = json.Marshal(kept)
		if err := setRecordMemberJSON(&profile.Surge, "smart_groups", raw); err != nil {
			return err
		}
	}
	document.Profiles = append(document.Profiles, profile)
	return nil
}

func scopeImportedDialers(profiles []importProfile) error {
	type edge struct {
		node         *model.Node
		destination  *model.Node
		kind, target string
	}
	definitions := map[string]map[string]bool{}
	groupNames := map[string]bool{}
	usedNames := map[string]bool{"DIRECT": true, "REJECT": true}
	targets := map[string]bool{}
	edges := []edge{}
	for _, source := range profiles {
		config := source.config
		if err := dialerValidation(convert.DialerFailure(convert.ConfigShapes(config, false))); err != nil {
			return err
		}
		subscriptions := map[string]*model.Subscription{}
		for i := range config.Subscriptions {
			subscriptions[config.Subscriptions[i].ID] = &config.Subscriptions[i]
		}
		for i := range config.Nodes {
			node := &config.Nodes[i]
			identity := canonicalImportValue(node) + "\x00"
			if node.SubscriptionID != "" {
				subscription, ok := subscriptions[node.SubscriptionID]
				if !ok {
					return validationError(fmt.Sprintf(msgLegacyNodeSubscription, source.id))
				}
				identity += canonicalImportValue(subscription)
			}
			if definitions[node.Name] == nil {
				definitions[node.Name] = map[string]bool{}
			}
			definitions[node.Name][identity] = true
			usedNames[node.Name] = true
		}
		for _, group := range config.Profile.ProxyGroups {
			groupNames[group.Name] = true
			usedNames[group.Name] = true
		}
		for i := range config.Nodes {
			node := &config.Nodes[i]
			if !model.SwitchEnabled(node, "enabled", node.Enabled, true) {
				continue
			}
			raw, err := convert.NodeDialer(*node)
			if err != nil {
				return convertFailure(err)
			}
			if raw == nil {
				continue
			}
			target := raw.Value
			if raw.ShortTag() != "!!str" || trimText(target) == "" {
				return validationError(msgRawDialerName)
			}
			if isBuiltinPolicy(target) {
				continue
			}
			matches := []edge{}
			for j := range config.Nodes {
				item := &config.Nodes[j]
				if item.Name == target && model.SwitchEnabled(item, "enabled", item.Enabled, true) {
					matches = append(matches, edge{node, item, "node", target})
				}
			}
			for _, item := range config.Profile.ProxyGroups {
				if item.Name == target && model.SwitchEnabled(&item, "enabled", item.Enabled, true) {
					matches = append(matches, edge{node, nil, "group", target})
				}
			}
			if len(matches) != 1 {
				return validationError(fmt.Sprintf(msgLegacyDialerTarget, source.id, target))
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
	for grew := true; grew; {
		grew = false
		for _, item := range edges {
			if item.kind == "node" && scoped[item.target] && targets[item.node.Name] && !scoped[item.node.Name] {
				scoped[item.node.Name] = true
				grew = true
			}
		}
	}
	for _, source := range profiles {
		renamed := map[string]string{}
		for i := range source.config.Nodes {
			node := &source.config.Nodes[i]
			old := node.Name
			if !scoped[old] {
				continue
			}
			name := old + " (" + source.id + ")"
			for index := 2; usedNames[name]; index++ {
				name = old + " (" + source.id + "-" + strconv.Itoa(index) + ")"
			}
			usedNames[name] = true
			node.Name = name
			if model.SwitchEnabled(node, "enabled", node.Enabled, true) {
				renamed[old] = name
			}
		}
		localGroups := map[string]bool{}
		for _, group := range source.config.Profile.ProxyGroups {
			localGroups[group.Name] = true
		}
		for i := range source.config.Profile.RuleConfigs {
			rule := &source.config.Profile.RuleConfigs[i]
			if rule.Policy != nil && renamed[*rule.Policy] != "" && !localGroups[*rule.Policy] {
				value := renamed[*rule.Policy]
				rule.Policy = &value
			}
		}
	}
	for _, item := range edges {
		if item.kind != "node" || item.destination.Name == item.target {
			continue
		}
		name := item.destination.Name
		if item.node.ProxyString != nil && *item.node.ProxyString != "" {
			proxy, err := convert.StructuredProxy(*item.node.ProxyString)
			if err != nil {
				return convertFailure(err)
			}
			if proxy == nil {
				return validationError(msgLegacyDialerProxyString)
			}
			yamlx.Set(proxy, "dialer-proxy", yamlx.Str(name))
			raw, err := yamlx.ToJSON(proxy)
			if err != nil {
				return err
			}
			text := string(raw)
			item.node.ProxyString = &text
		} else {
			node, err := item.node.Params.Node()
			if err != nil {
				return err
			}
			yamlx.Set(node, "dialer-proxy", yamlx.Str(name))
			if err := item.node.Params.SetNode(node); err != nil {
				return err
			}
		}
	}
	return nil
}

func importGroupSource(source *string) string {
	if source == nil {
		return ""
	}
	return sourceKinds[*source]
}

func copyImportResource(resource importResource) (importResource, error) {
	switch value := resource.(type) {
	case *model.Subscription:
		return copyRecord(value)
	case *model.Node:
		return copyRecord(value)
	case *model.Aggregation:
		return copyRecord(value)
	case *model.RuleLibraryItem:
		return copyRecord(value)
	default:
		return nil, fmt.Errorf("copy import resource: unsupported record %T", resource)
	}
}
