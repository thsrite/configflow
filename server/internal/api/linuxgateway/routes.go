// Package linuxgateway translates gateway editor and authenticated provider HTTP
// routes. Draft transactions and device operations belong to their owners.
package linuxgateway

import (
	"encoding/json"
	"errors"
	"strings"

	"configflow/internal/agents"
	"configflow/internal/auth"
	"configflow/internal/gateway"
	"configflow/internal/gateway/draft"
	"configflow/internal/gateway/host"
	"configflow/internal/httpx"
	"configflow/internal/repo"
	"configflow/internal/routersplit"
	"configflow/internal/setupflow"
)

// Register adds the legacy and explicit-profile editor/provider routes.
func Register(r *httpx.Router) {
	for _, root := range []struct{ editor, provider, endpoint string }{{"/api/custom-config/linux-gateway", "/api/custom-config/linux-gateway", "custom_config"}, {"/api/profiles/<profile_id>/custom-config/linux-gateway", "/api/profiles/<profile_id>/linux-gateway", "profiles"}} {
		for _, suffix := range []string{"", "/preview", "/restore", "/restore/preview"} {
			methods := []string{"POST"}
			if suffix == "" {
				methods = append(methods, "GET")
			}
			r.Handle(methods, root.editor+suffix, root.endpoint+".linux_gateway_settings", auth.Require(editor))
		}
		r.GET(root.provider+"/rules/<path:rule_id>", root.endpoint+".linux_gateway_rule_provider", provider)
		r.GET(root.provider+"/source/<path:rule_id>", root.endpoint+".linux_gateway_source_provider", provider)
	}
}
func responseError(err error, status int) (*httpx.Response, error) {
	if errors.Is(err, setupflow.ErrReadSession) {
		return httpx.Message(500, false, setupflow.ErrReadSession.Error()), nil
	}
	var split *routersplit.RouterSplitError
	if errors.As(err, &split) {
		return httpx.JSONStatus(split.Status, struct {
			Success bool   `json:"success"`
			Code    string `json:"code"`
			Message string `json:"message"`
		}{false, split.Code, split.Error()}), nil
	}
	var gw *gateway.Error
	if errors.As(err, &gw) {
		return httpx.Message(status, false, gw.Error()), nil
	}
	return nil, err
}
func editor(c *httpx.Ctx) (*httpx.Response, error) {
	profile, err := repo.ResolveProfileID(c.Context(), c.Param("profile_id"))
	if err != nil {
		return nil, err
	}
	r, err := repo.GetRepository(c.Context())
	if err != nil {
		return nil, err
	}
	m, err := agents.Get(c.Context())
	if err != nil {
		return nil, err
	}
	op := draft.Operation{ProfileID: profile, BaseURL: strings.TrimRight(c.HostURL(), "/"), Write: c.Request.Method == "POST", Preview: strings.HasSuffix(c.Request.URL.Path, "/preview"), Restore: strings.Contains(c.Request.URL.Path, "/linux-gateway/restore")}
	if op.Write {
		if !c.BindSilent(&op.Body) || op.Body == nil {
			return httpx.Message(400, false, msgSettingsJSON), nil
		}
	}
	service := draft.Service{Repository: r, Host: &host.Bridge{Repository: r, Manager: m}}
	data, err := service.Execute(c.Context(), op)
	if err != nil {
		return responseError(err, 400)
	}
	return httpx.JSON(json.RawMessage(data)).NoStore(), nil
}
func provider(c *httpx.Ctx) (*httpx.Response, error) {
	config, err := repo.GetConfig(c.Context(), c.Param("profile_id"))
	if err != nil {
		return nil, err
	}
	validation, err := auth.ValidateSystemTokenOrJWT(c, nil)
	if err != nil {
		return nil, err
	}
	if !validation.Valid {
		return httpx.Message(401, false, validation.Message), nil
	}
	gw := gateway.NewConfig(config)
	var content string
	if strings.Contains(c.Request.URL.Path, "/linux-gateway/source/") {
		content, err = gw.SourceYAML(c.Context(), c.Param("rule_id"), nil)
	} else {
		content, err = gw.ProviderYAML(c.Context(), c.Param("rule_id"), nil)
	}
	if err != nil {
		return responseError(err, 503)
	}
	return httpx.Bytes(200, "text/yaml; charset=utf-8", []byte(content)).NoStore(), nil
}
