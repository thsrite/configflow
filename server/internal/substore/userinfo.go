package substore

import (
	"math"
	"math/big"
	"strconv"
	"unicode/utf8"

	"encoding/json"
	"fmt"
	"strings"
	"unicode"
)

// userinfoFields is subscription_health._USERINFO_FIELDS.
var userinfoFields = map[string]bool{"upload": true, "download": true, "total": true, "expire": true}

// TrafficError identifies a non-finite traffic counter.
type TrafficError struct{ Field string }

// Error describes the affected protocol field.
func (e *TrafficError) Error() string { return fmt.Sprintf(msgTrafficNonfinite, e.Field) }

// ReadUserinfo parses the source header using native numeric conversion.
func ReadUserinfo(header string) (*Traffic, error) {
	if header == "" {
		return nil, nil
	}
	info := &Traffic{}
	found := false
	for pos := 0; pos < len(header); {
		key, value, end, ok := matchUserinfo(header, pos)
		if !ok {
			_, size := utf8.DecodeRuneInString(header[pos:])
			pos += size
			continue
		}
		pos = end
		key = strings.ToLower(key)
		if !userinfoFields[key] {
			continue
		}
		f, err := strconv.ParseFloat(value, 64)
		if err != nil && !math.IsInf(f, 0) {
			continue // ValueError
		}
		if math.IsInf(f, 0) {
			return nil, &TrafficError{Field: key}
		}
		n, _ := new(big.Float).SetFloat64(f).Int(nil)
		number := json.Number(n.String())
		switch key {
		case "upload":
			info.Upload = &number
		case "download":
			info.Download = &number
		case "total":
			info.Total = &number
		case "expire":
			info.Expire = &number
		}
		found = true
	}
	if !found {
		return nil, nil
	}
	return info, nil
}

func isValueChar(c byte) bool {
	return c >= '0' && c <= '9' || c == '.' || c == 'e' || c == 'E' || c == '+'
}

// matchUserinfo tries (\w+)\s*=\s*([0-9.eE+]+) at pos.
func matchUserinfo(s string, pos int) (key, value string, end int, ok bool) {
	i := pos
	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		if !(unicode.IsLetter(r) || unicode.IsNumber(r) || r == '_') {
			break
		}
		i += size
	}
	if i == pos {
		return "", "", 0, false
	}
	key = s[pos:i]
	skip := func() {
		for i < len(s) {
			r, size := utf8.DecodeRuneInString(s[i:])
			if !unicode.IsSpace(r) {
				break
			}
			i += size
		}
	}
	skip()
	if i >= len(s) || s[i] != '=' {
		return "", "", 0, false
	}
	i++
	skip()
	start := i
	for i < len(s) && isValueChar(s[i]) {
		i++
	}
	if i == start {
		return "", "", 0, false
	}
	return key, s[start:i], i, true
}
