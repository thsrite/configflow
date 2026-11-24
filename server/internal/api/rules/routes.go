package rules

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"configflow/internal/auth"
	"configflow/internal/httpx"
	"configflow/internal/model"
	"configflow/internal/oxidns"
	"configflow/internal/refresh"
	"configflow/internal/repo"
	"configflow/internal/rulefetch"
	"configflow/internal/rulesource"
)

// Deps supplies the collaborators for the rules HTTP Handler.
type Deps struct {
	Repo       *repo.Repository
	RuleSource *rulesource.Service
	Refresh    *refresh.Service
	Client     *rulefetch.Client
	Now        func() time.Time
}

// Handler serves rule composition, rule-set, DNS routing, and local rule routes.
type Handler struct {
	service *Service
}

// NewService constructs a rules Service with explicit dependencies.
func NewService(deps Deps) *Service {
	now := deps.Now
	if now == nil {
		now = time.Now
	}
	client := deps.Client
	if client == nil {
		client = rulefetch.DefaultClient
	}
	return &Service{
		repo:       deps.Repo,
		ruleSource: deps.RuleSource,
		refresh:    deps.Refresh,
		client:     client,
		now:        now,
	}
}

// New constructs a rules Handler with explicit dependencies.
func New(deps Deps) *Handler {
	return &Handler{service: NewService(deps)}
}

// Options supplies deterministic time and loopback-only rule transport in tests.
type Options struct {
	Repo       *repo.Repository
	RuleSource *rulesource.Service
	Client     *rulefetch.Client
	Now        func() time.Time
}

// Register mounts rule composition, DNS routing, and local delivery routes on r.
func (h *Handler) Register(r *httpx.Router) {
	s := h.service
	for _, path := range []struct {
		path, name string
		sets       bool
	}{{"/api/rules", "rules.handle_rules", false}, {"/api/profiles/<profile_id>/rules", "profiles.profile_rules", false}, {"/api/rule-sets", "rule_sets.handle_rule_sets", true}} {
		r.Handle([]string{"GET", "POST"}, path.path, path.name, auth.Require(func(c *httpx.Ctx) (*httpx.Response, error) {
			if c.Method() != "POST" {
				value, err := s.List(c.Context(), c.Param("profile_id"), path.sets)
				return respond(value, err)
			}
			body, err := requestBody(c, false)
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
			var body json.RawMessage
			var err error
			if c.Method() == "PUT" {
				body, err = requestBody(c, false)
				if err != nil {
					return nil, err
				}
			}
			out, err := s.Mutate(c.Context(), "", c.Param(path.param), body, c.Method() == "DELETE", path.sets, strings.TrimRight(c.URLRoot(), "/"))
			return respond(out, err)
		}))
	}
	r.POST("/api/rules/compose", "rules.compose_rules", auth.Require(func(c *httpx.Ctx) (*httpx.Response, error) {
		body, err := decodeBody[composeRequest](c, true)
		if err != nil {
			return nil, err
		}
		out, err := s.Compose(c.Context(), c.Param("profile_id"), body, requestBase(c, body, func(b *composeRequest) member[string] { return b.BaseURL }))
		return respond(out, err)
	}))
	r.POST("/api/rules/group-name", "rules.rename_rule_group", auth.Require(func(c *httpx.Ctx) (*httpx.Response, error) {
		body, err := decodeBody[renameRequest](c, true)
		if err != nil {
			return nil, err
		}
		err = s.Rename(c.Context(), "", body)
		return respond(struct {
			Success bool `json:"success"`
		}{true}, err)
	}))
	r.PUT("/api/rules/policy", "rules.set_rule_policy", auth.Require(func(c *httpx.Ctx) (*httpx.Response, error) {
		body, err := decodeBody[policyRequest](c, true)
		if err != nil {
			return nil, err
		}
		out, err := s.SetPolicy(c.Context(), "", body, requestBase(c, body, func(b *policyRequest) member[string] { return b.BaseURL }))
		return respond(out, err)
	}))
	r.POST("/api/rules/batch", "rules.batch_add_rules", auth.Require(func(c *httpx.Ctx) (*httpx.Response, error) {
		body, err := decodeBody[batchRequest](c, false)
		if err != nil {
			return nil, err
		}
		items, err := s.Batch(c.Context(), "", body)
		return respond(struct {
			Success bool               `json:"success"`
			Count   int                `json:"count"`
			Rules   []model.RuleConfig `json:"rules"`
		}{true, len(items), items}, err)
	}))
	r.POST("/api/rules/reorder", "rules.reorder_rules", auth.Require(func(c *httpx.Ctx) (*httpx.Response, error) {
		raw, err := requestBody(c, false)
		if err != nil {
			return nil, err
		}
		body, err := decodeRequest[orderRequest](raw)
		if err != nil {
			return nil, err
		}
		order, warnings, err := s.Reorder(c.Context(), "", raw, requestBase(c, body, func(b *orderRequest) member[string] { return b.BaseURL }))
		return respond(struct {
			Success  bool     `json:"success"`
			Order    []string `json:"order"`
			Warnings []string `json:"warnings,omitempty"`
		}{true, order, warnings}, err)
	}))
	r.Handle([]string{"GET", "PUT"}, "/api/rules/dns", "rules.handle_rule_dns", auth.Require(func(c *httpx.Ctx) (*httpx.Response, error) {
		if c.Method() != "PUT" {
			base, _ := c.Arg("base_url")
			value, err := s.DNSState(c.Context(), "", baseURL(c, okMember(base)))
			return respond(value, err)
		}
		body, err := decodeBody[dnsWriteRequest](c, true)
		if err != nil {
			return nil, err
		}
		out, err := s.SetDNS(c.Context(), "", body, requestBase(c, body, func(b *dnsWriteRequest) member[string] { return b.BaseURL }))
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
		body, err := decodeBody[matchRequest](c, false)
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

// decodeBody decodes an object body into a request DTO; nil for no object.
func decodeBody[T any](c *httpx.Ctx, silent bool) (*T, error) {
	raw, err := requestBody(c, silent)
	if err != nil {
		return nil, err
	}
	return decodeRequest[T](raw)
}

// requestBase is the request's base_url member, or the panel URL.
func requestBase[T any](c *httpx.Ctx, body *T, base func(*T) member[string]) string {
	if body == nil {
		return baseURL(c, member[string]{})
	}
	return baseURL(c, base(body))
}
func baseURL(c *httpx.Ctx, sent member[string]) string {
	if sent.OK && strings.TrimSpace(sent.Value) != "" {
		return strings.TrimRight(sent.Value, "/")
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
