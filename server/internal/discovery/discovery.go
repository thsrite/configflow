// Package discovery is the Go port of domain discovery (module C37:
// backend/utils/domain_discovery.py, domain_discovery_service.py,
// routes/domain_discovery.py).
//
// Ported early by A05 (traffic history and traffic store), which needs two
// of its functions; C37 extends this package instead of porting them again:
//
//   - RegistrableDomain is domain_discovery.registrable_domain (used by
//     traffic_history._target_key).
//   - ProviderRevisions is domain_discovery_service.provider_revisions,
//     with the parts of read_settings / ruleset_info / _served_rule_content
//     it needs (used by the Agent traffic-report route).
//
// This package must never import internal/agents/metrics (which imports it
// for RegistrableDomain); the traffic store lives in
// internal/agents/trafficstore so that C37 can import it from here.
package discovery

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"os"

	"configflow/internal/jsonx"
	"configflow/internal/repo"
	"configflow/internal/rules"
	"configflow/internal/subs"
)

// RegistrableDomain is registrable_domain(host): a.b.example.co.uk →
// example.co.uk with tldextract's bundled public suffix list (no private
// suffixes, no network); host unchanged when no suffix is recognised.
func RegistrableDomain(host string) string {
	_, domain, suffix := subs.ExtractDomain(host, false)
	if domain != "" && suffix != "" {
		return domain + "." + suffix
	}
	return host
}

// Targets are domain_discovery.TARGETS.
var Targets = []string{"direct", "proxy"}

// WritableBehaviors are domain_discovery_service.WRITABLE_BEHAVIORS.
var WritableBehaviors = []string{"classical", "domain"}

// settingValue is read_settings(config)[key] for the keys provider_revisions
// uses: raw.get(key, default) with direct_ruleset / proxy_ruleset defaulting
// to None and regional_rulesets replaced by {} when it is not a dict.
func settingValue(config *jsonx.Object, key string) any {
	raw := config.Value("domain_discovery")
	if !jsonx.Truthy(raw) {
		raw = jsonx.NewObject()
	}
	object, ok := raw.(*jsonx.Object)
	if !ok {
		// raw.get on a non-dict truthy value: AttributeError in Python.
		return attributeError{raw}
	}
	value, present := object.Get(key)
	if !present {
		if key == "regional_rulesets" {
			return jsonx.NewObject()
		}
		return nil
	}
	return value
}

// attributeError marks read_settings on a domain_discovery value that is
// not a dict (Python raised AttributeError: '<type>' object has no
// attribute 'get').
type attributeError struct{ value any }

// AttributeError is Python's AttributeError for a method call on a value of
// the wrong type.
type AttributeError struct{ Message string }

func (e *AttributeError) Error() string { return e.Message }

func noGet(value any) error {
	return &AttributeError{Message: "'" + pyTypeName(value) + "' object has no attribute 'get'"}
}

func pyTypeName(value any) string {
	switch v := value.(type) {
	case nil:
		return "NoneType"
	case bool:
		return "bool"
	case string:
		return "str"
	case []any:
		return "list"
	case *jsonx.Object:
		return "dict"
	case jsonx.Number:
		if v.IsInt() {
			return "int"
		}
		return "float"
	}
	return "object"
}

// rulesetProblem is ruleset_problem(library_item).
func rulesetProblem(item *jsonx.Object) any {
	if item == nil {
		return "missing"
	}
	if !jsonx.Equal(item.GetOr("source_type", "url"), "content") {
		return "not_content"
	}
	behavior := item.GetOr("behavior", "classical")
	for _, allowed := range WritableBehaviors {
		if jsonx.Equal(behavior, allowed) {
			return nil
		}
	}
	return "unsupported_behavior"
}

// listOf is config.get(key, []) iterated as a list of dicts (items that are
// not dicts raise AttributeError on .get in Python).
func listOf(config *jsonx.Object, key string) ([]*jsonx.Object, error) {
	value, present := config.Get(key)
	if !present {
		return nil, nil
	}
	list, ok := value.([]any)
	if !ok {
		if object, isObject := value.(*jsonx.Object); isObject {
			// Iterating a dict yields its keys (str): .get fails on the first.
			if object.Len() == 0 {
				return nil, nil
			}
			return nil, noGet("")
		}
		if text, isString := value.(string); isString {
			if text == "" {
				return nil, nil
			}
			return nil, noGet("")
		}
		return nil, &TypeError{Message: "'" + pyTypeName(value) + "' object is not iterable"}
	}
	out := make([]*jsonx.Object, 0, len(list))
	for _, entry := range list {
		item, ok := entry.(*jsonx.Object)
		if !ok {
			return nil, noGet(entry)
		}
		out = append(out, item)
	}
	return out, nil
}

// TypeError is Python's TypeError.
type TypeError struct{ Message string }

func (e *TypeError) Error() string { return e.Message }

