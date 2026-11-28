// Package clientdevices translates client identities, live discovery and previewed
// DHCP reservations to HTTP. Device writes and transactions belong to clients.
package clientdevices

import (
	"configflow/internal/routers/device"
	"configflow/internal/store"
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"

	"configflow/internal/agents"
	"configflow/internal/auth"
	"configflow/internal/clientrange"
	"configflow/internal/clients"
	"configflow/internal/deploy"
	"configflow/internal/devicenotes"
	"configflow/internal/httpx"
	"configflow/internal/jsondecode"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/oxidns"
	"configflow/internal/redact"
	"configflow/internal/repo"
	"configflow/internal/setupflow"
)

// Handler serves the /api/client-devices routes backed by an explicit
// Repository, Agent Manager, and per-handler discovery cache.
type Handler struct {
	Repo   *repo.Repository
	Agents *agents.Manager
	cache  *clients.DiscoveryCache
	viewMu sync.Mutex
	view   *deviceView
}

// NewHandler constructs a Handler with an app-owned discovery cache. Nil
// dependencies fall back to context/package defaults until W11.
func NewHandler(r *repo.Repository, m *agents.Manager) *Handler {
	return &Handler{
		Repo: r, Agents: m,
		cache: clients.NewDiscoveryCache(nil, nil),
		view:  &deviceView{},
	}
}

// Register installs the five HTTP routes on r.
func (h *Handler) Register(r *httpx.Router) {
	if h.cache == nil {
		h.cache = clients.NewDiscoveryCache(nil, nil)
	}
	r.Handle([]string{"GET", "PUT"}, "/api/client-devices", "client_devices.client_devices", auth.Require(h.devices))
	r.GET("/api/client-devices/discovery", "client_devices.client_device_discovery", auth.Require(h.discovery))
	r.POST("/api/client-devices/fixed-ip/preview", "client_devices.preview_fixed_ip", auth.Require(h.preview))
	r.POST("/api/client-devices/fixed-ip/apply", "client_devices.apply_fixed_ip", auth.Require(h.apply))
}

