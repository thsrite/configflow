package rulelibrary

import (
	"errors"

	"configflow/internal/auth"
	"configflow/internal/httpx"
	"configflow/internal/repo"
	"configflow/internal/rulefetch"
)

// Register installs shared rule editing and public handwritten-source delivery.
func Register(r *httpx.Router) { RegisterWithOptions(r, Options{}) }

// Options injects loopback-only HTTP transport for boundary tests.
type Options struct{ Client *rulefetch.Client }

// RegisterWithOptions installs the library routes with an injected transport.
func RegisterWithOptions(r *httpx.Router, options Options) {
	if options.Client == nil {
		options.Client = rulefetch.DefaultClient
	}
	s := &Service{client: options.Client}
	r.Handle([]string{"GET", "POST"}, "/api/rule-library", "rule_library.handle_rule_library", auth.Require(func(c *httpx.Ctx) (*httpx.Response, error) {
		if c.Method() != "POST" {
			out, err := s.List(c.Context())
			return respond(out, err)
		}
		body, ok, err := readBody(c)
		if err != nil {
			return nil, err
		}
		if !ok {
			return respond(nil, fail(400, msgObject))
		}
		out, err := s.Create(c.Context(), body)
		return respond(out, err)
	}))
	r.POST("/api/rule-library/check-content", "rule_library.check_rule_content", auth.Require(func(c *httpx.Ctx) (*httpx.Response, error) {
		body, ok, err := readBody(c)
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
		body, _, err := readBody(c)
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
		body, ok, err := readBody(c)
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
