package httpx_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"configflow/internal/httpx"
)

// W11 removed the repository locator behind response sanitisation and the
// profile context: an App without a Repository must fail the request instead
// of writing an unsanitised JSON body.
func TestAppWithoutRepositoryFailsClosed(t *testing.T) {
	const secret = "synthetic-review-w11-secret"
	tests := []struct{ route, endpoint, path string }{
		{"/api/review-w11", "test.review_w11", "/api/review-w11"},
		{"/api/profiles/<profile_id>/review-w11", "test.review_w11_profile", "/api/profiles/default/review-w11"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			app := httpx.New()
			app.Router.GET(tt.route, tt.endpoint, func(*httpx.Ctx) (*httpx.Response, error) {
				return httpx.JSON(map[string]string{"token": secret}), nil
			})
			w := httptest.NewRecorder()
			app.ServeHTTP(w, httptest.NewRequest("GET", tt.path, nil))
			if w.Code < 500 {
				t.Fatalf("status = %d, want a server error", w.Code)
			}
			if strings.Contains(w.Body.String(), secret) {
				t.Fatalf("body leaked the payload: %s", w.Body.String())
			}
		})
	}
}
