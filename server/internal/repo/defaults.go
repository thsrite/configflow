package repo

import (
	"bytes"
	_ "embed"
	"encoding/json"

	"configflow/internal/model"
)

// configTemplate is a copy of backend/config_template.json, the first-start
// template (go:embed cannot reach outside the package directory;
// TestConfigTemplateMatchesBackend keeps the copy identical).
//
//go:embed config_template.json
var configTemplate []byte

// Field groups (module constants of config_repository.py).
var (
	SharedFields   = []string{"subscriptions", "nodes", "subscription_aggregations", "rule_library"}
	ResourceFields = SharedFields[:3]
	ProfileFields  = []string{"proxy_groups", "rule_configs", "mihomo", "surge", "mosdns", "oxidns", "loon",
		"domain_discovery", "routers", "device_notes", "client_fixed_ips"}
	RuleSourceFields       = []string{"name", "url", "behavior", "content", "source_type", "format"}
	RuleDNSFields          = []string{"rule_actions", "rule_dns_follow", "rule_dns_parked"}
	RouterIdentityFields   = []string{"type", "endpoint"}
	RouterConnectionFields = []string{"type", "endpoint", "username", "password", "verify_tls"}
	RouterManifestFields   = []string{"ros_policy_state", "dns_guard_state", "ipv6_fakeip_state", "ipv6_routes_state",
		"ipv6_pd_guard_state"}
	AgentHistoryFields = []string{"deployments", "ipv6_verifications"}
	systemSections     = []string{"system_config", "backup", "agents"}
)

// profileObjectFields are the profile fields whose missing default is {}.
var profileObjectFields = map[string]bool{"mihomo": true, "surge": true, "mosdns": true, "oxidns": true, "loon": true,
	"domain_discovery": true, "routers": true, "device_notes": true, "client_fixed_ips": true}

// IsProfileField reports whether key is one of ProfileFields.
func IsProfileField(key string) bool {
	return profileObjectFields[key] || key == "proxy_groups" || key == "rule_configs"
}

func isBuiltinPolicy(value any) bool {
	return value == "DIRECT" || value == "REJECT"
}

//go:embed oxidns_defaults.json
var oxidnsDefaults []byte

// defaultOxidnsJSON strips formatting from the built-in source before it
// becomes stored data. Existing user documents retain their original bytes.
func defaultOxidnsJSON() json.RawMessage {
	var data bytes.Buffer
	_ = json.Compact(&data, oxidnsDefaults)
	return data.Bytes()
}

// DefaultOxidnsSettings returns the built-in schema-free OxiDNS settings.
func DefaultOxidnsSettings() model.Dynamic {
	var settings model.Dynamic
	// The embedded constant is verified by TestConfigTemplateMatchesBackend and defaults tests.
	_ = settings.UnmarshalJSON(defaultOxidnsJSON())
	return settings
}

// defaultConfigJSON contains the stored defaults without resolved view metadata.
func defaultConfigJSON() json.RawMessage {
	raw := []byte(`{"subscriptions":[],"nodes":[],"rule_configs":[],"proxy_groups":[],"rule_library":[],"system_config":{"server_domain":"","github_proxy_domain":"","rule_fetch_proxy":""},"subscription_aggregations":[],"mihomo":{"custom_config":""},"mosdns":{"direct_rulesets":[],"proxy_rulesets":[],"direct_rules":[],"proxy_rules":[],"local_dns":"","remote_dns":"","fallback_dns":"","default_forward":"forward_remote","custom_hosts":"","custom_config":"","custom_matches":[],"custom_match_position":"tail","cache_enabled":true,"cache_size":10240,"cache_lazy_ttl":21600,"cache_dump_enabled":true,"cache_dump_file":"./cache.dump","cache_dump_interval":300}}`)
	var fields model.Fields
	_ = fields.UnmarshalJSON(raw)
	_ = fields.Set("oxidns", defaultOxidnsJSON())
	result, _ := fields.MarshalJSON()
	return result
}

// DefaultConfig returns caller-owned built-in settings and empty resource catalogs.
func DefaultConfig() *model.Config {
	config, _ := model.DecodeConfig(defaultConfigJSON())
	return config
}

