package repo

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"configflow/internal/ctxx"
	"configflow/internal/model"
)

// ProfileInfo is public profile metadata without credentials or resources.
type ProfileInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// ProfileCreation supplies validated metadata for creation or cloning.
type ProfileCreation struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// ProfileMetadataUpdate changes only the supplied metadata members.
type ProfileMetadataUpdate struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
}

func profileInfo(profile *model.Profile) ProfileInfo {
	return ProfileInfo{profile.ID, profile.Name, profile.Description, profile.CreatedAt, profile.UpdatedAt}
}

// ProfileMetadataRecord returns the public metadata of one profile.
func (r *Repository) ProfileMetadataRecord(ctx context.Context, id string) (ProfileInfo, error) {
	r.metadataReads.Add(1)
	var result ProfileInfo
	err := r.View(ctx, func(document *model.Document) error {
		profile, err := profileFor(document, id)
		if err != nil {
			return err
		}
		result = profileInfo(profile)
		return nil
	})
	return result, err
}

// CreateProfileRecord creates a profile, optionally cloning settings while
// clearing device ownership and fixed client IPs from the clone.
func (r *Repository) CreateProfileRecord(ctx context.Context, input ProfileCreation, cloneFrom string) (ProfileInfo, error) {
	ctx = ctxx.Detach(ctx)
	id := input.ID
	if id == "" {
		id = "profile_" + randomHex(12)
	}
	if err := ValidateProfileID(id); err != nil {
		return ProfileInfo{}, err
	}
	name := input.Name
	if name == "" {
		name = id
	}
	if trimText(name) == "" {
		return ProfileInfo{}, validationError(msgProfileNameEmpty)
	}
	var profile *model.Profile
	err := r.Update(ctx, func(ctx context.Context, document *model.Document) error {
		if profileByKey(document, id) != nil {
			return exists(id)
		}
		var err error
		if cloneFrom != "" {
			source, err := profileFor(document, cloneFrom)
			if err != nil {
				return err
			}
			profile, err = copyRecord(source)
			if err != nil {
				return err
			}
			if err := profile.SetJSON("routers", []byte(`{"items":[]}`)); err != nil {
				return err
			}
			if err := profile.SetJSON("client_fixed_ips", []byte(`{}`)); err != nil {
				return err
			}
		} else {
			profile, err = r.emptyProfileRecord(id, name, "")
			if err != nil {
				return err
			}
		}
		now := nowISO()
		profile.ID, profile.Name, profile.Description = id, trimText(name), input.Description
		profile.CreatedAt, profile.UpdatedAt, profile.Revision = now, now, 0
		for _, entry := range []struct {
			name  string
			value any
		}{{"id", id}, {"name", trimText(name)}, {"description", input.Description}, {"_revision", 0}, {"created_at", now}, {"updated_at", now}} {
			raw, err := json.Marshal(entry.value)
			if err != nil {
				return err
			}
			if err := profile.SetJSON(entry.name, raw); err != nil {
				return err
			}
		}

		document.Profiles = append(document.Profiles, profile)
		return nil
	})
	if err != nil {
		return ProfileInfo{}, err
	}
	return profileInfo(profile), nil
}

// CloneProfileRecord clones a profile with caller-supplied new metadata.
func (r *Repository) CloneProfileRecord(ctx context.Context, sourceID string, input ProfileCreation) (ProfileInfo, error) {
	return r.CreateProfileRecord(ctx, input, sourceID)
}

// UpdateProfileMetadataRecord updates a name and/or description atomically.
func (r *Repository) UpdateProfileMetadataRecord(ctx context.Context, id string, input ProfileMetadataUpdate) (ProfileInfo, error) {
	ctx = ctxx.Detach(ctx)
	var result ProfileInfo
	err := r.Update(ctx, func(ctx context.Context, document *model.Document) error {
		profile, err := profileFor(document, id)
		if err != nil {
			return err
		}
		if input.Name != nil {
			if trimText(*input.Name) == "" {
				return validationError(msgMetadataStrings)
			}
			raw, err := json.Marshal(trimText(*input.Name))
			if err != nil {
				return err
			}
			if err := profile.SetJSON("name", raw); err != nil {
				return err
			}
		}
		if input.Description != nil {
			raw, err := json.Marshal(*input.Description)
			if err != nil {
				return err
			}
			if err := profile.SetJSON("description", raw); err != nil {
				return err
			}
		}
		profile.Revision++
		profile.UpdatedAt = nowISO()
		result = profileInfo(profile)
		return nil
	})
	return result, err
}

