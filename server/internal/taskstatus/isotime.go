package taskstatus

import (
	"fmt"
	"time"
	"unicode/utf8"
)

// This file ports datetime.fromisoformat as CPython 3.11 implements it in C
// (Modules/_datetimemodule.c: parse_isoformat_date, parse_hh_mm_ss_ff,
// parse_isoformat_time, _find_isoformat_datetime_separator). The C code reads a
// NUL-terminated buffer, so the input is read through at(), which yields 0
// beyond the end. Its leniencies (junk between the time and a zone designator,
// "12:" meaning 12:00, an empty fraction) are kept on purpose.

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

type isoBuffer []byte

func (b isoBuffer) at(i int) byte {
	if i < 0 || i >= len(b) {
		return 0
	}
	return b[i]
}

// parseDigits reads exactly n ASCII digits at p.
func (b isoBuffer) parseDigits(p, n int) (value, next int, ok bool) {
	for i := 0; i < n; i++ {
		c := b.at(p + i)
		if !isDigit(c) {
			return 0, 0, false
		}
		value = value*10 + int(c-'0')
	}
	return value, p + n, true
}

func isLeap(year int) bool { return year%4 == 0 && (year%100 != 0 || year%400 == 0) }

func daysInMonth(year, month int) int {
	switch month {
	case 2:
		if isLeap(year) {
			return 29
		}
		return 28
	case 4, 6, 9, 11:
		return 30
	}
	return 31
}

// findSeparator is _find_isoformat_datetime_separator; -1 means invalid.
func findSeparator(b isoBuffer) int {
	n := len(b)
	if n == 7 {
		return 7
	}
	if b.at(4) == '-' {
		if b.at(5) == 'W' {
			if n < 8 {
				return -1
			}
			if n > 8 && b.at(8) == '-' {
				if n == 9 {
					return -1
				}
				if n > 10 && isDigit(b.at(10)) {
					return 8
				}
				return 10
			}
			return 8
		}
		return 10
	}
	if b.at(4) == 'W' {
		idx := 7
		for ; idx < n; idx++ {
			if !isDigit(b.at(idx)) {
				break
			}
		}
		if idx < 9 {
			return idx
		}
		if idx%2 == 0 {
			return 7
		}
		return 8
	}
	return 8
}

// isoToYMD is iso_to_ymd: the Gregorian date of an ISO year, week and weekday.
func isoToYMD(isoYear, isoWeek, isoDay int) (year, month, day int, ok bool) {
	if isoYear < 1 || isoYear > 9999 {
		return 0, 0, 0, false
	}
	if isoWeek <= 0 || isoWeek >= 53 {
		outOfRange := true
		if isoWeek == 53 {
			first := int(time.Date(isoYear, 1, 1, 0, 0, 0, 0, time.UTC).Weekday()) // Sunday = 0
			first = (first + 6) % 7                                                // Monday = 0
			if first == 3 || (first == 2 && isLeap(isoYear)) {
				outOfRange = false
			}
		}
		if outOfRange {
			return 0, 0, 0, false
		}
	}
	if isoDay <= 0 || isoDay >= 8 {
		return 0, 0, 0, false
	}
	jan4 := time.Date(isoYear, 1, 4, 0, 0, 0, 0, time.UTC)
	monday := jan4.AddDate(0, 0, -((int(jan4.Weekday()) + 6) % 7))
	date := monday.AddDate(0, 0, (isoWeek-1)*7+(isoDay-1))
	if date.Year() < 1 || date.Year() > 9999 {
		return 0, 0, 0, false
	}
	return date.Year(), int(date.Month()), date.Day(), true
}

// parseDate is parse_isoformat_date over b[:length].
func parseDate(b isoBuffer, length int) (year, month, day int, ok bool) {
	year, p, ok := b.parseDigits(0, 4)
	if !ok {
		return 0, 0, 0, false
	}
	usesSeparator := b.at(p) == '-'
	if usesSeparator {
		p++
	}
	if b.at(p) == 'W' {
		p++
		week, np, ok := b.parseDigits(p, 2)
		if !ok {
			return 0, 0, 0, false
		}
		p = np
		isoDay := 1
		if p < length {
			if usesSeparator {
				c := b.at(p)
				p++
				if c != '-' {
					return 0, 0, 0, false
				}
			}
			if isoDay, _, ok = b.parseDigits(p, 1); !ok {
				return 0, 0, 0, false
			}
		}
		return isoToYMD(year, week, isoDay)
	}
	month, p, ok = b.parseDigits(p, 2)
	if !ok {
		return 0, 0, 0, false
	}
	if usesSeparator {
		c := b.at(p)
		p++
		if c != '-' {
			return 0, 0, 0, false
		}
	}
	day, _, ok = b.parseDigits(p, 2)
	return year, month, day, ok
}

var fractionCorrection = [...]int{100000, 10000, 1000, 100, 10}

