package repo

import (
	"encoding/json"
	"reflect"
	"testing"

	"configflow/internal/model"
)

func TestHydratedRulesetMatchesStoredProjection(t *testing.T) {
	rule := model.RuleConfig{ItemType: "ruleset", LibraryRuleID: "lib", Name: "old", Behavior: new("old"), Content: new("unused"), Policy: new("DIRECT"), Enabled: new(false)}
	for _, item := range []model.RuleLibraryItem{
		{ID: "lib"}, {ID: "lib", Name: "source", Behavior: new(""), Enabled: new(false), URL: new("https://example.com/rules")},
		{ID: "lib", Name: "source", SourceType: new("content"), Format: new("text"), Content: new("example.com")},
	} {
		for _, id := range []string{"", "phone"} {
			got, err := HydratedRuleset(rule, item, id)
			want, oldErr := legacyHydratedRuleset(&rule, &item, id)
			if err != nil || oldErr != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("got %#v (%v), want %#v (%v)", got, err, want, oldErr)
			}
			*got.Policy = "changed"
			if *rule.Policy != "DIRECT" {
				t.Fatal("shared caller policy")
			}
		}
	}
}

var rulesetSource = []string{"name", "url", "behavior", "content", "source_type", "format"}

func legacyHydratedRuleset(rule *model.RuleConfig, item *model.RuleLibraryItem, profileID string) (model.RuleConfig, error) {
	members, err := recordMembers(rule)
	if err != nil {
		return model.RuleConfig{}, err
	}
	source, err := recordMembers(item)
	if err != nil {
		return model.RuleConfig{}, err
	}
	for _, name := range append(append([]string(nil), rulesetSource...), "library_enabled") {
		members.Delete(name)
	}
	for _, name := range rulesetSource {
		if value, ok := source.Get(name); ok {
			if err := members.Set(name, value); err != nil {
				return model.RuleConfig{}, err
			}
		}
	}
	if !members.Has("behavior") {
		if err := members.Set("behavior", json.RawMessage(`"domain"`)); err != nil {
			return model.RuleConfig{}, err
		}
	}
	enabled, ok := source.Get("enabled")
	if !ok {
		enabled = json.RawMessage("true")
	}
	if err := members.Set("library_enabled", enabled); err != nil {
		return model.RuleConfig{}, err
	}
	if item.SourceType != nil && *item.SourceType == "content" {
		if profileID == "" {
			profileID = DefaultProfileID
		}
		url, err := model.Marshal("/api/profiles/" + profileID + "/rule-library/content/" + item.ID)
		if err != nil {
			return model.RuleConfig{}, err
		}
		if err := members.Set("url", url); err != nil {
			return model.RuleConfig{}, err
		}
	}
	encoded, err := members.MarshalJSON()
	if err != nil {
		return model.RuleConfig{}, err
	}
	var result model.RuleConfig
	err = json.Unmarshal(encoded, &result)
	return result, err
}
