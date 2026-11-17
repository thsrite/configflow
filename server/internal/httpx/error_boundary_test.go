package httpx_test

import (
	"errors"
	"strings"
	"testing"

	"configflow/internal/httpx"
	"configflow/internal/httpx/httpxtest"
)

func TestConfigFindingsEveryAPIErrorUsesJSON(t *testing.T) {
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(func(r *httpx.Router) {
		r.GET("/api/failure", "test.failure", func(*httpx.Ctx) (*httpx.Response, error) { return nil, errors.New("private diagnostic") })
		r.POST("/api/parse", "test.parse", func(c *httpx.Ctx) (*httpx.Response, error) { _, err := c.JSON(); return nil, err })
		r.GET("/api/empty", "test.empty", func(*httpx.Ctx) (*httpx.Response, error) { return nil, nil })
		r.GET("/api/plain", "test.plain", func(*httpx.Ctx) (*httpx.Response, error) {
			return httpx.Bytes(500, "text/html", []byte("private diagnostic")), nil
		})
	})})
	for _, test := range []struct {
		method, path string
		status       int
	}{{"GET", "/api/failure", 500}, {"GET", "/api/empty", 500}, {"GET", "/api/missing", 404}, {"GET", "/api/parse", 405}, {"POST", "/api/parse", 400}, {"GET", "/api/plain", 500}} {
		t.Run(test.path+test.method, func(t *testing.T) {
			result := srv.Do(test.method, test.path, httpxtest.Body([]byte("{"), "application/json"))
			if result.Status != test.status || !strings.HasPrefix(result.Header.Get("Content-Type"), "application/json") {
				t.Errorf("status=%d type=%s body=%s", result.Status, result.Header.Get("Content-Type"), result.Body)
				return
			}
			body := result.Object()
			if body.GetBool("success") || body.GetString("message") == "" || strings.Contains(string(result.Body), "private diagnostic") {
				t.Errorf("invalid error envelope: %s", result.Body)
			}
		})
	}
}
