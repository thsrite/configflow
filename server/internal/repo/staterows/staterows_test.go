package staterows

import (
	"bytes"
	"reflect"
	"slices"
	"strings"
	"testing"

	"configflow/internal/model"
	"configflow/internal/store"
)

// configMembers are the Agent members that stay in the configuration rows.
// A new model.Agent member must be added here or to model.AgentStateMembers.
var configMembers = []string{
	"logging_config", "id", "name", "host", "port", "token", "service_type", "profile_id", "deployment_method",
	"enabled", "created_at", "updated_at", "gateway_installation_id", "docker_ha_installation_id",
	"domain_discovery_enabled", "traffic_history_enabled", "traffic_history_since", "rule_sync", "dns_follow",
	"oxidns_ha", "ipv6_verification_settings", "node_test_dns_endpoints",
}

func TestEveryAgentMemberIsClassified(t *testing.T) {
	kind := reflect.TypeFor[model.Agent]()
	for i := range kind.NumField() {
		name, _, _ := strings.Cut(kind.Field(i).Tag.Get("json"), ",")
		if name == "" || name == "-" {
			continue
		}
		state := slices.Contains(model.AgentStateMembers, name)
		config := slices.Contains(configMembers, name)
		if state == config {
			t.Errorf("member %q: state=%v config=%v; classify it exactly once", name, state, config)
		}
	}
	for _, path := range model.AgentStateMembers {
		if container, _, nested := strings.Cut(path, "."); nested && !slices.Contains(configMembers, container) {
			t.Errorf("nested state member %q: its container must stay configuration", path)
		}
	}
}

// decoded is the registry the model reads from an agents row.
func decoded(t testing.TB, row []byte) string {
	t.Helper()
	var system model.System
	if _, _, err := model.DecodeStoredMember(&system, "agents", row, "agents"); err != nil {
		return "error: " + err.Error()
	}
	data, err := model.Marshal(system.Agents)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func checkRoundTrip(t testing.TB, row []byte) ([]byte, []store.StateRow) {
	t.Helper()
	config, rows := Split(row)
	merged := Merge(config, rows)
	again, rowsAgain := Split(merged)
	if !bytes.Equal(again, config) || !reflect.DeepEqual(normal(rowsAgain), normal(rows)) {
		t.Fatalf("Split(Merge(x)) != x\nconfig %s\nagain  %s\nrows %v\nagain %v", config, again, rows, rowsAgain)
	}
	if got, want := decoded(t, merged), decoded(t, row); got != want {
		t.Fatalf("merged view differs\ngot  %s\nwant %s", got, want)
	}
	if len(rows) == 0 && !bytes.Equal(config, row) {
		t.Fatalf("nothing moved but the row changed: %s", config)
	}
	return config, rows
}

func normal(rows []store.StateRow) []store.StateRow {
	out := slices.Clone(rows)
	slices.SortFunc(out, func(a, b store.StateRow) int {
		return strings.Compare(a.Owner+"\x00"+a.Member, b.Owner+"\x00"+b.Member)
	})
	if out == nil {
		out = []store.StateRow{}
	}
	return out
}

func TestSplitMerge(t *testing.T) {
	tests := []struct {
		name       string
		row        string
		config     string
		stateCount int
	}{
		{"empty", `[]`, `[]`, 0},
		{"config only", `[{"id":"a","name":"A","host":"192.0.2.1"}]`, `[{"id":"a","name":"A","host":"192.0.2.1"}]`, 0},
		{
			"deployments and versions",
			`[{"id":"a","version":"1.2.3","deployments":{"d1":{"status":"succeeded","success":true}},"latest_deployment":{"deployment_id":"d1","status":"succeeded"},"name":"A","config_version":"abcd1234","pushed_at":"2026-01-01T00:00:00Z"}]`,
			`[{"id":"a","name":"A"}]`, 5,
		},
		{
			"rule sync state stays nested, settings stay",
			`[{"id":"a","rule_sync":{"settings":{"library":true},"state":{"status":"running"}}}]`,
			`[{"id":"a","rule_sync":{"settings":{"library":true}}}]`, 1,
		},
		{"rule sync with state only", `[{"id":"a","rule_sync":{"state":{"status":"idle"}}}]`, `[{"id":"a","rule_sync":{}}]`, 1},
		{
			"HA journal and follow state",
			`[{"id":"o","oxidns_ha":{"enabled":true,"peer_id":"p"},"oxidns_ha_state":{"status":"ready"},"oxidns_ha_publication":{"id":"j","status":"applying"},"dns_follow":{"source_agent_id":"m","upstream_index":0},"dns_follow_state":{"status":"waiting"},"dns_follow_job":{"binding_id":"b"}}]`,
			`[{"id":"o","oxidns_ha":{"enabled":true,"peer_id":"p"},"dns_follow":{"source_agent_id":"m","upstream_index":0}}]`, 4,
		},
		{"python legacy members", `[{"id":"a","status":"online","last_heartbeat":"x","cpu":{"core_count":2},"system_metrics":{}}]`, `[{"id":"a"}]`, 4},
		{"whitespace from python", `[ {"id": "a", "version": "1.0.0", "name": "A"} , {"id":"b"} ]`, `[{"id": "a","name": "A"},{"id":"b"}]`, 1},
		{"no id keeps everything", `[{"name":"x","deployments":{"d":{}}}]`, `[{"name":"x","deployments":{"d":{}}}]`, 0},
		{"empty id keeps everything", `[{"id":"","version":"1"}]`, `[{"id":"","version":"1"}]`, 0},
		{"id of another type keeps everything", `[{"id":true,"version":"1"}]`, `[{"id":true,"version":"1"}]`, 0},
		{"duplicate id: first owns state", `[{"id":"a","version":"1"},{"id":"a","version":"2"}]`, `[{"id":"a"},{"id":"a","version":"2"}]`, 1},
		{"non-object entries", `[1,"x",null,{"id":"a","version":"1"},[]]`, `[1,"x",null,{"id":"a"},[]]`, 1},
		{"not an array", `{"id":"a","version":"1"}`, `{"id":"a","version":"1"}`, 0},
		{"rule sync not an object", `[{"id":"a","rule_sync":"broken","version":"1"}]`, `[{"id":"a","rule_sync":"broken"}]`, 1},
		{"damaged HA journal (fails closed) moves byte for byte", `[{"id":"o","oxidns_ha_publication":"applying"}]`, `[{"id":"o"}]`, 1},
		{"damaged follow state", `[{"id":"o","dns_follow_state":[1]}]`, `[{"id":"o"}]`, 1},
		{"members of the wrong type move unchanged", `[{"id":"a","deployments":[],"ipv6_verifications":"x","latest_upgrade":7}]`, `[{"id":"a"}]`, 3},
		{"empty member name is no container (fuzz)", `[{"id":"0","version":"0","":{"version":1}}]`, `[{"id":"0","":{"version":1}}]`, 1},
				{"escaped key and nested strings", `[{"id":"a","name":"say \"version\": 1","version":"9"}]`, `[{"id":"a","name":"say \"version\": 1"}]`, 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config, rows := checkRoundTrip(t, []byte(test.row))
			if string(config) != test.config {
				t.Fatalf("config row %s, want %s", config, test.config)
			}
			if len(rows) != test.stateCount {
				t.Fatalf("%d state rows, want %d: %v", len(rows), test.stateCount, rows)
			}
		})
	}
}