// ExportProfileJSON exports only a profile's stored settings and metadata.
func (r *Repository) ExportProfileJSON(ctx context.Context, id string) (json.RawMessage, error) {
	profile, err := r.Profile(ctx, id)
	if err != nil {
		return nil, err
	}
	return model.Marshal(profile)
}

// ImportProfileJSON imports current profile references, refusing shared copies.
func (r *Repository) ImportProfileJSON(ctx context.Context, id string, raw json.RawMessage) (*model.Profile, error) {
	ctx = ctxx.Detach(ctx)
	if jsonKind(raw) != '{' {
		return nil, validationError("Imported profile must be an object")
	}
	var fields model.Fields
	if err := fields.UnmarshalJSON(raw); err != nil {
		return nil, err
	}
	if inner, present := fields.Get("config"); present {
		if jsonKind(inner) != '{' {
			return nil, validationError(msgProfileImportReferences)
		}
		if err := fields.UnmarshalJSON(inner); err != nil {
			return nil, err
		}
	}
	for _, key := range SharedFields {
		if fields.Has(key) {
			return nil, validationError(msgProfileImportReferences)
		}
	}
	if fields.Has("resource_refs") || fields.Has("node_dialers") {
		return nil, validationError(msgProfileImportFormat)
	}
	var saved *model.Profile
	err := r.Update(ctx, func(ctx context.Context, document *model.Document) error {
		var err error
		saved, err = updateProfileIn(document, id, func(profile *model.Profile) error {
			if raw, present := fields.Get("_revision"); present {
				current, _, err := recordMemberJSON(profile, "_revision")
				if err != nil {
					return err
				}
				if !sameStored(raw, current) {
					return inUse(msgProfileChanged)
				}
			}
			for key, raw := range fields.All() {
				if IsProfileField(key) {
					if err := profile.SetJSON(key, raw); err != nil {
						return err
					}
				}
			}
			return pruneProfileDNS(profile)
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	return copyRecord(saved)
}

func pruneProfileDNS(profile *model.Profile) error {
	raw, _, err := recordMemberJSON(profile, "oxidns")
	if err != nil {
		return err
	}
	if jsonKind(raw) != '{' {
		return nil
	}
	var fields model.Fields
	if err := fields.UnmarshalJSON(raw); err != nil {
		return err
	}
	rules, _, err := recordMemberJSON(profile, "rule_configs")
	if err != nil {
		return err
	}
	if jsonKind(rules) != '[' {
		return nil
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(rules, &entries); err != nil {
		return err
	}
	ids := map[string]bool{}
	for _, entry := range entries {
		if jsonKind(entry) != '{' {
			continue
		}
		fields := viewOfJSON(entry)
		id, _ := fields.get("id")
		ids[canonicalImportValue(id)] = true
	}

	for _, field := range RuleDNSFields {
		raw, present := fields.Get(field)
		if !present {
			continue
		}
		switch jsonKind(raw) {
		case '{':
			var values model.Fields
			if err := values.UnmarshalJSON(raw); err != nil {
				return err
			}
			for key := range values.All() {
				encoded, _ := json.Marshal(key)
				if !ids[canonicalImportValue(json.RawMessage(encoded))] {
					values.Delete(key)
				}
			}
			raw, err = values.MarshalJSON()
			if err != nil {
				return err
			}
		case '[':
			var values []json.RawMessage
			if err := json.Unmarshal(raw, &values); err != nil {
				return err
			}
			kept := []json.RawMessage{}
			for _, value := range values {
				if ids[canonicalImportValue(value)] {
					kept = append(kept, value)
				}
			}
			raw, err = json.Marshal(kept)
			if err != nil {
				return err
			}
		default:
			continue
		}
		if err := fields.Set(field, raw); err != nil {
			return err
		}
	}
	raw, err = fields.MarshalJSON()
	if err != nil {
		return err
	}
	return profile.SetJSON("oxidns", raw)
}

// ClientProfileImport is a prepared client import. Details keeps public import
// diagnostics opaque; Profile is the importer's profile object (only profile
// settings are accepted; its members are applied as given), Shared additions.
type ClientProfileImport struct {
	Shared  model.Shared
	Profile json.RawMessage
	Details model.Fields
}

// ClientProfileResult returns metadata and the importer's public diagnostics.
type ClientProfileResult struct {
	Profile ProfileInfo
	Details model.Fields
}

// ImportClientProfileRecord creates a profile and appends prepared resources in
// the same transaction. The builder receives an owned shared snapshot.
func (r *Repository) ImportClientProfileRecord(ctx context.Context, input ProfileCreation, build func(context.Context, *model.Shared) (*ClientProfileImport, error)) (*ClientProfileResult, error) {
	ctx = ctxx.Detach(ctx)
	if build == nil {
		return nil, validationError(msgImportResultShape)
	}
	id := input.ID
	if id == "" {
		id = "profile_" + randomHex(12)
	}
	if err := ValidateProfileID(id); err != nil {
		return nil, err
	}
	if trimText(input.Name) == "" {
		return nil, validationError(msgProfileNameEmpty)
	}
	var output *ClientProfileResult
	err := r.Update(ctx, func(ctx context.Context, document *model.Document) error {
		if profileByKey(document, id) != nil {
			return exists(id)
		}
		shared, err := copyRecord(&document.Shared)
		if err != nil {
			return err
		}
		result, err := build(ctx, shared)
		if err != nil {
			return err
		}
		if result == nil {
			return validationError(msgImportResultShape)
		}
		additions, err := recordMembers(&result.Shared)
		if err != nil {
			return err
		}
		adds := false
		for _, field := range SharedFields {
			raw, present := additions.Get(field)
			if !present {
				continue
			}
			if jsonKind(raw) != '[' {
				return validationError(fmt.Sprintf(msgResourceIDRequired, field))
			}
			var extra []json.RawMessage
			if err := json.Unmarshal(raw, &extra); err != nil {
				return err
			}
			if len(extra) == 0 {
				continue
			}
			before, _, err := recordMemberJSON(document.Shared, field)
			if err != nil {
				return err
			}
			var current []json.RawMessage
			if err := json.Unmarshal(before, &current); err != nil {
				return err
			}
			raw, err = json.Marshal(append(slices.Clone(current), extra...))
			if err != nil {
				return err
			}
			if err := document.Shared.SetJSON(field, raw); err != nil {
				return err
			}
			adds = true
		}
		if adds {
			document.Shared.Revision++
		}
		profile, err := r.emptyProfileRecord(id, trimText(input.Name), input.Description)
		if err != nil {
			return err
		}
		if result.Profile != nil {
			if jsonKind(result.Profile) != '{' {
				return validationError(msgImportResultShape)
			}
			var fields model.Fields
			if err := fields.UnmarshalJSON(result.Profile); err != nil {
				return err
			}
			for key, raw := range fields.All() {
				if !IsProfileField(key) {
					return validationError(msgImportUnknownFieldPrefix + key)
				}
				base, _, err := recordMemberJSON(profile, key)
				if err != nil {
					return err
				}
				if jsonKind(base) == '{' {
					if jsonKind(raw) != '{' {
						return validationError(msgImportResultShape)
					}
					raw, err = mergeObjectsJSON(base, raw)
					if err != nil {
						return err
					}
				}
				if err := profile.SetJSON(key, raw); err != nil {
					return err
				}
			}
		}
		document.Profiles = append(document.Profiles, profile)
		output = &ClientProfileResult{profileInfo(profile), result.Details}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return output, nil
}

// AgentBinding is a public Agent identity; opaque scalar tokens preserve malformed
// stored values without exposing credentials or deployment histories.
type AgentBinding struct {
	ID          json.RawMessage `json:"id"`
	Name        json.RawMessage `json:"name"`
	Host        json.RawMessage `json:"host"`
	Port        json.RawMessage `json:"port"`
	ServiceType json.RawMessage `json:"service_type"`
}

// ProfileListing adds revision, default status and Agent bindings to metadata.
type ProfileListing struct {
	ProfileInfo
	Revision    int64          `json:"revision"`
	IsDefault   bool           `json:"is_default"`
	BoundAgents []AgentBinding `json:"bound_agents"`
}

// ProfileBindings is the public profile-to-Agent binding view.
type ProfileBindings struct {
	Profile ProfileInfo    `json:"profile"`
	Agents  []AgentBinding `json:"agents"`
}

// AgentBindingRecord returns an Agent's public identity.
func AgentBindingRecord(agent model.Agent) (AgentBinding, error) {
	return agentBinding(&agent)
}

// agentBinding projects one Agent's public identity: the name falls back to
// the ID, the service type to mihomo, a missing port is null.
func agentBinding(a *model.Agent) (AgentBinding, error) {
	id, err := json.Marshal(a.ID)
	if err != nil {
		return AgentBinding{}, err
	}
	name := id
	if a.Name != "" {
		if name, err = json.Marshal(a.Name); err != nil {
			return AgentBinding{}, err
		}
	}
	host, err := json.Marshal(a.Host)
	if err != nil {
		return AgentBinding{}, err
	}
	// A missing port (zero: no Agent is stored with port 0) is null, as
	// Python's agent.get('port') returned.
	port := json.RawMessage(`null`)
	if a.Port != 0 {
		if port, err = json.Marshal(int64(a.Port)); err != nil {
			return AgentBinding{}, err
		}
	}
	service := json.RawMessage(`"mihomo"`)
	if a.ServiceType != "" {
		if service, err = json.Marshal(a.ServiceType); err != nil {
			return AgentBinding{}, err
		}
	}
	return AgentBinding{id, name, host, port, service}, nil
}

// agentProfileBinding is the profile an Agent is bound to: the default
// profile when it names none.
func agentProfileBinding(document *model.Document, a *model.Agent) string {
	if a.ProfileID != "" {
		return a.ProfileID
	}
	return defaultProfileIn(document)
}

// agentBindingRecords groups the Agents by bound profile.
func agentBindingRecords(document *model.Document) (map[string][]AgentBinding, error) {
	result := map[string][]AgentBinding{}
	for i := range document.System.Agents {
		a := &document.System.Agents[i]
		binding, err := agentBinding(a)
		if err != nil {
			return nil, err
		}
		id := agentProfileBinding(document, a)
		result[id] = append(result[id], binding)
	}
	return result, nil
}

// ListProfileRecords lists public metadata and bindings from one generation.
func (r *Repository) ListProfileRecords(ctx context.Context) ([]ProfileListing, error) {
	result := []ProfileListing{}
	err := r.View(ctx, func(document *model.Document) error {
		bindings, err := agentBindingRecords(document)
		if err != nil {
			return err
		}
		for _, profile := range document.Profiles {
			agents := bindings[profile.Key()]
			if agents == nil {
				agents = []AgentBinding{}
			}
			result = append(result, ProfileListing{profileInfo(profile), int64(profile.Revision) + int64(document.Shared.Revision), profile.Key() == defaultProfileIn(document), agents})
		}
		return nil
	})
	return result, err
}

// ProfileAgentBindingsRecord returns one profile's public Agent bindings.
func (r *Repository) ProfileAgentBindingsRecord(ctx context.Context, id string) (ProfileBindings, error) {
	var result ProfileBindings
	err := r.View(ctx, func(document *model.Document) error {
		profile, err := profileFor(document, id)
		if err != nil {
			return err
		}
		bindings, err := agentBindingRecords(document)
		if err != nil {
			return err
		}
		agents := bindings[id]
		if agents == nil {
			agents = []AgentBinding{}
		}
		result = ProfileBindings{profileInfo(profile), agents}
		return nil
	})
	return result, err
}

// SaveProfileFieldsJSON merges profile settings against the caller's resolved
// baseline. It returns the committed stored profile and generated-config revision.
func (r *Repository) SaveProfileFieldsJSON(ctx context.Context, id string, raw, baseline json.RawMessage) (*model.Profile, int64, error) {
	ctx = ctxx.Detach(ctx)
	if jsonKind(raw) != '{' || (baseline != nil && jsonKind(baseline) != '{') {
		return nil, 0, validationError(msgProfileFieldsObject)
	}
	var proposed, base model.Fields
	if err := proposed.UnmarshalJSON(raw); err != nil {
		return nil, 0, err
	}
	if proposed.Has("resource_refs") || proposed.Has("node_dialers") {
		return nil, 0, validationError(msgProfileComposition)
	}
	if baseline != nil {
		if err := base.UnmarshalJSON(baseline); err != nil {
			return nil, 0, err
		}
	}
	var saved *model.Profile
	var sharedRevision int64
	err := r.UpdateConfig(ctx, id, func(ctx context.Context, config *model.Config) error {
		current, err := recordMembers(config)
		if err != nil {
			return err
		}
		if baseline == nil {
			if revision, present := proposed.Get("_revision"); present {
				stored, _ := current.Get("_revision")
				if !sameStored(revision, stored) {
					return inUse(msgProfileChanged)
				}
			}
		}
		if err := mergeSectionMembers(ProfileFields, proposed, base, config, config.Profile.SetJSON, false); err != nil {
			return err
		}
		document, err := r.viewDocument(ctx)
		if err != nil {
			return err
		}
		saved, err = profileFor(document, id)
		sharedRevision = document.Shared.Revision
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	result, err := copyRecord(saved)
	return result, saved.Revision + sharedRevision, err
}

// MarshalJSON returns the importer's diagnostics with the committed metadata.
func (v ClientProfileResult) MarshalJSON() ([]byte, error) {
	fields := v.Details
	raw, err := json.Marshal(v.Profile)
	if err != nil {
		return nil, err
	}
	if err := fields.Set("profile", raw); err != nil {
		return nil, err
	}
	return fields.MarshalJSON()
}
