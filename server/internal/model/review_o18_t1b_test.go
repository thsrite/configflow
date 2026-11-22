package model

import (
	"encoding/json"
	"slices"
	"testing"
)

// HR O18 T1b ruling: a stored switch that makes the panel write to a device or
// push automatically reads as off when its JSON type is not a boolean (plus a
// type issue); other switches keep the flag-truthiness rule.
const reviewO18T1bAgent = `{
	"id":"a1","enabled":"yes","domain_discovery_enabled":1,"traffic_history_enabled":"x",
	"rule_sync":{"settings":{"library":"yes","rules":1},"state":{"status":"pending","force_refresh":"yes"}},
	"dns_follow_state":{"status":"failed","retry_requested":1},
	"oxidns_ha":{"enabled":"true","group_id":"g"},
	"oxidns_ha_publication":{"id":"j","status":"applying","activate":1,"handoff_required":"yes"},
	"latest_deployment":{"deployment_id":"d","manual_rollback":"yes","success":1}
}`

func reviewO18T1bCheckAgent(t *testing.T, how string, a *Agent) {
	t.Helper()
	off := map[string]*bool{
		"enabled": a.Enabled, "domain_discovery_enabled": a.DomainDiscoveryEnabled,
		"traffic_history_enabled": a.TrafficHistoryEnabled,
	}
	if a.RuleSync != nil {
		off["rule_sync.settings.library"] = a.RuleSync.Settings.Library
		off["rule_sync.settings.rules"] = a.RuleSync.Settings.Rules
		if a.RuleSync.State != nil {
			off["rule_sync.state.force_refresh"] = a.RuleSync.State.ForceRefresh
		}
	}
	if a.DNSFollowState != nil {
		off["dns_follow_state.retry_requested"] = a.DNSFollowState.RetryRequested
	}
	if a.OxiDNSHA != nil {
		off["oxidns_ha.enabled"] = a.OxiDNSHA.Enabled
	}
	if a.OxiDNSHAPublication != nil {
		off["oxidns_ha_publication.activate"] = a.OxiDNSHAPublication.Activate
	}
	if len(off) != 9 {
		t.Fatalf("%s: decoded %d gate members, want 9", how, len(off))
	}
	for name, flag := range off {
		if flag == nil || *flag {
			t.Errorf("%s: %s = %v, want a stored false (gate of another type is off)", how, name, flag)
		}
	}
	if a.IsEnabled() {
		t.Errorf("%s: Agent with enabled:\"yes\" reads as enabled", how)
	}
	// Not gates: truthiness as before.
	if j := a.OxiDNSHAPublication; j.HandoffRequired == nil || !*j.HandoffRequired {
		t.Errorf("%s: handoff_required \"yes\" = %v, want true (not a gate)", how, j.HandoffRequired)
	}
	if d := a.LatestDeployment; d == nil || d.ManualRollback == nil || !*d.ManualRollback || d.Success == nil || !*d.Success {
		t.Errorf("%s: latest_deployment flags %+v, want truthy (not gates)", how, d)
	}
}

