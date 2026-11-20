package settings_test

import (
	"testing"

	"configflow/internal/api/settings"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/testjson"
)

func TestConfigFindingsRejectUnpairedSurrogatesWithoutCommit(t *testing.T) {
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(settings.Register)})
	before, err := srv.Repo.ExportAllJSON(srv.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ path, key string }{{"/api/server-domain", "new_domain"}, {"/api/config-token", "token"}} {
		for _, scalar := range []string{`\ud800`, `\udfff`, `\ud800x`, `\ud800\ud800`} {
			body := `{"` + test.key + `":"` + scalar + `"}`
			result := srv.Post(test.path, httpxtest.Body([]byte(body), "application/json"))
			if result.Status != 400 {
				t.Errorf("%s %s status=%d body=%s", test.path, scalar, result.Status, result.Body)
				continue
			}
			if success, _ := decodeMap(t, result.Body)["success"].(bool); success {
				t.Error("accepted invalid Unicode")
			}
		}
	}
	after, err := srv.Repo.ExportAllJSON(srv.Context(), false)
	if err != nil || !testjson.Equal(before, after) {
		t.Fatalf("invalid Unicode committed: %v", err)
	}
	// Valid pairs and a literal U+FFFD remain valid data. An escaped backslash
	// before 'ud800' is ordinary text, not an invalid Unicode escape.
	for _, scalar := range []string{`\ud83e\uddea`, `\ufffd`, `\\ud800`} {
		srv.Post("/api/config-token", httpxtest.Body([]byte(`{"token":"`+scalar+`"}`), "application/json")).WantStatus(200)
	}
}