// parseHHMMSSFF is parse_hh_mm_ss_ff over b[start:end]. rv is the C return
// value: negative for an error, 1 when the text does not end after the
// parsed part, 0 otherwise.
func parseHHMMSSFF(b isoBuffer, start, end int) (comps [4]int, rv int) {
	p := start
	hasSeparator := true
	for i := 0; i < 3; i++ {
		v, np, ok := b.parseDigits(p, 2)
		if !ok {
			return comps, -3
		}
		comps[i] = v
		p = np
		c := b.at(p)
		p++
		if i == 0 {
			hasSeparator = c == ':'
		}
		if p >= end {
			if c != 0 {
				return comps, 1
			}
			return comps, 0
		} else if hasSeparator && c == ':' {
			continue
		} else if c == '.' || c == ',' {
			break
		} else if !hasSeparator {
			p--
		} else {
			return comps, -4
		}
	}
	remains := end - p
	toParse := remains
	if remains >= 6 {
		toParse = 6
	}
	v, np, ok := b.parseDigits(p, toParse)
	if !ok {
		return comps, -3
	}
	p = np
	if toParse < 6 && toParse > 0 {
		v *= fractionCorrection[toParse-1]
	}
	comps[3] = v
	for isDigit(b.at(p)) {
		p++
	}
	if b.at(p) != 0 {
		return comps, 1
	}
	return comps, 0
}

// parseTime is parse_isoformat_time: rv 0 = no zone, 1 = zone (offset in
// microseconds), negative = error.
func parseTime(b isoBuffer, start int) (comps [4]int, offsetMicros int64, rv int) {
	end := len(b)
	tzPos := start
	for {
		c := b.at(tzPos)
		if c == 'Z' || c == '+' || c == '-' {
			break
		}
		tzPos++
		if tzPos >= end {
			break
		}
	}
	comps, rv = parseHHMMSSFF(b, start, tzPos)
	if rv < 0 {
		return comps, 0, rv
	}
	if tzPos == end {
		if rv == 1 {
			return comps, 0, -5
		}
		return comps, 0, 0
	}
	if b.at(tzPos) == 'Z' {
		if b.at(tzPos+1) != 0 {
			return comps, 0, -5
		}
		return comps, 0, 1
	}
	sign := int64(1)
	if b.at(tzPos) == '-' {
		sign = -1
	}
	tz, rv := parseHHMMSSFF(b, tzPos+1, end)
	offset := sign*(int64(tz[0])*3600+int64(tz[1])*60+int64(tz[2]))*1_000_000 + sign*int64(tz[3])
	if rv != 0 {
		return comps, offset, -5
	}
	return comps, offset, 1
}

// FromISOFormat is datetime.fromisoformat(value) followed by the
// `tzinfo or UTC` treatment of task_status.parse_time: an aware time.Time in
// the parsed offset (see pyTimezone), a naive one taken as UTC. ok is false
// where Python raises ValueError.
func FromISOFormat(value string) (time.Time, bool) {
	if len(value) < 7 {
		return time.Time{}, false
	}
	b := isoBuffer(value)
	separator := findSeparator(b)
	if separator < 0 || separator > len(b) {
		return time.Time{}, false
	}
	year, month, day, ok := parseDate(b, separator)
	if !ok {
		return time.Time{}, false
	}
	var comps [4]int
	var offsetMicros int64
	if len(b) > separator {
		_, size := utf8.DecodeRuneInString(value[separator:])
		start := separator + size
		var rv int
		comps, offsetMicros, rv = parseTime(b, start)
		if rv < 0 {
			return time.Time{}, false
		}
	}
	hour, minute, second, micro := comps[0], comps[1], comps[2], comps[3]
	if year < 1 || month < 1 || month > 12 || day < 1 || day > daysInMonth(year, month) ||
		hour > 23 || minute > 59 || second > 59 {
		return time.Time{}, false
	}
	const day24 = int64(24 * 3600 * 1_000_000)
	if offsetMicros <= -day24 || offsetMicros >= day24 {
		return time.Time{}, false
	}
	utc := time.Date(year, time.Month(month), day, hour, minute, second, micro*1000, time.UTC)
	return utc.Add(-time.Duration(offsetMicros) * time.Microsecond).In(pyTimezone(offsetMicros)), true
}

// pyTimezone is the tzinfo fromisoformat attaches (datetime.timezone): UTC
// for a zero offset, else a fixed zone named like timezone.tzname()
// ("UTC+05:30", "UTC-05:30:15.250000"). time.Location offsets are whole
// seconds; a sub-second part only shows in the name (the instant is exact).
func pyTimezone(offsetMicros int64) *time.Location {
	if offsetMicros == 0 {
		return time.UTC
	}
	sign := '+'
	rest := offsetMicros
	if rest < 0 {
		sign, rest = '-', -rest
	}
	hours, minutes := rest/3_600_000_000, rest/60_000_000%60
	seconds, micros := rest/1_000_000%60, rest%1_000_000
	name := fmt.Sprintf("UTC%c%02d:%02d", sign, hours, minutes)
	if seconds != 0 || micros != 0 {
		name += fmt.Sprintf(":%02d", seconds)
		if micros != 0 {
			name += fmt.Sprintf(".%06d", micros)
		}
	}
	return time.FixedZone(name, int(offsetMicros/1_000_000))
}
