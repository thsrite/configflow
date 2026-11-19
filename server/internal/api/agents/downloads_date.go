package agents

import "net/http"

// werkzeugParseDate now accepts the three HTTP date formats provided by the
// native parser. RFC 9110 invalid dates are ignored as conditional evidence.
func werkzeugParseDate(value string, present bool) (instant int64, ok bool, problem string) {
	if !present {
		return 0, false, ""
	}
	parsed, err := http.ParseTime(value)
	if err != nil {
		return 0, false, ""
	}
	return parsed.Unix(), true, ""
}
