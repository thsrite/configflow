package agents

// Registry helpers used by internal/agents/upgrades.

import (
	"configflow/internal/model"
	"configflow/internal/taskstatus"
)

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

// UpgradeInProgress reports the native upgrade owner's active reservation.
func UpgradeInProgress(a *model.Agent) bool { return upgradeBusyModel(a) }

// DNSFollowInProgress reports whether a native follow job owns this Agent.
func DNSFollowInProgress(records []model.Agent, id string) bool {
	return dnsFollowBusyModel(records, id)
}