// active is ruleset_info(...)['active']: some enabled reference to the
// ruleset that lies before the first enabled MATCH rule (_references).
func active(ruleConfigs []*jsonx.Object, libraryID any) bool {
	matchIndex := -1
	var references []int
	enabled := map[int]bool{}
	for index, item := range ruleConfigs {
		if jsonx.Equal(item.Value("itemType"), "rule") && jsonx.Equal(item.Value("rule_type"), "MATCH") &&
			jsonx.Truthy(item.GetOr("enabled", true)) && matchIndex < 0 {
			matchIndex = index
		}
		if jsonx.Equal(item.Value("itemType"), "ruleset") && jsonx.Equal(item.Value("library_rule_id"), libraryID) {
			references = append(references, index)
			enabled[index] = jsonx.Truthy(item.GetOr("enabled", true))
		}
	}
	for _, index := range references {
		afterMatch := matchIndex >= 0 && index > matchIndex
		if enabled[index] && !afterMatch {
			return true
		}
	}
	return false
}

// ProviderRevisions is provider_revisions(profile_id): for every default
// discovery ruleset (direct, proxy, then the regional ones) that exists,
// is writable content and is referenced before the MATCH rule, the MD5 of
// what /rules/local serves for it (the rule cache file when it exists,
// else the stored content), keyed by the ruleset name. Agents refresh the
// matching rule-provider when a digest changes.
func ProviderRevisions(ctx context.Context, profileID string) (*jsonx.Object, error) {
	config, err := repo.GetConfig(ctx, profileID)
	if err != nil {
		return nil, err
	}
	var rulesetIDs []any
	for _, target := range Targets {
		value := settingValue(config, target+"_ruleset")
		if bad, ok := value.(attributeError); ok {
			return nil, noGet(bad.value)
		}
		rulesetIDs = append(rulesetIDs, value)
	}
	// read_settings iterates ignored and history (`value or []`).
	for _, key := range []string{"ignored", "history"} {
		value := settingValue(config, key)
		switch value.(type) {
		case attributeError, nil, string, []any, *jsonx.Object:
		default:
			if jsonx.Truthy(value) {
				return nil, &TypeError{Message: "'" + pyTypeName(value) + "' object is not iterable"}
			}
		}
	}
	regional := settingValue(config, "regional_rulesets")
	if bad, ok := regional.(attributeError); ok {
		return nil, noGet(bad.value)
	}
	if object, ok := regional.(*jsonx.Object); ok {
		rulesetIDs = append(rulesetIDs, object.Values()...)
	}
	libraryItems, err := listOf(config, "rule_library")
	if err != nil {
		return nil, err
	}
	// {item.get('id'): item for item in ...}: the last item with an id wins.
	library := func(id any) *jsonx.Object {
		var found *jsonx.Object
		for _, item := range libraryItems {
			if jsonx.Equal(item.Value("id"), id) {
				found = item
			}
		}
		return found
	}
	result := jsonx.NewObject()
	for _, rulesetID := range rulesetIDs {
		if !jsonx.Truthy(rulesetID) {
			continue
		}
		// ruleset_info: the first library item with that id.
		var item *jsonx.Object
		for _, candidate := range libraryItems {
			if jsonx.Equal(candidate.Value("id"), rulesetID) {
				item = candidate
				break
			}
		}
		ruleConfigs, err := listOf(config, "rule_configs")
		if err != nil {
			return nil, err
		}
		if rulesetProblem(item) != nil || !active(ruleConfigs, rulesetID) {
			continue
		}
		content, err := servedRuleContent(ctx, library(rulesetID))
		if err != nil {
			return nil, err
		}
		sum := md5.Sum(content)
		result.Set(jsonKey(item.Value("name")), hex.EncodeToString(sum[:]))
	}
	return result, nil
}

// jsonKey is the text json.dumps writes for a dict key: str unchanged,
// None/True/False as null/true/false, numbers as their repr. (Python's
// sort_keys then fails on keys of mixed types; not reproduced.)
func jsonKey(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case nil:
		return "null"
	case bool:
		if v {
			return "true"
		}
		return "false"
	case jsonx.Number:
		return v.String()
	}
	return jsonx.Str(value)
}

// servedRuleContent is _served_rule_content(library_item): the validated
// rule cache (read_validated_rule_cache; content rulesets are never
// validated) or the stored content as UTF-8.
func servedRuleContent(ctx context.Context, item *jsonx.Object) ([]byte, error) {
	repository, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	shared, err := repository.GetShared(ctx)
	if err != nil {
		return nil, err
	}
	rulesDir, err := repository.SharedRulesDir()
	if err != nil {
		return nil, err
	}
	path := rules.RuleCacheReadPath(item, nil, shared.GetList("rule_library"), rulesDir)
	if data, err := os.ReadFile(path); err == nil {
		return data, nil
	}
	content := item.Value("content")
	if !jsonx.Truthy(content) {
		return []byte{}, nil
	}
	text, ok := content.(string)
	if !ok {
		return nil, &AttributeError{Message: "'" + pyTypeName(content) + "' object has no attribute 'encode'"}
	}
	return []byte(text), nil
}
