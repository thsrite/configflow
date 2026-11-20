package agents

import (
	"encoding/json"
	"testing"

	"configflow/internal/model"
)

func TestBatch10RuntimeSnapshotOwnsRawMembers(t *testing.T) {
	e, id := setupRuntime(t)
	var payload model.Agent
	const sample = `{"cpu":{"usage_percent":1e0},"memory":false,"network":null,"future":2.00}`
	if err := json.Unmarshal([]byte(`{"version":"1.0.0","service_status":null,"system_metrics":`+sample+`,"token":"example-value"}`), &payload); err != nil {
		t.Fatal(err)
	}
	if ok, err := e.m.ObserveHeartbeat(e.ctx, id, &payload); err != nil || !ok {
		t.Fatal(ok, err)
	}
	// Editing the input and a diagnostic copy cannot mutate the live snapshot.
	if err := payload.SetJSON("system_metrics", []byte(`{"cpu":{"usage_percent":99}}`)); err != nil {
		t.Fatal(err)
	}
	copied, ok := runtimeEntryCopy(e.m.Runtime(), id)
	if !ok {
		t.Fatal("missing runtime entry")
	}
	copied.Fields[3] = json.RawMessage(`{"cpu":{"usage_percent":99}}`)
	copied.Fields[5] = json.RawMessage(`"changed"`)
	if ok, err := e.m.ObserveHeartbeat(e.ctx, id, nil); err != nil || !ok {
		t.Fatal(ok, err)
	}
	current, err := e.m.ReadAgent(e.ctx, id, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ name, want string }{
		{"version", `"1.0.0"`}, {"service_status", `null`}, {"system_metrics", sample},
		{"cpu", `{"usage_percent":1e0}`}, {"memory", `false`}, {"network", `null`},
	} {
		raw, present, err := current.MemberJSON(tt.name)
		if err != nil || !present || string(raw) != tt.want {
			t.Fatalf("%s=%s present=%v err=%v; want %s", tt.name, raw, present, err, tt.want)
		}
	}
	if current.Token == "example-value" {
		t.Fatal("credential overwritten by runtime snapshot")
	}
	// Null is a reported value, and does not erase the previous CPU component.
	nullPayload := &model.Agent{}
	if err := nullPayload.SetJSON("system_metrics", []byte(`null`)); err != nil {
		t.Fatal(err)
	}
	if ok, err := e.m.ObserveHeartbeat(e.ctx, id, nullPayload); err != nil || !ok {
		t.Fatal(ok, err)
	}
	current, err = e.m.ReadAgent(e.ctx, id, false)
	if err != nil {
		t.Fatal(err)
	}
	raw, present, err := current.MemberJSON("system_metrics")
	if err != nil || !present || string(raw) != "null" {
		t.Fatalf("null sample lost: %s %v %v", raw, present, err)
	}
	raw, present, err = current.MemberJSON("cpu")
	if err != nil || !present || string(raw) != `{"usage_percent":1e0}` {
		t.Fatalf("partial sample erased CPU: %s %v %v", raw, present, err)
	}
}
