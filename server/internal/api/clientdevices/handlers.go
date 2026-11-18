// Package clientdevices translates client identities, live discovery and previewed
// DHCP reservations to HTTP. Device writes and transactions belong to clients.
package clientdevices

import (
	"context"
	"encoding/json"
	"errors"

	"configflow/internal/agents"
	"configflow/internal/auth"
	"configflow/internal/clientrange"
	"configflow/internal/clients"
	"configflow/internal/deploy"
	"configflow/internal/devicenotes"
	"configflow/internal/httpx"
	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
	"configflow/internal/oxidns"
	"configflow/internal/redact"
	"configflow/internal/repo"
	"configflow/internal/routers"
	"configflow/internal/setupflow"
)

type handler struct{ cache *clients.DiscoveryCache }

// Register creates an app-owned discovery cache and installs five HTTP routes.
func Register(r *httpx.Router) {
	h := &handler{cache: clients.NewDiscoveryCache(nil, nil)}
	r.Handle([]string{"GET", "PUT"}, "/api/client-devices", "client_devices.client_devices", auth.Require(h.devices))
	r.GET("/api/client-devices/discovery", "client_devices.client_device_discovery", auth.Require(h.discovery))
	r.POST("/api/client-devices/fixed-ip/preview", "client_devices.preview_fixed_ip", auth.Require(h.preview))
	r.POST("/api/client-devices/fixed-ip/apply", "client_devices.apply_fixed_ip", auth.Require(h.apply))
}
func config(ctx context.Context, db *repo.Repository, profile string) (json.RawMessage, error) {
	value, err := db.GetCompatConfig(ctx, profile, repo.ResourceRoots{})
	if err != nil {
		return nil, err
	}
	return json.Marshal(value)
}
func manager(c *httpx.Ctx) (*repo.Repository, *agents.Manager, error) {
	db, err := repo.GetRepository(c.Context())
	if err != nil {
		return nil, nil, err
	}
	m, err := agents.New(c.Context(), db)
	return db, m, err
}
func profile(c *httpx.Ctx, db *repo.Repository, m *agents.Manager, byAgent bool) (string, *httpx.Response, error) {
	if id, exists := c.Arg("agent"); exists && byAgent {
		agent, err := m.GetAgentByID(c.Context(), id, false)
		if err != nil {
			return "", nil, err
		}
		if agent == nil || agent.GetString("service_type") != "oxidns" {
			return "", httpx.Message(404, false, messageAgentMissing).NoStore(), nil
		}
		id = agent.GetString("profile_id")
		if id == "" {
			id, err = db.DefaultProfileID(c.Context())
			if err != nil {
				return "", nil, err
			}
		}
		return id, nil, nil
	}
	return c.ProfileID, nil, nil
}
func body(c *httpx.Ctx) json.RawMessage { raw, _ := json.Marshal(c.JSONSilent()); return raw }
func exact(raw json.RawMessage, keys ...string) (map[string]json.RawMessage, bool) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) != len(keys) {
		return nil, false
	}
	for _, key := range keys {
		if fields[key] == nil {
			return nil, false
		}
	}
	return fields, true
}
func text(raw json.RawMessage) (string, bool) {
	if string(raw) == "null" || len(raw) == 0 {
		return "", false
	}
	var value string
	err := json.Unmarshal(raw, &value)
	return value, err == nil
}
func sanitize(ctx context.Context, config json.RawMessage, value any) (json.RawMessage, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	secrets, err := redact.ExternalPayloadSecrets(ctx, nil)
	if err != nil {
		return nil, err
	}
	return redact.SanitizeCredentials(json.RawMessage(encoded), redact.CollectCredentials(config, secrets)), nil
}
func public(ctx context.Context, config json.RawMessage, value any, status int) (*httpx.Response, error) {
	safe, err := sanitize(ctx, config, value)
	if err != nil {
		return nil, err
	}
	return httpx.JSONStatus(status, safe).NoStore(), nil
}

