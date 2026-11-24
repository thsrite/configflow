package agents

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"configflow/internal/agents"
	"configflow/internal/auth"
	"configflow/internal/gateway/installation"
	"configflow/internal/httpx"
	"configflow/internal/repo"
)

const (
	installationMaxBody = 16384
)

type gatewayInstallationHandler struct {
	deps Deps
}

func (h *gatewayInstallationHandler) register(r *httpx.Router) {
	base := "/api/agents/gateway-installations"
	r.Handle([]string{"GET", "POST"}, base, "agents.gateway_installations", auth.Require(h.installationHandler("index")))
	r.GET(base+"/<session_id>", "agents.gateway_installation_status", auth.Require(h.installationHandler("get")))
	r.POST(base+"/<session_id>/network", "agents.gateway_installation_network", auth.Require(h.installationHandler("network")))
	r.POST(base+"/<session_id>/publish", "agents.gateway_installation_publish", auth.Require(h.installationHandler("publish")))
	r.POST(base+"/<session_id>/enroll", "agents.gateway_installation_enroll", h.installationHandler("enroll"))
	r.GET(base+"/<session_id>/script", "agents.gateway_installation_script", h.installationHandler("script"))
}
func installationBody(c *httpx.Ctx) (json.RawMessage, error) {
	if c.Request.ContentLength > installationMaxBody {
		return nil, &installation.Error{Message: msgInstallationSize, Status: 413}
	}
	b, err := io.ReadAll(io.LimitReader(c.Request.Body, installationMaxBody+1))
	if err != nil {
		return nil, err
	}
	if len(b) > installationMaxBody {
		return nil, &installation.Error{Message: msgInstallationSize, Status: 413}
	}
	if len(b) == 0 {
		return json.RawMessage(`{}`), nil
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(b))
	previous := c.Request.Header.Get("Content-Type")
	c.Request.Header.Set("Content-Type", "application/json")
	defer c.Request.Header.Set("Content-Type", previous)
	var body map[string]json.RawMessage
	if !c.BindSilent(&body) || body == nil {
		return nil, &installation.Error{Message: msgInstallationJSON, Status: 400}
	}
	return json.Marshal(body)
}
func (h *gatewayInstallationHandler) installationHandler(operation string) httpx.HandlerFunc {
	return func(c *httpx.Ctx) (*httpx.Response, error) {
		r, err := h.deps.repository(c.Context())
		if err != nil {
			return nil, err
		}
		var m *agents.Manager
		if operation != "script" {
			m, err = h.deps.manager(c.Context())
			if err != nil {
				return nil, err
			}
		}
		s := h.deps.installationService(r, m)
		var body, jsonBody json.RawMessage
		status := 200
		if c.Method() == "POST" {
			body, err = installationBody(c)
			if err != nil {
				return installationResponse(&s, c, nil, err, 200)
			}
		}
		id := c.Param("session_id")
		switch operation {
		case "index":
			if c.Method() == "POST" {
				jsonBody, err = s.Create(c.Context(), body)
			} else {
				jsonBody, err = s.Latest(c.Context(), c.Request.URL.Query().Get("profile_id"))
			}
		case "get":
			jsonBody, err = s.Get(c.Context(), id)
		case "network":
			jsonBody, err = s.UpdateNetwork(c.Context(), id, body)
		case "publish":
			jsonBody, err = s.Publish(c.Context(), id, body)
			status = 202
		case "enroll":
			header, present := c.Header("Authorization")
			token, _ := auth.ParseBearerToken(header, present)
			jsonBody, err = s.Enroll(c.Context(), id, token, body)
		case "script":
			var content string
			content, err = s.Script(c.Context(), id, c.Request.URL.Query().Get("token"))
			if err == nil {
				return httpx.Text(200, content).WithHeader("Content-Type", "text/plain; charset=utf-8").WithHeader("Referrer-Policy", "no-referrer").NoStore(), nil
			}
		}
		return installationResponse(&s, c, jsonBody, err, status)
	}
}
func installationResponse(s *installation.Service, c *httpx.Ctx, value json.RawMessage, err error, status int) (*httpx.Response, error) {
	if err != nil {
		err = s.PublicError(c.Context(), err)
		var e *installation.Error
		if errors.As(err, &e) {
			status = e.Status
			v := map[string]any{"success": false, "message": e.Message}
			if e.Code != "" {
				v["code"] = e.Code
			}
			value, _ = json.Marshal(v)
		} else if errors.Is(err, repo.ErrValidation) {
			status = 400
			value, _ = json.Marshal(map[string]any{"success": false, "message": strings.TrimSpace(err.Error())})
		} else {
			return nil, err
		}
	}
	return httpx.JSONStatus(status, value).NoStore(), nil
}
