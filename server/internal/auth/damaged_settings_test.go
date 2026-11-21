package auth_test

import (
	"database/sql"
	"testing"

	"configflow/internal/auth"
	"configflow/internal/httpx"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/model"
)

// HR O18 T1a: callers that pass their own snapshot (live configs, Linux
// gateway providers) must refuse damaged stored settings too. They read as
// empty settings, which without login would be anonymous access.
func TestSystemTokenRefusesDamagedSettingsFromCallerSnapshot(t *testing.T) {
	for _, settings := range []string{`null`, `123`, `"malformed"`, `["unexpected"]`} {
		t.Run(settings, func(t *testing.T) {
			s := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(func(r *httpx.Router) {
				r.GET("/static/damaged-token", "test.damaged_token", func(c *httpx.Ctx) (*httpx.Response, error) {
					var system model.System
					result, err := auth.ValidateSystemTokenOrJWT(c, &system)
					if err != nil {
						return nil, err
					}
					if !result.Valid {
						return httpx.Message(401, false, result.Message), nil
					}
					return httpx.JSON(map[string]bool{"success": true}), nil
				})
			})})
			// Undamaged empty settings without login stay open (Python).
			s.Get("/static/damaged-token").WantStatus(200)
			db, err := sql.Open("sqlite", s.Repo.DatabasePath())
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
			s.Get("/static/damaged-token").WantStatus(401)
		})
	}
}
