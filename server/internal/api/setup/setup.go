// Package setup exposes the first-use wizard. Installation, device inspection
// and publication use the native owners registered by server wiring.
package setup

import (
	"encoding/json"
	"errors"
	"io"
	"strings"

	"configflow/internal/auth"
	"configflow/internal/httpx"
	"configflow/internal/repo"
	flow "configflow/internal/setup"
)

const msgDeletedProfile = "该配置空间已删除，请重新开始向导"

// Handler serves the first-use setup wizard routes backed by an explicit
// Repository and optional ExternalOwners.
type Handler struct {
	Repo   *repo.Repository
	Owners *flow.ExternalOwners
}

// NewHandler constructs a setup Handler. Nil dependencies fall back to
// context/package defaults until W11.
func NewHandler(r *repo.Repository, owners *flow.ExternalOwners) *Handler {
	return &Handler{Repo: r, Owners: owners}
}

// Register installs the five authenticated Flask-equivalent wizard methods on
// router. GET's implicit HEAD is read-only, including collection and item
// requests.
func (h *Handler) Register(router *httpx.Router) {
	router.Handle([]string{"GET", "POST"}, "/api/setup", "settings.setup_sessions", auth.Require(h.sessions))
	router.Handle([]string{"GET", "PATCH"}, "/api/setup/<session_id>", "settings.setup_session", auth.Require(h.session))
	router.POST("/api/setup/<session_id>/<operation>", "settings.setup_action", auth.Require(h.action))
}

func (h *Handler) sessions(c *httpx.Ctx) (*httpx.Response, error) {
	service, err := h.serviceFor(c)
	if err != nil {
		return respond(nil, err)
	}
	if c.Method() != "POST" {
		out, err := service.Index(c.Context())
		return respond(out, err)
	}
	body, err := requestBody(c)
	if err != nil {
		return respond(nil, err)
	}
	out, err := service.Create(c.Context(), body)
	return respond(out, err)
}

func (h *Handler) session(c *httpx.Ctx) (*httpx.Response, error) {
	service, err := h.serviceFor(c)
	if err != nil {
		return respond(nil, err)
	}
	if c.Method() != "PATCH" {
		out, err := service.Get(c.Context(), c.Param("session_id"))
		return respond(out, err)
	}
	body, err := requestBody(c)
	if err != nil {
		return respond(nil, err)
	}
	out, err := service.Patch(c.Context(), c.Param("session_id"), body)
	return respond(out, err)
}

func (h *Handler) action(c *httpx.Ctx) (*httpx.Response, error) {
	service, err := h.serviceFor(c)
	if err != nil {
		return respond(nil, err)
	}
	body, err := requestBody(c)
	if err != nil {
		return respond(nil, err)
	}
	out, err := service.Action(c.Context(), c.Param("session_id"), c.Param("operation"), body, strings.TrimRight(c.URLRoot(), "/"))
	return respond(out, err)
}

func (h *Handler) serviceFor(_ *httpx.Ctx) (*flow.Service, error) {
	if h == nil || h.Repo == nil {
		return nil, errors.New("api/setup: repository is nil")
	}
	r := h.Repo
	if h.Owners != nil {
		return flow.NewWithOptions(r, flow.Options{Owners: *h.Owners})
	}
	return flow.New(r)
}
func requestBody(c *httpx.Ctx) (json.RawMessage, error) {
	// Size and object validation happen inside the session operation, preserving
	// missing-session/profile precedence without allocating an unbounded body.
	if c.Request.ContentLength > 32768 {
		return make(json.RawMessage, 32769), nil
	}
	raw, err := io.ReadAll(io.LimitReader(c.Stream(), 32769))
	if err != nil {
		return nil, err
	}
	if !c.IsJSON() {
		return nil, nil
	}
	return raw, nil
}
func respond(value any, err error) (*httpx.Response, error) {
	status := 200
	if err != nil {
		var wizard *flow.Error
		switch {
		case errors.As(err, &wizard):
			status = wizard.Status
			value = map[string]any{"success": false, "message": wizard.Message}
		case errors.Is(err, repo.ErrNotFound):
			status = 404
			value = map[string]any{"success": false, "message": msgDeletedProfile}
		case errors.Is(err, repo.ErrInUse), errors.Is(err, repo.ErrValidation):
			status = 409
			value = map[string]any{"success": false, "message": err.Error()}
		default:
			return nil, err
		}
	}
	response := httpx.JSONStatus(status, value)
	response.NoStore()
	return response, nil
}
