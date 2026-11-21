package agents

import (
	"fmt"
	"math"
	"math/big"
	"strings"
	"time"

	"configflow/internal/taskstatus"
)

// Business timestamps retain microsecond granularity in local registry text
// and UTC operation journals. Tests replace these two sources deterministically.
var clock = time.Now
var localZone = time.Local

func isoformat(moment time.Time, aware bool) string {
	text := moment.Format("2006-01-02T15:04:05")
	if micro := moment.Nanosecond() / 1000; micro != 0 {
		text += fmt.Sprintf(".%06d", micro)
	}
	if aware {
		_, offset := moment.Zone()
		if offset%60 != 0 {
			text += moment.Format("-07:00:00")
		} else {
			text += moment.Format("-07:00")
		}
	}
	return text
}
func nowISO() string    { return isoformat(clock(), false) }
func utcNowISO() string { return isoformat(clock().UTC(), true) }
func utcNowZ() string   { return strings.TrimSuffix(utcNowISO(), "+00:00") + "Z" }

// fromISO uses the shared accepted ISO grammar. Naive registry timestamps are
// local wall times; aware values retain their instant and offset.
func fromISO(value string) (moment time.Time, naive bool, ok bool) {
	parsed, naive, ok := taskstatus.ParseISO(value)
	if !ok {
		return time.Time{}, false, false
	}
	if !naive {
		return parsed, false, true
	}
	return resolveLocalWall(parsed, localZone), true, true
}

func wallTime(moment time.Time) time.Time {
	return time.Date(moment.Year(), moment.Month(), moment.Day(), moment.Hour(), moment.Minute(), moment.Second(), moment.Nanosecond(), time.UTC)
}

// resolveLocalWall defines the registry's DST policy using Go's zone data:
// the earlier instant in a repeated hour, and the later side of a missing hour.
// No interpreter-specific local-time inversion algorithm is maintained here.
func resolveLocalWall(wall time.Time, zone *time.Location) time.Time {
	candidate := time.Date(wall.Year(), wall.Month(), wall.Day(), wall.Hour(), wall.Minute(), wall.Second(), wall.Nanosecond(), zone)
	requested := wallTime(wall)
	actual := wallTime(candidate)
	if !actual.Equal(requested) {
		if shift := requested.Sub(actual); shift > 0 {
			candidate = candidate.Add(shift)
		}
		return candidate
	}
	_, offset := candidate.Zone()
	_, previousOffset := candidate.Add(-24 * time.Hour).Zone()
	earlier := candidate.Add(time.Duration(offset-previousOffset) * time.Second)
	if earlier.Before(candidate) && wallTime(earlier).Equal(requested) {
		return earlier
	}
	return candidate
}

// localTimestampRange keeps the registry's supported local calendar window,
// including the adjacent day required to disambiguate DST. The local view of
// the UTC wall spelling must also remain representable in that calendar.
func localTimestampRange(wall time.Time, zone *time.Location) bool {
	moment := resolveLocalWall(wall, zone)
	for _, candidate := range []time.Time{wall.In(zone), moment, moment.Add(-24 * time.Hour)} {
		if year := candidate.Year(); year < 1 || year > 9999 {
			return false
		}
	}
	return true
}

func localEpochSeconds(moment time.Time) float64 {
	return float64(moment.Unix()) + float64(moment.Nanosecond()/1000)/1e6
}

// timestampSeconds preserves the historical local-clock encoding while using
// an exact microsecond rational for offset instants, avoiding double rounding
// for distant dates. Both are native math/big operations, not scalar coercions.
func timestampSeconds(moment time.Time, naive bool) float64 {
	if naive {
		return localEpochSeconds(moment)
	}
	micros := new(big.Int).Mul(big.NewInt(moment.Unix()), big.NewInt(1e6))
	micros.Add(micros, big.NewInt(int64(moment.Nanosecond()/1000)))
	value, _ := new(big.Rat).SetFrac(micros, big.NewInt(1e6)).Float64()
	return value
}

func fromTimestamp(value float64) time.Time {
	seconds := math.Floor(value)
	micro := math.RoundToEven((value - seconds) * 1e6)
	if micro >= 1e6 {
		seconds++
		micro -= 1e6
	}
	return time.Unix(int64(seconds), int64(micro)*1000).In(localZone)
}
func epochSecondsFromNanos(nanos int64) float64 {
	if nanos%1e9 == 0 {
		return float64(nanos / 1e9)
	}
	return float64(nanos) / 1e9
}
func nowTimestamp() float64 { return epochSecondsFromNanos(clock().UnixNano()) }
