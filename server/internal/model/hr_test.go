package model

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
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
	// A stored null is a missing member (O18 T1b); unknown members are dropped.
	out, err := Marshal(agent)
	if err != nil || bytes.Contains(bytes.ReplaceAll(out, []byte(`"active_agent_id":null`), nil), []byte("null")) || bytes.Contains(out, []byte("future")) {
		t.Fatalf("null or unknown member written: %s", out)
	}
	task, _ := agent.Deployments.Get("task-example")
	task.Status = "unknown"
	agent.Deployments.Set("task-example", task)
	changed, err := Marshal(agent)
	if err != nil {
		t.Fatal("task edit failed")
	}
	_, before := fuzzMembers(t, out)
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
	issues, err := DecodeStored([]byte(`{"deployments":{"private-task":{"submitted":"synthetic-secret","config_revision":true}},"oxidns_ha_publication":{"identities":{"private-agent":{"port":false}}}}`), &agent, "agents[0]")
	if err != nil {
		t.Fatal("decode failed")
	}
	for _, want := range []FieldIssue{{"agents[0].deployments{*}.submitted", IssueType}, {"agents[0].deployments{*}.config_revision", IssueType}, {"agents[0].oxidns_ha_publication.identities{*}.port", IssueType}} {
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
		var target json.Unmarshaler
		switch row.Name {
		case "Agent":
			target = &Agent{}
		case "Router":
			target = &Router{}
		case "RouterRuleSyncReceipt":
			target = &RouterRuleSyncReceipt{}
		default:
			t.Fatal("unknown synthetic schema")
		}
		if target.UnmarshalJSON(row.Value) != nil {
			t.Fatal("Python HR decode failed")
		}
		assertStoredTree(t, row.Value, reflect.ValueOf(target), counts)
	}
	// The boundary starts from the Go-written form (as TestTypesPythonRoundTrip
	// does): plain records drop the nulls the Python writers leave and write
	// always-written members (dns_follow.upstream_index, O18 T1b), which the
	// exact Python audit would otherwise report.
	for i, row := range rows {
		value := reflect.New(map[string]reflect.Type{"Agent": reflect.TypeFor[Agent](), "Router": reflect.TypeFor[Router](), "RouterRuleSyncReceipt": reflect.TypeFor[RouterRuleSyncReceipt]()}[row.Name])
		if _, err := DecodeStored(row.Value, value.Interface(), ""); err != nil {
			t.Fatal("Python HR decode failed")
		}
		encoded, err := Marshal(value.Interface())
		if err != nil {
			t.Fatal("Go HR encode failed")
		}
		rows[i].Value = encoded
	}
	runTypesInterchange(t, rows, false)
}

