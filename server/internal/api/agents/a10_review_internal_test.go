package agents

// A10 adversarial review: download_agent_binary's conditional handling.
// Werkzeug's make_conditional runs inside the view's `except Exception`, so
// its RequestedRangeNotSatisfiable and parse_date's OverflowError become the
// 500 JSON answer. References come from Werkzeug 3.1 / CPython 3.11
// (testdata/review_a10_conditional.py): the fixed tables below were produced
// by it. TestReviewConditionalMatchesWerkzeug replays fingerprinted seeded
// references; explicit record/check mode reruns Python on the test server.

import (
	"encoding/json"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"configflow/internal/testgolden"
)

const reviewLastModified = "Sun, 01 Nov 2026 12:30:15 GMT"
const reviewETag = `"1793536215.1234567-70000-12345"`

func reviewDateResult(value string) string {
	instant, ok, problem := werkzeugParseDate(value, true)
	switch {
	case problem != "":
		return "raise:OverflowError:" + problem
	case !ok:
		return "none"
	}
	return "ok:" + strconv.FormatInt(instant, 10)
}

type reviewConditionalCase struct {
	Method       string            `json:"method"`
	Size         int64             `json:"size"`
	ETag         string            `json:"etag"`
	LastModified string            `json:"last_modified"`
	Headers      map[string]string `json:"headers"` // Latin-1 decoded
}

func (c reviewConditionalCase) result() string {
	req, _ := http.NewRequest(c.Method, "http://x/x", nil)
	for name, value := range c.Headers {
		raw := make([]byte, 0, len(value))
		for _, r := range value {
			raw = append(raw, byte(r))
		}
		req.Header[http.CanonicalHeaderKey(name)] = []string{string(raw)}
	}
	problem := werkzeugConditionalError(req, c.Size, c.ETag, c.LastModified)
	switch {
	case problem == "":
		return ""
	case problem == werkzeugRangeNotSatisfiable:
		return "raise:RequestedRangeNotSatisfiable:" + problem
	}
	return "raise:OverflowError:" + problem
}

// Fixed references (werkzeug.http.parse_date under CPython 3.11.17).
func TestReviewParseDateFollowsEmailUtils(t *testing.T) {
	policy := nativeDownloadPolicy(t)
	for _, c := range []struct{ value, want string }{
		{"Sun, 01 Nov 2026 12:30:15 GMT", "ok:1793536215"},
		{"01 Nov 2026 12:30:15 +0000", "ok:1793536215"},     // no weekday: http.ParseTime refuses
		{"Sunday, 01-Nov-26 12:30:15 GMT", "ok:1793536215"}, // RFC 850
		{"Sun Nov  1 12:30:15 2026", "ok:1793536215"},       // asctime
		{"1 nov 2026 13:30:15 +0100", "ok:1793536215"},
		{"01 Nov 2026 12:30 -0000", "ok:1793536200"},
		{"01 Nov 2026 12.30.15 EST", "ok:1793554215"},
		{"Sun, 01 Nov 2026 12:30:60 GMT", "none"},
		{"31 Feb 2026 00:00:00 GMT", "none"},
		{"01 Nov 2026 12:30:15 +2400", "none"},
		{"", "none"},
		{"01 Nov 99999999999 12:30:15 GMT", "raise:OverflowError:signed integer is greater than maximum"},
		{"01 Nov 99999999999999999999 12:30:15 GMT", "raise:OverflowError:Python int too large to convert to C long"},
		{"-99999999999 Nov 2026 12:30:15 GMT", "raise:OverflowError:signed integer is less than minimum"},
		{"01 Nov 2026 12:30:15 +99999999999999", "raise:OverflowError:Python int too large to convert to C int"},
		{"01 Nov 2026 12:30:15 +3000000000000", "raise:OverflowError:days=1250000000; must have magnitude <= 999999999"},
		{"01 Nov 2026 12:30:15 -3000000000000", "raise:OverflowError:days=-1250000000; must have magnitude <= 999999999"},
		{"01 Nov 2026 12:30:15 +9999999999999999999999", "raise:OverflowError:Python int too large to convert to C int"},
		{"01 Nov 2026 12:30:15 +99999999", "none"},
	} {
		if got := reviewDateResult(c.value); got != policy("date", c.value, c.want) {
			t.Errorf("parse_date(%q): go %q, python %q", c.value, got, c.want)
		}
	}
}

