package agents

import (
	"context"
	"encoding/json"
	"testing"

	"configflow/internal/model"
)

// Heartbeat members are matched by their exact JSON name, like Python's
// heartbeat.get("version"). encoding/json struct decoding folds case, so a
// differently cased member must neither be reported nor split into components.
func TestReviewB10HeartbeatMembersMatchExactNames(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    map[string]string // member -> expected overlaid JSON ("" = absent)
	}{
		{
			name:    "folded version",
			payload: `{"Version":"9.9.9","CONFIG_VERSION":"9"}`,
			want:    map[string]string{"version": `"1.0.0"`},
		},
		{
			name:    "exact wins over folded duplicate",
			payload: `{"version":"2.0.0","VERSION":"9.9.9"}`,
			want:    map[string]string{"version": `"2.0.0"`},
		},
		{
			name:    "folded metric component",
			payload: `{"system_metrics":{"CPU":{"usage_percent":1},"memory":false,"Disk":null}}`,
			want: map[string]string{
				"system_metrics": `{"CPU":{"usage_percent":1},"memory":false,"Disk":null}`,
				"cpu":            "", "memory": `false`, "disk": "",
			},
		},
		{
			name:    "exact component wins over folded duplicate",
			payload: `{"system_metrics":{"cpu":{"usage_percent":2},"Cpu":{"usage_percent":9}}}`,
			want:    map[string]string{"cpu": `{"usage_percent":2}`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, id := setupRuntime(t)
			var agent model.Agent
			if err := json.Unmarshal([]byte(tt.payload), &agent); err != nil {
				t.Fatal(err)
			}
			if !must[bool](t)(e.m.ObserveHeartbeat(e.ctx, id, &agent)) {
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

// The public entry point preserves exact JSON bytes through model.Agent +
// MemberJSON for every scalar shape an Agent may send, including escapes and
// number spellings.
func TestReviewB10PublicHeartbeatBytesMatchModelPath(t *testing.T) {
	values := []string{
		`"1.0.0"`, `"a<>&\"\\\u2028中😀"`, `""`, `null`, `true`, `false`,
		`7`, `1e0`, `-0.50`, `1.5`,
		`{"cpu":{"usage_percent":1.0},"x":"<"}`,
		`["a",2,null]`,
	}
	for _, value := range values {
		for _, key := range []string{"version", "config_version", "service_status", "system_metrics"} {
			var agent model.Agent
			raw := `{"` + key + `":` + value + `,"token":"example-value"}`
			if err := json.Unmarshal([]byte(raw), &agent); err != nil {
				t.Fatal(err)
			}
			got, err := reportedHeartbeat(&agent)
			if err != nil {
				t.Fatal(err)
			}
			member, present, err := agent.MemberJSON(key)
			if err != nil || !present {
				t.Fatalf("%s=%s: member err=%v present=%v", key, value, err, present)
			}
			var field json.RawMessage
			switch key {
			case "version":
				field = got.Version
			case "config_version":
				field = got.ConfigVersion
			case "service_status":
				field = got.ServiceStatus
			case "system_metrics":
				field = got.SystemMetrics
			}
			if string(field) != string(member) {
				t.Fatalf("%s=%s: got %s want %s", key, value, field, member)
			}
		}
	}
}

func TestAHeartbeatRacingADeleteLeavesNothingBehind(t *testing.T) {
	e, id := setupRuntime(t)
	testRecordHeartbeat = func(ctx context.Context, m *Manager, source heartbeatSource, payload heartbeatPayload, mark int64) (bool, error) {
		if !must[bool](t)(m.DeleteAgent(e.ctx, id)) {
			t.Fatal("delete")
		}
		return m.runtime.recordFields(ctx, source, payload, mark)
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
	testRecordHeartbeat = func(ctx context.Context, m *Manager, source heartbeatSource, payload heartbeatPayload, mark int64) (bool, error) {
		ok, err := m.runtime.recordFields(ctx, source, payload, mark)
		if err != nil || !ok || !must[bool](t)(m.DeleteAgent(e.ctx, id)) {
			t.Fatal("setup", ok, err)
		}
		return true, nil
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
