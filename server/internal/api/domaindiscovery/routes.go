// Package domaindiscovery exposes the authenticated discovery HTTP boundary.
package domaindiscovery

import (
	"context"
	"encoding/json"
	"errors"

	"configflow/internal/auth"
	"configflow/internal/discovery"
	"configflow/internal/httpx"
	"configflow/internal/repo"
)

// Handler serves the authenticated domain-discovery routes.
type Handler struct {
	Repo    *repo.Repository
	Service *discovery.Service
}

// NewHandler constructs a domain-discovery Handler for r and svc.
func NewHandler(r *repo.Repository, svc *discovery.Service) *Handler {
	return &Handler{Repo: r, Service: svc}
}

func (h *Handler) service(ctx context.Context) (*discovery.Service, error) {
	if h != nil && h.Service != nil {
		return h.Service, nil
	}
	r := h.Repo
	if r == nil {
		var err error
		r, err = repo.GetRepository(ctx)
		if err != nil {
			return nil, err
		}
	}
	return discovery.New(discovery.Options{Repo: r}), nil
}

func (h *Handler) ctx(c *httpx.Ctx) context.Context {
	ctx := c.Context()
	if h != nil && h.Repo != nil {
		ctx = repo.WithRepository(ctx, h.Repo)
	}
	return ctx
}

// Register declares all seventeen domain-discovery routes on r.
func (h *Handler) Register(r *httpx.Router) {
	routes := []struct {
		method, path, endpoint string
		handler                httpx.HandlerFunc
	}{
		{"GET", "settings", "get_settings", h.settings}, {"PUT", "settings", "update_settings", h.updateSettings}, {"POST", "rulesets/init", "init_rulesets", h.initRulesets}, {"GET", "domains", "list_domains", h.domains}, {"GET", "agents", "list_agents", h.agents}, {"POST", "apply", "apply_rules", h.apply}, {"POST", "undo", "undo", h.undo}, {"GET", "history", "history", h.history}, {"POST", "ignore", "add_ignored", h.ignore}, {"DELETE", "ignore", "remove_ignored", h.ignore}, {"POST", "probe", "start_probe", h.probe}, {"GET", "probe/<job_id>", "get_probe", h.getProbe}, {"GET", "region", "region_status", h.region}, {"POST", "region/check", "start_region_check", h.checkRegion}, {"POST", "region/route", "route_region", h.routeRegion}, {"POST", "website/identify", "identify_website", h.website}, {"PUT", "website/note", "website_note", h.note},
	}
	for _, rt := range routes {
		handler := rt.handler
		if rt.method != "GET" {
			next := handler
			handler = func(c *httpx.Ctx) (*httpx.Response, error) {
				var raw json.RawMessage
				if !c.BindSilent(&raw) {
					return nil, repo.NewValidationError(msgObject)
				}
				return next(c)
			}
		}
		r.Handle([]string{rt.method}, "/api/domain-discovery/"+rt.path, "domain_discovery."+rt.endpoint, auth.Require(handler))
	}
}

// Register declares all seventeen domain-discovery routes.
func Register(r *httpx.Router) { NewHandler(nil, nil).Register(r) }
func body(c *httpx.Ctx) json.RawMessage {
	var raw json.RawMessage
	if !c.BindSilent(&raw) {
		return nil
	}
	return raw
}

// requestDTO preserves field shape for the existing business validators while
// matching JSON member names exactly (encoding/json's case folding is not used).
type requestDTO struct {
	Targets, ProxyPolicy, Items, Value, Target, Values, Domains, Services, Kind, Policy json.RawMessage
}

func payload(c *httpx.Ctx) (requestDTO, error) {
	raw := body(c)
	var fields map[string]json.RawMessage
	if len(raw) > 0 && json.Unmarshal(raw, &fields) != nil {
		if !discovery.JSONOptionEnabled(raw) {
			return requestDTO{}, nil
		}
		return requestDTO{}, repo.NewValidationError(msgObject)
	}
	return requestDTO{Targets: fields["targets"], ProxyPolicy: fields["proxy_policy"], Items: fields["items"], Value: fields["value"], Target: fields["target"], Values: fields["values"], Domains: fields["domains"], Services: fields["services"], Kind: fields["kind"], Policy: fields["policy"]}, nil
}
func text(raw json.RawMessage) string {
	var out string
	_ = json.Unmarshal(raw, &out)
	return out
}
func values(raw json.RawMessage) []string {
	var out []string
	if json.Unmarshal(raw, &out) != nil {
		return nil
	}
	return out
}