// TestRouterOwnershipPreservation (O18 T1c): Router is a plain record. A
// manifest with an undeclared member is kept whole as stored (fail closed);
// explicit false switches stay explicit; a credential edit changes only that
// member of Go's encoding; built manifests invent no members; a loaded nil
// manifest is not written. (The stored source bytes are no longer kept.)
func TestRouterOwnershipPreservation(t *testing.T) {
	const manifest = `{"spec":{"naming_version":1,"policy_version":2},"resources":[{"path":"/ip/route","id":"*synthetic","config":{"future":{"ordered":[false,1.0,null]},"disabled":"false"},"future-proof":true}]}`
	source := []byte(` { "id":"router-example", "password":"synthetic-old", "verify_tls":false, "dns_guard":{"enabled":false,"failover":false}, "ros_policy_state":` + manifest + `, "ipv6_pd_guard_state":{"version":1,"spec":{"hold":3600},"resources":[{"role":"hook","path":"/ipv6/dhcp-client","id":"*client-example"}]}, "last_release":{"fingerprint":"opaque-stored","ok":false}, "future":null } `)
	var router Router
	issues, err := DecodeStored(source, &router, "")
	if err != nil {
		t.Fatal("router decode failed")
	}
	if router.VerifyTLS == nil || *router.VerifyTLS || router.DNSGuard.Failover == nil || *router.DNSGuard.Failover {
		t.Fatal("explicit false lost")
	}
	if !slices.Contains(issues, FieldIssue{"future", IssueUnknown}) || !slices.Contains(issues, FieldIssue{"ros_policy_state", IssueUnknown}) {
		t.Fatalf("issues = %v", issues)
	}
	encoded, err := Marshal(router)
	if err != nil {
		t.Fatal(err)
	}
	_, before := fuzzMembers(t, encoded)
	if string(before["ros_policy_state"]) != manifest {
		t.Fatalf("manifest with an undeclared member not kept: %s", before["ros_policy_state"])
	}
	if _, ok := before["future"]; ok {
		t.Fatal("undeclared router member written")
	}
	router.Password = "synthetic-new"
	edited, err := Marshal(router)
	if err != nil {
		t.Fatal("router edit failed")
	}
	_, after := fuzzMembers(t, edited)
	for key, value := range before {
		if key != "password" && !bytes.Equal(value, after[key]) {
			t.Fatal("credential edit changed ownership or release identity")
		}
	}
	var policy ROSPolicyState
	var pd PDGuardResource
	policy.Spec.Gateway = "192.0.2.1"
	pd.ID = "*synthetic"
	policyJSON, _ := Marshal(policy)
	pdJSON, _ := Marshal(pd)
	_, policyMembers := fuzzMembers(t, policyJSON)
	_, pdMembers := fuzzMembers(t, pdJSON)
	if _, ok := policyMembers["version"]; ok {
		t.Fatal("invented policy version")
	}
	if _, ok := pdMembers["config"]; ok {
		t.Fatal("invented PD proof config")
	}
	for _, fragment := range []string{`{}`, `{"ros_policy_state":null}`, `{"dns_guard_state":null}`} {
		var value Router
		if _, err := DecodeStored([]byte(fragment), &value, ""); err != nil {
			t.Fatal("null decode failed")
		}
		out, err := Marshal(value)
		if err != nil || bytes.Contains(out, []byte("_state")) {
			t.Fatalf("loaded nil became a manifest: %s", out)
		}
	}
}

// TestOwnershipFallbackIsDiagnosed: wrong-typed members inside a stored
// manifest read leniently and are reported; a numeric role uses the text codec.
func TestOwnershipFallbackIsDiagnosed(t *testing.T) {
	source := []byte(`{"ros_policy_state":{"spec":false,"resources":"synthetic-private"},"dns_guard_state":{"resources":[{"id":false,"config":{"future":[1,true]}}]},"ipv6_pd_guard_state":{"resources":[{"role":7}]}}`)
	var router Router
	issues, err := DecodeStored(source, &router, "routers.items[0]")
	if err != nil {
		t.Fatal("malformed proof decode failed")
	}
	if len(issues) != 3 {
		t.Fatalf("missing ownership schema issue: %v", issues)
	}
	for _, issue := range issues {
		if issue.Kind != IssueType || strings.Contains(issue.Path, "synthetic") {
			t.Fatalf("issue %v", issue)
		}
	}
	// The model is intentionally tolerant; empty fallback resources are not
	// verified absence. Device consumers must reject incomplete evidence.
	if router.IPv6PDGuardState.Resources[0].Role != "7" {
		t.Fatal("numeric role did not use the shared text codec")
	}
	if len(router.ROSPolicyState.Resources) != 0 {
		t.Fatal("invalid resources did not fall back")
	}
	t.Run("paths and stored proof", func(t *testing.T) {
		// Issues inside a manifest keep the manifest path (decodeManifest
		// decodes with a sink rooted at the manifest).
		for _, path := range []string{"ros_policy_state.spec", "ros_policy_state.resources", "dns_guard_state.resources[0].id"} {
			if !slices.Contains(issues, FieldIssue{"routers.items[0]." + path, IssueType}) {
				t.Fatalf("missing ownership schema issue: %v", issues)
			}
		}
	})
}