func TestReviewO18T1bWriteGatesOfAnotherTypeReadOff(t *testing.T) {
	var direct Agent
	issues, err := DecodeStored([]byte(reviewO18T1bAgent), &direct, "agents[0]")
	if err != nil {
		t.Fatal(err)
	}
	reviewO18T1bCheckAgent(t, "DecodeStored(Agent)", &direct)
	for _, want := range []string{"agents[0].enabled", "agents[0].rule_sync.settings.library", "agents[0].oxidns_ha_publication.activate"} {
		if !slices.Contains(issues, FieldIssue{Path: want, Kind: IssueType}) {
			t.Errorf("no type issue at %s: %v", want, issues)
		}
	}

	var system System
	if _, err := DecodeStored([]byte(`{"agents":[`+reviewO18T1bAgent+`]}`), &system, "system"); err != nil {
		t.Fatal(err)
	}
	reviewO18T1bCheckAgent(t, "DecodeStored(System)", &system.Agents[0])

	// A storage row decodes the agents member on its own.
	var row System
	if _, _, err := DecodeStoredMember(&row, "agents", []byte(`[`+reviewO18T1bAgent+`]`), "agents"); err != nil {
		t.Fatal(err)
	}
	reviewO18T1bCheckAgent(t, "DecodeStoredMember(agents)", &row.Agents[0])

	var unmarshaled Agent
	if err := json.Unmarshal([]byte(reviewO18T1bAgent), &unmarshaled); err != nil {
		t.Fatal(err)
	}
	reviewO18T1bCheckAgent(t, "json.Unmarshal", &unmarshaled)

	// One gate member decoded on its own (DecodeStoredMember, SetStoredMember).
	for _, member := range []string{"enabled", "traffic_history_enabled"} {
		var single Agent
		if _, _, err := DecodeStoredMember(&single, member, []byte(`"yes"`), member); err != nil {
			t.Fatal(err)
		}
		var follow DNSFollowState
		if err := SetStoredMember(&follow, "retry_requested", []byte(`1`)); err != nil {
			t.Fatal(err)
		}
		got, _, _ := EncodeMember(&single, member)
		if string(got) != "false" || follow.RetryRequested == nil || *follow.RetryRequested {
			t.Errorf("single member %s = %s, retry_requested %v; want false", member, got, follow.RetryRequested)
		}
	}

	// Real booleans and absence are unchanged.
	var plain Agent
	if _, err := DecodeStored([]byte(`{"enabled":true,"rule_sync":{"settings":{"library":true,"rules":false}},"oxidns_ha":{}}`), &plain, ""); err != nil {
		t.Fatal(err)
	}
	if !plain.IsEnabled() || !*plain.RuleSync.Settings.Library || *plain.RuleSync.Settings.Rules || plain.OxiDNSHA.Enabled != nil {
		t.Errorf("boolean switches changed: %+v %+v", plain.RuleSync.Settings, plain.OxiDNSHA)
	}
}

// HR O18 T1b: a stored oxidns_ha_publication of another JSON kind than an
// object is an empty (unfinished) journal, not absent: it keeps the pair
// locked like {} does (dns_follow_state too, L4). Other records of another
// kind stay absent.
func TestReviewO18T1bDamagedHAJournalFailsClosed(t *testing.T) {
	for _, value := range []string{`"applying"`, `1`, `true`, `[]`, `["x"]`} {
		var a Agent
		issues, err := DecodeStored([]byte(`{"id":"a","oxidns_ha_publication":`+value+`,"oxidns_ha":`+value+`,"dns_follow":`+value+`}`), &a, "")
		if err != nil {
			t.Fatal(err)
		}
		if a.OxiDNSHAPublication == nil || a.OxiDNSHAPublication.Status != "" {
			t.Errorf("journal %s = %+v, want an empty journal", value, a.OxiDNSHAPublication)
		}
		if a.OxiDNSHA != nil || a.DNSFollow != nil {
			t.Errorf("other records %s: %+v %+v, want absent", value, a.OxiDNSHA, a.DNSFollow)
		}
		if !slices.Contains(issues, FieldIssue{Path: "oxidns_ha_publication", Kind: IssueType}) {
			t.Errorf("journal %s: no type issue: %v", value, issues)
		}
	}
	var a Agent
	if _, err := DecodeStored([]byte(`{"oxidns_ha_publication":null}`), &a, ""); err != nil {
		t.Fatal(err)
	}
	if a.OxiDNSHAPublication != nil {
		t.Errorf("null journal = %+v, want absent", a.OxiDNSHAPublication)
	}
}

