// Package routers translates authenticated router and upstream-reminder HTTP
// routes. DHCP/routes/releases and configuration commit ownership live outside it.
package routers

import (
	"encoding/json"
	"errors"
	"strings"

	"configflow/internal/agents"
	"configflow/internal/agents/monitor"
	"configflow/internal/auth"
	"configflow/internal/gateway/host"
	"configflow/internal/httpx"
	"configflow/internal/lifecycle"
	"configflow/internal/logx"
	"configflow/internal/oxidns"
	"configflow/internal/redact"
	"configflow/internal/repo"
	core "configflow/internal/routers"
)

type handler struct{ pending *core.PendingCache }

var logger = logx.Get("backend.routes.routers")

// Register installs all R15 routes on the shared Flask-priority matcher. Static
// segments win per segment; method mismatches may fall through to a path route.
func Register(r *httpx.Router) {
	h := &handler{pending: core.NewPendingCache(nil, nil)}
	r.Handle([]string{"GET", "POST"}, "/api/routers", "routers.list_or_create_routers", auth.Require(h.collection))
	r.Handle([]string{"GET", "POST"}, "/api/routers/gateway-forwarding", "routers.gateway_forwarding", auth.Require(h.forwarding))
	r.Handle([]string{"PUT", "DELETE"}, "/api/routers/<router_id>", "routers.update_or_delete_router", auth.Require(h.job(h.settings)))
	r.POST("/api/routers/<router_id>/probe", "routers.probe_router", auth.Require(h.job(h.probe)))
	r.GET("/api/routers/<router_id>/report", "routers.router_report", auth.Require(h.inspect))
	r.POST("/api/routers/<router_id>/plan", "routers.router_plan", auth.Require(h.inspect))
	r.POST("/api/routers/<router_id>/apply", "routers.router_apply", auth.Require(h.job(h.apply)))
	r.GET("/api/routers/<router_id>/routes", "routers.list_device_routes", auth.Require(h.routes))
	r.POST("/api/routers/<router_id>/routes/plan", "routers.preview_device_route", auth.Require(h.routeChange))
	r.POST("/api/routers/<router_id>/routes/apply", "routers.apply_device_route", auth.Require(h.job(h.routeChange)))
	r.GET("/api/routers/<router_id>/policy", "routers.list_device_policy", auth.Require(h.policy))
	r.Handle([]string{"GET", "PUT"}, "/api/routers/<router_id>/policy-settings", "routers.policy_settings", auth.Require(h.settingsJob(h.workspace)))
	r.POST("/api/routers/<router_id>/policy-settings/preview", "routers.preview_policy_settings", auth.Require(h.previewWorkspace))
	r.Handle([]string{"GET", "PUT", "POST"}, "/api/routers/<router_id>/rule-sync", "routers.router_rule_sync", auth.Require(h.settingsJob(h.sync)))
	r.GET("/api/agents/<agent_id>/upstream-routers", "agents.upstream_router_pending", auth.Require(h.upstream))
}
func service(c *httpx.Ctx) (*core.ManagementService, error) {
	db, err := repo.GetRepository(c.Context())
	return &core.ManagementService{Repo: db}, err
}
func manager(c *httpx.Ctx) (*agents.Manager, error) { return agents.Get(c.Context()) }
func body(c *httpx.Ctx) json.RawMessage             { raw, _ := json.Marshal(c.JSONSilent()); return raw }
func fields(raw json.RawMessage) (map[string]json.RawMessage, bool) {
	var out map[string]json.RawMessage
	if json.Unmarshal(raw, &out) != nil || out == nil {
		return nil, false
	}
	return out, true
}
func exact(raw map[string]json.RawMessage, keys ...string) bool {
	if len(raw) != len(keys) {
		return false
	}
	for _, key := range keys {
		if raw[key] == nil {
			return false
		}
	}
	return true
}
func stringValue(raw json.RawMessage) (string, bool) {
	var s *string
	if json.Unmarshal(raw, &s) != nil || s == nil {
		return "", false
	}
	return *s, true
}
func selected(c *httpx.Ctx) (*core.ManagementService, json.RawMessage, *core.Router, error) {
	s, err := service(c)
	if err != nil {
		return nil, nil, nil, err
	}
	cfg, rows, err := s.Records(c.Context(), c.ProfileID)
	if err != nil {
		return s, nil, nil, err
	}
	for _, r := range rows {
		if r.ID == c.Param("router_id") {
			return s, cfg, r, nil
		}
	}
	return s, cfg, nil, nil
}
func public(c *httpx.Ctx, cfg json.RawMessage, value any, status int, noStore bool) (*httpx.Response, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	secrets, err := redact.ExternalPayloadSecrets(c.Context(), nil)
	if err != nil {
		return nil, err
	}
	safe := redact.SanitizeCredentials(json.RawMessage(raw), redact.CollectCredentials(cfg, secrets))
	response := httpx.JSONStatus(status, safe)
	if noStore {
		response.NoStore()
	}
	return response, nil
}
func failure(c *httpx.Ctx, cfg json.RawMessage, err error, status int, noStore bool) (*httpx.Response, error) {
	if err == nil {
		return nil, nil
	}
	var validation *core.ValidationError
	var settings *oxidns.SettingsError
	var re *core.RouterError
	if errors.As(err, &validation) || errors.As(err, &settings) {
		status = 400
	} else if errors.As(err, &re) {
		if re.Kind == core.KindConflict {
			status = 409
		} else if status == 0 {
			status = 502
		}
	} else {
		return nil, err
	}
	return public(c, cfg, struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}{false, err.Error()}, status, noStore)
}
func notFound() (*httpx.Response, error) { return httpx.Message(404, false, messageMissing), nil }
func (h *handler) job(next httpx.HandlerFunc) httpx.HandlerFunc {
	return func(c *httpx.Ctx) (*httpx.Response, error) {
		ctx := lifecycle.Detach(c.Context())
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		original := c.Context()
		c.SetContext(ctx)
		defer c.SetContext(original)
		_, cfg, r, err := selected(c)
		if err != nil {
			return nil, err
		}
		held, release, ok := core.JobLock(ctx, c.ProfileID, c.Param("router_id"), r)
		if !ok {
			return public(c, cfg, struct {
				Success bool   `json:"success"`
				Message string `json:"message"`
			}{false, messageBusy}, 409, false)
		}
		defer release()
		c.SetContext(held)
		return next(c)
	}
}
func (h *handler) settingsJob(next httpx.HandlerFunc) httpx.HandlerFunc {
	return func(c *httpx.Ctx) (*httpx.Response, error) {
		if c.Method() == "PUT" || c.Method() == "DELETE" {
			return h.job(next)(c)
		}
		return next(c)
	}
}
func (h *handler) collection(c *httpx.Ctx) (*httpx.Response, error) {
	s, err := service(c)
	if err != nil {
		return nil, err
	}
	cfg, rows, err := s.Records(c.Context(), c.ProfileID)
	if err != nil {
		return nil, err
	}
	if c.Method() == "POST" {
		r, err := s.Create(c.Context(), c.ProfileID, body(c))
		if err != nil {
			return failure(c, cfg, err, 0, false)
		}
		return public(c, cfg, struct {
			Success bool            `json:"success"`
			Router  core.RouterView `json:"router"`
		}{true, r.View()}, 200, false)
	}
	views := make([]core.RouterView, 0, len(rows))
	for _, r := range rows {
		views = append(views, r.View())
	}
	return public(c, cfg, struct {
		Success bool              `json:"success"`
		Routers []core.RouterView `json:"routers"`
	}{true, views}, 200, false)
}
func (h *handler) settings(c *httpx.Ctx) (*httpx.Response, error) {
	s, cfg, r, err := selected(c)
	if err != nil {
		return nil, err
	}
	if r == nil {
		return notFound()
	}
	next, err := s.Save(c.Context(), c.ProfileID, r, body(c), c.Method() == "DELETE")
	if err != nil {
		return failure(c, cfg, err, 0, false)
	}
	if next == nil {
		return httpx.JSON(struct {
			Success bool `json:"success"`
		}{true}), nil
	}
	return public(c, cfg, struct {
		Success bool            `json:"success"`
		Router  core.RouterView `json:"router"`
	}{true, next.View()}, 200, false)
}
func (h *handler) probe(c *httpx.Ctx) (*httpx.Response, error) {
	s, cfg, r, err := selected(c)
	if err != nil {
		return nil, err
	}
	if r == nil {
		return notFound()
	}
	result, err := s.Probe(c.Context(), c.ProfileID, r)
	if err != nil {
		return failure(c, cfg, err, 0, false)
	}
	status := 200
	if !result.OK {
		status = 502
	}
	return public(c, cfg, struct {
		Success bool             `json:"success"`
		Probe   core.ProbeResult `json:"probe"`
		Message string           `json:"message"`
	}{result.OK, result, result.Message}, status, false)
}
func inspect(c *httpx.Ctx, db *repo.Repository, cfg json.RawMessage, r *core.Router) (core.Driver, *core.Inspection, error) {
	m, err := manager(c)
	if err != nil {
		return nil, nil, err
	}
	d := core.NewDriver(r)
	if d == nil {
		return nil, nil, &core.RouterError{Message: messageMissing}
	}
	value, err := core.Inspect(c.Context(), core.InspectRequest{ProfileID: c.ProfileID, Config: cfg, Router: r, Driver: d, Agents: core.ManagerAgentSource{Manager: m}, Repo: db})
	return d, value, err
}
func stamp(c *httpx.Ctx) string {
	return lifecycle.ClockFrom(c.Context()).Now().Local().Format("2006-01-02T15:04:05")
}
func (h *handler) inspect(c *httpx.Ctx) (*httpx.Response, error) {
	s, cfg, r, err := selected(c)
	if err != nil {
		return nil, err
	}
	if r == nil {
		return notFound()
	}
	d, value, err := inspect(c, s.Repo, cfg, r)
	if d != nil {
		defer d.Close()
	}
	if err != nil {
		return failure(c, cfg, err, 0, false)
	}
	if strings.HasSuffix(c.Request.URL.Path, "/report") {
		recordNotice(c, cfg, r, value.Report, nil)
		return public(c, cfg, struct {
			Success     bool         `json:"success"`
			Report      *core.Report `json:"report"`
			GeneratedAt string       `json:"generated_at"`
		}{true, value.Report, stamp(c)}, 200, false)
	}
	plan, err := core.BuildPlan(c.Context(), r, value, d)
	if err != nil {
		return failure(c, cfg, err, 0, false)
	}
	recordNotice(c, cfg, r, value.Report, plan)
	if plan.Empty && len(plan.Blockers) == 0 {
		baseline, err := core.NeedsBaseline(c.Context(), c.ProfileID, r)
		if err != nil {
			return nil, err
		}
		plan.SyncBaseline = &baseline
	}
	return public(c, cfg, struct {
		Success     bool       `json:"success"`
		Plan        *core.Plan `json:"plan"`
		GeneratedAt string     `json:"generated_at"`
	}{true, plan, stamp(c)}, 200, false)
}
func (h *handler) apply(c *httpx.Ctx) (*httpx.Response, error) {
	s, cfg, r, err := selected(c)
	if err != nil {
		return nil, err
	}
	if r == nil {
		return notFound()
	}
	var confirmation struct {
		Fingerprint string `json:"fingerprint"`
	}
	_ = json.Unmarshal(body(c), &confirmation)
	m, err := manager(c)
	if err != nil {
		return nil, err
	}
	out := core.Publish(c.Context(), core.PublishRequest{ProfileID: c.ProfileID, RouterID: r.ID, Expected: confirmation.Fingerprint, Router: r, Repo: s.Repo, Agents: core.ManagerAgentSource{Manager: m}})
	if out.Err != nil {
		return failure(c, cfg, out.Err, 0, false)
	}
	switch out.Status {
	case core.PublishRouterChanged, core.PublishBusy, core.PublishBlocked, core.PublishStale:
		response := map[string]any{"success": false, "message": out.Message}
		if out.Plan != nil {
			response["plan"] = out.Plan
		}
		return public(c, cfg, response, 409, false)
	case core.PublishDeviceError:
		return httpx.Message(502, false, out.Message), nil
	}
	status := 200
	if out.Status == core.PublishFailed {
		status = 502
	}
	response := map[string]any{"success": out.Result != nil && out.Result.OK, "plan": out.Plan, "result": out.Result}
	if out.Plan == nil || !out.Plan.Empty {
		response["message"] = out.Message
	}
	return public(c, cfg, response, status, false)
}
func (h *handler) routes(c *httpx.Ctx) (*httpx.Response, error) {
	_, cfg, r, err := selected(c)
	if err != nil {
		return nil, err
	}
	if r == nil {
		return notFound()
	}
	d := core.NewDriver(r)
	if d == nil {
		return httpx.Message(502, false, messageMissing), nil
	}
	defer d.Close()
	rows, err := core.ReadRoutes(c.Context(), d)
	if err != nil {
		return failure(c, cfg, err, 0, false)
	}
	publicRows := make([]*core.PublicRoute, 0, len(rows))
	for _, row := range rows {
		publicRows = append(publicRows, row.Public())
	}
	return public(c, cfg, struct {
		Success bool                `json:"success"`
		Routes  []*core.PublicRoute `json:"routes"`
	}{true, publicRows}, 200, false)
}
func (h *handler) routeChange(c *httpx.Ctx) (*httpx.Response, error) {
	s, cfg, r, err := selected(c)
	if err != nil {
		return nil, err
	}
	if r == nil {
		return notFound()
	}
	apply := strings.HasSuffix(c.Request.URL.Path, "/apply")
	plan, result, err := s.EditRoute(c.Context(), c.ProfileID, r, body(c), apply)
	if err != nil {
		return failure(c, cfg, err, 0, false)
	}
	if !apply {
		return public(c, cfg, struct {
			Success bool            `json:"success"`
			Plan    *core.RoutePlan `json:"plan"`
		}{true, plan}, 200, false)
	}
	message := messageRouteUnchanged
	if result.Changed {
		message = messageRouteWritten
	}
	return public(c, cfg, struct {
		Success bool              `json:"success"`
		Result  *core.RouteResult `json:"result"`
		Message string            `json:"message"`
	}{true, result, message}, 200, false)
}
func (h *handler) policy(c *httpx.Ctx) (*httpx.Response, error) {
	s, cfg, r, err := selected(c)
	if err != nil {
		return nil, err
	}
	if r == nil {
		return notFound()
	}
	if r.Type != "routeros" {
		return httpx.Message(400, false, messagePolicyViewOnly), nil
	}
	held, release, err := core.RouterOperationLock(c.Context(), *r, false)
	if err != nil {
		return failure(c, cfg, err, 0, false)
	}
	defer release()
	original := c.Context()
	c.SetContext(held)
	defer c.SetContext(original)
	_, _, current, err := selected(c)
	if err != nil {
		return nil, err
	}
	if !equalRouter(current, r) {
		return httpx.Message(409, false, messageChanged), nil
	}
	d := core.NewRouterOS(r.Endpoint, r.Username, r.Password, r.VerifyTLS)
	defer d.Close()
	objects, err := core.ReadPolicyObjects(held, d, r.Extra["ros_policy_state"])
	if err != nil {
		return failure(c, cfg, err, 0, false)
	}
	peers, _, err := workspacePeers(c, s.Repo)
	if err != nil {
		return nil, err
	}
	sources := []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}{}
	for _, a := range peers {
		if a.ServiceType == "oxidns" && a.ProfileID == c.ProfileID {
			sources = append(sources, struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			}{a.ID, a.Name})
		}
	}
	return public(c, cfg, map[string]any{"success": true, "policy": map[string]any{"objects": objects, "source_agents": sources}, "generated_at": stamp(c)}, 200, true)
}
func equalRouter(a, b *core.Router) bool {
	if a == nil || b == nil {
		return a == b
	}
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return string(left) == string(right)
}
func (h *handler) upstream(c *httpx.Ctx) (*httpx.Response, error) {
	s, err := service(c)
	if err != nil {
		return nil, err
	}
	m, err := manager(c)
	if err != nil {
		return nil, err
	}
	var profile *string
	if value, ok := c.Arg("profile"); ok {
		profile = &value
	}
	value, err := core.AgentRouters(c.Context(), core.PendingRequest{AgentID: c.Param("agent_id"), ProfileID: profile, Refresh: c.ArgDefault("refresh", "") == "1" || c.ArgDefault("refresh", "") == "true", Repo: s.Repo, Agents: core.ManagerAgentSource{Manager: m}, Cache: h.pending})
	if errors.Is(err, core.ErrPendingTimeout) {
		return httpx.Message(504, false, messagePendingTimeout).NoStore(), nil
	}
	if err != nil {
		return nil, err
	}
	return public(c, nil, struct {
		Success bool `json:"success"`
		*core.PendingResponse
	}{true, value}, 200, true)
}
func (h *handler) sync(c *httpx.Ctx) (*httpx.Response, error) {
	s, cfg, r, err := selected(c)
	if err != nil {
		return nil, err
	}
	if r == nil {
		return notFound()
	}
	if r.Type != "routeros" {
		return httpx.Message(400, false, messageSyncOnly), nil
	}
	if c.Method() == "PUT" {
		raw := body(c)
		values, ok := fields(raw)
		if !ok || !exact(values, "auto_rule_sync", "rule_sync_interval") {
			return httpx.Message(400, false, messageSyncFormat), nil
		}
		if err = s.SaveSyncSettings(c.Context(), c.ProfileID, r, raw); err != nil {
			return failure(c, cfg, err, 409, false)
		}
		_, cfg, r, err = selected(c)
		if err != nil {
			return nil, err
		}
	}
	store := core.RuleSyncStore{DataDir: s.Repo.DataDir()}
	receipt, err := store.Read(c.Context(), c.ProfileID, r.ID)
	if err != nil {
		return nil, err
	}
	if c.Method() == "POST" {
		if !r.AutoRuleSync || receipt.Baseline == nil {
			return httpx.Message(409, false, messageSyncFirst), nil
		}
		if receipt.Status == "paused" {
			return public(c, cfg, map[string]any{"success": false, "message": core.PausedMessage(receipt)}, 409, false)
		}
		if receipt.Status == "running" || receipt.Status == "queued" {
			message := receipt.Message
			if message == "" {
				message = messageSyncNotReady
			}
			return public(c, cfg, map[string]any{"success": false, "message": message}, 409, false)
		}
		m, err := manager(c)
		if err != nil {
			return nil, err
		}
		runner := core.RuleSyncService{Repo: s.Repo, Agents: core.ManagerAgentSource{Manager: m}}
		if err = runner.StartJob(c.Context(), c.ProfileID, r.ID); err != nil {
			return failure(c, cfg, err, 409, false)
		}
		receipt, err = store.Read(c.Context(), c.ProfileID, r.ID)
		if err != nil {
			return nil, err
		}
	}
	status := 200
	if c.Method() == "POST" {
		status = 202
	}
	return public(c, cfg, map[string]any{"success": true, "settings": map[string]any{"auto_rule_sync": r.AutoRuleSync, "rule_sync_interval": r.RuleSyncInterval}, "sync": receipt.Public(r.AutoRuleSync)}, status, true)
}
func (h *handler) forwarding(c *httpx.Ctx) (*httpx.Response, error) {
	s, err := service(c)
	if err != nil {
		return nil, err
	}
	cfg, rows, err := s.Records(c.Context(), c.ProfileID)
	if err != nil {
		return nil, err
	}
	m, err := manager(c)
	if err != nil {
		return nil, err
	}
	bridge := host.Bridge{Repository: s.Repo, Manager: m}
	links := map[string][]host.RouterRef{}
	ids := []string{}
	for _, r := range rows {
		id := r.ProxyGatewayAgentID
		if id == "" {
			continue
		}
		if _, ok := links[id]; !ok {
			ids = append(ids, id)
			links[id] = []host.RouterRef{}
		}
		name := r.Name
		if name == "" {
			name = r.ID
		}
		links[id] = append(links[id], host.RouterRef{ID: r.ID, Name: name})
	}
	if c.Method() != "POST" {
		entries := []host.ForwardingEntry{}
		for _, id := range ids {
			entry, err := bridge.Forwarding(c.Context(), id, links[id])
			if err != nil {
				return nil, err
			}
			entries = append(entries, entry)
		}
		return public(c, cfg, map[string]any{"success": true, "gateways": entries}, 200, true)
	}
	values, ok := fields(body(c))
	id, valid := stringValue(values["agent_id"])
	if !ok || !valid || !exact(values, "agent_id") {
		return httpx.JSONStatus(400, map[string]any{"success": false, "code": "INVALID_REQUEST", "message": messageForwardFormat}).NoStore(), nil
	}
	refs, ok := links[id]
	if !ok {
		return httpx.JSONStatus(404, map[string]any{"success": false, "code": "NOT_LINKED", "message": messageForwardNotLinked}).NoStore(), nil
	}
	value, err := bridge.PersistForwarding(c.Context(), id, refs)
	if err != nil {
		var monitored *monitor.Error
		if errors.As(err, &monitored) {
			return public(c, cfg, map[string]any{"success": false, "code": monitored.Code, "message": monitored.Message}, monitored.Status, true)
		}
		return nil, err
	}
	logger.Infof(messageForwardSaved, logx.LogText(id, 120))
	return public(c, cfg, value, 200, true)
}
