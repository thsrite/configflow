package generate

import (
	"configflow/internal/auth"
	"configflow/internal/configgen"
	"configflow/internal/httpx"
)

func registerArtifacts(r *httpx.Router) {
	for _, route := range []struct{ path, name string }{
		{"/api/generate/<service>/artifact", "generate.generate_artifact"},
		{"/api/profiles/<profile_id>/generate/<service>/artifact", "profiles.profile_generate_artifact"},
	} {
		r.POST(route.path, route.name, auth.Require(func(c *httpx.Ctx) (*httpx.Response, error) {
			if !configgen.Engine(c.Param("service")).Valid() {
				return httpx.Message(400, false, msgUnsupported), nil
			}
			return httpx.Message(503, false, msgAwaitRelease).NoStore(), nil
		}))
	}
	r.GET("/api/generate/artifacts/<preview_id>/<action>", "generate.read_artifact", auth.Require(func(c *httpx.Ctx) (*httpx.Response, error) {
		switch c.Param("action") {
		case "download", "file":
			return httpx.Message(503, false, msgAwaitRelease).NoStore(), nil
		}
		return httpx.Message(404, false, msgUnknownAction), nil
	}))
}
