package domaindiscovery_test

import (
	"regexp"
	"testing"

	"configflow/internal/api/domaindiscovery"
	"configflow/internal/httpx/httpxtest"
)

func TestConfigFindingsNewRulesetIDsUseHyphenatedUUID4(t *testing.T) {
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(domaindiscovery.Register)})
	srv.Post("/api/domain-discovery/rulesets/init", httpxtest.JSON(map[string]any{"targets": []string{"direct"}})).WantStatus(200)
	profile, err := srv.Repo.Profile(srv.Context(), "default")
	if err != nil {
		t.Fatal(err)
	}
	pattern := regexp.MustCompile(`^ruleset_[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	refs := profile.RuleConfigs
	if len(refs) != 1 {
		t.Fatalf("references=%d", len(refs))
	}
	id := refs[0].ID
	if !pattern.MatchString(id) {
		t.Fatalf("ruleset ID is not UUID4: %s", id)
	}
	shared, err := srv.Repo.Shared(srv.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^lib_[0-9a-f]{32}$`).MatchString(shared.RuleLibrary[0].ID) {
		t.Fatal("library ID changed format")
	}
}
