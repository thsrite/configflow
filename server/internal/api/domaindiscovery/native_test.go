package domaindiscovery_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"configflow/internal/api/domaindiscovery"
	"configflow/internal/httpx/httpxtest"
)

func TestNativeRequestsRejectMalformedAndNonFinite(t *testing.T) {
	s := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(domaindiscovery.Register)})
	for _, route := range []struct{ method, path string }{
		{"PUT", "settings"}, {"POST", "rulesets/init"}, {"POST", "apply"}, {"POST", "undo"}, {"POST", "ignore"}, {"DELETE", "ignore"}, {"POST", "probe"}, {"POST", "region/check"}, {"POST", "region/route"}, {"POST", "website/identify"}, {"PUT", "website/note"},
	} {
		for _, body := range []string{`{"unused":NaN}`, `{"unused":Infinity}`, `{"unused":-Infinity}`, `{`, ``} {
			t.Run(route.method+route.path+body, func(t *testing.T) {
				request := httptest.NewRequest(route.method, "/api/domain-discovery/"+route.path, strings.NewReader(body))
				request.Header.Set("Content-Type", "application/json; charset=utf-8")
				response := httptest.NewRecorder()
				s.Handler().ServeHTTP(response, request)
				if response.Code != 400 {
					t.Fatalf("status=%d body=%s", response.Code, response.Body)
				}
			})
		}
	}
}