func failure(ctx context.Context, cfg json.RawMessage, err error) (*httpx.Response, error) {
	if err == nil {
		return nil, nil
	}
	status := 0
	var re *routers.RouterError
	if errors.As(err, &re) {
		status = 502
		if re.Kind == routers.KindConflict {
			status = 409
		}
	}
	if errors.Is(err, clients.ErrDeviceChanged) {
		status = 409
	}
	var settings *oxidns.SettingsError
	if errors.As(err, &settings) {
		status = 400
	}
	if status == 0 {
		return nil, err
	}
	return public(ctx, cfg, struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}{false, err.Error()}, status)
}
func state(ctx context.Context, db *repo.Repository, m *agents.Manager, id string) (json.RawMessage, any, error) {
	cfg, err := config(ctx, db, id)
	if err != nil {
		return nil, nil, err
	}
	list, err := clients.ListDevices(cfg)
	if err != nil {
		return cfg, nil, err
	}
	profile, err := db.GetProfile(ctx, id)
	if err != nil {
		return cfg, nil, err
	}
	def, err := db.DefaultProfileID(ctx)
	if err != nil {
		return cfg, nil, err
	}
	all, err := m.GetAllAgents(ctx, true)
	if err != nil {
		return cfg, nil, err
	}
	rows := []json.RawMessage{}
	for _, agent := range all {
		bound := agent.GetString("profile_id")
		if bound == "" {
			bound = def
		}
		if agent.GetString("service_type") != "oxidns" || bound != id {
			continue
		}
		name := agent.GetString("name")
		if name == "" {
			name = agent.GetString("id")
		}
		status := agent.GetString("status")
		if status == "" {
			status = "offline"
		}
		var deployment *jsonx.Object
		if raw := agent.GetObject("latest_deployment"); raw != nil {
			deployment = deploy.PublicDeployment(raw)
		}
		data, err := json.Marshal(struct {
			ID         string        `json:"id"`
			Name       string        `json:"name"`
			Host       string        `json:"host"`
			Status     string        `json:"status"`
			HA         any           `json:"oxidns_ha"`
			Deployment *jsonx.Object `json:"deployment"`
		}{agent.GetString("id"), name, agent.GetString("host"), status, agent.Value("oxidns_ha"), deployment})
		if err != nil {
			return cfg, nil, err
		}
		rows = append(rows, data)
	}
	p, err := oxidns.ProfileFromJSON(cfg)
	if err != nil {
		return cfg, nil, err
	}
	name := profile.GetString("name")
	if name == "" {
		name = id
	}
	return cfg, struct {
		Success     bool              `json:"success"`
		ProfileID   string            `json:"profile_id"`
		ProfileName string            `json:"profile_name"`
		Revision    string            `json:"oxidns_revision"`
		Agents      []json.RawMessage `json:"agents"`
		clients.DeviceList
	}{true, id, name, oxidns.Revision(p.Settings()), rows, list}, nil
}
func (h *handler) devices(c *httpx.Ctx) (*httpx.Response, error) {
	db, m, err := manager(c)
	if err != nil {
		return nil, err
	}
	id, early, err := profile(c, db, m, c.Method() != "PUT")
	if err != nil || early != nil {
		return early, err
	}
	cfg, err := config(c.Context(), db, id)
	if err != nil {
		return nil, err
	}
	if c.Method() == "PUT" {
		raw := body(c)
		fields, ok := exact(raw, "profile_id", "ip", "name", "mode", "previous_name", "previous_mode", "base_url")
		baseURL, valid := text(fields["base_url"])
		if !ok || !valid {
			return httpx.Message(400, false, messageDeviceFormat).NoStore(), nil
		}
		submitted, ok := text(fields["profile_id"])
		if !ok || submitted != id {
			return httpx.Message(409, false, messageProfileChanged).NoStore(), nil
		}
		change, err := deviceChange(fields)
		if err != nil {
			return failure(c.Context(), cfg, err)
		}
		work := lifecycle.Detach(c.Context())
		if err = work.Err(); err != nil {
			return nil, err
		}
		_, _, err = db.UpdateProfileTransaction(work, id, func(ctx context.Context, current *jsonx.Object) (*jsonx.Object, error) {
			raw, err := json.Marshal(current)
			if err != nil {
				return nil, err
			}
			replacement, err := clients.UpdateDevice(ctx, raw, change, baseURL)
			if err != nil {
				return nil, err
			}
			var next jsonx.Object
			if err = json.Unmarshal(replacement, &next); err != nil {
				return nil, err
			}
			return &next, ctx.Err()
		})
		if err != nil {
			return failure(work, cfg, err)
		}
	}
	cfg, value, err := state(c.Context(), db, m, id)
	if err != nil {
		return failure(c.Context(), cfg, err)
	}
	return public(c.Context(), cfg, value, 200)
}

