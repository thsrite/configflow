package discovery

import (
	"configflow/internal/model"
	"encoding/json"
	"strings"
)

// Settings is the persisted discovery configuration of a profile.
type Settings struct {
	Extra            map[string]json.RawMessage `json:"-"`
	DirectRuleset    *string                    `json:"direct_ruleset"`
	ProxyRuleset     *string                    `json:"proxy_ruleset"`
	Ignored          []string                   `json:"ignored"`
	AutoProbe        bool                       `json:"auto_probe"`
	AutoApply        bool                       `json:"auto_apply"`
	Confidence       int                        `json:"auto_apply_min_confidence"`
	History          []HistoryEntry             `json:"history"`
	RegionEnabled    bool                       `json:"region_check_enabled"`
	RegionPort       int                        `json:"region_probe_port"`
	RegionalRulesets map[string]string          `json:"regional_rulesets"`
	WebsiteNotes     map[string]string          `json:"website_notes"`
}

// UnmarshalJSON reads persisted settings while retaining future fields.
func (s *Settings) UnmarshalJSON(b []byte) error {
	var record model.DomainDiscovery
	if err := json.Unmarshal(b, &record); err != nil {
		return err
	}
	p := defaultSettings()
	p.DirectRuleset, p.ProxyRuleset = record.DirectRuleset, record.ProxyRuleset
	if record.AutoProbe != nil {
		p.AutoProbe = *record.AutoProbe
	}
	if record.AutoApply != nil {
		p.AutoApply = *record.AutoApply
	}
	if record.RegionCheckEnabled != nil {
		p.RegionEnabled = *record.RegionCheckEnabled
	}
	if record.AutoApplyMinConfidence != nil {
		p.Confidence = int(*record.AutoApplyMinConfidence)
	}
	if record.RegionProbePort != nil {
		p.RegionPort = int(*record.RegionProbePort)
	}
	for key, value := range record.RegionalRulesets.All() {
		p.RegionalRulesets[key] = value
	}
	for key, value := range record.WebsiteNotes.All() {
		p.WebsiteNotes[key] = value
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(b, &fields); err != nil {
		return err
	}
	var ignored, history []json.RawMessage
	_ = json.Unmarshal(fields["ignored"], &ignored)
	for _, raw := range ignored {
		var value *string
		if json.Unmarshal(raw, &value) == nil && value != nil {
			p.Ignored = append(p.Ignored, *value)
		}
	}
	_ = json.Unmarshal(fields["history"], &history)
	for _, raw := range history {
		var object map[string]json.RawMessage
		if json.Unmarshal(raw, &object) != nil || object == nil {
			continue
		}
		var entry HistoryEntry
		if err := json.Unmarshal(raw, &entry); err != nil {
			return err
		}
		p.History = append(p.History, entry)
	}
	p.normalize()
	*s = p
	return capture(b, &s.Extra, "direct_ruleset", "proxy_ruleset", "ignored", "auto_probe", "auto_apply", "auto_apply_min_confidence", "history", "region_check_enabled", "region_probe_port", "regional_rulesets", "website_notes")
}

// MarshalJSON retains fields unknown to this server version.
func (s Settings) MarshalJSON() ([]byte, error) {
	type plain Settings
	return preserve(plain(s), s.Extra)
}
func defaultSettings() Settings {
	return Settings{Ignored: []string{}, Confidence: 90, History: []HistoryEntry{}, RegionPort: 17999, RegionalRulesets: map[string]string{}, WebsiteNotes: map[string]string{}}
}
func (s *Settings) normalize() {
	if s.Ignored == nil {
		s.Ignored = []string{}
	}
	if s.History == nil {
		s.History = []HistoryEntry{}
	}
	if s.Confidence != 80 && s.Confidence != 90 && s.Confidence != 95 {
		s.Confidence = 90
	}
	if s.RegionPort < 1024 || s.RegionPort > 65535 {
		s.RegionPort = 17999
	}
	if s.RegionalRulesets == nil {
		s.RegionalRulesets = map[string]string{}
	}
	if s.WebsiteNotes == nil {
		s.WebsiteNotes = map[string]string{}
	}
}

// HistoryEntry records an adopted rule and its most recent recheck.
type HistoryEntry struct {
	original      json.RawMessage
	previousKnown map[string]json.RawMessage
	Value         string   `json:"value"`
	RuleType      string   `json:"rule_type"`
	Target        string   `json:"target"`
	RulesetID     string   `json:"ruleset_id"`
	Source        string   `json:"source"`
	AppliedAt     string   `json:"applied_at"`
	Verdict       *string  `json:"verdict"`
	Confidence    *int     `json:"confidence"`
	Recheck       *Recheck `json:"recheck"`
}

// UnmarshalJSON retains a historical entry without filling absent legacy fields.
func (h *HistoryEntry) UnmarshalJSON(b []byte) error {
	type plain HistoryEntry
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	*h = HistoryEntry(p)
	h.original = append(json.RawMessage(nil), b...)
	known, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return json.Unmarshal(known, &h.previousKnown)
}

// MarshalJSON retains future fields and updates only changed known fields.
func (h HistoryEntry) MarshalJSON() ([]byte, error) {
	type plain HistoryEntry
	known, err := json.Marshal(plain(h))
	if err != nil {
		return nil, err
	}
	if h.original == nil {
		return known, nil
	}
	var fields, current map[string]json.RawMessage
	if err = json.Unmarshal(h.original, &fields); err != nil {
		return nil, err
	}
	if err = json.Unmarshal(known, &current); err != nil {
		return nil, err
	}
	for k, v := range current {
		if string(v) != string(h.previousKnown[k]) {
			fields[k] = v
		}
	}
	return json.Marshal(fields)
}

// Recheck is the retained outcome of an automatic recheck.
type Recheck struct {
	At            string  `json:"at"`
	Verdict       *string `json:"verdict"`
	SuggestRemove bool    `json:"suggest_remove"`
	Reason        string  `json:"reason"`
}

type libraryItem struct {
	original        json.RawMessage
	originalContent string
	ID              string                     `json:"id"`
	Name            string                     `json:"name"`
	SourceType      string                     `json:"source_type"`
	Behavior        string                     `json:"behavior"`
	Content         string                     `json:"content"`
	Extra           map[string]json.RawMessage `json:"-"`
}

// UnmarshalJSON decodes the known fields while retaining the original data.
func (v *libraryItem) UnmarshalJSON(b []byte) error {
	var record model.RuleLibraryItem
	if err := json.Unmarshal(b, &record); err != nil {
		return err
	}
	item, err := libraryFromModel(record)
	if err != nil {
		return err
	}
	*v = item
	return capture(b, &v.Extra, "id", "name", "source_type", "behavior", "content")
}

// MarshalJSON preserves fields owned by other features or newer versions.
func (v libraryItem) MarshalJSON() ([]byte, error) {
	if v.original != nil {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(v.original, &fields); err != nil {
			return nil, err
		}
		if v.Content != v.originalContent {
			b, err := json.Marshal(v.Content)
			if err != nil {
				return nil, err
			}
			fields["content"] = b
		}
		return json.Marshal(fields)
	}
	type plain libraryItem
	return preserve(plain(v), v.Extra)
}

type ruleConfig struct {
	original  json.RawMessage
	ID        string                     `json:"id"`
	ItemType  string                     `json:"itemType"`
	RuleType  string                     `json:"rule_type,omitempty"`
	Value     string                     `json:"value,omitempty"`
	LibraryID string                     `json:"library_rule_id,omitempty"`
	Policy    string                     `json:"policy"`
	Enabled   bool                       `json:"enabled"`
	Extra     map[string]json.RawMessage `json:"-"`
}

// UnmarshalJSON decodes the known fields while retaining the original data.
func (v *ruleConfig) UnmarshalJSON(b []byte) error {
	var record model.RuleConfig
	if err := json.Unmarshal(b, &record); err != nil {
		return err
	}
	item, err := ruleFromModel(record)
	if err != nil {
		return err
	}
	*v = item
	return capture(b, &v.Extra, "id", "itemType", "rule_type", "value", "library_rule_id", "policy", "enabled")
}

// MarshalJSON preserves fields owned by other features or newer versions.
func (v ruleConfig) MarshalJSON() ([]byte, error) {
	if v.original != nil {
		return v.original, nil
	}
	type plain ruleConfig
	return preserve(plain(v), v.Extra)
}
func capture(b []byte, extra *map[string]json.RawMessage, keys ...string) error {
	if err := json.Unmarshal(b, extra); err != nil {
		return err
	}
	for _, k := range keys {
		delete(*extra, k)
	}
	return nil
}
func preserve[T any](v T, extra map[string]json.RawMessage) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(b, &fields); err != nil {
		return nil, err
	}
	for k, v := range extra {
		fields[k] = v
	}
	return json.Marshal(fields)
}

