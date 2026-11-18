package host

import (
	"context"
	"encoding/json"
	"errors"
	"unicode"

	"configflow/internal/agents"
	"configflow/internal/agents/monitor"
	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
)

// RouterRef identifies the authorizing router and, for global views, its profile.
type RouterRef struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ProfileID   string `json:"profile_id,omitempty"`
	ProfileName string `json:"profile_name,omitempty"`
}

// ProxyRouters returns this profile's routers linked to the Agent.
func ProxyRouters(config *jsonx.Object, id string) []RouterRef {
	out := []RouterRef{}
	if id == "" {
		return out
	}
	for _, raw := range config.GetObject("routers").GetList("items") {
		r, ok := raw.(*jsonx.Object)
		if ok && r.GetString("proxy_gateway_agent_id") == id {
			name := r.GetString("name")
			if name == "" {
				name = r.GetString("id")
			}
			out = append(out, RouterRef{ID: r.GetString("id"), Name: name})
		}
	}
	return out
}

// LinkedAgent is one stable-order forwarding target and its router references.
type LinkedAgent struct {
	AgentID string
	Routers []RouterRef
}

// ProxyGatewaysAllProfiles follows links across profiles, without changing binding.
func (b *Bridge) ProxyGatewaysAllProfiles(ctx context.Context) ([]LinkedAgent, error) {
	profiles, err := b.Repository.ListProfiles(ctx)
	if err != nil {
		return nil, err
	}
	out := []LinkedAgent{}
	positions := map[string]int{}
	for _, raw := range profiles {
		p, ok := raw.(*jsonx.Object)
		if !ok {
			continue
		}
		id := p.GetString("id")
		config, err := b.Repository.GetCompatConfig(ctx, id, repo.ResourceRoots{})
		if err != nil {
			return nil, err
		}
		for _, raw := range config.GetObject("routers").GetList("items") {
			r, ok := raw.(*jsonx.Object)
			if !ok {
				continue
			}
			aid := r.GetString("proxy_gateway_agent_id")
			if aid == "" {
				continue
			}
			pos, found := positions[aid]
			if !found {
				pos = len(out)
				positions[aid] = pos
				out = append(out, LinkedAgent{aid, []RouterRef{}})
			}
			name := r.GetString("name")
			if name == "" {
				name = r.GetString("id")
			}
			pn := p.GetString("name")
			if pn == "" {
				pn = id
			}
			out[pos].Routers = append(out[pos].Routers, RouterRef{r.GetString("id"), name, id, pn})
		}
	}
	return out, nil
}

// ForwardingEntry is the cross-profile forwarding view consumed by Agents and routers.
type ForwardingEntry struct {
	AgentID             string          `json:"agent_id"`
	AgentName           string          `json:"agent_name"`
	AgentVersion        string          `json:"agent_version"`
	Routers             []RouterRef     `json:"routers"`
	State               string          `json:"state"`
	CanPersist          bool            `json:"can_persist"`
	Message             string          `json:"message"`
	MinimumVersion      *string         `json:"minimum_version"`
	Current             json.RawMessage `json:"current"`
	Persisted           json.RawMessage `json:"persisted"`
	Config              json.RawMessage `json:"config"`
	HookScript          json.RawMessage `json:"hook_script"`
	StartupHook         json.RawMessage `json:"startup_hook"`
	Layout              json.RawMessage `json:"layout"`
	GatewayRecord       bool            `json:"gateway_record"`
	PreservesForwarding bool            `json:"preserves_forwarding"`
	ForwardFilter       *ForwardFilter  `json:"forward_filter"`
	SystemSource        *string         `json:"system_source"`
}