// Fixed references (Response.make_conditional, Werkzeug 3.1.8).
func TestReviewConditionalFollowsWerkzeug(t *testing.T) {
	policy := nativeDownloadPolicy(t)
	base := reviewConditionalCase{Method: "GET", Size: 70000, ETag: reviewETag, LastModified: reviewLastModified}
	with := func(headers ...string) reviewConditionalCase {
		c := base
		c.Headers = map[string]string{}
		for i := 0; i+1 < len(headers); i += 2 {
			c.Headers[headers[i]] = headers[i+1]
		}
		return c
	}
	refused := "raise:RequestedRangeNotSatisfiable:" + werkzeugRangeNotSatisfiable
	for _, c := range []struct {
		name string
		c    reviewConditionalCase
		want string
	}{
		{"end beyond int64", with("Range", "bytes=0-99999999999999999999"), ""},
		{"end at int64 max", with("Range", "bytes=0-9223372036854775807"), ""},
		{"begin beyond int64", with("Range", "bytes=99999999999999999999-"), refused},
		{"latin-1 nbsp stripped", with("Range", "bytes=0-9 "), ""},
		{"latin-1 NEL stripped", with("Range", "\u0085bytes=0-9"), ""},
		{"if-range date without weekday, unsatisfiable", with("Range", "bytes=80000-", "If-Range", "01 Nov 2026 12:30:15 +0000"), refused},
		{"if-range overflow", with("Range", "bytes=0-1", "If-Range", "01 Nov 99999999999 12:30:15 GMT"), "raise:OverflowError:signed integer is greater than maximum"},
		{"if-modified-since overflow", with("If-Modified-Since", "01 Nov 99999999999 12:30:15 GMT"), "raise:OverflowError:signed integer is greater than maximum"},
		{"if-modified-since overflow with 206", with("Range", "bytes=0-1", "If-Modified-Since", "01 Nov 99999999999 12:30:15 GMT"), ""},
		{"if-range without range parses if-modified-since", with("If-Range", "x", "If-Modified-Since", "01 Nov 99999999999 12:30:15 GMT"), "raise:OverflowError:signed integer is greater than maximum"},
		{"etag list with latin-1 space", with("Range", "bytes=80000-", "If-Range", "x", "If-None-Match", "\"a\" , "+reviewETag), ""},
	} {
		if got := c.c.result(); got != policy("request", c.c, c.want) {
			t.Errorf("%s: go %q, python %q", c.name, got, c.want)
		}
	}
}

