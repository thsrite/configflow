package proxygroups

import (
	"encoding/json"
	"errors"
	"strings"

	"configflow/internal/auth"
	"configflow/internal/convert"
	"configflow/internal/health/check"
	"configflow/internal/httpx"
	"configflow/internal/model"
	"configflow/internal/oxidns"
	"configflow/internal/repo"
	shared "configflow/internal/rules"
	"configflow/internal/subs"
	"configflow/internal/substore"
)

// Handler serves the /api/proxy-groups routes backed by an explicit Service.
type Handler struct {
	Service *Service
}

// NewHandler constructs a proxygroups Handler. Nil dependencies fall back to
// context/package defaults until W11.
func NewHandler(r *repo.Repository, sub *substore.Service) *Handler {
	return &Handler{Service: &Service{Repo: r, SubStore: sub}}
}

func (h *Handler) service() *Service {
	if h != nil && h.Service != nil {
		return h.Service
	}
	return &Service{}
}

// Register installs profile group CRUD, probe defaults and candidate previews
// on r.
func (h *Handler) Register(r *httpx.Router) {
	s := h.service()
	for _, route := range []struct{ path, name string }{{"/api/proxy-groups", "proxy_groups.handle_proxy_groups"}, {"/api/profiles/<profile_id>/proxy-groups", "profiles.profile_proxy_groups"}} {
		r.Handle([]string{"GET", "POST"}, route.path, route.name, auth.Require(func(c *httpx.Ctx) (*httpx.Response, error) {
			if c.Method() != "POST" {
				out, err := s.List(c.Context(), c.Param("profile_id"))
				return respond(out, err)
			}
			body, ok, err := httpx.BindObject[json.RawMessage](c, false)
			if err != nil {
				return nil, err
			}
			if !ok {
				return respond(nil, bad(msgObject))
			}
			out, err := s.Create(c.Context(), c.Param("profile_id"), body, strings.TrimRight(c.URLRoot(), "/"))
			return respond(out, err)
		}))
	}
	r.Handle([]string{"PUT", "DELETE"}, "/api/proxy-groups/<group_id>", "proxy_groups.handle_proxy_group", auth.Require(func(c *httpx.Ctx) (*httpx.Response, error) {
		var body json.RawMessage
		if c.Method() == "PUT" {
			var ok bool
			var err error
			body, ok, err = httpx.BindObject[json.RawMessage](c, false)
			if err != nil {
				return nil, err
			}
			if !ok {
				return respond(nil, bad(msgObject))
			}
		}
		out, err := s.Mutate(c.Context(), "", c.Param("group_id"), body, c.Method() == "DELETE", strings.TrimRight(c.URLRoot(), "/"))
		return respond(out, err)
	}))
	r.PUT("/api/proxy-groups/batch-health-url", "proxy_groups.update_batch_health_url", auth.Require(func(c *httpx.Ctx) (*httpx.Response, error) {
		body, ok, err := decodeObject[HealthURLRequest](c, true)
		if err != nil {
			return nil, err
		}
		if !ok {
			return respond(nil, bad(msgBatchObject))
		}
		out, err := s.BatchHealthURL(c.Context(), "", body)
		return respond(struct {
			Success bool               `json:"success"`
			Data    []model.ProxyGroup `json:"data"`
		}{true, out}, err)
	}))
	r.GET("/api/proxy-groups/health-defaults", "proxy_groups.health_defaults", auth.Require(func(c *httpx.Ctx) (*httpx.Response, error) { out, err := defaults(); return respond(out, err) }))
	r.POST("/api/proxy-groups/reorder", "proxy_groups.reorder_proxy_groups", auth.Require(func(c *httpx.Ctx) (*httpx.Response, error) {
		body, ok, err := httpx.BindObject[json.RawMessage](c, false)
		if err != nil {
			return nil, err
		}
		if !ok && boundNonempty(c) {
			return nil, &fault{500, "reorder request must be a JSON object"}
		}
		out, err := s.Reorder(c.Context(), "", body)
		return respond(struct {
			Success bool     `json:"success"`
			Order   []string `json:"order"`
		}{true, out}, err)
	}))
	r.POST("/api/proxy-groups/preview-regex", "proxy_groups.preview_proxy_group_regex", auth.Require(func(c *httpx.Ctx) (*httpx.Response, error) {
		body, ok, err := decodeObject[previewRequest](c, false)
		if err != nil {
			return nil, &fault{500, "invalid JSON request"}
		}
		if !ok && !boundNonempty(c) {
			body, ok = &previewRequest{}, true
		}
		if !ok {
			return respond(nil, bad(msgPreviewObject))
		}
		out, err := s.Preview(c.Context(), body)
		return respond(out, err)
	}))
}

func respond(out any, err error) (*httpx.Response, error) {
	if err == nil {
		return httpx.JSON(out), nil
	}
	var f *fault
	if errors.As(err, &f) {
		return httpx.Message(f.status, false, f.message), nil
	}
	var candidate *shared.CandidateValidationError
	var dialer *convert.DialerReferenceError
	var listener *subs.ListenerReferenceError
	var health *check.ValidationError
	if errors.As(err, &candidate) || errors.As(err, &dialer) || errors.As(err, &listener) || errors.As(err, &health) {
		return httpx.Message(400, false, err.Error()), nil
	}
	if _, ok := oxidns.IsSettingsError(err); ok {
		return httpx.Message(400, false, err.Error()), nil
	}
	var locked *oxidns.RoutingLockedError
	if errors.As(err, &locked) {
		return httpx.JSONStatus(409, struct {
			Success bool                  `json:"success"`
			Message string                `json:"message"`
			Usages  []oxidns.RoutingUsage `json:"usages"`
		}{false, locked.Error(), locked.Usages}), nil
	}
	return nil, err
}
