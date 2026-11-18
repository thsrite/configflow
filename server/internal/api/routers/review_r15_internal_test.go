package routers

import (
	"errors"
	"testing"

	"configflow/internal/httpx"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/oxidns"
	"configflow/internal/repo"
)

// Review R15 F2/F3: Python policy_settings catches OxidnsSettingsError (and
// its OxidnsValidationError / OxidnsReferenceError subclasses) and
// ProfileRepositoryError as 400; preview catches ValueError as 400. A plain
// error stays a 500 (negative control).
func TestReviewR15OxidnsAndRepositoryRejectionsAre400(t *testing.T) {
	validation := &oxidns.ValidationError{Issues: []oxidns.Issue{{Path: oxidns.P("plugins", 0), Message: "synthetic issue", Severity: "error"}}}
	reference := oxidns.NewReferenceError("synthetic reference", nil)
	stored := &repo.Error{Kind: repo.ErrRepository, Message: "配置数据库在事务外被修改，已放弃本次写入，请重试"}
	for _, tc := range []struct {
		name     string
		err      error
		override int
		want     int
		message  string
	}{
		{"validation-save", validation, 0, 400, validation.Error()},
		{"validation-preview", validation, 400, 400, validation.Error()},
		{"reference", reference, 0, 400, "synthetic reference"},
		{"repository", stored, 0, 400, stored.Message},
		{"plain", errors.New("synthetic internal"), 0, 500, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(func(r *httpx.Router) {
				r.GET("/api/failure", "routers.synthetic_failure", func(c *httpx.Ctx) (*httpx.Response, error) {
					return repositoryFailure(c, nil)(failure(c, nil, tc.err, tc.override, false))
				})
			})})
			result := s.Get("/api/failure").WantStatus(tc.want)
			if tc.message != "" && result.Object().GetString("message") != tc.message {
				t.Fatalf("message=%q want %q", result.Object().GetString("message"), tc.message)
			}
		})
	}
}
