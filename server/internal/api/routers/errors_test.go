package routers

import (
	"configflow/internal/httpx"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/routers/device"
	"testing"
)

// Python report/plan catch RouterError as 502, while the joint preview catches
// it as 400 and the editor catches RouterConflictError separately as 409.
func TestR15ErrorClassesRespectTheirHTTPBoundary(t *testing.T) {
	for _, tc := range []struct {
		name           string
		override, want int
	}{{"editor", 0, 409}, {"joint-preview", 400, 400}, {"sync", 409, 409}, {"report-plan", 502, 502}} {
		t.Run(tc.name, func(t *testing.T) {
			s := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(func(r *httpx.Router) {
				r.GET("/api/failure", "routers.synthetic_failure", func(c *httpx.Ctx) (*httpx.Response, error) {
					return failure(c, nil, &device.RouterError{Kind: device.KindConflict, Message: messageChanged}, tc.override, false)
				})
			})})
			s.Get("/api/failure").WantStatus(tc.want)
		})
	}
}
