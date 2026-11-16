package domaindiscovery

import (
	"configflow/internal/httpx/httpxtest"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReviewEmptyRegionServiceOptions(t *testing.T) {
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(Register)})
	for _, raw := range []string{`{"services":[]}`, `{"services":{}}`, `{"services":0.0}`, `{"services":0e0}`, `{"services":false,"domains":false}`, `{"services":false,"domains":0}`, `{"services":false,"domains":""}`} {
		t.Run(raw, func(t *testing.T) {
			request := httptest.NewRequest("POST", "/api/domain-discovery/region/check", strings.NewReader(raw))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			srv.Handler().ServeHTTP(response, request)
			if response.Code != 400 {
				t.Fatalf("empty region request status=%d body=%s", response.Code, response.Body)
			}
		})
	}
}
