package agents

// Registry helpers used by internal/agents/upgrades (A15: upgrades.py and
// address_follow.py import them from manager.py).

import "configflow/internal/jsonx"

// AgentLabel is _agent_label(agent): log_text(name or id).
func AgentLabel(agent *jsonx.Object) string { return agentLabel(agent) }

// UpgradeBusy is _upgrade_busy(agent): an update not yet succeeded, failed
// or rolled back.
func UpgradeBusy(agent *jsonx.Object) bool { return upgradeBusy(agent) }

// NowTimestamp is time.time().
func NowTimestamp() float64 { return nowTimestamp() }

// ISOTimestamp is datetime.fromisoformat(value).timestamp(); ok is false
// where fromisoformat raises ValueError. A naive text is local wall-clock
// time, resolved like CPython (fold=0).
func ISOTimestamp(value string) (float64, bool) {
	moment, naive, ok := fromISO(value)
	if !ok {
		return 0, false
	}
	return pyTimestampOf(moment, naive), true
}
