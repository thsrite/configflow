package gateway

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"slices"
)

// Candidate is a rule set the gateway editor offers (IP-CIDR and Classical
// rule sets, plus every selected one).
type Candidate struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Behavior  string `json:"behavior"`
	Policy    string `json:"policy"`
	Enabled   bool   `json:"enabled"`
	Available bool   `json:"available"`
	Selected  bool   `json:"selected"`
	Reason    string `json:"reason"`
	// Description is empty (omitted) for a selected rule set that is gone.
	Description string `json:"description,omitempty"`
}

// EditorState is what the gateway editor shows.
type EditorState struct {
	Settings   Settings    `json:"settings"`
	Candidates []Candidate `json:"candidates"`
	// FakeIPRange is the base configuration's fake-ip network ("" when the
	// base configuration is unusable; a warning says why).
	FakeIPRange     string   `json:"fake_ip_range"`
	Warnings        []string `json:"warnings"`
	RefreshInterval int      `json:"refresh_interval"`
}

// EditorState lists the gateway setting, the candidate rule sets with why
// each is unusable, the fake-ip range of the base configuration and the
// warnings (the fixed guidance first when includeGuidance). A malformed
// stored setting is an *Error.
func (c *Config) EditorState(includeGuidance bool) (EditorState, error) {
	settings, err := c.Settings()
	if err != nil {
		return EditorState{}, err
	}
	state := EditorState{Settings: settings, Candidates: []Candidate{}, Warnings: []string{}, RefreshInterval: RefreshInterval}
	if includeGuidance {
		state.Warnings = append(state.Warnings, guidance...)
	}
	if fakeRange, err := c.baseFakeIPNetwork(); err != nil {
		state.Warnings = append(state.Warnings, err.Error())
	} else {
		state.FakeIPRange = fakeRange.String()
	}
	seen := map[string]bool{}
	for _, item := range c.rules {
		if item.itemType.or("") != "ruleset" {
			continue
		}
		id := item.id.text
		selected := item.id.isString && slices.Contains(settings.RuleSetIDs, id)
		library := c.Library(item)
		src := source(item, library)
		b := behavior(item, library)
		if !ipBehavior(b) && !selected {
			continue
		}
		seen[id] = true
		reason := c.Reason(item)
		description := descriptionIPCIDR
		if b == "classical" {
			description = descriptionClassic
		}
		name := src.name.or(item.name.or(id))
		state.Candidates = append(state.Candidates, Candidate{
			ID: id, Name: name, Behavior: b, Policy: item.policy.or(""),
			Enabled:   item.enabled.or(true) && item.libraryEnabled.or(true) && src.enabled.or(true),
			Available: reason == "", Selected: selected, Reason: reason, Description: description,
		})
		if reason != "" && selected {
			state.Warnings = append(state.Warnings, fmt.Sprintf(fmtCandidateWarning, src.name.or(id), reason))
		}
	}
	for _, id := range settings.RuleSetIDs {
		if seen[id] {
			continue
		}
		state.Candidates = append(state.Candidates, Candidate{ID: id, Name: id, Selected: true, Reason: reasonMissing})
		state.Warnings = append(state.Warnings, fmt.Sprintf(fmtMissingWarning, reasonMissing, id))
	}
	return state, nil
}

// baseFakeIPNetwork is the fake-ip network of the profile's Mihomo base
// configuration.
func (c *Config) baseFakeIPNetwork() (netip.Prefix, error) {
	doc, err := c.BaseDocument()
	if err != nil {
		return netip.Prefix{}, err
	}
	return FakeIPNetwork(doc)
}

// ValidateRuleSetIDs checks a selection of rule set IDs: each must be an
// existing IP-CIDR/Classical rule set whose inline content the gateway can
// read and that is not MRS. IDs in previous (the stored selection) may stay
// even when they no longer qualify, so a stale selection can always be
// edited or switched off. Errors are *Error.
func (c *Config) ValidateRuleSetIDs(ids, previous []string) error {
	for _, id := range ids {
		item := c.RuleSet(id)
		if item == nil {
			if !slices.Contains(previous, id) {
				return gatewayError(msgSelectedMissing)
			}
			continue
		}
		library := c.Library(item)
		src := source(item, library)
		b := behavior(item, library)
		if !ipBehavior(b) {
			if slices.Contains(previous, id) {
				continue
			}
			return gatewayError(msgSelectBehavior)
		}
		reason := c.Reason(item)
		if src.sourceType.or("") == "content" && reason == "" {
			if src.content.present && !src.content.isString {
				return gatewayError(msgContentTooLarge)
			}
			if _, err := ExtractCIDRs(src.content.or(""), b); err != nil {
				return err
			}
		}
		if reason == reasonMRS && !slices.Contains(previous, id) {
			return gatewayError(reason)
		}
	}
	return nil
}

// ValidateSelection checks a new gateway setting (JSON) against the
// profile: the setting itself, the selected rule sets (ValidateRuleSetIDs
// with the stored selection as previous) and, when it turns the gateway on,
// that the Mihomo base configuration can carry it (Apply on the base
// configuration). Switching the gateway off is always accepted once the
// value is well formed. Errors are *Error or *routersplit.RouterSplitError.
func (c *Config) ValidateSelection(value json.RawMessage) (Settings, error) {
	settings, err := NormalizeSettings(value)
	if err != nil || !settings.Enabled {
		return settings, err
	}
	stored, err := c.Settings()
	if err != nil {
		return Settings{}, err
	}
	if err := c.ValidateRuleSetIDs(settings.RuleSetIDs, stored.RuleSetIDs); err != nil {
		return Settings{}, err
	}
	preview := c.withSettings(settings)
	doc, err := preview.BaseDocument()
	if err != nil {
		return Settings{}, err
	}
	if err := preview.Apply(doc, ""); err != nil {
		return Settings{}, err
	}
	return settings, nil
}
