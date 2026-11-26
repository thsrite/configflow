package discovery

import (
	"configflow/internal/jsondecode"
	"encoding/json"
)

// QueryRuleset describes one ruleset in a supplied profile snapshot. Callers
// holding a transaction can supply freshly read data without using request
// caches or duplicating discovery's reference and eligibility logic.
func QueryRuleset(profile json.RawMessage, rulesetID string) (*RulesetInfo, error) {
	var snapshot struct {
		Library []libraryItem `json:"rule_library"`
		Rules   []ruleConfig  `json:"rule_configs"`
	}
	if err := jsondecode.Unmarshal(profile, &snapshot); err != nil {
		return nil, err
	}
	return info(config{Library: snapshot.Library, Rules: snapshot.Rules}, &rulesetID), nil
}