// deviceChange reports the first invalid field in the page's order: address,
// name, previous name, then both modes. A value of the wrong JSON type fails
// the same rule as an invalid value of that field.
func deviceChange(fields map[string]json.RawMessage) (clients.Change, error) {
	change := clients.Change{}
	rules := []struct {
		key     string
		target  *string
		message string
		check   func(string) bool
	}{
		{"ip", &change.IP, devicenotes.AddressMessage, func(v string) bool { _, ok := clientrange.Normalize(v); return ok }},
		{"name", &change.Name, devicenotes.NameMessage, func(v string) bool { _, err := devicenotes.NormalizeName(v); return err == nil }},
		{"previous_name", &change.PreviousName, devicenotes.NameMessage, func(v string) bool { _, err := devicenotes.NormalizeName(v); return err == nil }},
		{"mode", &change.Mode, messageDeviceMode, nil},
		{"previous_mode", &change.PreviousMode, messageDeviceMode, nil},
	}
	for _, rule := range rules {
		value, ok := text(fields[rule.key])
		if !ok || rule.check != nil && !rule.check(value) {
			return clients.Change{}, &oxidns.SettingsError{Msg: rule.message}
		}
		*rule.target = value
	}
	return clients.NormalizeChange(change)
}
func setupGuard(ctx context.Context, db *repo.Repository, id string) error {
	sessions, err := setupflow.ProfileSessions(ctx, db.DataDir(), id)
	if err != nil {
		return err
	}
	for _, session := range sessions {
		var flags struct {
			Completed bool `json:"completed"`
			Abandoned bool `json:"abandoned"`
		}
		if err = json.Unmarshal(session.Raw, &flags); err != nil {
			return err
		}
		if !flags.Completed && !flags.Abandoned {
			return &routers.RouterError{Kind: routers.KindConflict, Message: messageSetup}
		}
	}
	return nil
}
func (h *handler) fixed(db *repo.Repository) *clients.FixedIPService {
	return &clients.FixedIPService{Repo: db, GuardSetup: setupGuard, Cache: h.cache}
}
func (h *handler) discovery(c *httpx.Ctx) (*httpx.Response, error) {
	db, m, err := manager(c)
	if err != nil {
		return nil, err
	}
	id, early, err := profile(c, db, m, true)
	if err != nil || early != nil {
		return early, err
	}
	cfg, err := config(c.Context(), db, id)
	if err != nil {
		return nil, err
	}
	def, err := db.DefaultProfileID(c.Context())
	if err != nil {
		return nil, err
	}
	all, err := m.GetAllAgents(c.Context(), false)
	if err != nil {
		return nil, err
	}
	hosts := []string{}
	for _, agent := range all {
		bound := agent.GetString("profile_id")
		if bound == "" {
			bound = def
		}
		if bound == id {
			hosts = append(hosts, agent.GetString("host"))
		}
	}
	service := clients.NewDiscoveryService()
	service.Enrich = clients.EnrichFixedIP
	service.GatewayHost = func(ctx context.Context, id string) string {
		agent, err := m.GetAgentByID(ctx, id, false)
		if err != nil || agent == nil {
			return ""
		}
		return agent.GetString("host")
	}
	value, err := service.Discover(c.Context(), cfg, clients.DiscoveryOptions{AgentHosts: hosts, ProfileID: id, Refresh: c.ArgDefault("refresh", "") == "1", Cache: h.cache})
	if err != nil {
		return failure(c.Context(), cfg, err)
	}
	return public(c.Context(), cfg, struct {
		Success   bool   `json:"success"`
		ProfileID string `json:"profile_id"`
		clients.Discovery
	}{true, id, value}, 200)
}
func (h *handler) preview(c *httpx.Ctx) (*httpx.Response, error) {
	db, m, err := manager(c)
	if err != nil {
		return nil, err
	}
	id, early, err := profile(c, db, m, false)
	if err != nil || early != nil {
		return early, err
	}
	cfg, err := config(c.Context(), db, id)
	if err != nil {
		return nil, err
	}
	raw, intent, err := clients.NormalizeFixedIPRequest(body(c))
	if err != nil {
		return failure(c.Context(), cfg, err)
	}
	if submitted, ok := text(raw); !ok || submitted != id {
		return httpx.Message(409, false, messageProfileChanged).NoStore(), nil
	}
	value, err := h.fixed(db).Preview(c.Context(), id, intent)
	if err != nil {
		return failure(c.Context(), cfg, err)
	}
	safe, err := sanitize(c.Context(), cfg, value.Actions)
	if err != nil {
		return nil, err
	}
	return httpx.JSON(struct {
		Success bool            `json:"success"`
		Token   string          `json:"token"`
		Actions json.RawMessage `json:"actions"`
	}{true, value.Token, safe}).NoStore(), nil
}
func (h *handler) apply(c *httpx.Ctx) (*httpx.Response, error) {
	db, m, err := manager(c)
	if err != nil {
		return nil, err
	}
	id, early, err := profile(c, db, m, false)
	if err != nil || early != nil {
		return early, err
	}
	cfg, err := config(c.Context(), db, id)
	if err != nil {
		return nil, err
	}
	fields, ok := exact(body(c), "profile_id", "token")
	if !ok {
		return httpx.Message(400, false, messageConfirmFormat).NoStore(), nil
	}
	submitted, ok := text(fields["profile_id"])
	if !ok || submitted != id {
		return httpx.Message(409, false, messageProfileChanged).NoStore(), nil
	}
	token, _ := text(fields["token"])
	if err = h.fixed(db).Apply(c.Context(), id, token); err != nil {
		return failure(c.Context(), cfg, err)
	}
	cfg, value, err := state(lifecycle.Detach(c.Context()), db, m, id)
	if err != nil {
		return failure(c.Context(), cfg, err)
	}
	return public(c.Context(), cfg, value, 200)
}