func TestReviewConditionalMatchesWerkzeug(t *testing.T) {
	policy := nativeDownloadPolicy(t)
	t.Parallel()
	n := 3000
	if v, err := strconv.Atoi(os.Getenv("A10_CONDITIONAL_FUZZ")); err == nil {
		n = v
	}
	rng := rand.New(rand.NewSource(20261116))
	pick := func(items ...string) string { return items[rng.Intn(len(items))] }
	spaces := []string{" ", "  ", "\t", " ", "\u0085", "\u001c", "\u000b", ""}
	space := func() string { return spaces[rng.Intn(len(spaces))] }
	number := func() string {
		return pick("0", "1", "9", "01", "12", "26", "68", "69", "99", "100", "2026", "9999", "10000", "0000", "-5", "+5", "1_0",
			"2147483647", "2147483648", "-2147483649", "9223372036854775807", "9223372036854775808", "99999999999999999999",
			"²", "1e3", "", "60", "59", "23", "24", "31", "29", "30")
	}
	date := func() string {
		var parts []string
		if rng.Intn(2) == 0 {
			parts = append(parts, pick("Sun,", "sun", "Mon,", "Sunday,", "x,", ",", "Thu"))
		}
		day, month, year := number(), pick("Nov", "nov", "NOVEMBER", "Feb", "Sept", "dec", "x", "11"), number()
		clock := number() + pick(":", ".") + number() + pick("", ":"+number(), "."+number(), ":"+number()+":"+number())
		zone := pick("GMT", "UTC", "UT", "Z", "EST", "PDT", "+0000", "-0000", "+0100", "-0530", "+2359", "+2400", "-9999",
			"+99999999", "+99999999999999", "+3000000000000", "-3000000000000", "+2400000000000", "-2400000000000", "+2399999999999", "+9999999999999999999999", "gmt", "", "XYZ", "+00", "-0")
		switch rng.Intn(5) {
		case 0:
			parts = append(parts, month, day, clock, year) // asctime order
		case 1:
			parts = append(parts, day+"-"+month+"-"+year, clock, zone) // RFC 850
		case 2:
			parts = append(parts, day, month, year, clock+zone)
		default:
			parts = append(parts, day+pick("", ","), month, year+pick("", ","), clock+pick("", ","), zone)
		}
		var b strings.Builder
		for i, p := range parts {
			if i > 0 {
				s := space()
				if s == "" {
					s = " "
				}
				b.WriteString(s)
			}
			b.WriteString(p)
		}
		return b.String()
	}
	var dates []string
	for i := 0; i < n; i++ {
		dates = append(dates, date())
	}
	rangeValue := func() string {
		var items []string
		for i := 0; i < 1+rng.Intn(3); i++ {
			switch rng.Intn(5) {
			case 0:
				items = append(items, space()+"-"+number()+space())
			case 1:
				items = append(items, space()+number()+space()+"-"+space())
			default:
				items = append(items, space()+number()+space()+"-"+space()+number()+space())
			}
		}
		return space() + pick("bytes", "BYTES", "Bytes", "items", "") + space() + pick("=", "", "==") + strings.Join(items, pick(",", ", ", ";"))
	}
	etagValue := func() string {
		return pick(reviewETag, "W/"+reviewETag, strings.Trim(reviewETag, `"`), `"other"`, "*", `"a", `+reviewETag, `"a"`+space()+","+space()+reviewETag, "", `"`, `W/"`)
	}
	var requests []reviewConditionalCase
	for i := 0; i < n; i++ {
		c := reviewConditionalCase{Method: pick("GET", "GET", "HEAD", "POST"), Size: []int64{70000, 70000, 1, 0}[rng.Intn(4)],
			ETag: reviewETag, LastModified: reviewLastModified, Headers: map[string]string{}}
		if rng.Intn(5) > 0 {
			c.Headers["Range"] = rangeValue()
		}
		if rng.Intn(2) == 0 {
			if rng.Intn(2) == 0 {
				c.Headers["If-Range"] = pick(reviewLastModified, "Sun, 01 Nov 2026 12:30:14 GMT", "01 Nov 2026 12:30:15 +0000") + pick("", " ")
			} else if rng.Intn(2) == 0 {
				c.Headers["If-Range"] = date()
			} else {
				c.Headers["If-Range"] = etagValue()
			}
		}
		if rng.Intn(3) == 0 {
			c.Headers["If-Modified-Since"] = pick(reviewLastModified, date(), "Sun, 01 Nov 2026 12:30:16 GMT", "")
		}
		if rng.Intn(4) == 0 {
			c.Headers["If-None-Match"] = etagValue()
		}
		if rng.Intn(6) == 0 {
			c.Headers["If-Match"] = etagValue()
		}
		requests = append(requests, c)
	}
	payload, _ := json.Marshal(map[string]any{"dates": dates, "requests": requests})
	file := filepath.Join(t.TempDir(), "cases.json")
	if err := os.WriteFile(file, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	out := testgolden.New(t, testgolden.Spec{
		Sources: testgolden.Sources(t, "backend", "server/internal/api/agents/testdata/review_a10_conditional.py"),
		Input:   payload,
		Run: func() ([]byte, error) {
			cmd := exec.Command(testgolden.Python(), "testdata/review_a10_conditional.py", file)
			return cmd.Output()
		},
	}).Output(t)
	var want struct {
		Dates    []string `json:"dates"`
		Requests []string `json:"requests"`
	}
	if err := json.Unmarshal(out, &want); err != nil {
		t.Fatal(err)
	}
	differ, raised := 0, 0
	for i, value := range dates {
		if got := reviewDateResult(value); got != policy("date", value, want.Dates[i]) {
			differ++
			if differ <= 20 {
				t.Errorf("parse_date(%q): go %q, python %q", value, got, want.Dates[i])
			}
		}
	}
	for i, c := range requests {
		if want.Requests[i] != "" {
			raised++
		}
		if got := c.result(); got != policy("request", c, want.Requests[i]) {
			differ++
			if differ <= 40 {
				t.Errorf("request %+v: go %q, python %q", c, got, want.Requests[i])
			}
		}
	}
	t.Logf("%d dates, %d conditional requests (%d raise in Werkzeug): %d differ", len(dates), len(requests), raised, differ)
}
