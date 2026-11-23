package model

import (
	"slices"
	"strings"
	"testing"
)

// hrSchemaSurvey collects value-free schema facts for HR records: tolerated
// type issues, undeclared members and null stored in non-pointer members.
// TestIssuesSeparateFallbackFromEmptyProof is the type-level guard of the
// N8-4f clarification: a lenient zero value read from a wrong-typed stored
// manifest can be told apart from a stored empty or absent proof. Since O18
// T1c the evidence is the read's issue list (DecodeStored), not Issues() on
// the record (plain records keep no stored provenance).
func TestIssuesSeparateFallbackFromEmptyProof(t *testing.T) {
	tests := []struct {
		name, source, path string
		want               bool
		// nestedPath: the issue is inside a manifest decoded on its own; its
		// path lacks the manifest prefix (gap reported to the T1c author).
		nestedPath bool
	}{
		{"absent manifest", `{}`, "ros_policy_state", false, false},
		{"null manifest", `{"ros_policy_state":null}`, "ros_policy_state", false, false},
		{"wrong manifest type", `{"ros_policy_state":"synthetic"}`, "ros_policy_state", true, false},
		{"empty resources", `{"ros_policy_state":{"resources":[]}}`, "ros_policy_state", false, false},
		{"wrong resources type", `{"ros_policy_state":{"resources":"synthetic"}}`, "ros_policy_state", true, true},
		{"numeric resource id text", `{"dns_guard_state":{"resources":[{"id":7}]}}`, "dns_guard_state", false, false},
		{"wrong resource id", `{"dns_guard_state":{"resources":[{"id":false}]}}`, "dns_guard_state", true, true},
		{"wrong pd proof role", `{"ipv6_pd_guard_state":{"resources":[{"role":false}]}}`, "ipv6_pd_guard_state", true, true},
		{"settings are not the manifest", `{"ipv6_pd_guard":{"enabled":"yes"}}`, "ipv6_pd_guard_state", false, false},
		{"settings issue", `{"ipv6_pd_guard":{"enabled":"yes"}}`, "ipv6_pd_guard", true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var router Router
			issues, err := DecodeStored([]byte(tt.source), &router, "")
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if got := IssueUnder(issues, tt.path); got != tt.want {
				t.Fatalf("IssueUnder(%q) = %v, want %v; issues %v", tt.path, got, tt.want, issues)
			}
		})
	}

	// A manifest stored with another JSON type is kept whole (fail closed);
	// records built in Go have no stored issues.
	var router Router
	issues, err := DecodeStored([]byte(`{"ros_policy_state":"synthetic"}`), &router, "")
	if err != nil || router.ROSPolicyState == nil || string(router.ROSPolicyState.Stored) != `"synthetic"` {
		t.Fatalf("damaged manifest not kept: %v %+v", err, router.ROSPolicyState)
	}
	if !slices.Equal(issues, []FieldIssue{{"ros_policy_state", IssueType}}) {
		t.Fatalf("issues = %v", issues)
	}

	var agent Agent
	issues, err = DecodeStored([]byte(`{"deployments":{"private-task":"synthetic"},"applied_content":{"files":false}}`), &agent, "")
	if err != nil {
		t.Fatal("agent decode failed")
	}
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
		verification.ErrorCode == nil || *verification.ErrorCode != "CLIENT_UNAVAILABLE" {
		t.Fatal("verification error code is not typed")
	}
	var manifest DeploymentManifest
	if manifest.UnmarshalJSON([]byte(`{"protocol_version":1,"final_config":true}`)) != nil ||
		manifest.FinalConfig == nil || !*manifest.FinalConfig {
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
