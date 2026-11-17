package discovery

import "encoding/json"

// QueryRuleset describes one ruleset in a supplied profile snapshot. Callers
// holding a transaction can supply freshly read data without using request
// caches or duplicating discovery's reference and eligibility logic.
func QueryRuleset(profile json.RawMessage, rulesetID string) (*RulesetInfo, error) {
	c := config{Settings: defaultSettings()}
	if err := json.Unmarshal(profile, &c); err != nil {
		return nil, err
	}
	return info(c, &rulesetID), nil
}
