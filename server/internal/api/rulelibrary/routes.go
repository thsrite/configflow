package rulelibrary

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"configflow/internal/auth"
	"configflow/internal/httpx"
	"configflow/internal/refresh"
	"configflow/internal/repo"
	"configflow/internal/rulefetch"
)

// Deps supplies the collaborators for the rule-library HTTP Handler.
type Deps struct {
	Repo    *repo.Repository
	Client  *rulefetch.Client
	Refresh *refresh.Service
}

// Handler serves shared rule-library editing and public delivery routes.
type Handler struct {
	service *Service
}

// New constructs a rule-library Handler with explicit dependencies. Nil fields
// fall back to transitional process defaults until W11.
func New(deps Deps) *Handler {
	client := deps.Client
	if client == nil {
		client = rulefetch.DefaultClient
	}
	return &Handler{service: &Service{repo: deps.Repo, client: client, refresh: deps.Refresh}}
}

// Register installs shared rule editing and public handwritten-source delivery.
func Register(r *httpx.Router) { New(Deps{}).Register(r) }

// Options injects loopback-only HTTP transport for boundary tests.
type Options struct {
	Repo         *repo.Repository
	Client       *rulefetch.Client
	Refresh      *refresh.Service
	replaceCache func(context.Context, string, string) error
}

// RegisterWithOptions installs the library routes with an injected transport.
func RegisterWithOptions(r *httpx.Router, options Options) {
	h := New(Deps{Repo: options.Repo, Client: options.Client, Refresh: options.Refresh})
	h.service.replaceCache = options.replaceCache
	h.Register(r)
}

// Register mounts shared rule editing and public delivery routes on r.
func (h *Handler) Register(r *httpx.Router) {
	s := h.service
	r.Handle([]string{"GET", "POST"}, "/api/rule-library", "rule_library.handle_rule_library", auth.Require(func(c *httpx.Ctx) (*httpx.Response, error) {
		if c.Method() != "POST" {
			out, err := s.List(c.Context())
			return respond(out, err)
		}
		body, ok, err := decodeRequestObject[libraryRequest](c)
		if err != nil {
			return nil, err
		}
		if !ok {
			return respond(nil, fail(400, msgObject))
		}
		out, err := s.Create(c.Context(), body)
		return respond(out, err)
	}))
	r.Handle([]string{"PUT", "DELETE"}, "/api/rule-library/<rule_id>", "rule_library.handle_rule_library_item", auth.Require(func(c *httpx.Ctx) (*httpx.Response, error) {
		var body *libraryRequest
		if c.Method() == "PUT" {
			var ok bool
			var err error
			body, ok, err = decodeRequestObject[libraryRequest](c)
			if err != nil {
				return nil, err
			}
			if !ok {
				return respond(nil, fail(400, msgObject))
			}
		}
		out, err := s.Mutate(c.Context(), c.Param("rule_id"), body, c.Method() == "DELETE", strings.TrimRight(c.URLRoot(), "/"))
		return respond(out, err)
	}))
	r.Handle([]string{"GET", "PUT"}, "/api/rule-library/<rule_id>/draft-content", "rule_library.edit_rule_library_content", auth.Require(func(c *httpx.Ctx) (*httpx.Response, error) {
		if c.Method() != "PUT" {
			out, err := s.Draft(c.Context(), c.Param("rule_id"))
			if err != nil {
				return respond(nil, err)
			}
			return httpx.JSON(out).WithHeader("Cache-Control", "no-store"), nil
		}
		raw, _, err := httpx.BindObject[json.RawMessage](c, false)
		if err != nil {
			return nil, err
		}
		out, err := s.SaveDraft(c.Context(), c.Param("rule_id"), decodeDraftRequest(raw))
		return respond(out, err)
	}))
	r.POST("/api/rule-library/test-single", "rule_library.test_single_rule", auth.Require(func(c *httpx.Ctx) (*httpx.Response, error) {
		body, ok, err := decodeRequestObject[singleRequest](c)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, errors.New("invalid URL request object")
		}
		out, err := s.Single(c.Context(), body)
		return respond(out, err)
	}))
	r.POST("/api/rule-library/test", "rule_library.test_rules", auth.Require(func(c *httpx.Ctx) (*httpx.Response, error) {
		out, err := s.Test(c.Context(), strings.TrimRight(c.URLRoot(), "/"))
		return respond(out, err)
	}))
	r.POST("/api/rule-library/cache", "rule_library.cache_rules", auth.Require(func(c *httpx.Ctx) (*httpx.Response, error) {
		body, ok, err := decodeCaughtObject[cacheRequest](c)
		if err != nil {
			return nil, err
		}
		if !ok {
			return respond(nil, fail(500, "invalid rule cache request object"))
		}
		out, err := s.Cache(c.Context(), body)
		return respond(out, err)
	}))
	r.POST("/api/rule-library/check-content", "rule_library.check_rule_content", auth.Require(func(c *httpx.Ctx) (*httpx.Response, error) {
		body, ok, err := decodeRequestObject[checkContentRequest](c)
		if err != nil {
			return nil, err
		}
		if !ok {
			return respond(nil, fail(400, msgCheckBody))
		}
		out, err := s.Check(body)
		return respond(out, err)
	}))
	r.POST("/api/rule-library/reorder", "rule_library.reorder_rule_library", auth.Require(func(c *httpx.Ctx) (*httpx.Response, error) {
		body, err := readReorderBody(c)
		if err != nil {
			return nil, err
		}
		out, err := s.Reorder(c.Context(), body)
		return respond(struct {
			Success bool     `json:"success"`
			Order   []string `json:"order"`
		}{true, out}, err)
	}))
	for _, route := range []struct{ path, name string }{{"/api/rule-library/content/<rule_id>", "rule_library.get_rule_library_content"}, {"/api/profiles/<profile_id>/rule-library/content/<rule_id>", "profiles.profile_rule_library_content"}} {
		r.GET(route.path, route.name, func(c *httpx.Ctx) (*httpx.Response, error) {
			content, err := s.Content(c.Context(), c.Param("rule_id"))
			if err != nil {
				return respond(nil, err)
			}
			return httpx.Bytes(200, "text/plain; charset=utf-8", content), nil
		})
	}
	r.Handle([]string{"GET", "POST"}, "/api/rule-library/proxy-domains", "rule_library.handle_proxy_domains", auth.Require(func(c *httpx.Ctx) (*httpx.Response, error) {
		if c.Method() != "POST" {
			out, err := s.ProxyDomains(c.Context())
			return respond(struct {
				Domains jsonValue `json:"proxy_domains"`
			}{out}, err)
		}
		body, ok, err := decodeCaughtObject[proxyDomainsRequest](c)
		if err != nil {
			return nil, err
		}
		if !ok {
			return respond(nil, fail(500, "expected proxy domain object"))
		}
		out, err := s.SetProxyDomains(c.Context(), body)
		return respond(struct {
			Success bool   `json:"success"`
			Domains string `json:"proxy_domains"`
		}{true, out}, err)
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
	if errors.Is(err, repo.ErrRuleLibraryCreationUncertain) {
		return httpx.JSONStatus(503, struct {
			Success      bool   `json:"success"`
			Confirmation bool   `json:"confirmation_required"`
			Message      string `json:"message"`
		}{false, true, err.Error()}), nil
	}
	return nil, err
}
