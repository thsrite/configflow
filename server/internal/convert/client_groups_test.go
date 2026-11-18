package convert

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"configflow/internal/model"
)

func TestClientStoredTextGuards(t *testing.T) {
	for _, field := range []string{"name", "type", "follow_group"} {
		t.Run("group/"+field, func(t *testing.T) {
			var group model.ProxyGroup
			source := `{"id":"g","name":"Group","type":"select","follow_group":"","manual_nodes":["DIRECT"]}`
			source = strings.Replace(source, `"`+field+`":"`+map[string]string{"name": "Group", "type": "select", "follow_group": ""}[field]+`"`, `"`+field+`":false`, 1)
			if err := json.Unmarshal([]byte(source), &group); err != nil {
				t.Fatal(err)
			}
			config := &model.Config{Profile: &model.Profile{ProxyGroups: []model.ProxyGroup{group}}}
			_, err := SurgeGroup(group, config, "", nil)
			if err == nil || !strings.Contains(err.Error(), fmt.Sprintf(msgMihomoStoredText, kindGroup, "g", field, "false")) {
				t.Fatalf("%v", err)
			}
		})
	}
	for _, field := range []string{"rule_type", "value", "policy", "name", "behavior"} {
		t.Run("rule/"+field, func(t *testing.T) {
			kind := kindRule
			itemType := "rule"
			if field == "name" || field == "behavior" {
				kind = kindRuleSet
				itemType = "ruleset"
			}
			var rule model.RuleConfig
			source := fmt.Sprintf(`{"id":"r","itemType":%q,"rule_type":"DOMAIN","value":"example.com","policy":"DIRECT","name":"RS","behavior":"domain"}`, itemType)
			source = strings.Replace(source, `"`+field+`":"`+map[string]string{"rule_type": "DOMAIN", "value": "example.com", "policy": "DIRECT", "name": "RS", "behavior": "domain"}[field]+`"`, `"`+field+`":123`, 1)
			if err := json.Unmarshal([]byte(source), &rule); err != nil {
				t.Fatal(err)
			}
			config := &model.Config{Profile: &model.Profile{RuleConfigs: []model.RuleConfig{rule}}}
			for _, target := range []string{"surge", "loon"} {
				var err error
				if target == "surge" {
					_, err = SurgeRules(config, "")
				} else {
					_, _, err = LoonRules(config, "")
				}
				if err == nil || !strings.Contains(err.Error(), fmt.Sprintf(msgMihomoStoredText, kind, "r", field, "123")) {
					t.Fatalf("%s: %v", target, err)
				}
			}
		})
	}
}
