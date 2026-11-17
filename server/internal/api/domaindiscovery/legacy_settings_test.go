package domaindiscovery_test

import (
	"testing"

	"configflow/internal/api/domaindiscovery"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/jsonx"
)

func TestConfigFindingsPreserveLegacyDiscoveryExtension(t *testing.T) {
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(domaindiscovery.Register)})
	extension := jsonx.Obj("flag", false, "values", []any{})
	_, _, err := srv.Repo.UpdateProfileFields(srv.Context(), "default", jsonx.Obj("domain_discovery", jsonx.Obj("synthetic_legacy_extension", extension)), nil)
	if err != nil {
		t.Fatal(err)
	}
	srv.Put("/api/domain-discovery/settings", httpxtest.JSON(jsonx.Obj("auto_probe", true))).WantStatus(200)
	profile, err := srv.Repo.GetProfile(srv.Context(), "default")
	if err != nil {
		t.Fatal(err)
	}
	if !jsonx.Equal(extension, profile.GetObject("domain_discovery").Value("synthetic_legacy_extension")) {
		t.Fatal("unknown persisted settings lost")
	}
}
