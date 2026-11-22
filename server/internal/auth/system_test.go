package auth_test

import (
	"database/sql"
	"testing"

	"configflow/internal/auth"
	"configflow/internal/httpx"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/repo"
)

func TestSystemTokenUsesOriginalCredentialTypes(t *testing.T) {
	tests := []struct {
		name, raw, query string
		status           int
	}{
		{"configured token", `{"config_token":"public-capability"}`, "public-capability", 200},
		{"numeric projection", `{"config_token":42}`, "42", 401},
		{"fractional zero", `{"config_token":0.0}`, "", 401},
		{"empty object", `{"config_token":{ }}`, "", 401},
		{"empty list", `{"config_token":[ ]}`, "", 401},
		{"internal current", `{"rule_proxy_token":"private-capability"}`, "private-capability", 401},
		{"internal mixed retired", `{"retired_rule_proxy_tokens":[42,"retired-capability"]}`, "retired-capability", 401},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(func(r *httpx.Router) {
				r.GET("/static/native-token", "test.native_token", func(c *httpx.Ctx) (*httpx.Response, error) {
					// The caller's snapshot is the repository's (O18 T1c): a
					// stored token that is not text reads as empty and marks
					// the settings damaged.
					r, err := repo.GetRepository(c.Context())
					if err != nil {
						return nil, err
					}
					system, err := r.System(c.Context())
					if err != nil {
						return nil, err
					}
					result, err := auth.ValidateSystemTokenOrJWT(c, system)
					if err != nil {
						return nil, err
					}
					if !result.Valid {
						return httpx.Message(401, false, result.Message), nil
					}
					return httpx.JSON(map[string]bool{"success": true}), nil
				})
			})})
			storeSettings(t, s.Repo.DatabasePath(), tt.raw)
			s.Get("/static/native-token?token=" + tt.query).WantStatus(tt.status)
		})
	}
}

// storeSettings writes system_config as given, as a damaged or Python-written
// store would hold it.
func storeSettings(t *testing.T, path, settings string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("UPDATE fields SET value=?, revision=revision+1 WHERE scope='system' AND owner='' AND field='system_config'", settings); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE meta SET value=CAST(value AS INTEGER)+1 WHERE key='generation'"); err != nil {
		t.Fatal(err)
	}
}
