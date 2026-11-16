package agents

// download_agent_binary calls flask.send_file inside `try: … except
// Exception`, so the RequestedRangeNotSatisfiable that Werkzeug 3.1's
// make_conditional raises for an unusable Range header becomes the view's
// 500 JSON answer instead of a 416 page. werkzeugRangeRefused decides when
// that happens (Response._process_range_request with
// _is_range_request_processable, parse_range_header, Range.range_for_length
// and the If-Range branch of is_resource_modified).

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// werkzeugRangeNotSatisfiable is str(RequestedRangeNotSatisfiable()).
const werkzeugRangeNotSatisfiable = "416 Requested Range Not Satisfiable: The server cannot provide the requested range."

var (
	werkzeugPlainInt = regexp.MustCompile(`\A-?[0-9]+\z`)
	werkzeugETag     = regexp.MustCompile(`\A([Ww]/)?(?:"(.*?)"|(.*?))(?:\s*,\s*|$)`)
)

type werkzeugRange struct {
	begin int64
	end   *int64
}

// parseRangeHeader is werkzeug.http.parse_range_header (units, ranges).
func parseRangeHeader(value string) (string, []werkzeugRange, bool) {
	if value == "" || !strings.Contains(value, "=") {
		return "", nil, false
	}
	units, spec, _ := strings.Cut(value, "=")
	units = strings.ToLower(strings.TrimSpace(units))
	plainInt := func(text string) (int64, bool) {
		if !werkzeugPlainInt.MatchString(text) {
			return 0, false
		}
		n, err := strconv.ParseInt(text, 10, 64)
		return n, err == nil
	}
	var ranges []werkzeugRange
	lastEnd := int64(0)
	for _, item := range strings.Split(spec, ",") {
		item = strings.TrimSpace(item)
		if !strings.Contains(item, "-") {
			return "", nil, false
		}
		if strings.HasPrefix(item, "-") {
			if lastEnd < 0 {
				return "", nil, false
			}
			begin, ok := plainInt(item)
			if !ok {
				return "", nil, false
			}
			ranges = append(ranges, werkzeugRange{begin, nil})
			lastEnd = -1
			continue
		}
		beginText, endText, _ := strings.Cut(item, "-")
		begin, ok := plainInt(strings.TrimSpace(beginText))
		if !ok || begin < lastEnd || lastEnd < 0 {
			return "", nil, false
		}
		endText = strings.TrimSpace(endText)
		if endText == "" {
			ranges = append(ranges, werkzeugRange{begin, nil})
			lastEnd = -1
			continue
		}
		end, ok := plainInt(endText)
		if !ok {
			return "", nil, false
		}
		end++
		if begin >= end {
			return "", nil, false
		}
		ranges = append(ranges, werkzeugRange{begin, &end})
		lastEnd = end
	}
	return units, ranges, true
}

// rangeSatisfiable is Range.range_for_length(length) is not None.
func rangeSatisfiable(units string, ranges []werkzeugRange, length int64) bool {
	if units != "bytes" || len(ranges) != 1 {
		return false
	}
	start := ranges[0].begin
	var end int64
	if ranges[0].end == nil {
		end = length
		if start < 0 {
			start += length
		}
	} else {
		end = *ranges[0].end
	}
	return start < end && 0 <= start && start < length
}

// unquoteETag is werkzeug.http.unquote_etag (tag only).
func unquoteETag(value string) string {
	value = strings.TrimSpace(value)
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

func parseHTTPDate(value string) (time.Time, bool) {
	if value == "" {
		return time.Time{}, false
	}
	t, err := http.ParseTime(value)
	return t, err == nil
}

// resourceModifiedForRange is is_resource_modified(environ, etag, None,
// last_modified, ignore_if_range=False) for a request with a Range header.
func resourceModifiedForRange(req *http.Request, etag, lastModified string) bool {
	unmodified := false
	modified, hasModified := parseHTTPDate(lastModified)
	ifRange := req.Header.Get("If-Range")
	var ifRangeDate time.Time
	hasIfRangeDate, hasIfRangeETag := false, false
	ifRangeETag := ""
	if ifRange != "" {
		if d, ok := parseHTTPDate(ifRange); ok {
			ifRangeDate, hasIfRangeDate = d, true
		} else {
			ifRangeETag, hasIfRangeETag = unquoteETag(ifRange), true
		}
	}
	since, hasSince := ifRangeDate, hasIfRangeDate
	if !hasIfRangeDate {
		since, hasSince = parseHTTPDate(req.Header.Get("If-Modified-Since"))
	}
	if hasSince && hasModified && !modified.After(since) {
		unmodified = true
	}
	if etag != "" {
		tag := unquoteETag(etag)
		if hasIfRangeETag {
			unmodified = parseETags(ifRangeETag).contains(tag)
		} else {
			if noneMatch := parseETags(req.Header.Get("If-None-Match")); !noneMatch.empty() {
				unmodified = noneMatch.containsWeak(tag)
			}
			if match := parseETags(req.Header.Get("If-Match")); !match.empty() {
				unmodified = !match.isStrong(tag)
			}
		}
	}
	return !unmodified
}

// werkzeugRangeRefused reports whether make_conditional(accept_ranges=True,
// complete_length=size) raises RequestedRangeNotSatisfiable.
func werkzeugRangeRefused(req *http.Request, size int64, etag, lastModified string) bool {
	if (req.Method != "GET" && req.Method != "HEAD") || size == 0 {
		return false
	}
	values, present := req.Header["Range"]
	if !present {
		return false
	}
	if _, ok := req.Header["If-Range"]; ok && resourceModifiedForRange(req, etag, lastModified) {
		return false
	}
	units, ranges, ok := parseRangeHeader(strings.Join(values, ","))
	return !ok || !rangeSatisfiable(units, ranges, size)
}
