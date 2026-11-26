package agentfake

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
)

// Segment models a two-device L2 VIP election; it never configures a real interface.
// All state transitions and HTTP requests are serialized for race-safe assertions.
type Segment struct {
	mu      sync.Mutex
	devices map[string]*haDevice
	holder  string
	calls   []HACall
}
type haDevice struct {
	fake                *Agent
	token               string
	managed             bool
	spec                json.RawMessage
	priority            int
	healthy, vipHealthy bool
	handoff             string
	archives            map[string][]byte
	latest              string
}

// HACall is an observed raw request before the simulated device handles it.
type HACall struct {
	AgentID, Method, Path string
	Header                http.Header
	Body                  []byte
}

// NewSegment creates an isolated VIP segment with an initial elected member.
func NewSegment(holder string) *Segment {
	return &Segment{devices: map[string]*haDevice{}, holder: holder}
}

// Handler attaches an Agent's ordinary deployment receiver and HA protocol endpoints.
func (s *Segment) Handler(id, token string, a *Agent) http.Handler {
	s.mu.Lock()
	s.devices[id] = &haDevice{fake: a, token: token, healthy: true, vipHealthy: true, priority: 100, archives: map[string][]byte{}}
	if id == s.holder {
		s.devices[id].priority = 150
	}
	s.mu.Unlock()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, e := io.ReadAll(r.Body)
		if e != nil {
			http.Error(w, "read failed", 400)
			return
		}
		r.Body = io.NopCloser(strings.NewReader(string(body)))
		s.mu.Lock()
		s.calls = append(s.calls, HACall{id, r.Method, r.URL.RequestURI(), r.Header.Clone(), append([]byte(nil), body...)})
		d := s.devices[id]
		if r.Header.Get("Authorization") != "Bearer "+d.token {
			s.mu.Unlock()
			http.Error(w, "unauthorized", 401)
			return
		}
		if r.URL.Path == "/api/rule-sync/snapshot" {
			journal := d.fake.Journal()
			state := journal[d.latest]
			archive := append([]byte(nil), d.archives[d.latest]...)
			s.mu.Unlock()
			if state.Status != "succeeded" || len(archive) == 0 {
				http.Error(w, "no active snapshot", 409)
				return
			}
			w.Header().Set("Content-Type", "application/gzip")
			_, _ = w.Write(archive)
			return
		}
		if r.Method == "POST" && r.URL.Path == "/api/deployments" {
			d.latest = r.Header.Get("X-Deployment-ID")
			d.archives[d.latest] = append([]byte(nil), body...)
		}
		if strings.HasPrefix(r.URL.Path, "/api/oxidns-ha") {
			defer s.mu.Unlock()
			switch r.URL.Path {
			case "/api/oxidns-ha/preview":
				var spec map[string]json.RawMessage
				if json.Unmarshal(body, &spec) != nil || len(spec) != 6 {
					respond(w, 400, map[string]any{"error": "invalid spec"})
					return
				}
				respond(w, 200, map[string]any{"ok": true, "blockers": []string{}, "cidr": "192.0.2.0/24", "interface": "eth0"})
			case "/api/oxidns-ha":
				switch r.Method {
				case "PUT":
					var spec struct {
						Priority int `json:"priority"`
					}
					if json.Unmarshal(body, &spec) != nil {
						respond(w, 400, map[string]any{"error": "invalid spec"})
						return
					}
					d.spec = append([]byte(nil), body...)
					d.priority = spec.Priority
					d.managed = true
					s.elect()
				case "DELETE":
					d.managed = false
					d.spec = nil
					d.handoff = ""
					s.elect()
				}
				respond(w, 200, s.view(id))
			case "/api/oxidns-ha/handoff":
				if r.Method != "GET" {
					var req struct {
						ID string `json:"operation_id"`
					}
					if json.Unmarshal(body, &req) != nil || req.ID == "" {
						respond(w, 400, map[string]any{"error": "operation required"})
						return
					}
					if d.handoff != "" && d.handoff != req.ID {
						respond(w, 409, map[string]any{"error": "different operation"})
						return
					}
					if r.Method == "POST" {
						d.handoff = req.ID
					} else if r.Method == "DELETE" {
						d.handoff = ""
					}
					s.elect()
				}
				respond(w, 200, s.view(id))
			default:
				http.NotFound(w, r)
			}
			return
		}
		s.mu.Unlock()
		a.ServeHTTP(w, r)
	})
}
func (s *Segment) elect() {
	best := ""
	priority := -1
	for id, d := range s.devices {
		if !d.managed || !d.healthy {
			continue
		}
		p := d.priority
		if d.handoff != "" {
			p = 0
		}
		if p > priority || p == priority && id < best {
			best = id
			priority = p
		}
	}
	s.holder = best
}
func (s *Segment) view(id string) map[string]any {
	d := s.devices[id]
	var managed any
	if d.managed {
		managed = map[string]any{"active": true, "pending": false, "spec": d.spec}
	}
	var handoff any
	if d.handoff != "" {
		handoff = map[string]any{"operation_id": d.handoff, "active": true}
	}
	return map[string]any{"protocol": 1, "managed": managed, "runtime": map[string]any{"vip_present": s.holder == id, "keepalived_running": d.managed, "healthy": d.healthy, "vip_healthy": d.vipHealthy}, "handoff": handoff, "error": ""}
}

// Configure marks pre-existing devices managed for paired-publication scenarios.
func (s *Segment) Configure(ids ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range ids {
		if d := s.devices[id]; d != nil {
			d.managed = true
		}
	}
	s.elect()
}

// SetHolder models failover without changing the configured priority.
func (s *Segment) SetHolder(id string) { s.mu.Lock(); defer s.mu.Unlock(); s.holder = id }

// SetHealth controls local and VIP DNS probes independently.
func (s *Segment) SetHealth(id string, healthy, vipHealthy bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d := s.devices[id]; d != nil {
		d.healthy = healthy
		d.vipHealthy = vipHealthy
	}
}

// Calls returns an owned copy of raw HTTP observations.
func (s *Segment) Calls() []HACall {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, _ := json.Marshal(s.calls)
	var out []HACall
	_ = json.Unmarshal(b, &out)
	return out
}

// Holder returns the simulated current VIP owner.
func (s *Segment) Holder() string { s.mu.Lock(); defer s.mu.Unlock(); return s.holder }
