package substore

import (
	"math"
	"math/big"
	"strconv"
	"unicode/utf8"

	"configflow/internal/jsonx"
)

// userinfoFields is subscription_health._USERINFO_FIELDS.
var userinfoFields = map[string]bool{"upload": true, "download": true, "total": true, "expire": true}

// ParseUserinfo is subscription_health.parse_userinfo (C15's module; the
// client needs it): `upload=1; download=2; total=3; expire=4` → the fields
// present, nil (None) when there are none. Matches are found like
// re.findall(r'(\w+)\s*=\s*([0-9.eE+]+)') with Unicode \w and \s; values are
// int(float(v)). A value float() reads as infinity is OverflowError, as in
// Python.
func ParseUserinfo(header string) (*jsonx.Object, error) {
	if header == "" {
		return nil, nil
	}
	info := jsonx.NewObject()
	for pos := 0; pos < len(header); {
		key, value, end, ok := matchUserinfo(header, pos)
		if !ok {
			_, size := utf8.DecodeRuneInString(header[pos:])
			pos += size
			continue
		}
		pos = end
		key = pyLower(key)
		if !userinfoFields[key] {
			continue
		}
		f, err := strconv.ParseFloat(value, 64)
		if err != nil && !math.IsInf(f, 0) {
			continue // ValueError
		}
		if math.IsInf(f, 0) {
			return nil, pyErr("OverflowError", "cannot convert float infinity to integer")
		}
		n, _ := new(big.Float).SetFloat64(f).Int(nil)
		if n.IsInt64() {
			info.Set(key, jsonx.Int(n.Int64()))
		} else {
			info.Set(key, jsonx.BigInt(n))
		}
	}
	if info.Len() == 0 {
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
		if !isPyWord(r) {
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
			if !pyIsSpace(r) {
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