func config(document *model.Document, profile string) (json.RawMessage, error) {
	value, err := repo.ConfigWithOxiDNSAgentsFrom(document, profile)
	if err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

func readConfig(ctx context.Context, db *repo.Repository, profile string) (raw json.RawMessage, err error) {
	err = db.View(lifecycle.Detach(ctx), func(doc *model.Document) error {
		raw, err = config(doc, profile)
		return err
	})
	return raw, err
}

func (h *Handler) manager(c *httpx.Ctx) (*repo.Repository, *agents.Manager, error) {
	if h == nil || h.Repo == nil {
		return nil, nil, errors.New("api/clientdevices: repository is nil")
	}
	db := h.Repo
	if h.Agents != nil {
		return db, h.Agents, nil
	}
	m, err := agents.New(c.Context(), db)
	return db, m, err
}
func profile(c *httpx.Ctx, db *repo.Repository, m *agents.Manager, byAgent bool) (string, *httpx.Response, error) {
	if id, exists := c.Arg("agent"); exists && byAgent {
		agent, err := m.ReadAgent(lifecycle.Detach(c.Context()), id, false)
		if err != nil {
			return "", nil, err
		}
		if agent == nil || agent.ServiceType != "oxidns" {
			return "", httpx.Message(404, false, messageAgentMissing).NoStore(), nil
		}
		raw, _, readErr := model.EncodeMember(agent, "profile_id")
		if readErr != nil {
			return "", nil, readErr
		}
		id = ""
		_ = json.Unmarshal(raw, &id)
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
func body(c *httpx.Ctx) json.RawMessage { var raw json.RawMessage; _ = c.BindSilent(&raw); return raw }
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
func (h *Handler) sanitize(ctx context.Context, config json.RawMessage, value any) (json.RawMessage, error) {
	return h.sanitizeWith(ctx, redact.CollectCredentials(config, nil), value)
}
func (h *Handler) sanitizeWith(ctx context.Context, profileSecrets redact.Secrets, value any) (json.RawMessage, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	if h == nil || h.Repo == nil {
		return nil, errors.New("api/clientdevices: repository is nil")
	}
	r := h.Repo
	secrets, err := redact.ExternalPayloadSecrets(ctx, r, nil)
	if err == nil {
		secrets, err = redact.AgentRecordCredentials(ctx, r, secrets)
	}
	if err != nil {
		return nil, err
	}
	return redact.SanitizeCredentials(json.RawMessage(encoded), redact.NewSecrets(profileSecrets, secrets)), nil
}
func (h *Handler) public(ctx context.Context, config json.RawMessage, value any, status int) (*httpx.Response, error) {
	safe, err := h.sanitize(ctx, config, value)
	if err != nil {
		return nil, err
	}
	return httpx.JSONStatus(status, safe).NoStore(), nil
}

func (h *Handler) failure(ctx context.Context, cfg json.RawMessage, err error) (*httpx.Response, error) {
	if err == nil {
		return nil, nil
	}
	status := 0
	var re *device.RouterError
	if errors.As(err, &re) {
		status = 502
		if re.Kind == device.KindConflict {
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
	return h.public(ctx, cfg, struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}{false, err.Error()}, status)
}

type deviceView struct {
	head                store.Head
	id, registrationKey string
	config              json.RawMessage
	list                clients.DeviceList
	revision            string
	secrets             redact.Secrets
}

func (h *Handler) profileView(ctx context.Context, db *repo.Repository, id string) (*deviceView, error) {
	var result *deviceView
	err := db.ViewAt(ctx, func(doc *model.Document, head *store.Head) error {
		h.viewMu.Lock()
		defer h.viewMu.Unlock()
		registrationKey := os.Getenv("AGENT_REGISTRATION_KEY")
		if head != nil && h.view != nil && h.view.config != nil && h.view.id == id && h.view.head == *head && h.view.registrationKey == registrationKey {
			result = h.view
			return nil
		}
		raw, err := config(doc, id)
		if err != nil {
			return err
		}
		result = &deviceView{id: id, config: raw, registrationKey: registrationKey}
		p, err := oxidns.ProfileFromJSON(raw)
		if err != nil {
			return err
		}
		result.list, err = clients.ListProfileDevices(raw, p)
		if err != nil {
			return err
		}
		result.revision = oxidns.Revision(p.Settings())
		result.secrets = redact.CollectCredentials(raw, nil)
		if head != nil {
			result.head = *head
			h.view = result
		}
		return nil
	})
	return result, err
}

func (h *Handler) respondState(ctx context.Context, db *repo.Repository, m *agents.Manager, id string) (*httpx.Response, error) {
	view, value, err := h.state(ctx, db, m, id)
	if err != nil {
		var cfg json.RawMessage
		if view != nil {
			cfg = view.config
		}
		return h.failure(ctx, cfg, err)
	}
	safe, err := h.sanitizeWith(ctx, view.secrets, value)
	if err != nil {
		return nil, err
	}
	return httpx.JSONStatus(200, safe).NoStore(), nil
}

func (h *Handler) state(ctx context.Context, db *repo.Repository, m *agents.Manager, id string) (*deviceView, any, error) {
	ctx = lifecycle.Detach(ctx)
	cfg, err := h.profileView(ctx, db, id)
	if err != nil {
		return cfg, nil, err
	}
	def, err := db.DefaultProfileID(ctx)
	if err != nil {
		return cfg, nil, err
	}
	all, err := m.ListAgents(ctx, true)
	if err != nil {
		return cfg, nil, err
	}
	rows := []json.RawMessage{}
	for i := range all {
		agent := &all[i]
		// Display/identity fields require original string tokens, not R5 numeric projections.
		bound, err := storedAgentString(agent, "profile_id")
		if err != nil {
			return cfg, nil, err
		}
		if bound == "" {
			bound = def
		}
		serviceType, err := storedAgentString(agent, "service_type")
		if err != nil {
			return cfg, nil, err
		}
		if serviceType != "oxidns" || bound != id {
			continue
		}
		agentID, err := storedAgentString(agent, "id")
		if err != nil {
			return cfg, nil, err
		}
		name, err := storedAgentString(agent, "name")
		if err != nil {
			return cfg, nil, err
		}
		if name == "" {
			name = agentID
		}
		host, err := storedAgentString(agent, "host")
		if err != nil {
			return cfg, nil, err
		}
		status := "offline"
		if agents.FreshHeartbeat(agent.LastHeartbeat, nil) {
			status, err = storedAgentString(agent, "status")
			if err != nil {
				return cfg, nil, err
			}
			if status == "" {
				status = "offline"
			}
		}
		var deployment *model.Deployment
		if agent.LatestDeployment != nil {
			deployment = deploy.PublicDeployment(agent.LatestDeployment)
		}
		ha, ok, err := model.EncodeMember(agent, "oxidns_ha")
		if err != nil {
			return cfg, nil, err
		}
		if !ok || len(ha) == 0 {
			ha = json.RawMessage("null")
		}
		data, err := json.Marshal(struct {
			ID         string            `json:"id"`
			Name       string            `json:"name"`
			Host       string            `json:"host"`
			Status     string            `json:"status"`
			HA         json.RawMessage   `json:"oxidns_ha"`
			Deployment *model.Deployment `json:"deployment"`
		}{agentID, name, host, status, ha, deployment})
		if err != nil {
			return cfg, nil, err
		}
		rows = append(rows, data)
	}
	profile, err := db.ProfileMetadataRecord(ctx, id)
	if err != nil {
		return cfg, nil, err
	}
	name := profile.Name
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
	}{true, id, name, cfg.revision, rows, cfg.list}, nil
}
func (h *Handler) devices(c *httpx.Ctx) (*httpx.Response, error) {
	db, m, err := h.manager(c)
	if err != nil {
		return nil, err
	}
	id, early, err := profile(c, db, m, c.Method() != "PUT")
	if err != nil || early != nil {
		return early, err
	}
	if c.Method() == "PUT" {
		cfg, err := readConfig(c.Context(), db, id)
		if err != nil {
			return nil, err
		}
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
			return h.failure(c.Context(), cfg, err)
		}
		work := lifecycle.Detach(c.Context())
		if err = work.Err(); err != nil {
			return nil, err
		}
		_, err = db.UpdateProfile(work, id, func(ctx context.Context, current *model.Profile) error {
			raw, err := json.Marshal(current)
			if err != nil {
				return err
			}
			replacement, err := clients.UpdateDevice(ctx, raw, change, baseURL)
			if err != nil {
				return err
			}
			var next model.Profile
			if err = json.Unmarshal(replacement, &next); err != nil {
				return err
			}
			*current = next
			return ctx.Err()
		})
		if err != nil {
			return h.failure(work, cfg, err)
		}
	}
	return h.respondState(c.Context(), db, m, id)
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
		if err = jsondecode.Unmarshal(session.Raw, &flags); err != nil {
			return err
		}
		if !flags.Completed && !flags.Abandoned {
			return &device.RouterError{Kind: device.KindConflict, Message: messageSetup}
		}
	}
	return nil
}
func (h *Handler) fixed(db *repo.Repository) *clients.FixedIPService {
	return &clients.FixedIPService{Repo: db, GuardSetup: setupGuard, Cache: h.cache}
}
func (h *Handler) discovery(c *httpx.Ctx) (*httpx.Response, error) {
	db, m, err := h.manager(c)
	if err != nil {
		return nil, err
	}
	id, early, err := profile(c, db, m, true)
	if err != nil || early != nil {
		return early, err
	}
	cfg, err := readConfig(c.Context(), db, id)
	if err != nil {
		return nil, err
	}
	def, err := db.DefaultProfileID(c.Context())
	if err != nil {
		return nil, err
	}
	all, err := m.ReadAgents(lifecycle.Detach(c.Context()), false, nil)
	if err != nil {
		return nil, err
	}
	hosts := []string{}
	for _, agent := range all {
		raw, _, readErr := model.EncodeMember(&agent, "profile_id")
		if readErr != nil {
			return nil, readErr
		}
		var bound string
		_ = json.Unmarshal(raw, &bound)
		if bound == "" {
			bound = def
		}
		if bound == id {
			raw, _, readErr = model.EncodeMember(&agent, "host")
			if readErr != nil {
				return nil, readErr
			}
			var host string
			_ = json.Unmarshal(raw, &host)
			hosts = append(hosts, host)
		}
	}
	service := clients.NewDiscoveryService()
	service.Enrich = clients.EnrichFixedIP
	service.GatewayHost = func(ctx context.Context, id string) string {
		agent, err := m.ReadAgent(lifecycle.Detach(ctx), id, false)
		if err != nil || agent == nil {
			return ""
		}
		raw, _, readErr := model.EncodeMember(agent, "host")
		if readErr != nil {
			return ""
		}
		var host string
		_ = json.Unmarshal(raw, &host)
		return host
	}
	value, err := service.Discover(c.Context(), cfg, clients.DiscoveryOptions{AgentHosts: hosts, ProfileID: id, Refresh: c.ArgDefault("refresh", "") == "1", Cache: h.cache})
	if err != nil {
		return h.failure(c.Context(), cfg, err)
	}
	return h.public(c.Context(), cfg, struct {
		Success   bool   `json:"success"`
		ProfileID string `json:"profile_id"`
		clients.Discovery
	}{true, id, value}, 200)
}
func (h *Handler) preview(c *httpx.Ctx) (*httpx.Response, error) {
	db, m, err := h.manager(c)
	if err != nil {
		return nil, err
	}
	id, early, err := profile(c, db, m, false)
	if err != nil || early != nil {
		return early, err
	}
	cfg, err := readConfig(c.Context(), db, id)
	if err != nil {
		return nil, err
	}
	raw, intent, err := clients.NormalizeFixedIPRequest(body(c))
	if err != nil {
		return h.failure(c.Context(), cfg, err)
	}
	if submitted, ok := text(raw); !ok || submitted != id {
		return httpx.Message(409, false, messageProfileChanged).NoStore(), nil
	}
	value, err := h.fixed(db).Preview(c.Context(), id, intent)
	if err != nil {
		return h.failure(c.Context(), cfg, err)
	}
	safe, err := h.sanitize(c.Context(), cfg, value.Actions)
	if err != nil {
		return nil, err
	}
	return httpx.JSON(struct {
		Success bool            `json:"success"`
		Token   string          `json:"token"`
		Actions json.RawMessage `json:"actions"`
	}{true, value.Token, safe}).NoStore(), nil
}
func (h *Handler) apply(c *httpx.Ctx) (*httpx.Response, error) {
	db, m, err := h.manager(c)
	if err != nil {
		return nil, err
	}
	id, early, err := profile(c, db, m, false)
	if err != nil || early != nil {
		return early, err
	}
	cfg, err := readConfig(c.Context(), db, id)
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
		return h.failure(c.Context(), cfg, err)
	}
	return h.respondState(lifecycle.Detach(c.Context()), db, m, id)
}

func storedAgentString(agent *model.Agent, key string) (string, error) {
	raw, _, err := model.EncodeMember(agent, key)
	if err != nil {
		return "", err
	}
	var value string
	_ = json.Unmarshal(raw, &value)
	return value, nil
}
