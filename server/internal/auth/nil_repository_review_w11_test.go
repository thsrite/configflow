package auth_test

import (
	"testing"

	"configflow/internal/auth"
	"configflow/internal/httpx"
	"configflow/internal/httpx/httpxtest"
)

// Negative control for W11: token validation without a repository refuses the
// request instead of treating "no stored tokens" as valid.
func TestValidateSystemTokenOrJWTRefusesNilRepository(t *testing.T) {
	var valid bool
	var err error
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(func(r *httpx.Router) {
		r.GET("/static/review-w11", "test.review_w11", func(c *httpx.Ctx) (*httpx.Response, error) {
			var result auth.ValidateResult
			result, err = auth.ValidateSystemTokenOrJWT(c, nil, nil)
			valid = result.Valid
			return httpx.Empty(204), nil
		})
	})})
	srv.Get("/static/review-w11?token=synthetic-token").WantStatus(204)
	if err == nil || valid {
		t.Fatalf("valid=%v err=%v, want refusal", valid, err)
	}
}