func TestMergeEdgeCases(t *testing.T) {
	tests := []struct {
		name, row string
		rows      []store.StateRow
		want      string
	}{
		{"no rows: same bytes", `[ {"id":"a"} ]`, nil, `[ {"id":"a"} ]`},
		{"rows of an unknown Agent are not shown", `[{"id":"a"}]`, []store.StateRow{{Owner: "z", Member: "version", Value: []byte(`"1"`)}}, `[{"id":"a"}]`},
		{"state row wins over a hand-edited inline member", `[{"id":"a","version":"old"}]`, []store.StateRow{{Owner: "a", Member: "version", Value: []byte(`"new"`)}}, `[{"id":"a","version":"new"}]`},
		{"missing container is created", `[{"id":"a"}]`, []store.StateRow{{Owner: "a", Member: "rule_sync.state", Value: []byte(`{}`)}}, `[{"id":"a","rule_sync":{"state":{}}}]`},
		{"damaged container hides the nested row", `[{"id":"a","rule_sync":5}]`, []store.StateRow{{Owner: "a", Member: "rule_sync.state", Value: []byte(`{}`)}}, `[{"id":"a","rule_sync":5}]`},
		{"not an array", `null`, []store.StateRow{{Owner: "a", Member: "version", Value: []byte(`"1"`)}}, `null`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := string(Merge([]byte(test.row), test.rows)); got != test.want {
				t.Fatalf("got %s, want %s", got, test.want)
			}
		})
	}
}

func FuzzSplitMerge(f *testing.F) {
	for _, seed := range []string{
		`[]`, `[{"id":"a","version":"1","rule_sync":{"settings":{},"state":{"status":"x"}}}]`,
		`[{"id":"a","deployments":{"d":{"status":"ready"}}},{"id":"a","version":"2"},{"name":"n"}]`,
		`[ {"id" : "a" , "oxidns_ha_publication" : "applying" } ]`, `{"id":"a"}`, `[1,null,"x"]`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, row []byte) {
		if model.ValidJSON(row) != nil {
			return
		}
		checkRoundTrip(t, row)
	})
}
