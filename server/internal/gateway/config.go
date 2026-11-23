package gateway

import (
	"strings"

	"configflow/internal/model"
	"configflow/internal/weburl"
)

// member is a stored string member: absent, a string, or another JSON value
// (null, a number, ...), whose text reads as "" (model rule R5).
type member struct {
	present  bool   // the key exists
	isString bool   // the value is a string
	text     string // the string; "" for other values
	truthy   bool   // the value is not null, false, 0, "" or empty
}

// or is the member's text, def when it is absent.
func (m member) or(def string) string {
	if !m.present {
		return def
	}
	return m.text
}

// flag is a stored on/off member: absent, or the truthiness of its JSON value
// (true, a non-zero number, a non-empty string/list/object).
type flag struct{ present, on bool }

// or is the flag's value, def when it is absent.
func (f flag) or(def bool) bool {
	if !f.present {
		return def
	}
	return f.on
}

// Rule is a rule_configs entry or a shared rule_library entry as the gateway
// reads it. A rule set's effective declaration (behavior, format, URL,
// inline content, name) is that of its library entry when it has one.
type Rule struct {
	id, itemType, name, libraryRuleID member
	behavior, format, url             member
	sourceType, content               member
	policy                            member
	enabled, libraryEnabled           flag
	// ruleType and value are display text of rule_type/value of a single rule.
	ruleType, value string
	ruleRecord      model.RuleConfig
	libraryRecord   *model.RuleLibraryItem
}

// ID is the entry's id ("" when it has none).
func (r *Rule) ID() string { return r.id.text }

// URL is the entry's url ("" when it has none).
func (r *Rule) URL() string { return r.url.text }

// Name is the entry's name ("" when it has none).
func (r *Rule) Name() string { return r.name.text }

// source is the declaration a rule set uses: its library entry, else itself.
func source(item, library *Rule) *Rule {
	if library != nil {
		return library
	}
	return item
}

// behavior is the rule set's behavior: the library entry's, else the rule
// set's, "domain" when neither declares one.
func behavior(item, library *Rule) string {
	return source(item, library).behavior.or(item.behavior.or("domain"))
}

func ipBehavior(b string) bool { return b == "ipcidr" || b == "classical" }

// isMRS reports a binary MRS declaration: format "mrs" or a URL path ending
// in ".mrs".
func isMRS(r *Rule) bool {
	if r.format.or("") == "mrs" {
		return true
	}
	path := r.url.or("")
	if parsed, err := weburl.Parse(path); err == nil {
		path = weburl.RawPath(parsed)
	}
	return strings.HasSuffix(strings.ToLower(path), ".mrs")
}

// Config is the part of a profile configuration (repo's resolved view: the
// profile plus shared rule library and system settings) that the gateway
// reads. Build it with NewConfig; it is not modified afterwards.
type Config struct {
	profileID        member
	serverDomain     string
	configToken      string
	customConfig     string
	rules            []*Rule
	library          []*Rule
	settings         Settings
	settingsErr      error
	profile          *model.Profile
	system           model.SystemConfig
	nodesSlice       []model.Node
	rawLibrary       []model.RuleLibraryItem
	hasLibrary       bool
	ruleSourceReader RuleSourceReader
	hasCustomReader  bool
}

// Settings is the profile's gateway setting (gateway_settings): an *Error
// when the stored value is malformed.
func (c *Config) Settings() (Settings, error) {
	if c.settingsErr != nil {
		return Settings{}, c.settingsErr
	}
	return Settings{Enabled: c.settings.Enabled, RuleSetIDs: append([]string{}, c.settings.RuleSetIDs...)}, nil
}

// Enabled reports whether the gateway is on (false with a malformed setting).
func (c *Config) Enabled() bool { return c.settingsErr == nil && c.settings.Enabled }

// ProfileID is the configuration's profile ("" for the legacy global view).
func (c *Config) ProfileID() string { return c.profileID.text }

// RuleSet is the rule_configs rule set with this ID, nil when there is none.
func (c *Config) RuleSet(id string) *Rule {
	for _, item := range c.rules {
		if item.id.isString && item.id.text == id && item.itemType.or("") == "ruleset" {
			return item
		}
	}
	return nil
}

// Library is the shared library entry of a rule set, nil when it has none
// or the entry is gone.
func (c *Config) Library(item *Rule) *Rule {
	if !item.libraryRuleID.truthy || !item.libraryRuleID.isString {
		return nil
	}
	for _, entry := range c.library {
		if entry.id.isString && entry.id.text == item.libraryRuleID.text {
			return entry
		}
	}
	return nil
}

// Reason says why a rule set cannot feed the gateway ("" when it can).
// item nil: the rule set no longer exists.
func (c *Config) Reason(item *Rule) string {
	if item == nil {
		return reasonDeleted
	}
	library := c.Library(item)
	if item.libraryRuleID.truthy && library == nil {
		return reasonLibraryDeleted
	}
	src := source(item, library)
	if !ipBehavior(behavior(item, library)) {
		return reasonBehavior
	}
	if isMRS(src) {
		return reasonMRS
	}
	if !item.enabled.or(true) {
		return reasonDisabled
	}
	if !item.libraryEnabled.or(true) || !src.enabled.or(true) {
		return reasonLibraryDisabled
	}
	return ""
}

// ruleName names a rule for warnings: "TYPE,value" for a single rule, else
// the declaration's name, the rule set's name or its ID.
func (c *Config) ruleName(item *Rule) string {
	if item.itemType.or("") == "rule" {
		return item.ruleType + "," + strings.TrimSpace(item.value)
	}
	for _, name := range []member{source(item, c.Library(item)).name, item.name, item.id} {
		if name.isString && name.text != "" {
			return name.text
		}
	}
	return ""
}
