package agents

// werkzeug.http.parse_date for the download route's conditional headers:
// email.utils.parsedate_to_datetime (CPython 3.11 email._parseaddr
// ._parsedate_tz) with TypeError/ValueError → None and naive → UTC. Werkzeug
// does not catch OverflowError, which reaches download_agent_binary's
// `except Exception` (500 with str(e)).

import (
	"fmt"
	"math"
	"math/big"
	"strings"
	"time"

	"configflow/internal/pystr"
)

var (
	pyMonthNames = []string{"jan", "feb", "mar", "apr", "may", "jun", "jul", "aug", "sep", "oct", "nov", "dec",
		"january", "february", "march", "april", "may", "june", "july", "august", "september", "october", "november", "december"}
	pyDayNames  = []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"}
	pyTimezones = map[string]int64{"UT": 0, "UTC": 0, "GMT": 0, "Z": 0, "AST": -400, "ADT": -300, "EST": -500, "EDT": -400,
		"CST": -600, "CDT": -500, "MST": -700, "MDT": -600, "PST": -800, "PDT": -700}
)

func pyIndex(list []string, value string) int {
	for i, item := range list {
		if item == value {
			return i
		}
	}
	return -1
}

// pyFields is str.split() (runs of white space, no empty fields).
func pyFields(s string) []string { return strings.FieldsFunc(s, pystr.IsSpace) }

func lastChar(s string) string {
	runes := []rune(s)
	return string(runes[len(runes)-1:])
}

func dropLast(s string) string {
	runes := []rune(s)
	return string(runes[:len(runes)-1])
}

// pyParsedateTZ is email._parseaddr._parsedate_tz: year, month, day, hour,
// minute, second and the offset in seconds (nil: none or "-0000").
func pyParsedateTZ(data string) (fields [6]*big.Int, offset *big.Int, ok bool) {
	parts := pyFields(data)
	if len(parts) == 0 {
		return fields, nil, false
	}
	if strings.HasSuffix(parts[0], ",") || pyIndex(pyDayNames, pystr.Lower(parts[0])) >= 0 {
		parts = parts[1:]
	} else if i := strings.LastIndex(parts[0], ","); i >= 0 {
		parts[0] = parts[0][i+1:]
	}
	if len(parts) == 3 {
		if stuff := strings.Split(parts[0], "-"); len(stuff) == 3 {
			parts = append(stuff, parts[1:]...)
		}
	}
	if len(parts) == 4 {
		s := parts[3]
		i := strings.Index(s, "+")
		if i == -1 {
			i = strings.Index(s, "-")
		}
		if i > 0 {
			parts = append(parts[:3:3], s[:i], s[i:])
		} else {
			parts = append(parts, "")
		}
	}
	if len(parts) < 5 {
		return fields, nil, false
	}
	dd, mm, yy, tm, tz := parts[0], parts[1], parts[2], parts[3], parts[4]
	if dd == "" || mm == "" || yy == "" {
		return fields, nil, false
	}
	mm = pystr.Lower(mm)
	if pyIndex(pyMonthNames, mm) < 0 {
		dd, mm = mm, pystr.Lower(dd)
		if pyIndex(pyMonthNames, mm) < 0 {
			return fields, nil, false
		}
	}
	month := int64(pyIndex(pyMonthNames, mm) + 1)
	if month > 12 {
		month -= 12
	}
	if lastChar(dd) == "," {
		dd = dropLast(dd)
	}
	if i := strings.Index(yy, ":"); i > 0 {
		yy, tm = tm, yy
	}
	if lastChar(yy) == "," {
		yy = dropLast(yy)
		if yy == "" {
			return fields, nil, false
		}
	}
	if first := []rune(yy)[0]; !pystr.IsDigit(string(first)) {
		yy, tz = tz, yy
	}
	if lastChar(tm) == "," {
		tm = dropLast(tm)
	}
	var thh, tmm, tss string
	clock := strings.Split(tm, ":")
	switch {
	case len(clock) == 2:
		thh, tmm, tss = clock[0], clock[1], "0"
	case len(clock) == 3:
		thh, tmm, tss = clock[0], clock[1], clock[2]
	case len(clock) == 1 && strings.Contains(clock[0], "."):
		clock = strings.Split(clock[0], ".")
		switch len(clock) {
		case 2:
			thh, tmm, tss = clock[0], clock[1], "0"
		case 3:
			thh, tmm, tss = clock[0], clock[1], clock[2]
		default:
			return fields, nil, false
		}
	default:
		return fields, nil, false
	}
	var values [5]*big.Int
	for i, text := range []string{yy, dd, thh, tmm, tss} {
		n, err := pystr.Int(text, 10)
		if err != nil {
			return fields, nil, false
		}
		values[i] = n
	}
	year := values[0]
	if year.Cmp(big.NewInt(100)) < 0 {
		if year.Cmp(big.NewInt(68)) > 0 {
			year.Add(year, big.NewInt(1900))
		} else {
			year.Add(year, big.NewInt(2000))
		}
	}
	tz = pystr.Upper(tz)
	if value, found := pyTimezones[tz]; found {
		offset = big.NewInt(value)
	} else if n, err := pystr.Int(tz, 10); err == nil {
		offset = n
		if n.Sign() == 0 && strings.HasPrefix(tz, "-") {
			offset = nil
		}
	}
	if offset != nil && offset.Sign() != 0 {
		sign := int64(1)
		magnitude := new(big.Int).Set(offset)
		if offset.Sign() < 0 {
			sign = -1
			magnitude.Neg(magnitude)
		}
		hours, minutes := new(big.Int).DivMod(magnitude, big.NewInt(100), new(big.Int))
		seconds := new(big.Int).Mul(hours, big.NewInt(3600))
		seconds.Add(seconds, minutes.Mul(minutes, big.NewInt(60)))
		offset = seconds.Mul(seconds, big.NewInt(sign))
	}
	return [6]*big.Int{year, big.NewInt(month), values[1], values[2], values[3], values[4]}, offset, true
}

