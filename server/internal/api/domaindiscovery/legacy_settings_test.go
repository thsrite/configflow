package domaindiscovery_test

import (
	"encoding/json"
	"testing"

	"configflow/internal/api/domaindiscovery"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/testjson"
)

func TestConfigFindingsPreserveLegacyDiscoveryExtension(t *testing.T) {
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(domaindiscovery.Register)})
	extension := map[string]any{"flag": false, "values": []any{}}
	_, _, err := srv.Repo.SaveProfileFieldsJSON(srv.Context(), "default", json.RawMessage(`{"domain_discovery":{"synthetic_legacy_extension":{"flag":false,"values":[]}}}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	srv.Put("/api/domain-discovery/settings", httpxtest.JSON(map[string]any{"auto_probe": true})).WantStatus(200)
	profile, err := srv.Repo.Profile(srv.Context(), "default")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(profile.DomainDiscovery)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := testjson.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	// Undeclared settings members are dropped (O18 T1c); the edit applies.
	if _, ok := testjson.Path(parsed, "synthetic_legacy_extension"); ok {
		t.Fatal("undeclared settings member kept")
	}
	_ = extension
	if got, ok := testjson.Path(parsed, "auto_probe"); !ok || got != true {
		t.Fatal("settings edit lost")
	}
}
