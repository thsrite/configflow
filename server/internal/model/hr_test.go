package model

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

// replaceStoredMember uses encoding/json offsets to target only the root member:
// nested records frequently use the same name and value as their parent.
func replaceStoredMember(t *testing.T, source []byte, name string, next []byte) []byte {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(source))
	if _, err := dec.Token(); err != nil {
		t.Fatal("object decode failed")
	}
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			t.Fatal("member decode failed")
		}
		var value json.RawMessage
		if dec.Decode(&value) != nil {
			t.Fatal("member value decode failed")
		}
		if key == name {
			end := int(dec.InputOffset())
			result := append(bytes.Clone(source[:end-len(value)]), next...)
			return append(result, source[end:]...)
		}
	}
	t.Fatal("missing synthetic member")
	return nil
}

func TestAgentJournalPreservation(t *testing.T) {
	source := []byte(` { "id":"agent-example", "enabled":false, "config_revision":null, "applied_content":null, "deployments":{"task-example":{"deployment_id":"task-example","status":"preparing","submitted":true,"config_revision":null,"future_proof":{"b":1.0,"a":null}}}, "latest_deployment":{"deployment_id":"task-example","status":"checking"}, "dns_follow":{"after_deployment_id":null}, "rule_sync":{"settings":{"library":false,"rules":true},"state":{"status":"running","native_task":"native-example","retry_at":1.25}}, "oxidns_ha_publication":{"id":"ha-example","active_agent_id":null,"enable":null,"handoff_required":false,"children":{"agent-example":"task-example"}}, "future":{"secret":"synthetic-token"} } `)
	var agent Agent
	if agent.UnmarshalJSON(source) != nil {
		t.Fatal("journal decode failed")
	}
	if agent.DNSFollow == nil || agent.DNSFollow.AfterDeploymentID != nil || len(valueIssues(reflect.ValueOf(&agent), "")) != 0 {
		t.Fatal("nullable follow baseline lost")
	}
	if agent.Enabled == nil || *agent.Enabled || agent.ConfigRevision != nil || agent.AppliedContent != nil {
		t.Fatal("optional values lost")
	}
	if agent.OxiDNSHAPublication.HandoffRequired == nil || *agent.OxiDNSHAPublication.HandoffRequired {
		t.Fatal("explicit recovery decision lost")
	}
	if agent.RuleSync.State.NativeTask == nil || *agent.RuleSync.State.NativeTask != "native-example" {
		t.Fatal("native task ID lost")
	}
	// Loaded null followed by nil is deliberately unchanged, not a clear command.
	agent.ConfigRevision = nil
	out, err := Marshal(agent)
	if err != nil || !bytes.Equal(out, source) {
		t.Fatal("null or journal changed on read")
	}
	task, _ := agent.Deployments.Get("task-example")
	task.Status = "unknown"
	agent.Deployments.Set("task-example", task)
	changed, err := Marshal(agent)
	if err != nil {
		t.Fatal("task edit failed")
	}
	_, before := fuzzMembers(t, source)
	_, after := fuzzMembers(t, changed)
	for key, value := range before {
		if key != "deployments" && !bytes.Equal(value, after[key]) {
			t.Fatal("task edit changed unrelated state")
		}
	}
	_, tasks := fuzzMembers(t, before["deployments"])
	_, changedTasks := fuzzMembers(t, after["deployments"])
	_, originalTask := fuzzMembers(t, tasks["task-example"])
	_, editedTask := fuzzMembers(t, changedTasks["task-example"])
	for key, value := range originalTask {
		if key != "status" && !bytes.Equal(value, editedTask[key]) {
			t.Fatal("task edit changed receipt or identity")
		}
	}
}

func TestAgentJournalIssuesHideIdentifiers(t *testing.T) {
	var agent Agent
	if agent.UnmarshalJSON([]byte(`{"deployments":{"private-task":{"submitted":"synthetic-secret","config_revision":true}},"oxidns_ha_publication":{"identities":{"private-agent":{"port":false}}}}`)) != nil {
		t.Fatal("decode failed")
	}
	issues := valueIssues(reflect.ValueOf(&agent), "agents[0]")
	for _, want := range []FieldIssue{{"agents[0].deployments[0].submitted", IssueType}, {"agents[0].deployments[0].config_revision", IssueType}, {"agents[0].oxidns_ha_publication.identities[0].port", IssueType}} {
		if !slices.Contains(issues, want) {
			t.Fatalf("missing schema issue: %v", issues)
		}
	}
}

func TestHRPythonWriters(t *testing.T) {
	if os.Getenv("CONFIGFLOW_PY") == "" {
		t.Skip("set CONFIGFLOW_PY for Python HR writers")
	}
	path := filepath.Join(t.TempDir(), "synthetic.json")
	cmd := exec.Command(os.Getenv("CONFIGFLOW_PY"), "testdata/hr_interchange.py", path)
	cmd.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1")
	if cmd.Run() != nil {
		t.Fatal("Python synthetic HR writers failed (output suppressed)")
	}
	data, err := os.ReadFile(path)
	var rows []interchangeRow
	if err != nil || json.Unmarshal(data, &rows) != nil {
		t.Fatal("synthetic rows read failed")
	}
	var agent Agent
	if agent.UnmarshalJSON(rows[0].Value) != nil || agent.OxiDNSHAState == nil || len(agent.OxiDNSHAState.Members) != 2 {
		t.Fatal("Python leftover member IDs lost")
	}
	counts := map[string]int{}
	for _, row := range rows {
		var target Agent
		if target.UnmarshalJSON(row.Value) != nil {
			t.Fatal("Python Agent decode failed")
		}
		assertStoredTree(t, row.Value, reflect.ValueOf(&target), counts)
	}
	runTypesInterchange(t, rows, false)
}
