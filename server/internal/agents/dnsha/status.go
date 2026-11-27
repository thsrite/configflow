package dnsha

import (
	"configflow/internal/jsonresp"
	"context"
	"encoding/json"
	"strings"

	"configflow/internal/deploy"
	"configflow/internal/model"
)

// MarshalJSON keeps Python's distinction between an unconfigured and a paired view.
func (v StatusView) MarshalJSON() ([]byte, error) {
	type plain StatusView
	b, e := json.Marshal(plain(v))
	if e != nil {
		return nil, e
	}
	o := parseObject(b)
	if v.Config != nil {
		o["state"] = jsonresp.Value(v.State)
		o["publication"] = jsonresp.Value(v.Publication)
	}
	if v.Config != nil || v.Publication != nil {
		w := v.Warnings
		if w == nil {
			w = []string{}
		}
		r := v.Routers
		if r == nil {
			r = []model.HARouter{}
		}
		o["warnings"] = jsonresp.Value(w)
		o["routers"] = jsonresp.Value(r)
	}
	return json.Marshal(o)
}

// MarshalJSON always includes capability lists for a successful guide response.
func (v EnvironmentView) MarshalJSON() ([]byte, error) {
	if !v.Success {
		return json.Marshal(struct {
			Success bool   `json:"success"`
			Message string `json:"message"`
		}{false, v.Message})
	}
	type plain EnvironmentView
	b, e := json.Marshal(plain(v))
	if e != nil {
		return nil, e
	}
	o := parseObject(b)
	for k, list := range map[string][]int{"oxidns_ha_protocols": v.Protocols, "oxidns_ha_container_protocols": v.ContainerProtocols, "oxidns_ha_handoff_protocols": v.HandoffProtocols} {
		if list == nil {
			list = []int{}
		}
		o[k] = jsonresp.Value(list)
	}
	return json.Marshal(o)
}
func (s *Service) member(ctx context.Context, a *agent, role *string, problem string) MemberView {
	v := MemberView{AgentID: a.ID, Name: name(a), Host: a.Host, Online: a.Status == "online", Role: role, Error: problem}
	if problem != "" {
		return v
	}
	if !v.Online {
		v.Error = msgOffline
		return v
	}
	o, e := s.call(ctx, a, "GET", "", nil, 15)
	if e != nil {
		v.Error = s.safe(ctx, e.Error())
		return v
	}
	v.Runtime = runtimeOf(o)
	managed := o.obj("managed")
	if o.truthy("error") {
		v.Error = s.safe(ctx, o.text("error"))
	} else if managedRaw, ok := o["managed"]; !ok || string(managedRaw) == "null" || len(managed) == 0 && string(managedRaw) != "{}" {
		v.Error = msgNoManaged
	} else if !managed.isTrue("active") || managed.isTrue("pending") {
		v.Error = msgPendingManaged
	}
	return v
}
func health(members []MemberView, state *model.HAState) string {
	runtimes, holders, running := 0, 0, 0
	healthy, clean := true, true
	for _, m := range members {
		if m.Error != "" {
			clean = false
		}
		if m.Runtime == nil || string(m.Runtime.raw) == "{}" {
			continue
		}
		runtimes++
		if m.Runtime.VIPPresent {
			holders++
		}
		if m.Runtime.Running {
			running++
		}
		healthy = healthy && m.Runtime.Healthy
	}
	if holders >= 2 || (running == 2 && holders == 0) {
		return "split"
	}
	if holders == 0 {
		if runtimes == 1 {
			return "degraded"
		}
		return "down"
	}
	if running == 2 && healthy && clean && state != nil && state.Status == "succeeded" {
		return "ok"
	}
	return "degraded"
}

