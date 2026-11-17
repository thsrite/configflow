package agents

// download_agent_binary calls flask.send_file inside `try: … except
// Exception`, so the RequestedRangeNotSatisfiable that Werkzeug 3.1's
// make_conditional raises for an unusable Range header becomes the view's
// 500 JSON answer instead of a 416 page. L1 D7 deliberately corrects that
// range error to 416; parse_date OverflowError remains a 500.
// werkzeugConditionalError decides when that happens
// (Response.make_conditional, _process_range_request with
// _is_range_request_processable, parse_range_header, Range.range_for_length,
// is_resource_modified, parse_date = email.utils.parsedate_to_datetime).

import (
	"math/big"
	"net/http"
	"regexp"
	"strings"

	"configflow/internal/pystr"
)

// werkzeugRangeNotSatisfiable is str(RequestedRangeNotSatisfiable()).
const werkzeugRangeNotSatisfiable = "416 Requested Range Not Satisfiable: The server cannot provide the requested range."

// pyLatinSpace is Python's \s (str.isspace) over the Latin-1 range: WSGI
// decodes header values as Latin-1, so U+001C–U+001F, U+0085 and U+00A0
// are white space for str.strip/split and re's \s there.
const pyLatinSpace = `[\t\n\x0b\f\r \x1c-\x1f\x{85}\x{a0}]`

var (
	werkzeugPlainInt = regexp.MustCompile(`\A-?[0-9]+\z`)
	werkzeugETag     = regexp.MustCompile(`\A([Ww]/)?(?:"(.*?)"|(.*?))(?:` + pyLatinSpace + `*,` + pyLatinSpace + `*|$)`)
)

type werkzeugRange struct {
	begin *big.Int
	end   *big.Int // nil: open-ended
}

// parseRangeHeader is werkzeug.http.parse_range_header (units, ranges) for
// a Latin-1 decoded value; bounds are Python ints (no overflow).
func parseRangeHeader(value string) (string, []werkzeugRange, bool) {
	if value == "" || !strings.Contains(value, "=") {
		return "", nil, false
	}
	units, spec, _ := strings.Cut(value, "=")
	units = pystr.Lower(pystr.Strip(units))
	plainInt := func(text string) (*big.Int, bool) {
		text = pystr.Strip(text)
		if !werkzeugPlainInt.MatchString(text) {
			return nil, false
		}
		n, ok := new(big.Int).SetString(text, 10)
		return n, ok
	}
	var ranges []werkzeugRange
	lastEnd := big.NewInt(0)
	minusOne := big.NewInt(-1)
	for _, item := range strings.Split(spec, ",") {
		item = pystr.Strip(item)
		if !strings.Contains(item, "-") {
			return "", nil, false
		}
		if strings.HasPrefix(item, "-") {
			if lastEnd.Sign() < 0 {
				return "", nil, false
			}
			begin, ok := plainInt(item)
			if !ok {
				return "", nil, false
			}
			ranges = append(ranges, werkzeugRange{begin, nil})
			lastEnd = minusOne
			continue
		}
		beginText, endText, _ := strings.Cut(item, "-")
		begin, ok := plainInt(beginText)
		if !ok || begin.Cmp(lastEnd) < 0 || lastEnd.Sign() < 0 {
			return "", nil, false
		}
		endText = pystr.Strip(endText)
		if endText == "" {
			ranges = append(ranges, werkzeugRange{begin, nil})
			lastEnd = minusOne
			continue
		}
		end, ok := plainInt(endText)
		if !ok {
			return "", nil, false
		}
		end.Add(end, big.NewInt(1))
		if begin.Cmp(end) >= 0 {
			return "", nil, false
		}
		ranges = append(ranges, werkzeugRange{begin, end})
		lastEnd = end
	}
	return units, ranges, true
}

// rangeSatisfiable is Range.range_for_length(length) is not None.
func rangeSatisfiable(units string, ranges []werkzeugRange, length int64) bool {
	if units != "bytes" || len(ranges) != 1 {
		return false
	}
	size := big.NewInt(length)
	start := new(big.Int).Set(ranges[0].begin)
	end := ranges[0].end
	if end == nil {
		end = size
		if start.Sign() < 0 {
			start.Add(start, size)
		}
	}
	return start.Cmp(end) < 0 && start.Sign() >= 0 && start.Cmp(size) < 0
}

// unquoteETag is werkzeug.http.unquote_etag (tag only).
func unquoteETag(value string) string {
	value = pystr.Strip(value)
	if strings.HasPrefix(value, "W/") || strings.HasPrefix(value, "w/") {
		value = value[2:]
	}
	if len(value) >= 1 && value[0] == '"' && value[len(value)-1] == '"' {
		if len(value) == 1 {
			return ""
		}
		value = value[1 : len(value)-1]
	}
	return value
}

type werkzeugETags struct {
	star         bool
	strong, weak []string
}

