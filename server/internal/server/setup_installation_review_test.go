package server

import (
	"encoding/json"
	"testing"

	"configflow/internal/setup"
)

// A22 review F3: the public installation view carries raw registry members.
// Python's setup check treats only a real false as disabled; an Agent whose
// enabled flag an administrator stored as text must not fail the check with a
// decoding error.
func TestInstallationStateReadsRawAgentMembersLikePython(t *testing.T) {
	view := json.RawMessage(`{"id":"cccccccccccccccccccccccccccccccc","phase":"published","config_changed":false,"target_changed":false,
		"agents":{"mihomo":{"id":"agent_a","name":"synthetic-linux-mihomo","host":"192.0.2.10","port":"8080","enabled":"no","status":"online"},
		"oxidns":{"id":"agent_b","name":"synthetic-linux-oxidns","host":"192.0.2.10","port":8082,"enabled":false,"status":"online"}}}`)
	var strict setup.InstallationState
	if json.Unmarshal(view, &strict) == nil {
		t.Fatal("control: the typed decode no longer rejects this view")
	}
	state, err := installationState(view)
	if err != nil {
		t.Fatal(err)
	}
	if state.Agents.Mihomo.Enabled != nil || state.Agents.Mihomo.ID != "agent_a" || state.Agents.Mihomo.Host != "192.0.2.10" {
		t.Fatalf("text enabled projected wrongly: %+v", state.Agents.Mihomo)
	}
	if state.Agents.OxiDNS.Enabled == nil || *state.Agents.OxiDNS.Enabled || state.Phase != "published" {
		t.Fatalf("false enabled lost: %+v", state.Agents.OxiDNS)
	}
	if none, err := installationState(json.RawMessage(`null`)); err != nil || none != nil {
		t.Fatal("no installation", none, err)
	}
}
