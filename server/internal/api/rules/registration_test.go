package rules_test

import (
	"configflow/internal/server"
	"testing"
)

func TestWholeApplicationRegistersRuleLiteralsAndParameters(t *testing.T) {
	app := server.NewApp()
	for _, name := range []string{"rules.handle_rules", "profiles.profile_rules", "rules.handle_rule", "rule_sets.handle_rule_set", "rules.handle_rule_dns", "rules.set_rule_policy", "rules.reorder_rules"} {
		if !app.Router.HasEndpoint(name) {
			t.Fatalf("missing %s", name)
		}
	}
}
