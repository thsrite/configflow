package auth_test

import (
	"encoding/json"
	"testing"

	"configflow/internal/auth"
	"configflow/internal/httpx"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/model"
)

func TestSystemTokenUsesOriginalCredentialTypes(t *testing.T) {
	tests := []struct {
		name, raw, query string
		status           int
	}{
		{"configured token", `{"config_token":"public-capability"}`, "public-capability", 200},
		{"numeric projection", `{"config_token":42}`, "42", 401},
		{"fractional zero", `{"config_token":0.0}`, "", 200},
		{"empty object", `{"config_token":{ }}`, "", 200},
		{"empty list", `{"config_token":[ ]}`, "", 200},
		{"internal current", `{"rule_proxy_token":"private-capability"}`, "private-capability", 401},
		{"internal mixed retired", `{"retired_rule_proxy_tokens":[42,"retired-capability"]}`, "retired-capability", 401},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var system model.System
			if err := json.Unmarshal([]byte(`{"system_config":`+tt.raw+`}`), &system); err != nil {
				t.Fatal(err)
			}
			s := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(func(r *httpx.Router) {
				r.GET("/static/native-token", "test.native_token", func(c *httpx.Ctx) (*httpx.Response, error) {
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
			s.Get("/static/native-token?token=" + tt.query).WantStatus(tt.status)
		})
	}
}
