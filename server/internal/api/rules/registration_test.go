package rules_test

import (
	"configflow/internal/repo"
	"configflow/internal/server"
	"testing"
)

func TestWholeApplicationRegistersRuleLiteralsAndParameters(t *testing.T) {
	r, err := repo.OpenDefaultWithOptions(t.TempDir(), server.RepoOptions())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	srv, err := server.New(t.Context(), server.Options{Repo: r})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Agents.Close)
	app := srv.HTTP
	for _, name := range []string{"rules.handle_rules", "profiles.profile_rules", "rules.handle_rule", "rule_sets.handle_rule_set", "rules.handle_rule_dns", "rules.set_rule_policy", "rules.reorder_rules", "rules.get_local_rule", "profiles.profile_local_rule", "rules.match_test_rule", "rules.find_duplicate_rules"} {
		if !app.Router.HasEndpoint(name) {
			t.Fatalf("missing %s", name)
		}
	}
}
