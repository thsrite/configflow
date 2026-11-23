// Package timefmt formats the ISO 8601 timestamps stored in data files and
// returned by the API: seconds always, a six-digit fraction only when the
// microseconds are not zero. It does not parse timestamps (see
// taskstatus.ParseISO) and never changes the zone of a time except in UTC
// and UTCZ.
package timefmt

import (
	"strconv"
	"time"
)

// Naive formats the wall clock of t without a zone:
// "2006-01-02T15:04:05" or "2006-01-02T15:04:05.000001".
func Naive(t time.Time) string {
	text := t.Format("2006-01-02T15:04:05")
	if micro := t.Nanosecond() / 1000; micro != 0 {
		fraction := strconv.Itoa(micro)
		text += ".000000"[:7-len(fraction)] + fraction
	}
	return text
}

// Offset formats t with its zone offset: "+08:00", or "+05:17:36" for an
// offset that is not a whole number of minutes.
func Offset(t time.Time) string {
	if _, offset := t.Zone(); offset%60 != 0 {
		return Naive(t) + t.Format("-07:00:00")
	}
	return Naive(t) + t.Format("-07:00")
}

// UTC formats t in UTC with a "+00:00" suffix.
func UTC(t time.Time) string { return Naive(t.UTC()) + "+00:00" }

// UTCZ formats t in UTC with a "Z" suffix.
func UTCZ(t time.Time) string { return Naive(t.UTC()) + "Z" }
