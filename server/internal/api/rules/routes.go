package rules

import (
	"configflow/internal/rulefetch"
	"errors"
	"strconv"
	"strings"
	"time"

	"configflow/internal/auth"
	"configflow/internal/httpx"
	"configflow/internal/oxidns"
)

// Register installs rule composition and DNS routes. Public delivery routes are
// registered separately with their own cache-owner cancellation semantics.
func Register(r *httpx.Router) { RegisterWithOptions(r, Options{}) }

// Options supplies deterministic time and loopback-only rule transport in tests.
type Options struct {
	Client *rulefetch.Client
	Now    func() time.Time
}

// RegisterWithOptions installs the routes with injected external collaborators.
func RegisterWithOptions(r *httpx.Router, options Options) {
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.Client == nil {
		options.Client = rulefetch.DefaultClient
	}
	s := &Service{client: options.Client, now: options.Now}
	for _, path := range []struct {
		path, name string
		sets       bool
	}{{"/api/rules", "rules.handle_rules", false}, {"/api/profiles/<profile_id>/rules", "profiles.profile_rules", false}, {"/api/rule-sets", "rule_sets.handle_rule_sets", true}} {
		r.Handle([]string{"GET", "POST"}, path.path, path.name, auth.Require(func(c *httpx.Ctx) (*httpx.Response, error) {
			if c.Method() != "POST" {
				value, err := s.List(c.Context(), c.Param("profile_id"), path.sets)
				return respond(value, err)
			}
			body, err := readBody(c, false)
			if err != nil {
				return nil, err
			}
			out, err := s.Create(c.Context(), c.Param("profile_id"), body, path.sets)
			return respond(out, err)
		}))
	}
	for _, path := range []struct {
		path, name, param string
		sets              bool
	}{{"/api/rules/<rule_id>", "rules.handle_rule", "rule_id", false}, {"/api/rule-sets/<rule_set_id>", "rule_sets.handle_rule_set", "rule_set_id", true}} {
		r.Handle([]string{"PUT", "DELETE"}, path.path, path.name, auth.Require(func(c *httpx.Ctx) (*httpx.Response, error) {
			var body record
			var err error
			if c.Method() == "PUT" {
				body, err = readBody(c, false)
				if err != nil {
					return nil, err
				}
			}
			out, err := s.Mutate(c.Context(), "", c.Param(path.param), body, c.Method() == "DELETE", path.sets, strings.TrimRight(c.URLRoot(), "/"))
			return respond(out, err)
		}))
	}
	r.POST("/api/rules/compose", "rules.compose_rules", auth.Require(func(c *httpx.Ctx) (*httpx.Response, error) {
		body, err := readBody(c, true)
		if err != nil {
			return nil, err
		}
		out, err := s.Compose(c.Context(), c.Param("profile_id"), body, requestBase(c, body))
		return respond(out, err)
	}))
	r.POST("/api/rules/group-name", "rules.rename_rule_group", auth.Require(func(c *httpx.Ctx) (*httpx.Response, error) {
		body, err := readBody(c, true)
		if err != nil {
			return nil, err
		}
		err = s.Rename(c.Context(), "", body)
		return respond(struct {
			Success bool `json:"success"`
		}{true}, err)
	}))
	r.PUT("/api/rules/policy", "rules.set_rule_policy", auth.Require(func(c *httpx.Ctx) (*httpx.Response, error) {
		body, err := readBody(c, true)
		if err != nil {
			return nil, err
		}
		out, err := s.SetPolicy(c.Context(), "", body, requestBase(c, body))
		return respond(out, err)
	}))
	r.POST("/api/rules/batch", "rules.batch_add_rules", auth.Require(func(c *httpx.Ctx) (*httpx.Response, error) {
		body, err := readBody(c, false)
		if err != nil {
			return nil, err
		}
		items, err := s.Batch(c.Context(), "", body)
		return respond(struct {
			Success bool     `json:"success"`
			Count   int      `json:"count"`
			Rules   []record `json:"rules"`
		}{true, len(items), items}, err)
	}))
	r.POST("/api/rules/reorder", "rules.reorder_rules", auth.Require(func(c *httpx.Ctx) (*httpx.Response, error) {
		body, err := readBody(c, false)
		if err != nil {
			return nil, err
		}
		order, warnings, err := s.Reorder(c.Context(), "", body, requestBase(c, body))
		return respond(struct {
			Success  bool     `json:"success"`
			Order    []string `json:"order"`
			Warnings []string `json:"warnings,omitempty"`
		}{true, order, warnings}, err)
	}))
	r.Handle([]string{"GET", "PUT"}, "/api/rules/dns", "rules.handle_rule_dns", auth.Require(func(c *httpx.Ctx) (*httpx.Response, error) {
		if c.Method() != "PUT" {
			base, _ := c.Arg("base_url")
			value, err := s.DNSState(c.Context(), "", requestBase(c, record{"base_url": stringJSON(base)}))
			return respond(value, err)
		}
		body, err := readBody(c, true)
		if err != nil {
			return nil, err
		}
		out, err := s.SetDNS(c.Context(), "", body, requestBase(c, body))
		return respond(out, err)
	}))
	for _, path := range []struct{ path, name string }{{"/api/rules/local/<name>", "rules.get_local_rule"}, {"/api/profiles/<profile_id>/rules/local/<name>", "profiles.profile_local_rule"}} {
		r.GET(path.path, path.name, func(c *httpx.Ctx) (*httpx.Response, error) {
			target, _ := c.Arg("target")
			out, err := s.Local(c.Context(), c.Param("profile_id"), c.Param("name"), target, c.HostURL())
			if err != nil {
				return respond(nil, err)
			}
			response := httpx.Bytes(200, "text/plain; charset=utf-8", out.Content).WithHeader("Content-Length", strconv.Itoa(len(out.Content)))
			if out.Stale {
				response.WithHeader("X-ConfigFlow-Cache", "stale").WithHeader("Warning", `110 ConfigFlow "Rule refresh failed; using validated cache"`)
			}
			return response, nil
		})
	}
	r.POST("/api/rules/match-test", "rules.match_test_rule", auth.Require(func(c *httpx.Ctx) (*httpx.Response, error) {
		body, err := readBody(c, false)
		if err != nil {
			return respond(nil, &fault{500, msgMatchFailed})
		}
		out, err := s.Match(c.Context(), "", body, c.HostURL())
		return respond(out, err)
	}))
	r.POST("/api/rules/find-duplicates", "rules.find_duplicate_rules", auth.Require(func(c *httpx.Ctx) (*httpx.Response, error) {
		out, err := s.FindDuplicates(c.Context(), "", c.HostURL())
		if err != nil {
			return respond(nil, err)
		}
		return httpx.External(c, out)
	}))

}
func requestBase(c *httpx.Ctx, body record) string {
	if value, ok := textValue(body["base_url"]); ok && strings.TrimSpace(value) != "" {
		return strings.TrimRight(value, "/")
	}
	return strings.TrimRight(c.URLRoot(), "/")
}
func respond(value any, err error) (*httpx.Response, error) {
	if err == nil {
		return httpx.JSON(value), nil
	}
	var source *sourceFailure
	if errors.As(err, &source) {
		status := source.Status
		if status == 0 {
			status = 422
		}
		return httpx.JSONStatus(status, source), nil
	}
	var paste *pasteError
	if errors.As(err, &paste) {
		return httpx.JSONStatus(400, struct {
			Success bool         `json:"success"`
			Message string       `json:"message"`
			Fields  []fieldError `json:"field_errors"`
		}{false, paste.Error(), paste.Fields}), nil
	}
	var f *fault
	if errors.As(err, &f) {
		return httpx.Message(f.status, false, f.message), nil
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
