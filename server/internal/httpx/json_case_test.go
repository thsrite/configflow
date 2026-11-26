package httpx_test

import (
	"configflow/internal/httpx"
	"configflow/internal/httpx/httpxtest"
	"testing"
)

func TestAuditBindingExactMembers(t *testing.T) {
	type dto struct {
		ID    string `json:"id"`
		Child struct {
			Enabled bool `json:"enabled"`
		} `json:"child"`
	}
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(func(r *httpx.Router) {
		for _, silent := range []bool{false, true} {
			path := "/api/exact"
			if silent {
				path = "/api/silent"
			}
			r.POST(path, path, func(c *httpx.Ctx) (*httpx.Response, error) {
				var v dto
				if silent {
					if !c.BindSilent(&v) {
						return nil, httpx.BadRequest("")
					}
				} else if err := c.Bind(&v); err != nil {
					return nil, err
				}
				return httpx.JSON(v), nil
			})
		}
	})})
	for _, path := range []string{"/api/exact", "/api/silent"} {
		wantJSON(t, srv.Post(path, httpxtest.Body([]byte(`{"ID":"spoofed"}`), "application/json")).WantStatus(200), map[string]any{"id": "", "child": map[string]any{"enabled": false}})
		wantJSON(t, srv.Post(path, httpxtest.Body([]byte(`{"id":"proven","ID":42}`), "application/json")).WantStatus(200), map[string]any{"id": "proven", "child": map[string]any{"enabled": false}})
		wantJSON(t, srv.Post(path, httpxtest.Body([]byte(`{"id":"proven","ID":"spoofed","child":{"ENABLED":true}}`), "application/json")).WantStatus(200), map[string]any{"id": "proven", "child": map[string]any{"enabled": false}})
	}
}
