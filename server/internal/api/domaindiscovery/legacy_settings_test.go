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
	got, ok := testjson.Path(parsed, "synthetic_legacy_extension")
	if !ok || !testjson.Equal(extension, got) {
		t.Fatal("unknown persisted settings lost")
	}
}
