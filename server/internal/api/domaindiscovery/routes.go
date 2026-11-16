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

// Register declares all seventeen domain-discovery routes.
func Register(r *httpx.Router) {
	routes := []struct {
		method, path, endpoint string
		handler                httpx.HandlerFunc
	}{
		{"GET", "settings", "get_settings", settings}, {"PUT", "settings", "update_settings", updateSettings}, {"POST", "rulesets/init", "init_rulesets", initRulesets}, {"GET", "domains", "list_domains", domains}, {"GET", "agents", "list_agents", agents}, {"POST", "apply", "apply_rules", apply}, {"POST", "undo", "undo", undo}, {"GET", "history", "history", history}, {"POST", "ignore", "add_ignored", ignore}, {"DELETE", "ignore", "remove_ignored", ignore}, {"POST", "probe", "start_probe", probe}, {"GET", "probe/<job_id>", "get_probe", getProbe}, {"GET", "region", "region_status", region}, {"POST", "region/check", "start_region_check", checkRegion}, {"POST", "region/route", "route_region", routeRegion}, {"POST", "website/identify", "identify_website", website}, {"PUT", "website/note", "website_note", note},
	}
	for _, rt := range routes {
		r.Handle([]string{rt.method}, "/api/domain-discovery/"+rt.path, "domain_discovery."+rt.endpoint, auth.Require(rt.handler))
	}
}
func body(c *httpx.Ctx) json.RawMessage {
	if !c.IsJSON() {
		return nil
	}
	v, err := c.Data()
	if err != nil || !json.Valid(v) {
		return nil
	}
	return v
}
func payload(c *httpx.Ctx) (map[string]json.RawMessage, error) {
	raw := body(c)
	var out map[string]json.RawMessage
	if len(raw) == 0 || json.Unmarshal(raw, &out) == nil {
		return out, nil
	}
	if !discovery.JSONOptionEnabled(raw) {
		return nil, nil
	}
	return nil, errors.New("request payload must be a JSON object")
}

func text(m map[string]json.RawMessage, key string) string {
	var out string
	_ = json.Unmarshal(m[key], &out)
	return out
}
func values(m map[string]json.RawMessage, key string) []string {
	var out []string
	if json.Unmarshal(m[key], &out) != nil {
		return nil
	}
	return out
}
func response(status int, v any, err error) (*httpx.Response, error) {
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
	v, e := discovery.UpdateSettings(context.WithoutCancel(c.Context()), c.ProfileID, body(c))
	return response(200, v, e)
}
func initRulesets(c *httpx.Ctx) (*httpx.Response, error) {
	p, err := payload(c)
	if err != nil {
		return nil, err
	}
	var targets []string
	if v, ok := p["targets"]; ok && discovery.JSONOptionEnabled(v) {
		if json.Unmarshal(v, &targets) != nil {
			return nil, repo.NewValidationError(msgTargets)
		}
	}
	v, e := discovery.InitRulesets(context.WithoutCancel(c.Context()), c.ProfileID, targets, text(p, "proxy_policy"))
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
	ctx := context.WithoutCancel(c.Context())
	p, err := payload(c)
	if err != nil {
		return nil, err
	}
	entries, e := discovery.ParseApplyItems(p["items"])
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
	v, e := discovery.UndoDomain(context.WithoutCancel(c.Context()), c.ProfileID, text(p, "value"), text(p, "target"))
	return response(200, map[string]any{"removed": v}, e)
}
func history(c *httpx.Ctx) (*httpx.Response, error) {
	v, e := discovery.HistoryPayload(c.Context(), c.ProfileID)
	return response(200, map[string]any{"items": v}, e)
}
func ignore(c *httpx.Ctx) (*httpx.Response, error) {
	p, err := payload(c)
	if err != nil {
		return nil, err
	}
	v, e := discovery.UpdateIgnored(context.WithoutCancel(c.Context()), c.ProfileID, values(p, "values"), c.Request.Method == "POST")
	return response(200, map[string]any{"ignored": v}, e)
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
	return response(202, map[string]any{"job": j}, err)
}
func probe(c *httpx.Ctx) (*httpx.Response, error) {
	p, err := payload(c)
	if err != nil {
		return nil, err
	}
	j, e := discovery.StartProbe(context.WithoutCancel(c.Context()), c.ProfileID, values(p, "domains"))
	return jobResponse(c, j, e)
}
func getProbe(c *httpx.Ctx) (*httpx.Response, error) {
	j := discovery.GetJob(c.Param("job_id"), c.ProfileID)
	if j == nil {
		return httpx.Message(404, false, "Probe job not found"), nil
	}
	return response(200, map[string]any{"job": j}, nil)
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
	if raw, ok := p["services"]; ok {
		services = discovery.JSONOptionEnabled(raw)
	}
	vs := values(p, "domains")
	if v, ok := p["domains"]; ok && discovery.JSONOptionEnabled(v) && vs == nil {
		return nil, repo.NewValidationError(msgDomains)
	}
	j, e := discovery.StartRegion(context.WithoutCancel(c.Context()), c.ProfileID, services, vs)
	return jobResponse(c, j, e)
}
func routeRegion(c *httpx.Ctx) (*httpx.Response, error) {
	p, err := payload(c)
	if err != nil {
		return nil, err
	}
	v, e := discovery.RouteToPolicy(context.WithoutCancel(c.Context()), c.ProfileID, text(p, "kind"), text(p, "value"), text(p, "policy"))
	return response(200, v, e)
}
func website(c *httpx.Ctx) (*httpx.Response, error) {
	p, _ := payload(c)
	j, e := discovery.StartWebsite(context.WithoutCancel(c.Context()), c.ProfileID, values(p, "domains"))
	return jobResponse(c, j, e)
}
func note(c *httpx.Ctx) (*httpx.Response, error) {
	v, e := discovery.SaveWebsiteNote(context.WithoutCancel(c.Context()), c.ProfileID, body(c))
	return response(200, v, e)
}
