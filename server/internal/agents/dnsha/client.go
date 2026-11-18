package dnsha

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"configflow/internal/agents"
)

type object map[string]json.RawMessage

func parseObject(b []byte) object {
	var o object
	if json.Unmarshal(b, &o) != nil || o == nil {
		return object{}
	}
	return o
}
func (o object) text(k string) string  { var s string; _ = json.Unmarshal(o[k], &s); return s }
func (o object) isTrue(k string) bool  { return string(o[k]) == "true" }
func (o object) isFalse(k string) bool { return string(o[k]) == "false" }
func (o object) obj(k string) object   { return parseObject(o[k]) }
func (o object) list(k string) []json.RawMessage {
	var v []json.RawMessage
	_ = json.Unmarshal(o[k], &v)
	return v
}
func (o object) strings(k string) []string {
	out := []string{}
	for _, b := range o.list(k) {
		var s string
		if json.Unmarshal(b, &s) == nil {
			out = append(out, s)
		}
	}
	return out
}
func (o object) truthy(k string) bool {
	b := strings.TrimSpace(string(o[k]))
	return b != "" && b != "null" && b != "false" && b != "0" && b != "0.0" && b != `""` && b != "[]" && b != "{}"
}
func runtimeOf(o object) *Runtime {
	b := o["runtime"]
	if len(parseObject(b)) == 0 {
		return nil
	}
	r := parseObject(b)
	return &Runtime{VIPPresent: r.isTrue("vip_present"), Running: r.isTrue("keepalived_running"), Healthy: r.isTrue("healthy"), VIPHealthy: r.isTrue("vip_healthy"), raw: b}
}
func hasProtocol(o object, key string) bool {
	for _, v := range o.list(key) {
		if string(v) == "true" {
			return true
		}
		var n float64
		if json.Unmarshal(v, &n) == nil && n == Protocol {
			return true
		}
	}
	return false
}
func (s *Service) call(ctx context.Context, a *agent, method, path string, body []byte, seconds float64) (object, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	h := http.Header{}
	if body != nil {
		h.Set("Content-Type", "application/json")
	}
	r, e := s.m.Call(ctx, a.target.Record, method, "/api/oxidns-ha"+path, h, body, agents.Pair(5, seconds))
	if e != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if agents.IsTransportError(e) {
			return nil, refuse(fmt.Sprintf(msgUnreachable, name(a)))
		}
		return nil, e
	}
	var message string
	if r.StatusCode == 404 {
		message = fmt.Sprintf(msgUpgrade, name(a))
	} else {
		o := parseObject(r.Body)
		if r.StatusCode == 200 && !o.isFalse("success") {
			return o, nil
		}
		message = o.text("error")
		if message == "" {
			message = o.text("message")
		}
		if message == "" {
			message = fmt.Sprintf(msgHTTP, r.StatusCode)
		}
	}
	return nil, refuse(fmt.Sprintf(msgDeviceError, name(a), s.safe(ctx, message)))
}
func (s *Service) runtime(ctx context.Context, a *agent) (*Runtime, error) {
	o, e := s.call(ctx, a, "GET", "", nil, 15)
	if e != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrStatusUnavailable
	}
	r := runtimeOf(o)
	if r == nil || o.truthy("error") {
		return nil, ErrStatusUnavailable
	}
	return r, nil
}
func (s *Service) handoffRequest(ctx context.Context, a *agent, method, id string) (object, error) {
	var b []byte
	if method != "GET" {
		b = rawObject(struct {
			ID string `json:"operation_id"`
		}{id})
	}
	r, e := s.call(ctx, a, method, "/handoff", b, 15)
	if e != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, &HandoffPending{msgHandoffUnknown, true}
	}
	if r.truthy("error") || r.isFalse("success") {
		return nil, &HandoffPending{msgHandoffInvalid, true}
	}
	return r, nil
}
func (s *Service) requireProtocols(ctx context.Context, members []*agent) error {
	caps := make([]object, 0, len(members))
	for _, a := range members {
		b, e := s.capabilities(ctx, a)
		if e != nil {
			if agents.IsTransportError(e) {
				return refuse(fmt.Sprintf(msgUnreachable, name(a)))
			}
			return e
		}
		o := parseObject(b)
		if len(o) == 0 && string(b) != "{}" {
			return refuse(fmt.Sprintf(msgCapability, name(a)))
		}
		if !hasProtocol(o, "oxidns_ha_protocols") {
			return refuse(fmt.Sprintf(msgProtocol, name(a)))
		}
		caps = append(caps, o)
	}
	return s.validateEnvironments(ctx, members, caps)
}
func validIdentity(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}
func (s *Service) validateEnvironments(ctx context.Context, members []*agent, caps []object) error {
	if len(members) != 2 || len(caps) != 2 {
		return refuse(msgHostID)
	}
	envs := []object{caps[0].obj("oxidns_ha_environment"), caps[1].obj("oxidns_ha_environment")}
	containers := false
	needBoot := false
	for i, a := range members {
		env := envs[i]
		container := a.DeploymentMethod == "docker" || env.text("kind") == "container"
		containers = containers || container
		if container && (!hasProtocol(caps[i], "oxidns_ha_container_protocols") || env.text("kind") != "container" || !env.isTrue("ready") || (env.text("network") != "host" && env.text("network") != "macvlan")) {
			blockers := []string{}
			for _, b := range env.strings("blockers") {
				if b != "" {
					blockers = append(blockers, s.safe(ctx, b))
				}
			}
			detail := strings.Join(blockers, "；")
			if detail == "" {
				detail = msgContainerGuide
			}
			return &Error{Message: fmt.Sprintf(msgContainer, name(a), detail), Blockers: blockers}
		}
		source := env.text("host_id_source")
		if _, ok := env["host_id_source"]; !ok {
			source = "file"
		}
		switch source {
		case "file":
		case "docker-volume", "installation":
			needBoot = true
		default:
			return refuse(msgIdentitySource)
		}
	}
	b0, b1 := envs[0].text("host_boot_id"), envs[1].text("host_boot_id")
	if needBoot && (!validIdentity(b0) || !validIdentity(b1)) {
		return refuse(msgBootID)
	}
	if validIdentity(b0) && b0 == b1 {
		return refuse(msgSameMachine)
	}
	h0, h1 := envs[0].text("host_id"), envs[1].text("host_id")
	if containers && (!validIdentity(h0) || !validIdentity(h1)) {
		return refuse(msgHostID)
	}
	if validIdentity(h0) && h0 == h1 {
		return refuse(msgSameMachine)
	}
	return nil
}
