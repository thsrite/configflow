package agents

// Registry helpers used by internal/agents/upgrades (A15: upgrades.py and
// address_follow.py import them from manager.py).

import (
	"time"

	"configflow/internal/jsonx"
	"configflow/internal/taskstatus"
)

// AgentLabel is _agent_label(agent): log_text(name or id).
func AgentLabel(agent *jsonx.Object) string { return agentLabel(agent) }

// UpgradeBusy is _upgrade_busy(agent): an update not yet succeeded, failed
// or rolled back.
func UpgradeBusy(agent *jsonx.Object) bool { return upgradeBusy(agent) }

// NowTimestamp is time.time().
func NowTimestamp() float64 { return nowTimestamp() }

// ISOTimestamp is datetime.fromisoformat(value).timestamp(); ok is false
// where Python raises ValueError: fromisoformat refuses the text, or a naive
// text (local wall-clock time, resolved like CPython with fold=0) needs a
// local time outside the years 1–9999 while it is resolved (CPython's
// local_to_seconds: "year 0 is out of range" for the first day of year 1 in
// every zone, "year 10000 ..." for the last hours of 9999 east of UTC).
func ISOTimestamp(value string) (float64, bool) {
	moment, naive, ok := fromISO(value)
	if !ok {
		return 0, false
	}
	if naive {
		parsed, _ := taskstatus.FromISOFormat(value)
		wall := time.Date(parsed.Year(), parsed.Month(), parsed.Day(), parsed.Hour(), parsed.Minute(),
			parsed.Second(), 0, time.UTC).Unix()
		if !localYearsInRange(wall, localZone) {
			return 0, false
		}
	}
	return pyTimestampOf(moment, naive), true
}

// localYearsInRange replays the local() calls of CPython's
// local_to_seconds(..., fold=0) for wall (the wall clock read as UTC) and
// reports whether every local time they produce lies in the years 1–9999
// (utc_to_seconds raises ValueError otherwise).
func localYearsInRange(wall int64, zone *time.Location) bool {
	inRange := true
	local := func(u int64) int64 {
		t := time.Unix(u, 0).In(zone)
		if year := t.Year(); year < 1 || year > 9999 {
			inRange = false
		}
		_, offset := t.Zone()
		return u + int64(offset)
	}
	const maxFoldSeconds = 24 * 3600
	a := local(wall) - wall
	if !inRange {
		return false
	}
	u1 := wall - a
	t1 := local(u1)
	if !inRange {
		return false
	}
	var b int64
	if t1 == wall {
		u2 := u1 - maxFoldSeconds
		b = local(u2) - u2
		if !inRange || a == b {
			return inRange
		}
	} else {
		b = t1 - u1
	}
	local(wall - b)
	return inRange
}
