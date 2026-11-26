package routersplit

import (
	"encoding/json"
	"errors"
	"testing"

	"configflow/internal/model"
	"configflow/internal/testjson"
)

func TestN89TopologyChecksOnlyConsumedIdentity(t *testing.T) {
	for _, tc := range []struct{ name, profile, want string }{
		{"marker-ignores-unrelated-link", `{"mihomo":{"custom_config":"tun: {enable: true, auto-route: true, auto-redirect: true, dns-hijack: [any:53]}","router_split":{"version":1,"takeover":"full_tun","origin":"setup","route_rule_set_ids":[]}},"routers":{"items":[{"id":"unrelated","proxy_gateway_agent_id":5}]}}`, `{"mode":"full_tun","source":"marker","mihomo_agent_ids":["agent-a"],"router_ids":[],"setup_session_id":null,"issues":[]}`},
		{"legacy-ignores-unrelated-link", `{"mihomo":{"linux_gateway":{"enabled":true}},"routers":{"items":[{"id":"r1","proxy_gateway_agent_id":"agent-a"},{"id":"unrelated","proxy_gateway_agent_id":5}]}}`, `{"mode":"legacy","source":"topology","mihomo_agent_ids":["agent-a"],"router_ids":["r1"],"setup_session_id":null,"issues":[]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var p model.Profile
			if err := json.Unmarshal([]byte(tc.profile), &p); err != nil {
				t.Fatal(err)
			}
			c := &Context{MihomoAgentIDs: []string{"agent-a"}}
			state, err := ProfileState(&p, c)
			if err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			if !testjson.Equal(json.RawMessage(tc.want), json.RawMessage(data)) {
				t.Fatalf("whole state differs: %s", testjson.Diff(json.RawMessage(tc.want), json.RawMessage(data)))
			}
			if tc.name == "legacy-ignores-unrelated-link" {
				ids, err := LegacyAgentIDs(&p, c)
				if err != nil || len(ids) != 1 || ids[0] != "agent-a" {
					t.Fatal("healthy binding was blocked", ids, err)
				}
			}
		})
	}
}

func TestN89OnlyLatestSessionIDIsConsumed(t *testing.T) {
	var rows []Session
	if err := json.Unmarshal([]byte(`[{"id":3,"goal":"router","abandoned":true},{"id":"latest","goal":"router","completed":true},{"id":4,"goal":"gateway"}]`), &rows); err != nil {
		t.Fatal(err)
	}
	c, err := SessionContext(rows)
	if err != nil || c.SessionID == nil || *c.SessionID != "latest" || !c.SessionCompleted || !c.HasGatewaySession {
		t.Fatalf("unconsumed session identity blocked context: %#v %v", c, err)
	}
	if err = json.Unmarshal([]byte(`[{"id":3,"goal":"router"}]`), &rows); err != nil {
		t.Fatal(err)
	}
	if _, err = SessionContext(rows); err == nil {
		t.Fatal("numeric latest identity became a usable session")
	}
	var split *RouterSplitError
	if !errors.As(err, &split) || split.Status != 409 || split.Code != CodeInvalid {
		t.Fatalf("identity failure escaped domain HTTP contract: %v", err)
	}
}