// InitialConfig returns built-in settings overlaid with the first-start template.
func InitialConfig() *model.Config {
	base := defaultConfigJSON()
	merged, _ := mergeObjectsJSON(base, configTemplate)
	config, _ := model.DecodeConfig(merged)
	return config
}

func defaultProfileRecord() (*model.Profile, error) {
	config := DefaultConfig()
	raw, err := model.Marshal(config.Profile)
	if err != nil {
		return nil, err
	}
	var fields model.Fields
	if err := fields.UnmarshalJSON(raw); err != nil {
		return nil, err
	}
	for key, value := range map[string]json.RawMessage{"surge": []byte(`{"custom_config":"","smart_groups":[]}`), "loon": []byte(`{"custom_config":""}`), "routers": []byte(`{"items":[]}`)} {
		if err := fields.Set(key, value); err != nil {
			return nil, err
		}
	}
	data, err := fields.MarshalJSON()
	if err != nil {
		return nil, err
	}
	var profile model.Profile
	err = json.Unmarshal(data, &profile)
	return &profile, err
}

func (r *Repository) profileDefaultsJSON() (json.RawMessage, error) {
	profile, err := defaultProfileRecord()
	if err != nil {
		return nil, err
	}
	raw, err := model.Marshal(profile)
	if err != nil {
		return nil, err
	}
	var fields model.Fields
	if err := fields.UnmarshalJSON(raw); err != nil {
		return nil, err
	}
	for _, key := range SharedFields {
		if err := fields.Set(key, []byte(`[]`)); err != nil {
			return nil, err
		}
	}
	base, err := fields.MarshalJSON()
	if err != nil {
		return nil, err
	}
	var override json.RawMessage
	if r.options.DefaultSettings != nil {
		override, err = configSettingsJSON(r.options.DefaultSettings())
	} else {
		override, err = legacyDefaultSettings(r.options)
	}
	if err != nil {
		return nil, err
	}
	return mergeObjectsJSON(base, override)
}
func (r *Repository) initialSettingsJSON() (json.RawMessage, error) {
	if r.options.InitialSettings != nil {
		return configSettingsJSON(r.options.InitialSettings())
	}
	if raw, present, err := legacyInitialSettings(r.options); present || err != nil {
		return raw, err
	}
	return r.profileDefaultsJSON()
}
func (r *Repository) emptyProfileRecord(id, name, description string) (*model.Profile, error) {
	raw, err := r.profileDefaultsJSON()
	if err != nil {
		return nil, err
	}
	var defaults model.Fields
	if err := defaults.UnmarshalJSON(raw); err != nil {
		return nil, err
	}
	var fields model.Fields
	for _, key := range ProfileFields {
		value, present := defaults.Get(key)
		if !present {
			value = []byte(`[]`)
			if profileObjectFields[key] {
				value = []byte(`{}`)
			}
		}
		if err := fields.Set(key, value); err != nil {
			return nil, err
		}
	}
	profileRaw, err := fields.MarshalJSON()
	if err != nil {
		return nil, err
	}
	var profile model.Profile
	if err := json.Unmarshal(profileRaw, &profile); err != nil {
		return nil, err
	}
	profile.ID, profile.Name, profile.Description = id, name, description
	profile.CreatedAt, profile.UpdatedAt = nowISO(), nowISO()
	descriptionRaw, _ := json.Marshal(description)
	if err := profile.SetJSON("description", descriptionRaw); err != nil {
		return nil, err
	}
	if err := profile.SetJSON("_revision", []byte(`0`)); err != nil {
		return nil, err
	}
	return &profile, nil
}

// configSettingsJSON emits settings rather than a resolved reader's envelope.
// profile_id is a view selector; an empty backup has the same import default.
func configSettingsJSON(config *model.Config) (json.RawMessage, error) {
	raw, err := model.Marshal(config)
	if err != nil {
		return nil, err
	}
	var fields model.Fields
	if err := fields.UnmarshalJSON(raw); err != nil {
		return nil, err
	}
	fields.Delete("profile_id")
	if backup, present := fields.Get("backup"); present {
		var settings model.Fields
		if jsonKind(backup) == '{' && settings.UnmarshalJSON(backup) == nil && settings.Len() == 0 {
			fields.Delete("backup")
		}
	}
	return fields.MarshalJSON()
}