func entry(id string, a *model.Agent, refs []RouterRef, state, message string, status Reply, can bool) ForwardingEntry {
	e := ForwardingEntry{AgentID: id, AgentName: id, Routers: refs, State: state, Message: message, CanPersist: can, GatewayRecord: HasRecord(status), PreservesForwarding: yes(object(status["managed"])["preserves_forwarding"]), ForwardFilter: SanitizeForwardFilter(status["forward_filter"])}
	if a != nil {
		e.AgentVersion = a.Version
		if a.Name != "" {
			e.AgentName = a.Name
		}
	}
	if state == "upgrade_required" {
		v := StandaloneVersion
		e.MinimumVersion = &v
	}
	f := object(status["ipv4_forwarding"])
	e.Current = f["current"]
	e.Persisted = f["persisted"]
	e.Config = f["config"]
	e.HookScript = f["hook_script"]
	e.StartupHook = f["startup_hook"]
	e.Layout = f["layout"]
	if state == "system_persisted" {
		s := text(f["system_source"])
		valid := len([]rune(s)) > 0 && len([]rune(s)) <= 300
		for _, r := range s {
			if !unicode.IsPrint(r) {
				valid = false
			}
		}
		if valid {
			e.SystemSource = &s
		}
	}
	return e
}
func statusEntry(id string, a *model.Agent, refs []RouterRef, status Reply) ForwardingEntry {
	f := object(status["ipv4_forwarding"])
	if upgradeNeeded(a, status) {
		m := msgStandalone
		if f == nil {
			m = msgLinkedUpgrade
		}
		return entry(id, a, refs, "upgrade_required", m, status, false)
	}
	if string(f["supported"]) == "false" {
		return entry(id, a, refs, "unsupported", text(f["unsupported_reason"]), status, false)
	}
	c := text(f["config"])
	if c == "foreign" || c == "symlink" {
		return entry(id, a, refs, "blocked", msgConfigBlocked, status, false)
	}
	if text(f["hook_script"]) == "modified" {
		return entry(id, a, refs, "blocked", msgHookBlocked, status, false)
	}
	if text(f["current"]) == "1" && yes(f["persisted"]) {
		return entry(id, a, refs, "persisted", "", status, false)
	}
	if text(f["current"]) == "1" && yes(f["system_persisted"]) {
		return entry(id, a, refs, "system_persisted", "", status, true)
	}
	message := text(f["error"])
	if message == "" {
		message = text(f["notice"])
	}
	return entry(id, a, refs, "needs_persist", message, status, true)
}
func precheck(a *model.Agent, raw *jsonx.Object) (string, string) {
	if a == nil {
		return "missing", msgLinkedMissing
	}
	if a.ServiceType != "mihomo" {
		return "not_mihomo", msgLinkedService
	}
	if a.DeploymentMethod == "docker" {
		return "docker", msgLinkedDocker
	}
	if !agents.IsOnline(raw, nil) {
		return "offline", msgOffline
	}
	if !Supports(a, ForwardingVersion) {
		return "upgrade_required", msgLinkedUpgrade
	}
	return "", ""
}

// Forwarding reads one target without failing the whole global status list.
func (b *Bridge) Forwarding(ctx context.Context, id string, refs []RouterRef) (ForwardingEntry, error) {
	a, raw, err := b.agent(ctx, id)
	if err != nil {
		return ForwardingEntry{}, err
	}
	if state, msg := precheck(a, raw); state != "" {
		return entry(id, a, refs, state, msg, nil, false), nil
	}
	s, err := b.Request(ctx, raw, "status", "GET", nil)
	if err != nil {
		var e *monitor.Error
		if !errors.As(err, &e) {
			return ForwardingEntry{}, err
		}
		if e.Code == "UPGRADE_REQUIRED" {
			return entry(id, a, refs, "upgrade_required", msgLinkedUpgrade, nil, false), nil
		}
		return entry(id, a, refs, "error", e.Message, nil, false), nil
	}
	if !validForwardFilter(s["forward_filter"]) {
		return entry(id, a, refs, "error", msgShape, nil, false), nil
	}
	return statusEntry(id, a, refs, s), nil
}

// PersistForwarding only enables persistence; its caller authorizes the router link.
func (b *Bridge) PersistForwarding(ctx context.Context, id string, refs []RouterRef) (Reply, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	a, raw, err := b.agent(ctx, id)
	if err != nil {
		return nil, err
	}
	if state, msg := precheck(a, raw); state != "" {
		code, status := "UPGRADE_REQUIRED", 409
		switch state {
		case "missing":
			code, status = "AGENT_NOT_FOUND", 404
		case "not_mihomo":
			code, status = "WRONG_SERVICE", 400
		case "docker":
			code = "UNSUPPORTED_ENVIRONMENT"
		case "offline":
			code = "AGENT_OFFLINE"
			msg = msgOfflineWrite
		}
		return nil, fail(code, status, msg)
	}
	if err := b.requireLayout(ctx, a, raw); err != nil {
		return nil, err
	}
	data, err := b.Request(ctx, raw, "forwarding", "POST", json.RawMessage(`{}`))
	if err != nil {
		return nil, err
	}
	status := Reply{"supported": json.RawMessage(`true`), "ipv4_forwarding": data["ipv4_forwarding"], "managed": data["managed"]}
	put(data, "gateway", statusEntry(id, a, refs, status))
	return data, nil
}
