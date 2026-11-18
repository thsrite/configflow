// Package domaindiscovery exposes the authenticated discovery HTTP boundary.
package domaindiscovery

import (
	"encoding/json"
	"errors"

	"configflow/internal/auth"
	"configflow/internal/discovery"
	"configflow/internal/httpx"
	"configflow/internal/repo"
)

// Register declares all seventeen domain-discovery routes.
func Register(r *httpx.Router) {
	routes := []struct {
		method, path, endpoint string
		handler                httpx.HandlerFunc
	}{
		{"GET", "settings", "get_settings", settings}, {"PUT", "settings", "update_settings", updateSettings}, {"POST", "rulesets/init", "init_rulesets", initRulesets}, {"GET", "domains", "list_domains", domains}, {"GET", "agents", "list_agents", agents}, {"POST", "apply", "apply_rules", apply}, {"POST", "undo", "undo", undo}, {"GET", "history", "history", history}, {"POST", "ignore", "add_ignored", ignore}, {"DELETE", "ignore", "remove_ignored", ignore}, {"POST", "probe", "start_probe", probe}, {"GET", "probe/<job_id>", "get_probe", getProbe}, {"GET", "region", "region_status", region}, {"POST", "region/check", "start_region_check", checkRegion}, {"POST", "region/route", "route_region", routeRegion}, {"POST", "website/identify", "identify_website", website}, {"PUT", "website/note", "website_note", note},
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
		return requestDTO{}, errors.New("request payload must be a JSON object")
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
func settings(c *httpx.Ctx) (*httpx.Response, error) {
	v, e := discovery.SettingsPayload(c.Context(), c.ProfileID)
	return response(200, v, e)
}
func updateSettings(c *httpx.Ctx) (*httpx.Response, error) {
	v, e := discovery.UpdateSettings(c.Context(), c.ProfileID, body(c))
	return response(200, v, e)
}
func initRulesets(c *httpx.Ctx) (*httpx.Response, error) {
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
	v, e := discovery.InitRulesets(c.Context(), c.ProfileID, targets, text(p.ProxyPolicy))
	return response(200, v, e)
}
func domains(c *httpx.Ctx) (*httpx.Response, error) {
	days := 7
	if c.ArgInt("days", 7) <= 1 {
		days = 1
	}
	view := c.ArgDefault("view", "uncovered")
	v, e := discovery.LoadDomains(c.Context(), c.ProfileID, days, view, c.ArgDefault("agent_id", ""))
	return response(200, v, e)
}
func agents(c *httpx.Ctx) (*httpx.Response, error) {
	v, e := discovery.AgentsPayload(c.Context(), c.ProfileID)
	return response(200, v, e)
}
func apply(c *httpx.Ctx) (*httpx.Response, error) {
	ctx := c.Context()
	p, err := payload(c)
	if err != nil {
		return nil, err
	}
	entries, e := discovery.ParseApplyItems(p.Items)
	if e != nil {
		return nil, e
	}
	proof, e := discovery.ProbeEvidence(ctx, c.ProfileID, entries)
	if e != nil {
		return nil, e
	}
	v, e := discovery.ApplyDomains(ctx, c.ProfileID, entries, "manual", proof)
	return response(200, v, e)
}
func undo(c *httpx.Ctx) (*httpx.Response, error) {
	p, err := payload(c)
	if err != nil {
		return nil, err
	}
	v, e := discovery.UndoDomain(c.Context(), c.ProfileID, text(p.Value), text(p.Target))
	return response(200, struct {
		Removed []string `json:"removed"`
	}{v}, e)
}
func history(c *httpx.Ctx) (*httpx.Response, error) {
	v, e := discovery.HistoryPayload(c.Context(), c.ProfileID)
	return response(200, struct {
		Items []map[string]json.RawMessage `json:"items"`
	}{v}, e)
}
func ignore(c *httpx.Ctx) (*httpx.Response, error) {
	p, err := payload(c)
	if err != nil {
		return nil, err
	}
	v, e := discovery.UpdateIgnored(c.Context(), c.ProfileID, values(p.Values), c.Request.Method == "POST")
	return response(200, struct {
		Ignored []string `json:"ignored"`
	}{v}, e)
}
func jobResponse(c *httpx.Ctx, j *discovery.Job, err error) (*httpx.Response, error) {
	var busy *discovery.Busy
	var pe *discovery.ProbeError
	if errors.As(err, &busy) {
		b, e := json.Marshal(discovery.BusyPayload(busy, c.ProfileID))
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
func probe(c *httpx.Ctx) (*httpx.Response, error) {
	p, err := payload(c)
	if err != nil {
		return nil, err
	}
	j, e := discovery.StartProbe(c.Context(), c.ProfileID, values(p.Domains))
	return jobResponse(c, j, e)
}
func getProbe(c *httpx.Ctx) (*httpx.Response, error) {
	j := discovery.GetJob(c.Param("job_id"), c.ProfileID)
	if j == nil {
		return httpx.Message(404, false, "Probe job not found"), nil
	}
	return response(200, struct {
		Job *discovery.Job `json:"job"`
	}{j}, nil)
}
func region(c *httpx.Ctx) (*httpx.Response, error) {
	v, e := discovery.RegionPayload(c.Context(), c.ProfileID)
	return response(200, v, e)
}
func checkRegion(c *httpx.Ctx) (*httpx.Response, error) {
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
	j, e := discovery.StartRegion(c.Context(), c.ProfileID, services, vs)
	return jobResponse(c, j, e)
}
func routeRegion(c *httpx.Ctx) (*httpx.Response, error) {
	p, err := payload(c)
	if err != nil {
		return nil, err
	}
	v, e := discovery.RouteToPolicy(c.Context(), c.ProfileID, text(p.Kind), text(p.Value), text(p.Policy))
	return response(200, v, e)
}
func website(c *httpx.Ctx) (*httpx.Response, error) {
	p, _ := payload(c)
	j, e := discovery.StartWebsite(c.Context(), c.ProfileID, values(p.Domains))
	return jobResponse(c, j, e)
}
func note(c *httpx.Ctx) (*httpx.Response, error) {
	v, e := discovery.SaveWebsiteNote(c.Context(), c.ProfileID, body(c))
	return response(200, v, e)
}
