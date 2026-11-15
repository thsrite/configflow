package agents

import (
	"fmt"
	"math"
	"math/big"
	"strings"
	"time"

	"configflow/internal/taskstatus"
)

// clock is time.Now, replaceable in tests.
var clock = time.Now

// isoformat is datetime.isoformat() of a naive (local wall clock) or aware
// time: microseconds only when non-zero, "+HH:MM" for aware times.
func isoformat(t time.Time, aware bool) string {
	text := fmt.Sprintf("%04d-%02d-%02dT%02d:%02d:%02d", t.Year(), int(t.Month()), t.Day(), t.Hour(), t.Minute(), t.Second())
	if micro := t.Nanosecond() / 1000; micro != 0 {
		text += fmt.Sprintf(".%06d", micro)
	}
	if aware {
		_, offset := t.Zone()
		sign := '+'
		if offset < 0 {
			sign, offset = '-', -offset
		}
		text += fmt.Sprintf("%c%02d:%02d", sign, offset/3600, offset/60%60)
		if offset%60 != 0 {
			text += fmt.Sprintf(":%02d", offset%60)
		}
	}
	return text
}

// nowISO is datetime.now().isoformat(): the local wall clock, naive.
func nowISO() string { return isoformat(clock(), false) }

// utcNowISO is datetime.now(timezone.utc).isoformat() ("...+00:00").
func utcNowISO() string { return isoformat(clock().UTC(), true) }

// utcNowZ is datetime.now(timezone.utc).isoformat().replace('+00:00', 'Z').
func utcNowZ() string { return strings.TrimSuffix(utcNowISO(), "+00:00") + "Z" }

// localZone is the zone of Python's naive datetimes (TZ at start-up);
// replaceable in tests only.
var localZone = time.Local

// fromISO is datetime.fromisoformat(value): the instant and whether the text
// was naive. A naive time is a local wall clock, as Python's .timestamp() and
// naive arithmetic against datetime.now() treat it; its instant is resolved
// like CPython's local_to_seconds with fold=0 (the earlier of two repeated
// wall-clock times; a time in a gap is read with the offset before the gap).
func fromISO(value string) (moment time.Time, naive bool, ok bool) {
	parsed, ok := taskstatus.FromISOFormat(value)
	if !ok {
		return time.Time{}, false, false
	}
	// FromISOFormat takes a naive text as UTC; tell the two apart by whether an
	// offset can still be appended (only a naive date-time or date accepts one).
	_, withOffset := taskstatus.FromISOFormat(value + "+00:00")
	_, dateOnly := taskstatus.FromISOFormat(value + "T00:00:00+00:00")
	if !withOffset && !dateOnly {
		return parsed, false, true
	}
	wall := time.Date(parsed.Year(), parsed.Month(), parsed.Day(), parsed.Hour(), parsed.Minute(),
		parsed.Second(), 0, time.UTC).Unix()
	return time.Unix(localToSeconds(wall, localZone), int64(parsed.Nanosecond())).In(localZone), true, true
}

// localToSeconds is CPython's local_to_seconds(..., fold=0): the POSIX time
// whose local wall clock in zone is wall (seconds of the wall clock read as
// UTC).
func localToSeconds(wall int64, zone *time.Location) int64 {
	local := func(u int64) int64 {
		_, offset := time.Unix(u, 0).In(zone).Zone()
		return u + int64(offset)
	}
	const maxFoldSeconds = 24 * 3600
	a := local(wall) - wall
	u1 := wall - a
	t1 := local(u1)
	var b int64
	if t1 == wall {
		// One solution; look for an earlier one (fold=0).
		u2 := u1 - maxFoldSeconds
		b = local(u2) - u2
		if a == b {
			return u1
		}
	} else {
		b = t1 - u1
	}
	u2 := wall - b
	if local(u2) == wall {
		return u2
	}
	if t1 == wall {
		return u1
	}
	// Neither offset gives the wall clock: it is in a gap.
	return max(u1, u2)
}

// pyTimestamp is the float .timestamp() of a naive datetime: whole seconds
// plus microsecond/1e6, as CPython computes it.
func pyTimestamp(t time.Time) float64 {
	return float64(t.Unix()) + float64(t.Nanosecond()/1000)/1e6
}

// pyTimestampOf is datetime.timestamp() of a fromISO result: naive as
// pyTimestamp; aware as (self - epoch).total_seconds(), one correctly rounded
// division of the microseconds by 10**6.
func pyTimestampOf(t time.Time, naive bool) float64 {
	if naive {
		return pyTimestamp(t)
	}
	micros := new(big.Int).Mul(big.NewInt(t.Unix()), big.NewInt(1e6))
	micros.Add(micros, big.NewInt(int64(t.Nanosecond()/1000)))
	f, _ := new(big.Rat).SetFrac(micros, big.NewInt(1e6)).Float64()
	return f
}

// fromTimestamp is datetime.fromtimestamp(ts) (local, naive) truncated to
// microseconds as Python rounds it (round-half-even of the fraction).
func fromTimestamp(ts float64) time.Time {
	seconds := math.Floor(ts)
	micro := math.RoundToEven((ts - seconds) * 1e6)
	if micro >= 1e6 {
		seconds++
		micro -= 1e6
	}
	return time.Unix(int64(seconds), int64(micro)*1000).In(localZone)
}

// pyTimeFromNanos is time.time() for a clock reading in nanoseconds
// (CPython's _PyTime_AsSecondsDouble).
func pyTimeFromNanos(ns int64) float64 {
	if ns%1e9 == 0 {
		return float64(ns / 1e9)
	}
	return float64(ns) / 1e9
}

// nowTimestamp is time.time().
func nowTimestamp() float64 { return pyTimeFromNanos(clock().UnixNano()) }