func response[T any](status int, v T, err error) (*httpx.Response, error) {
	if err != nil {
		return nil, err
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(b, &fields); err != nil {
		return nil, err
	}
	fields["success"] = json.RawMessage("true")
	b, err = json.Marshal(fields)
	if err != nil {
		return nil, err
	}
	return httpx.RawJSON(status, b), nil
}
func (h *Handler) settings(c *httpx.Ctx) (*httpx.Response, error) {
	ctx := h.ctx(c)
	svc, err := h.service(ctx)
	if err != nil {
		return nil, err
	}
	v, e := svc.SettingsPayload(ctx, c.ProfileID)
	return response(200, v, e)
}
func (h *Handler) updateSettings(c *httpx.Ctx) (*httpx.Response, error) {
	ctx := h.ctx(c)
	svc, err := h.service(ctx)
	if err != nil {
		return nil, err
	}
	v, e := svc.UpdateSettings(ctx, c.ProfileID, body(c))
	return response(200, v, e)
}
func (h *Handler) initRulesets(c *httpx.Ctx) (*httpx.Response, error) {
	p, err := payload(c)
	if err != nil {
		return nil, err
	}
	var targets []string
	if v := p.Targets; len(v) > 0 && discovery.JSONOptionEnabled(v) {
		if json.Unmarshal(v, &targets) != nil {
			return nil, repo.NewValidationError(msgTargets)
		}
	}
	ctx := h.ctx(c)
	svc, err := h.service(ctx)
	if err != nil {
		return nil, err
	}
	v, e := svc.InitRulesets(ctx, c.ProfileID, targets, text(p.ProxyPolicy))
	return response(200, v, e)
}
func (h *Handler) domains(c *httpx.Ctx) (*httpx.Response, error) {
	days := 7
	if c.ArgInt("days", 7) <= 1 {
		days = 1
	}
	view := c.ArgDefault("view", "uncovered")
	ctx := h.ctx(c)
	svc, err := h.service(ctx)
	if err != nil {
		return nil, err
	}
	v, e := svc.LoadDomains(ctx, c.ProfileID, days, view, c.ArgDefault("agent_id", ""))
	return response(200, v, e)
}
func (h *Handler) agents(c *httpx.Ctx) (*httpx.Response, error) {
	ctx := h.ctx(c)
	svc, err := h.service(ctx)
	if err != nil {
		return nil, err
	}
	v, e := svc.AgentsPayload(ctx, c.ProfileID)
	return response(200, v, e)
}
func (h *Handler) apply(c *httpx.Ctx) (*httpx.Response, error) {
	ctx := h.ctx(c)
	svc, err := h.service(ctx)
	if err != nil {
		return nil, err
	}
	p, err := payload(c)
	if err != nil {
		return nil, err
	}
	entries, e := discovery.ParseApplyItems(p.Items)
	if e != nil {
		return nil, e
	}
	proof, e := svc.ProbeEvidence(ctx, c.ProfileID, entries)
	if e != nil {
		return nil, e
	}
	v, e := svc.ApplyDomains(ctx, c.ProfileID, entries, "manual", proof)
	return response(200, v, e)
}
func (h *Handler) undo(c *httpx.Ctx) (*httpx.Response, error) {
	p, err := payload(c)
	if err != nil {
		return nil, err
	}
	ctx := h.ctx(c)
	svc, err := h.service(ctx)
	if err != nil {
		return nil, err
	}
	v, e := svc.UndoDomain(ctx, c.ProfileID, text(p.Value), text(p.Target))
	return response(200, struct {
		Removed []string `json:"removed"`
	}{v}, e)
}
func (h *Handler) history(c *httpx.Ctx) (*httpx.Response, error) {
	ctx := h.ctx(c)
	svc, err := h.service(ctx)
	if err != nil {
		return nil, err
	}
	v, e := svc.HistoryPayload(ctx, c.ProfileID)
	return response(200, struct {
		Items []map[string]json.RawMessage `json:"items"`
	}{v}, e)
}
func (h *Handler) ignore(c *httpx.Ctx) (*httpx.Response, error) {
	p, err := payload(c)
	if err != nil {
		return nil, err
	}
	ctx := h.ctx(c)
	svc, err := h.service(ctx)
	if err != nil {
		return nil, err
	}
	v, e := svc.UpdateIgnored(ctx, c.ProfileID, values(p.Values), c.Request.Method == "POST")
	return response(200, struct {
		Ignored []string `json:"ignored"`
	}{v}, e)
}
func (h *Handler) jobResponse(c *httpx.Ctx, svc *discovery.Service, j *discovery.Job, err error) (*httpx.Response, error) {
	var busy *discovery.Busy
	var pe *discovery.ProbeError
	if errors.As(err, &busy) {
		b, e := json.Marshal(svc.BusyPayload(busy, c.ProfileID))
		if e != nil {
			return nil, e
		}
		return httpx.RawJSON(409, b), nil
	}
	if errors.As(err, &pe) {
		return httpx.Message(400, false, pe.Message), nil
	}
	return response(202, struct {
		Job *discovery.Job `json:"job"`
	}{j}, err)
}
func (h *Handler) probe(c *httpx.Ctx) (*httpx.Response, error) {
	p, err := payload(c)
	if err != nil {
		return nil, err
	}
	ctx := h.ctx(c)
	svc, err := h.service(ctx)
	if err != nil {
		return nil, err
	}
	j, e := svc.StartProbe(ctx, c.ProfileID, values(p.Domains))
	return h.jobResponse(c, svc, j, e)
}
func (h *Handler) getProbe(c *httpx.Ctx) (*httpx.Response, error) {
	ctx := h.ctx(c)
	svc, err := h.service(ctx)
	if err != nil {
		return nil, err
	}
	j := svc.GetJob(c.Param("job_id"), c.ProfileID)
	if j == nil {
		return httpx.Message(404, false, "Probe job not found"), nil
	}
	return response(200, struct {
		Job *discovery.Job `json:"job"`
	}{j}, nil)
}
func (h *Handler) region(c *httpx.Ctx) (*httpx.Response, error) {
	ctx := h.ctx(c)
	svc, err := h.service(ctx)
	if err != nil {
		return nil, err
	}
	v, e := svc.RegionPayload(ctx, c.ProfileID)
	return response(200, v, e)
}
func (h *Handler) checkRegion(c *httpx.Ctx) (*httpx.Response, error) {
	p, err := payload(c)
	if err != nil {
		return nil, err
	}
	services := true
	if raw := p.Services; len(raw) > 0 {
		services = discovery.JSONOptionEnabled(raw)
	}
	vs := values(p.Domains)
	if v := p.Domains; len(v) > 0 && discovery.JSONOptionEnabled(v) && vs == nil {
		return nil, repo.NewValidationError(msgDomains)
	}
	ctx := h.ctx(c)
	svc, err := h.service(ctx)
	if err != nil {
		return nil, err
	}
	j, e := svc.StartRegion(ctx, c.ProfileID, services, vs)
	return h.jobResponse(c, svc, j, e)
}
func (h *Handler) routeRegion(c *httpx.Ctx) (*httpx.Response, error) {
	p, err := payload(c)
	if err != nil {
		return nil, err
	}
	ctx := h.ctx(c)
	svc, err := h.service(ctx)
	if err != nil {
		return nil, err
	}
	v, e := svc.RouteToPolicy(ctx, c.ProfileID, text(p.Kind), text(p.Value), text(p.Policy))
	return response(200, v, e)
}
func (h *Handler) website(c *httpx.Ctx) (*httpx.Response, error) {
	p, _ := payload(c)
	ctx := h.ctx(c)
	svc, err := h.service(ctx)
	if err != nil {
		return nil, err
	}
	j, e := svc.StartWebsite(ctx, c.ProfileID, values(p.Domains))
	return h.jobResponse(c, svc, j, e)
}
func (h *Handler) note(c *httpx.Ctx) (*httpx.Response, error) {
	ctx := h.ctx(c)
	svc, err := h.service(ctx)
	if err != nil {
		return nil, err
	}
	v, e := svc.SaveWebsiteNote(ctx, c.ProfileID, body(c))
	return response(200, v, e)
}
