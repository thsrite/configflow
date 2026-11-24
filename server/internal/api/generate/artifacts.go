package generate

import (
	"encoding/json"
	"errors"
	"strings"

	"configflow/internal/auth"
	"configflow/internal/configgen"
	"configflow/internal/deploy"
	"configflow/internal/httpx"
)

func (h *Handler) registerArtifacts(r *httpx.Router) {
	for _, route := range []struct{ path, name string }{
		{"/api/generate/<service>/artifact", "generate.generate_artifact"},
		{"/api/profiles/<profile_id>/generate/<service>/artifact", "profiles.profile_generate_artifact"},
	} {
		r.POST(route.path, route.name, auth.Require(h.generateArtifact))
	}
	r.GET("/api/generate/artifacts/<preview_id>/<action>", "generate.read_artifact", auth.Require(h.readArtifact))
}

func (h *Handler) generateArtifact(c *httpx.Ctx) (*httpx.Response, error) {
	engine := configgen.Engine(c.Param("service"))
	if !engine.Valid() {
		return httpx.Message(400, false, msgUnsupported), nil
	}
	var raw json.RawMessage
	if engine == configgen.OxiDNS {
		if !c.BindSilent(&raw) {
			raw = json.RawMessage(`{}`)
		}
	} else if err := c.Bind(&raw); err != nil {
		return nil, err
	}
	base, err := configgen.RequestBaseURL(raw)
	if err != nil {
		if errors.Is(err, configgen.ErrBaseURL) {
			return httpx.Message(400, false, err.Error()), nil
		}
		return httpx.Message(500, false, err.Error()), nil
	}
	if engine == configgen.MosDNS {
		base = strings.TrimSpace(base)
		if base == "" {
			base = forwardedBase(c)
		}
	}
	if engine == configgen.OxiDNS {
		if base == "" {
			base = c.URLRoot()
		}
		base = strings.TrimRight(base, "/")
	}
	m, err := h.manager(c.Context())
	if err != nil {
		return nil, err
	}
	artifact, err := h.deployService().GenerateArtifact(c.Context(), m, c.ProfileID, engine, base)
	if err != nil {
		return failure(engine, err, false)
	}
	return httpx.JSON(artifact).NoStore(), nil
}

func (h *Handler) readArtifact(c *httpx.Ctx) (*httpx.Response, error) {
	action := c.Param("action")
	if action != "download" && action != "file" {
		return httpx.Message(404, false, msgUnknownAction), nil
	}
	m, err := h.manager(c.Context())
	if err != nil {
		return nil, err
	}
	pv, err := h.deployService().LoadPreview(m, c.Param("preview_id"), "manual")
	if err != nil {
		var changed *deploy.PreviewChanged
		if errors.As(err, &changed) {
			return httpx.JSONStatus(409, struct {
				Success bool   `json:"success"`
				Code    string `json:"code"`
				Message string `json:"message"`
			}{false, "preview_changed", changed.Error()}), nil
		}
		return nil, err
	}
	options := httpx.FileOptions{MimeType: "application/octet-stream"}
	if action == "download" {
		options.Data, err = pv.Zip()
		options.MimeType = "application/zip"
		options.DownloadName = pv.ServiceType + "-final-config.zip"
		options.AsAttachment = true
	} else {
		path, _ := c.Arg("path")
		var ok bool
		options.Data, ok, err = pv.File(path)
		if err == nil && !ok {
			return httpx.Message(404, false, msgGeneratedFileMissing), nil
		}
	}
	if err != nil {
		return nil, err
	}
	response, err := httpx.File(options)
	if err != nil {
		return nil, err
	}
	return response.NoStore(), nil
}