// HR O18 T1b L3: a stored credential of another JSON type than a string
// (numbers included) reads as empty, never as the number's text, and is
// reported. Text credentials are unchanged.
func TestReviewO18T1bCredentialsOfAnotherTypeAreEmpty(t *testing.T) {
	for _, value := range []string{`7`, `7.0`, `true`, `{}`, `["t"]`} {
		var a Agent
		issues, err := DecodeStored([]byte(`{"id":"a","token":`+value+`}`), &a, "")
		if err != nil {
			t.Fatal(err)
		}
		if a.Token != "" || !slices.Contains(issues, FieldIssue{Path: "token", Kind: IssueType}) {
			t.Errorf("Agent token %s = %q (issues %v), want empty plus an issue", value, a.Token, issues)
		}
		var session AgentRegistrationSession
		if _, err := DecodeStored([]byte(`{"hash":`+value+`}`), &session, ""); err != nil {
			t.Fatal(err)
		}
		if session.Hash != "" {
			t.Errorf("session hash %s = %q, want empty", value, session.Hash)
		}
		var single Agent
		if err := SetStoredMember(&single, "token", []byte(value)); err != nil || single.Token != "" {
			t.Errorf("single token %s = %q, %v", value, single.Token, err)
		}
	}
	var a Agent
	if _, err := DecodeStored([]byte(`{"token":"t-1","name":7}`), &a, ""); err != nil || a.Token != "t-1" || a.Name != "7" {
		t.Errorf("text token / number name: %q %q %v", a.Token, a.Name, err)
	}

	var settings SystemConfig
	// SystemConfig is a plain record (O18 T1c): the issues come from the
	// decode, one per token and one per dropped list element.
	issues, err := DecodeStored([]byte(`{"config_token":7,"rule_proxy_token":8,"retired_rule_proxy_tokens":[9,"old",{}],"server_domain":"https://example.com"}`), &settings, "")
	if err != nil {
		t.Fatal(err)
	}
	if settings.ConfigToken != "" || settings.RuleProxyToken != "" || !slices.Equal(settings.RetiredRuleProxyTokens, []string{"old"}) || settings.ServerDomain != "https://example.com" {
		t.Errorf("system tokens %q %q %q", settings.ConfigToken, settings.RuleProxyToken, settings.RetiredRuleProxyTokens)
	}
	if len(issues) != 4 {
		t.Errorf("system token issues %v, want 4", issues)
	}
	var plain SystemConfig
	if err := plain.UnmarshalJSON([]byte(`{"config_token":"c","rule_proxy_token":"r","retired_rule_proxy_tokens":["o"]}`)); err != nil ||
		plain.ConfigToken != "c" || plain.RuleProxyToken != "r" || !slices.Equal(plain.RetiredRuleProxyTokens, []string{"o"}) {
		t.Errorf("text system tokens changed: %+v %v", plain, err)
	}
}

// HR O18 T1b L2: a registration session host of another JSON type reads as
// DamagedText (refused by admission), and stays damaged when written back;
// a missing or null host is no restriction.
func TestReviewO18T1bDamagedSessionHost(t *testing.T) {
	for _, value := range []string{`5`, `{}`, `true`, `["h"]`} {
		var session AgentRegistrationSession
		if _, err := DecodeStored([]byte(`{"hash":"h","host":`+value+`,"port":9090}`), &session, ""); err != nil {
			t.Fatal(err)
		}
		if session.Host != DamagedText {
			t.Errorf("host %s = %q, want DamagedText", value, session.Host)
		}
		data, err := Marshal(session)
		if err != nil {
			t.Fatal(err)
		}
		var again AgentRegistrationSession
		if _, err := DecodeStored(data, &again, ""); err != nil || again.Host != DamagedText {
			t.Errorf("host %s after a rewrite = %q (%s), want DamagedText", value, again.Host, data)
		}
	}
	for _, value := range []string{`null`, `"192.0.2.5"`} {
		var session AgentRegistrationSession
		if _, err := DecodeStored([]byte(`{"host":`+value+`}`), &session, ""); err != nil {
			t.Fatal(err)
		}
		if session.Host == DamagedText {
			t.Errorf("host %s reads as damaged", value)
		}
	}
	// The members of other records are not restrictions.
	var a Agent
	if _, err := DecodeStored([]byte(`{"host":{}}`), &a, ""); err != nil || a.Host != "" {
		t.Errorf("Agent host {} = %q, want empty", a.Host)
	}
}

// HR O18 T1b L4: a stored dns_follow_state of another JSON kind is an empty
// state that counts as busy; null is no state.
func TestReviewO18T1bDamagedDNSFollowStateBusy(t *testing.T) {
	for _, value := range []string{`"publishing"`, `1`, `[]`, `{}`} {
		var a Agent
		if _, err := DecodeStored([]byte(`{"dns_follow_state":`+value+`}`), &a, ""); err != nil {
			t.Fatal(err)
		}
		if !a.DNSFollowState.Busy() {
			t.Errorf("state %s = %+v, want busy", value, a.DNSFollowState)
		}
	}
	for _, tt := range []struct {
		value string
		busy  bool
	}{{`null`, false}, {`{"status":"waiting"}`, false}, {`{"status":"failed"}`, false}, {`{"status":"preparing"}`, true}, {`{"status":"publishing"}`, true}} {
		var a Agent
		if _, err := DecodeStored([]byte(`{"dns_follow_state":`+tt.value+`}`), &a, ""); err != nil {
			t.Fatal(err)
		}
		if a.DNSFollowState.Busy() != tt.busy {
			t.Errorf("state %s busy = %v, want %v", tt.value, !tt.busy, tt.busy)
		}
	}
}