// Status advances accepted work before reporting live HA roles.
func (s *Service) Status(ctx context.Context, id string) (out StatusView, err error) {
	err = deploy.RunDetached(ctx, "oxidns-ha-status", func(ctx context.Context) error {
		if _, e := s.advance(ctx, id); e != nil {
			return e
		}
		var e error
		out, e = s.status(ctx, id)
		return e
	})
	return
}
func (s *Service) status(ctx context.Context, id string) (StatusView, error) {
	list, e := s.all(ctx)
	if e != nil {
		return StatusView{}, e
	}
	a := selected(list, id)
	if a == nil {
		return StatusView{}, refuse(msgMissing)
	}
	v := StatusView{Success: true, Publication: summary(a.OxiDNSHAPublication)}
	if v.Publication != nil {
		v.Warnings = v.Publication.Warnings
		v.Routers = v.Publication.Routers
	}
	if !haEnabled(a) {
		return v, nil
	}
	v.Config = a.OxiDNSHA
	v.State = a.OxiDNSHAState
	v.Members = []MemberView{s.member(ctx, a, new(a.OxiDNSHA.Role), "")}
	b := selected(list, a.OxiDNSHA.PeerID)
	if b == nil {
		v.Members = append(v.Members, MemberView{AgentID: a.OxiDNSHA.PeerID, Error: msgPeerGone})
	} else if !paired(a, b) {
		v.Members = append(v.Members, s.member(ctx, b, nil, msgPeerDifferent))
	} else {
		v.Members = append(v.Members, s.member(ctx, b, new(b.OxiDNSHA.Role), ""))
	}
	if ctx.Err() != nil {
		return StatusView{}, ctx.Err()
	}
	v.Health = health(v.Members, v.State)
	return v, nil
}
func limited(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}
func cleanProtocols(o object, key string) []int {
	out := []int{}
	for _, r := range o.list(key) {
		var n int
		if json.Unmarshal(r, &n) == nil && n >= 0 && n < 100 && string(r) != "null" {
			out = append(out, n)
		}
	}
	return out
}

// Environment reads live installation proofs without advancing publications.
func (s *Service) Environment(ctx context.Context, id string) (EnvironmentView, int, error) {
	a, e := s.get(ctx, id)
	if e != nil {
		return EnvironmentView{}, 0, e
	}
	if a == nil || a.ServiceType != "oxidns" {
		return EnvironmentView{Message: "OxiDNS Agent not found"}, 404, nil
	}
	b, e := s.capabilities(ctx, a)
	if e != nil {
		return EnvironmentView{Message: msgEnvironmentConnection}, 502, nil
	}
	o := parseObject(b)
	if (len(o) == 0 && strings.TrimSpace(string(b)) != "{}") || o.isFalse("success") {
		return EnvironmentView{Message: msgEnvironmentFailed}, 502, nil
	}
	env := o.obj("oxidns_ha_environment")
	proof := EnvironmentProof{Kind: env.text("kind"), Ready: env.isTrue("ready"), Network: env.text("network"), HostID: env.text("host_id"), HostIDSource: env.text("host_id_source"), HostBootID: env.text("host_boot_id"), Interface: limited(env.text("interface"), 64), Blockers: []string{}}
	var missing []string
	if proof.Kind == "native" && json.Unmarshal(env["missing_tools"], &missing) == nil && missing != nil {
		proof.MissingTools = &missing
	}
	if proof.Kind != "native" && proof.Kind != "container" {
		proof.Kind = "unknown"
	}
	if proof.Network != "host" && proof.Network != "macvlan" {
		proof.Network = ""
	}
	if !validIdentity(proof.HostID) {
		proof.HostID = ""
	}
	if !validIdentity(proof.HostBootID) {
		proof.HostBootID = ""
	}
	if _, ok := env["host_id_source"]; !ok {
		proof.HostIDSource = "file"
	}
	switch proof.HostIDSource {
	case "file", "docker-volume", "installation":
	default:
		proof.HostIDSource = "unknown"
	}
	for i, r := range env.list("blockers") {
		if i == 20 {
			break
		}
		var v string
		if json.Unmarshal(r, &v) == nil {
			proof.Blockers = append(proof.Blockers, limited(s.safe(ctx, v), 500))
		}
	}
	return EnvironmentView{Success: true, AgentID: id, ServiceType: "oxidns", Protocols: cleanProtocols(o, "oxidns_ha_protocols"), ContainerProtocols: cleanProtocols(o, "oxidns_ha_container_protocols"), HandoffProtocols: cleanProtocols(o, "oxidns_ha_handoff_protocols"), Environment: &proof}, 200, nil
}