type policyGroup struct {
	Name string `json:"name"`
}

type config struct {
	Settings Settings      `json:"domain_discovery"`
	Library  []libraryItem `json:"rule_library"`
	Rules    []ruleConfig  `json:"rule_configs"`
	Groups   []policyGroup `json:"proxy_groups"`
	Mihomo   struct {
		Custom string `json:"custom_config"`
	} `json:"mihomo"`
}

func (c config) policies() []string {
	out := []string{}
	for _, g := range c.Groups {
		if g.Name != "" {
			out = append(out, g.Name)
		}
	}
	return out
}
func (c config) library(id string) *libraryItem {
	for i := range c.Library {
		if c.Library[i].ID == id {
			return &c.Library[i]
		}
	}
	return nil
}
func problem(item *libraryItem) *string {
	p := ""
	switch {
	case item == nil:
		p = "missing"
	case item.SourceType != "content":
		p = "not_content"
	case item.Behavior != "classical" && item.Behavior != "domain":
		p = "unsupported_behavior"
	}
	if p == "" {
		return nil
	}
	return &p
}

// Reference describes the position of a ruleset in a profile.
type Reference struct {
	ID         string `json:"id"`
	Policy     string `json:"policy"`
	Enabled    bool   `json:"enabled"`
	Index      int    `json:"index"`
	AfterMatch bool   `json:"after_match"`
}

