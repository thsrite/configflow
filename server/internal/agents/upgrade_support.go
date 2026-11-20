package agents

// Registry helpers used by internal/agents/upgrades (A15: upgrades.py and
// address_follow.py import them from manager.py).

import (
	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/taskstatus"
)

// AgentLabel is _agent_label(agent): log_text(name or id).
func AgentLabel(agent *jsonx.Object) string { return agentLabel(agent) }

// UpgradeBusy is _upgrade_busy(agent): an update not yet succeeded, failed
// or rolled back.
func UpgradeBusy(agent *jsonx.Object) bool { return upgradeBusy(agent) }

// NowTimestamp is time.time().
func NowTimestamp() float64 { return nowTimestamp() }

// ISOTimestamp resolves a supported ISO value to epoch seconds. Local registry
// spellings use the declared DST and local calendar range policy.
func ISOTimestamp(value string) (float64, bool) {
	moment, naive, ok := fromISO(value)
	if !ok {
		return 0, false
	}
	if naive {
		wall, _ := taskstatus.FromISOFormat(value)
		if !localTimestampRange(wall, localZone) {
			return 0, false
		}
	}
	return timestampSeconds(moment, naive), true
}

// DNSFollowBusy reports whether a live follow publication owns this Agent.
func DNSFollowBusy(records []any, agentID string) bool { return dnsFollowBusy(records, agentID) }

// UpgradeInProgress reports the native upgrade owner's active reservation.
func UpgradeInProgress(a *model.Agent) bool { return upgradeBusyModel(a) }

// DNSFollowInProgress reports whether a native follow job owns this Agent.
func DNSFollowInProgress(records []model.Agent, id string) bool {
	return dnsFollowBusyModel(records, id)
}
