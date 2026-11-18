package model

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// hrSchemaSurvey collects value-free schema facts for HR records: tolerated
// type issues, undeclared members and null stored in non-pointer members.
type hrSchemaSurvey struct {
	issues, unknown, nullValues, records map[string]int
}

var indexPattern = regexp.MustCompile(`\[\d+\]`)

func (s *hrSchemaSurvey) walk(value reflect.Value) {
	for value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface {
		if value.IsNil() {
			return
		}
		value = value.Elem()
	}
	switch value.Kind() {
	case reflect.Slice:
		if value.Type() == reflect.TypeFor[json.RawMessage]() {
			return
		}
		for i := 0; i < value.Len(); i++ {
			s.walk(value.Index(i))
		}
	case reflect.Struct:
		if value.CanInterface() {
			if walker, ok := value.Interface().(interface {
				walkValues(func(reflect.Value) error) error
			}); ok {
				_ = walker.walkValues(func(child reflect.Value) error { s.walk(child); return nil })
				return
			}
		}
		schema := schemaFor(value.Type())
		if schema.err != nil || schema.raw < 0 {
			return
		}
		name := value.Type().Name()
		s.records[name]++
		if schema.extra >= 0 {
			for _, key := range value.Field(schema.extra).Interface().(Fields).Keys() {
				s.unknown[name+"."+key]++
			}
		}
		raw := value.Field(schema.raw)
		members := raw.FieldByName("members")
		for i := 0; i < members.Len(); i++ {
			member := members.Index(i)
			if !member.FieldByName("known").Bool() {
				continue
			}
			text := bytes.TrimSpace(member.FieldByName("jsonMember").FieldByName("value").Bytes())
			field := schema.byName[member.FieldByName("jsonMember").FieldByName("name").String()]
			kind := value.Field(field.index).Kind()
			if bytes.Equal(text, []byte("null")) && kind != reflect.Pointer && kind != reflect.Slice &&
				value.Field(field.index).Type() != reflect.TypeFor[Dynamic]() {
				s.nullValues[name+"."+field.name]++
			}
		}
		for _, field := range schema.fields {
			s.walk(value.Field(field.index))
		}
	}
}

// TestHRSnapshotSchema checks the private snapshot against declared HR types:
// no tolerated type issue, no null in a non-pointer member and no undeclared
// member except retained derived/legacy ones. Only schema names are logged.
func TestHRSnapshotSchema(t *testing.T) {
	source := os.Getenv("CONFIGFLOW_SNAPSHOT")
	if source == "" || os.Getenv("CONFIGFLOW_PY") == "" {
		t.Skip("set CONFIGFLOW_SNAPSHOT and CONFIGFLOW_PY")
	}
	dir := t.TempDir()
	if os.Chmod(dir, 0700) != nil {
		t.Fatal("private directory permissions failed")
	}
	path := filepath.Join(dir, "rows.json")
	pythonTypes(t, "snapshot", filepath.Join(source, "configflow.sqlite3"), path)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("private rows read failed")
	}
	var rows []interchangeRow
	if json.Unmarshal(data, &rows) != nil {
		t.Fatal("private rows decode failed")
	}
	if os.Remove(path) != nil {
		t.Fatal("private rows cleanup failed")
	}
	survey := hrSchemaSurvey{issues: map[string]int{}, unknown: map[string]int{}, nullValues: map[string]int{}, records: map[string]int{}}
	for _, row := range rows {
		var holder json.Unmarshaler
		prefix := row.Name
		switch row.Name {
		case "agents":
			holder = &System{}
		case "routers":
			holder = &Profile{}
		case "AgentRuleSyncReceipt":
			holder = &AgentRuleSyncReceipt{}
		case "HAPayload":
			holder = &HAPayload{}
		case "RouterRuleSyncReceipt":
			holder = &RouterRuleSyncReceipt{}
		default:
			continue
		}
		value := row.Value
		if _, ok := holder.(*System); ok {
			value = []byte(`{"agents":` + string(row.Value) + `}`)
		} else if _, ok := holder.(*Profile); ok {
			value = []byte(`{"routers":` + string(row.Value) + `}`)
			prefix = ""
		} else {
			prefix = row.Name
		}
		if holder.UnmarshalJSON(value) != nil {
			t.Fatal("snapshot HR decode failed")
		}
		if row.Name == "agents" {
			prefix = ""
		}
		for _, issue := range valueIssues(reflect.ValueOf(holder), prefix) {
			survey.issues[indexPattern.ReplaceAllString(issue.Path, "[]")]++
		}
		survey.walk(reflect.ValueOf(holder))
	}
	keys := func(values map[string]int) []string {
		result := make([]string, 0, len(values))
		for key := range values {
			result = append(result, key)
		}
		slices.Sort(result)
		return result
	}
	// Derived members written by older releases and kept opaque on purpose.
	retained := map[string]bool{"Agent.has_update": true}
	t.Logf("HR records visited (counts only): %v", survey.records)
	unknown := keys(survey.unknown)
	t.Logf("HR undeclared members (names only): %v", unknown)
	t.Logf("HR null in non-pointer members (names only): %v", keys(survey.nullValues))
	if issues := keys(survey.issues); len(issues) != 0 {
		t.Fatalf("snapshot HR type issues (schema paths only): %v", issues)
	}
	if nulls := keys(survey.nullValues); len(nulls) != 0 {
		t.Fatalf("snapshot HR nulls in non-pointer members: %v", nulls)
	}
	for _, name := range unknown {
		if !retained[name] {
			t.Fatalf("snapshot HR member is not declared: %v", unknown)
		}
	}
}