// RulesetInfo describes a selected discovery ruleset.
type RulesetInfo struct {
	ID         string      `json:"id"`
	Name       *string     `json:"name"`
	Behavior   *string     `json:"behavior"`
	Problem    *string     `json:"problem"`
	References []Reference `json:"references"`
	Active     bool        `json:"active"`
}

func info(c config, id *string) *RulesetInfo {
	if id == nil || *id == "" {
		return nil
	}
	item := c.library(*id)
	out := &RulesetInfo{ID: *id, Problem: problem(item), References: []Reference{}}
	if item != nil {
		out.Name = &item.Name
		out.Behavior = &item.Behavior
	}
	match := -1
	for i, r := range c.Rules {
		if match < 0 && r.ItemType == "rule" && r.RuleType == "MATCH" && r.Enabled {
			match = i
		}
	}
	for i, r := range c.Rules {
		if r.ItemType == "ruleset" && r.LibraryID == *id {
			ref := Reference{r.ID, r.Policy, r.Enabled, i, match >= 0 && i > match}
			out.References = append(out.References, ref)
			out.Active = out.Active || (ref.Enabled && !ref.AfterMatch)
		}
	}
	return out
}
func (s Settings) target(target string) *string {
	if strings.HasPrefix(target, "policy:") {
		if v := s.RegionalRulesets[strings.TrimPrefix(target, "policy:")]; v != "" {
			return &v
		}
		return nil
	}
	if target == "direct" {
		return s.DirectRuleset
	}
	return s.ProxyRuleset
}
func proxyPath(c config) *string {
	if v := info(c, c.Settings.ProxyRuleset); v != nil {
		for _, r := range v.References {
			if r.Enabled && !r.AfterMatch && r.Policy != "DIRECT" && r.Policy != "REJECT" {
				return &r.Policy
			}
		}
	}
	return nil
}
func ptr[T any](v T) *T { return &v }