func (e werkzeugETags) empty() bool { return !e.star && len(e.strong) == 0 && len(e.weak) == 0 }

func (e werkzeugETags) contains(tag string) bool {
	if e.star {
		return true
	}
	for _, t := range e.strong {
		if t == tag {
			return true
		}
	}
	return false
}

func (e werkzeugETags) containsWeak(tag string) bool {
	for _, t := range e.weak {
		if t == tag {
			return true
		}
	}
	return e.contains(tag)
}

func (e werkzeugETags) isStrong(tag string) bool {
	for _, t := range e.strong {
		if t == tag {
			return true
		}
	}
	return false
}

// parseETags is werkzeug.http.parse_etags.
func parseETags(value string) werkzeugETags {
	var tags werkzeugETags
	for pos := 0; pos < len(value); {
		m := werkzeugETag.FindStringSubmatchIndex(value[pos:])
		if m == nil {
			break
		}
		weak := m[2] >= 0
		var raw string
		if m[4] >= 0 {
			raw = value[pos+m[4] : pos+m[5]]
		} else {
			raw = value[pos+m[6] : pos+m[7]]
			if raw == "*" {
				return werkzeugETags{star: true}
			}
		}
		if weak {
			tags.weak = append(tags.weak, raw)
		} else {
			tags.strong = append(tags.strong, raw)
		}
		if m[1] == 0 {
			break // an empty match at the end
		}
		pos += m[1]
	}
	return tags
}

// resourceModified is werkzeug's is_resource_modified(environ, etag, None,
// last_modified, ignore_if_range) for header values already Latin-1
// decoded. problem is the text of an exception parse_date lets through
// (OverflowError), which the view turns into its 500 answer.
func resourceModified(req *http.Request, etag, lastModified string, ignoreIfRange bool) (modified bool, problem string) {
	header := func(name string) (string, bool) {
		values, present := req.Header[name]
		if !present {
			return "", false
		}
		return latin1(strings.Join(values, ",")), true
	}
	unmodified := false
	lastInstant, hasLast, problem := werkzeugParseDate(lastModified, true)
	if problem != "" {
		return false, problem
	}
	var ifRangeInstant int64
	hasIfRangeDate, hasIfRangeETag := false, false
	ifRangeETag := ""
	_, rangePresent := req.Header["Range"]
	if !ignoreIfRange && rangePresent {
		// parse_if_range_header
		if value, _ := header("If-Range"); value != "" {
			instant, isDate, problem := werkzeugParseDate(value, true)
			if problem != "" {
				return false, problem
			}
			if isDate {
				ifRangeInstant, hasIfRangeDate = instant, true
			} else {
				ifRangeETag, hasIfRangeETag = unquoteETag(value), true
			}
		}
	}
	since, hasSince := ifRangeInstant, hasIfRangeDate
	if !hasIfRangeDate {
		value, present := header("If-Modified-Since")
		instant, ok, problem := werkzeugParseDate(value, present)
		if problem != "" {
			return false, problem
		}
		since, hasSince = instant, ok
	}
	if hasSince && hasLast && lastInstant <= since {
		unmodified = true
	}
	if etag != "" {
		tag := unquoteETag(etag)
		if hasIfRangeETag {
			unmodified = parseETags(ifRangeETag).contains(tag)
		} else {
			noneMatchValue, _ := header("If-None-Match")
			if noneMatch := parseETags(noneMatchValue); !noneMatch.empty() {
				unmodified = noneMatch.containsWeak(tag)
			}
			matchValue, _ := header("If-Match")
			if match := parseETags(matchValue); !match.empty() {
				unmodified = !match.isStrong(tag)
			}
		}
	}
	return !unmodified, ""
}

// werkzeugConditionalError is the str() of the exception Werkzeug's
// make_conditional(environ, accept_ranges=True, complete_length=size)
// raises inside flask.send_file, or "" when it raises nothing:
// RequestedRangeNotSatisfiable for a Range that cannot be parsed or
// satisfied, or an OverflowError of parse_date for an absurd If-Range /
// If-Modified-Since date.
func werkzeugConditionalError(req *http.Request, size int64, etag, lastModified string) string {
	if req.Method != "GET" && req.Method != "HEAD" {
		return ""
	}
	is206 := false
	// _process_range_request: `complete_length and
	// _is_range_request_processable(environ)`.
	if size != 0 {
		processable := true
		if _, present := req.Header["If-Range"]; present {
			modified, problem := resourceModified(req, etag, lastModified, false)
			if problem != "" {
				return problem
			}
			processable = !modified
		}
		if values, present := req.Header["Range"]; processable && present {
			units, ranges, ok := parseRangeHeader(latin1(strings.Join(values, ",")))
			if !ok || !rangeSatisfiable(units, ranges, size) {
				return werkzeugRangeNotSatisfiable
			}
			is206 = true
		}
	}
	if !is206 {
		if _, problem := resourceModified(req, etag, lastModified, true); problem != "" {
			return problem
		}
	}
	return ""
}
