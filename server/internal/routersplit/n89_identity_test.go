package routersplit

import (
	"encoding/json"
	"errors"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/model"
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
			got, err := jsonx.Parse(data)
			if err != nil {
				t.Fatal(err)
			}
			want, err := jsonx.Parse([]byte(tc.want))
			if err != nil || !jsonx.Equal(got, want) {
				t.Fatalf("whole state differs: %s %v", data, err)
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

func TestN89InstallationProofRequiresStoredText(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want bool
	}{{`0`, false}, {`1`, false}, {`true`, false}, {`[]`, false}, {`{}`, false}, {`""`, false}, {`"0"`, true}, {`"synthetic-proof"`, true}} {
		t.Run(tc.raw, func(t *testing.T) {
			agent, err := jsonx.Parse([]byte(`{"gateway_installation_id":` + tc.raw + `}`))
			if err != nil {
				t.Fatal(err)
			}
			got, err := GatewayWasUsed(jsonx.NewObject(), agent, nil)
			if err != nil || got != tc.want {
				t.Fatalf("proof %s: %v %v", tc.raw, got, err)
			}
		})
	}
}