// pyCInt is getargs "i" for datetime(): OverflowError texts for values
// outside C long / C int.
func pyCInt(n *big.Int) (int64, string) {
	if !n.IsInt64() {
		return 0, "Python int too large to convert to C long"
	}
	v := n.Int64()
	if v > math.MaxInt32 {
		return 0, "signed integer is greater than maximum"
	}
	if v < math.MinInt32 {
		return 0, "signed integer is less than minimum"
	}
	return v, ""
}

// werkzeugParseDate is parse_date(value) (value None when !present) as a
// UTC instant in seconds; problem is the text of an OverflowError.
func werkzeugParseDate(value string, present bool) (instant int64, ok bool, problem string) {
	if !present {
		return 0, false, ""
	}
	fields, offset, parsed := pyParsedateTZ(value)
	if !parsed {
		return 0, false, ""
	}
	// tzinfo=datetime.timezone(datetime.timedelta(seconds=tz)) is evaluated
	// before datetime() converts its positional arguments.
	var offsetSeconds int64
	if offset != nil {
		days := new(big.Int).Div(offset, big.NewInt(86400)) // floor division
		if !days.IsInt64() || days.Int64() > math.MaxInt32 || days.Int64() < math.MinInt32 {
			return 0, false, "Python int too large to convert to C int"
		}
		if d := days.Int64(); d > 999999999 || d < -999999999 {
			return 0, false, fmt.Sprintf("days=%d; must have magnitude <= 999999999", d)
		}
		offsetSeconds = offset.Int64()
		if offsetSeconds <= -86400 || offsetSeconds >= 86400 {
			return 0, false, "" // timezone(): ValueError
		}
	}
	var v [6]int64
	for i, n := range fields {
		value, problem := pyCInt(n)
		if problem != "" {
			return 0, false, problem
		}
		v[i] = value
	}
	year, month, day, hour, minute, second := v[0], v[1], v[2], v[3], v[4], v[5]
	if year < 1 || year > 9999 || month < 1 || month > 12 || hour < 0 || hour > 23 || minute < 0 || minute > 59 || second < 0 || second > 59 {
		return 0, false, ""
	}
	daysIn := time.Date(int(year), time.Month(month)+1, 0, 0, 0, 0, 0, time.UTC).Day()
	if day < 1 || day > int64(daysIn) {
		return 0, false, ""
	}
	t := time.Date(int(year), time.Month(month), int(day), int(hour), int(minute), int(second), 0, time.UTC)
	return t.Unix() - offsetSeconds, true, ""
}
