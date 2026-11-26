package server

import (
	"encoding/json"
	"testing"

	"configflow/internal/gateway/host"
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

// The status reply is the Agent's own JSON. Python's check reads it with
// truthiness, "is True" and == '1'; other member types must give the same
// refusal (409 in the check), never a decoding error.
func TestGatewayStatusReadsAgentReplyLikePython(t *testing.T) {
	reply := host.Reply{
		"ipv4_forwarding":   json.RawMessage(`{"current":1,"persisted":"yes"}`),
		"tun":               json.RawMessage(`{"configured":true,"present":1,"gateway_marker":"set","device":7}`),
		"managed":           json.RawMessage(`{"active":0,"pending":null,"healthy":true,"error":{"code":"journal"}}`),
		"enabled_in_config": json.RawMessage(`"no"`),
	}
	b, _ := json.Marshal(reply)
	var strict setup.GatewayStatus
	if json.Unmarshal(b, &strict) == nil {
		t.Fatal("control: the typed decode no longer rejects this reply")
	}
	got := gatewayStatus(reply)
	if got.IPv4Forwarding == nil || got.IPv4Forwarding.Current == "1" || got.IPv4Forwarding.Persisted != nil {
		t.Fatalf("forwarding %+v", got.IPv4Forwarding)
	}
	if got.TUN == nil || got.TUN.Configured == nil || got.TUN.Present != nil || !got.TUN.GatewayMarker || got.TUN.Device != "" {
		t.Fatalf("tun %+v", got.TUN)
	}
	if got.Managed.Active || got.Managed.Pending || got.Managed.Healthy == nil || got.Managed.Error == "" || !got.EnabledInConfig {
		t.Fatalf("managed %+v", got.Managed)
	}
	// An unreadable journal is a remaining record (A20 HasRecord), and an old
	// Agent without the forwarding/TUN objects keeps the update hint.
	got = gatewayStatus(host.Reply{"managed": json.RawMessage(`"broken"`), "tun": json.RawMessage(`[]`)})
	if !got.Managed.Active || got.Managed.Error == "" || got.TUN != nil || got.IPv4Forwarding != nil {
		t.Fatalf("unreadable journal %+v", got)
	}
}