// TestIssuesSeparateFallbackFromEmptyProof is the type-level guard of the
// N8-4f clarification: outside the package an R5 fallback zero value can be
// told apart from a stored empty or absent proof.
func TestIssuesSeparateFallbackFromEmptyProof(t *testing.T) {
	tests := []struct {
		name, source, path string
		want               bool
	}{
		{"absent manifest", `{}`, "ros_policy_state", false},
		{"null manifest", `{"ros_policy_state":null}`, "ros_policy_state", false},
		{"wrong manifest type", `{"ros_policy_state":"synthetic"}`, "ros_policy_state", true},
		{"empty resources", `{"ros_policy_state":{"resources":[]}}`, "ros_policy_state", false},
		{"wrong resources type", `{"ros_policy_state":{"resources":"synthetic"}}`, "ros_policy_state", true},
		{"numeric resource id text", `{"dns_guard_state":{"resources":[{"id":7}]}}`, "dns_guard_state", false},
		{"wrong resource id", `{"dns_guard_state":{"resources":[{"id":false}]}}`, "dns_guard_state", true},
		{"wrong pd proof role", `{"ipv6_pd_guard_state":{"resources":[{"role":false}]}}`, "ipv6_pd_guard_state", true},
		{"settings are not the manifest", `{"ipv6_pd_guard":{"enabled":"yes"}}`, "ipv6_pd_guard_state", false},
		{"settings issue", `{"ipv6_pd_guard":{"enabled":"yes"}}`, "ipv6_pd_guard", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var router Router
			if err := router.UnmarshalJSON([]byte(tt.source)); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if got := IssueUnder(Issues(&router), tt.path); got != tt.want {
				t.Fatalf("IssueUnder(%q) = %v, want %v; issues %v", tt.path, got, tt.want, Issues(router))
			}
			if got := IssueUnder(Issues(router), tt.path); got != tt.want {
				t.Fatal("value and pointer views disagree")
			}
		})
	}

	var router Router
	if router.UnmarshalJSON([]byte(`{"ros_policy_state":{"spec":{},"resources":[{"id":"*1"},{"id":false}]}}`)) != nil {
		t.Fatal("decode failed")
	}
	if got := Issues(router.ROSPolicyState); !slices.Equal(got, []FieldIssue{{"resources[1].id", IssueType}}) {
		t.Fatalf("subtree issues = %v", got)
	}
	built := Router{ROSPolicyState: &ROSPolicyState{}}
	if len(Issues(built)) != 0 {
		t.Fatal("records built in Go have no stored issues")
	}

	var agent Agent
	if agent.UnmarshalJSON([]byte(`{"deployments":{"private-task":"synthetic"},"applied_content":{"files":false}}`)) != nil {
		t.Fatal("agent decode failed")
	}
	issues := Issues(agent)
	if !IssueUnder(issues, "deployments") || !IssueUnder(issues, "applied_content") || IssueUnder(issues, "applied") {
		t.Fatalf("agent issues = %v", issues)
	}
	for _, issue := range issues {
		if strings.Contains(issue.Path, "private") {
			t.Fatal("issue path leaked a user map key")
		}
	}
}

// TestHRReviewMembers covers members found missing or presence-blind in review.
func TestHRReviewMembers(t *testing.T) {
	var verification IPv6Verification
	if verification.UnmarshalJSON([]byte(`{"status":"error","error_code":"CLIENT_UNAVAILABLE"}`)) != nil ||
		verification.ErrorCode == nil || *verification.ErrorCode != "CLIENT_UNAVAILABLE" || verification.Fields.Len() != 0 {
		t.Fatal("verification error code is not typed")
	}
	var manifest DeploymentManifest
	if manifest.UnmarshalJSON([]byte(`{"protocol_version":1,"final_config":true}`)) != nil ||
		manifest.FinalConfig == nil || !*manifest.FinalConfig || manifest.Fields.Len() != 0 {
		t.Fatal("final configuration marker is not typed")
	}
	tests := []struct {
		name, source string
		present      bool
	}{
		{"older spec without exempt", `{"bypass":["192.0.2.1/32"]}`, false},
		{"empty exempt", `{"bypass":[],"exempt":[]}`, true},
		{"exempt hosts", `{"exempt":["192.0.2.1"]}`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var spec DNSGuardSpec
			if spec.UnmarshalJSON([]byte(tt.source)) != nil {
				t.Fatal("decode failed")
			}
			if (spec.Exempt != nil) != tt.present {
				t.Fatalf("exempt presence = %v, want %v", spec.Exempt != nil, tt.present)
			}
			out, err := Marshal(spec)
			if err != nil || string(out) != tt.source {
				t.Fatal("spec no-op changed bytes")
			}
		})
	}
	written, err := Marshal(DNSGuardSpec{Exempt: &[]string{}})
	if err != nil || string(written) != `{"exempt":[]}` {
		t.Fatalf("explicit empty exempt = %s", written)
	}
}
