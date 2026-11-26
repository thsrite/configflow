package taskstatus

import "time"

// zonedLayouts and naiveLayouts are the timestamp forms ConfigFlow stores:
// datetime.isoformat() output of the former Python backend (naive local wall
// time or UTC offset, with or without microseconds), the "Z" form of
// NowISO, and plain dates. time.Parse accepts an optional fractional second
// after the seconds field without it being in the layout. The seconds-offset
// layout reads what agents' isoformat writes for historical zones.
const maxOffset = 24 * 60 * 60

var (
	zonedLayouts = []string{time.RFC3339, "2006-01-02T15:04:05Z07:00:00"}
	naiveLayouts = []string{"2006-01-02T15:04:05", time.DateOnly}
)

// ParseISO parses a stored ISO 8601 timestamp. A value with a zone
// designator keeps its offset (UTC for "Z" and "+00:00"); naive reports a
// value without one, whose wall clock is returned in UTC. Offsets of a day
// or more are rejected.
func ParseISO(value string) (t time.Time, naive bool, ok bool) {
	for _, layout := range zonedLayouts {
		if t, err := time.ParseInLocation(layout, value, time.UTC); err == nil {
			if _, offset := t.Zone(); offset <= -maxOffset || offset >= maxOffset {
				return time.Time{}, false, false
			}
			return t, false, true
		}
	}
	for _, layout := range naiveLayouts {
		if t, err := time.Parse(layout, value); err == nil {
			return t, true, true
		}
	}
	return time.Time{}, false, false
}
