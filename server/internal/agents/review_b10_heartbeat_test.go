package agents

import (
	"context"
	"encoding/json"
	"testing"

	"configflow/internal/jsonx"
)

func updateHeartbeatLegacy(m *Manager, ctx context.Context, agentID string, heartbeat *jsonx.Object) (bool, error) {
	payload, err := heartbeatFromObject(heartbeat)
	if err != nil {
		return false, err
	}
	return m.observeHeartbeat(ctx, agentID, payload, nil)
}

func recordHeartbeatLegacy(s *RuntimeState, agent, payload *jsonx.Object, mark int64) bool {
	record, err := agentModel(agent)
	if err != nil {
		return false
	}
	fields, err := agentModel(payload)
	if err != nil {
		return false
	}
	ok, err := s.recordModel(context.Background(), record, fields, mark)
	return err == nil && ok
}

// Heartbeat members are matched by their exact JSON name, like Python's
// heartbeat.get("version"). encoding/json struct decoding folds case, so a
// differently cased member must neither be reported nor split into components.
func TestReviewB10HeartbeatMembersMatchExactNames(t *testing.T) {
	tests := []struct {
		name    string
		payload *jsonx.Object
		want    map[string]string // member -> expected overlaid JSON ("" = absent)
	}{
		{
			name:    "folded version",
			payload: jsonx.Obj("Version", "9.9.9", "CONFIG_VERSION", "9"),
			want:    map[string]string{"version": `"1.0.0"`},
		},
		{
			name:    "exact wins over folded duplicate",
			payload: jsonx.Obj("version", "2.0.0", "VERSION", "9.9.9"),
			want:    map[string]string{"version": `"2.0.0"`},
		},
		{
			name: "folded metric component",
			payload: jsonx.Obj("system_metrics", jsonx.Obj(
				"CPU", jsonx.Obj("usage_percent", 1), "memory", false, "Disk", nil)),
			want: map[string]string{
				"system_metrics": `{"CPU":{"usage_percent":1},"memory":false,"Disk":null}`,
				"cpu":            "", "memory": `false`, "disk": "",
			},
		},
		{
			name: "exact component wins over folded duplicate",
			payload: jsonx.Obj("system_metrics", jsonx.Obj(
				"cpu", jsonx.Obj("usage_percent", 2), "Cpu", jsonx.Obj("usage_percent", 9))),
			want: map[string]string{"cpu": `{"usage_percent":2}`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, id := setupRuntime(t)
			if !must[bool](t)(updateHeartbeatLegacy(e.m, e.ctx, id, tt.payload)) {
				t.Fatal("heartbeat not recorded")
			}
			current, err := e.m.ReadAgent(e.ctx, id, false)
			if err != nil {
				t.Fatal(err)
			}
			for member, want := range tt.want {
				raw, present, err := current.MemberJSON(member)
				if err != nil {
					t.Fatal(err)
				}
				if want == "" {
					if present {
						t.Fatalf("%s=%s; want absent", member, raw)
					}
					continue
				}
				if !present || string(raw) != want {
					t.Fatalf("%s=%s present=%v; want %s", member, raw, present, want)
				}
			}
		})
	}
}

// The public entry point reports the same bytes the pre-batch-10 path did
// (marshal the object, decode an Agent, read MemberJSON), for every scalar
// shape an Agent may send, including escapes and number spellings.
func TestReviewB10PublicHeartbeatBytesMatchModelPath(t *testing.T) {
	values := []any{
		"1.0.0", "a<>&\"\\ 中\U0001F600", "", nil, true, false,
		json.Number("7"), json.Number("1e0"), json.Number("-0.50"), 7, 1.5,
		jsonx.Obj("cpu", jsonx.Obj("usage_percent", json.Number("1.0")), "x", "<"),
		[]any{"a", json.Number("2"), nil},
	}
	for _, value := range values {
		for _, key := range []string{"version", "config_version", "service_status", "system_metrics"} {
			object := jsonx.Obj(key, value, "token", "example-value")
			got, err := heartbeatFromObject(object)
			if err != nil {
				t.Fatal(err)
			}
			old, err := agentModel(object)
			if err != nil {
				t.Fatal(err)
			}
			want, err := reportedHeartbeat(old)
			if err != nil {
				t.Fatal(err)
			}
			if string(got.Version) != string(want.Version) || string(got.ConfigVersion) != string(want.ConfigVersion) ||
				string(got.ServiceStatus) != string(want.ServiceStatus) || string(got.SystemMetrics) != string(want.SystemMetrics) {
				t.Fatalf("%s=%#v: got %+v want %+v", key, value, got, want)
			}
		}
	}
}

// Legacy testRecordHeartbeat hook tests (W9: hook signature in manager.go uses *jsonx.Object).

func TestAHeartbeatRacingADeleteLeavesNothingBehind(t *testing.T) {
	e, id := setupRuntime(t)
	testRecordHeartbeat = func(m *Manager, agent, payload *jsonx.Object, mark int64) bool {
		if !must[bool](t)(m.DeleteAgent(e.ctx, id)) {
			t.Fatal("delete")
		}
		return recordHeartbeatLegacy(m.runtime, agent, payload, mark)
	}
	t.Cleanup(func() { testRecordHeartbeat = nil })
	revision := e.revision()
	if e.beat(id, map[string]any{"version": "2.0.0", "system_metrics": testMetricsMap()}) {
		t.Fatal("accepted")
	}
	if runtimeEntriesCount(e.m.Runtime()) != 0 || e.revision() != revision+1 {
		t.Fatal("left something")
	}
}

func TestADeleteAfterTheHeartbeatIsRecordedSkipsTheVersionWrite(t *testing.T) {
	e, id := setupRuntime(t)
	testRecordHeartbeat = func(m *Manager, agent, payload *jsonx.Object, mark int64) bool {
		if !recordHeartbeatLegacy(m.runtime, agent, payload, mark) || !must[bool](t)(m.DeleteAgent(e.ctx, id)) {
			t.Fatal("setup")
		}
		return true
	}
	t.Cleanup(func() { testRecordHeartbeat = nil })
	revision := e.revision()
	if e.beat(id, map[string]any{"version": "2.0.0", "config_version": "cfg-2"}) {
		t.Fatal("accepted")
	}
	if runtimeEntriesCount(e.m.Runtime()) != 0 || e.revision() != revision+1 || len(e.system().Agents) != 0 {
		t.Fatal("wrote")
	}
}
