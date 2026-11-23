// Package monitor owns typed Agent monitoring, DNS cache/rule controls,
// Mihomo panel operations and durable dashboard/ranking statistics.
// Device requests use the registered Agent's authenticated agents.Client;
// endpoint credentials and service identity come from original stored members.
// Deployment and HA coordination remain with their respective owners.
package monitor

import (
	"fmt"
	"math"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Versions, limits and timings of oxidns_monitor.py.
const (
	MinAgentVersion          = "1.7.0-go"
	MinClientsAgentVersion   = "1.7.1-go"
	MinDashboardAgentVersion = "1.8.14-go"
	ClientSampleLimit        = 10000
	RankingWindowMS          = 24 * 3600 * 1000
	MaxResponseBytes         = 5 * 1024 * 1024
	MaxInteger               = 1<<53 - 1
	BlockRankingMatcherLimit = 32
)

// RankingReuseSeconds and RankingBackoffSeconds are RANKING_REUSE_SECONDS
// and RANKING_BACKOFF_SECONDS (variables so tests can change them like the
// Python fixtures).
var (
	RankingReuseSeconds   = 60.0
	RankingBackoffSeconds = 180.0
)

// rankingBackoffMinutes is RANKING_BACKOFF_SECONDS // 60 (read at call
// time, like the f-string in _rankings_unavailable).
func rankingBackoffMinutes() int64 { return int64(math.Floor(RankingBackoffSeconds / 60)) }

// RcodeFilterText is RCODE_FILTER_TEXT.
var RcodeFilterText = map[string]string{
	"NOERROR":   "No Error",
	"FORMERR":   "Form Error",
	"SERVFAIL":  "Server Failure",
	"NXDOMAIN":  "Non-Existent Domain",
	"NOTIMP":    "Not Implemented",
	"REFUSED":   "Query Refused",
	"YXDOMAIN":  "Name should not exist",
	"YXRRSET":   "RR Set should not exist",
	"NXRRSET":   "RR Set does not exist",
	"NOTAUTH":   "Not authorized",
	"NOTZONE":   "Name not in zone",
	"BADVERS":   "Bad OPT Version",
	"BADSIG":    "TSIG Signature Failure",
	"BADKEY":    "Key not recognized",
	"BADTIME":   "Signature out of time window",
	"BADMODE":   "Bad TKEY mode",
	"BADNAME":   "Duplicate key name",
	"BADALG":    "Algorithm not supported",
	"BADTRUNC":  "Bad truncation",
	"BADCOOKIE": "Bad server cookie",
}

// Error is MonitorError: Code, HTTP Status and the user Message.
type Error struct {
	Code    string
	Status  int
	Message string
}

func (e *Error) Error() string { return e.Message }

// NewError is MonitorError(code, status, message): status 0 is 502, an
// empty message the code's text (INVALID_RESPONSE's for unknown codes).
func NewError(code string, status int, message string) *Error {
	if status == 0 {
		status = 502
	}
	if message == "" {
		var ok bool
		if message, ok = Messages[code]; !ok {
			message = Messages["INVALID_RESPONSE"]
		}
	}
	return &Error{Code: code, Status: status, Message: message}
}

func upgradeRequired(minVersion string) *Error {
	feature := msgDNSMonitor
	switch minVersion {
	case MinClientsAgentVersion:
		feature = msgClientsFeature
	case MinDashboardAgentVersion:
		feature = msgDashboardFeature
	}
	return NewError("UPGRADE_REQUIRED", 409, fmt.Sprintf(msgPanelUpgrade, minVersion, feature))
}

var hostnameRE = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9.-]{0,251}[A-Za-z0-9])?$`)

// query is an ordered str → str dict (request params).
type query [][2]string

func (q query) get(key string) (string, bool) {
	for _, pair := range q {
		if pair[0] == key {
			return pair[1], true
		}
	}
	return "", false
}

func (q query) set(key, value string) query {
	for i, pair := range q {
		if pair[0] == key {
			q[i][1] = value
			return q
		}
	}
	return append(q, [2]string{key, value})
}

// encode is requests' params encoding (urlencode: quote_plus of each key
// and value).
func (q query) encode() string {
	parts := make([]string, len(q))
	for i, pair := range q {
		parts[i] = url.QueryEscape(pair[0]) + "=" + url.QueryEscape(pair[1])
	}
	return strings.Join(parts, "&")
}

func unreachable() *Error { return NewError("AGENT_UNREACHABLE", 0, "") }

var RankingRanges = []string{"round", "24h", "7d", "30d"}

func uniqueKeys(args [][2]string) []string {
	var keys []string
	seen := map[string]bool{}
	for _, pair := range args {
		if !seen[pair[0]] {
			seen[pair[0]] = true
			keys = append(keys, pair[0])
		}
	}
	return keys
}

func getList(args [][2]string, key string) []string {
	var out []string
	for _, pair := range args {
		if pair[0] == key {
			out = append(out, pair[1])
		}
	}
	return out
}

func invalidQuery() *Error { return NewError("INVALID_QUERY", 400, "") }

func dashboardParams(args [][2]string) (query, error) {
	var result query
	for _, key := range uniqueKeys(args) {
		values := getList(args, key)
		if len(values) != 1 || key == "recorder" && !validRecorderTag(values[0]) ||
			key == "range" && !slices.Contains(RankingRanges, values[0]) || key != "recorder" && key != "range" {
			return nil, invalidQuery()
		}
		result = result.set(key, values[0])
	}
	return result, nil
}

func hasControl(value string) bool {
	for _, r := range value {
		if r < 32 || (r >= 127 && r <= 159) {
			return true
		}
	}
	return false
}

func floorDiv(a, b int64) int64 {
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}

func floorMod(a, b int64) int64 { return a - floorDiv(a, b)*b }

func isoUTC(t time.Time) string {
	text := t.Format("2006-01-02T15:04:05")
	if micro := t.Nanosecond() / 1000; micro != 0 {
		text += fmt.Sprintf(".%06d", micro)
	}
	return text + "+00:00"
}

// fromTimestampMS converts an Agent's collected_at_ms through float seconds,
// rounding to the nearest microsecond with ties to even. For current times
// this equals time.UnixMilli; for far-future values the Agent may report (up
// to year 9999) the float step shows in the microseconds, and the dashboard
// keeps the collected_at text it has always returned for them.
func fromTimestampMS(ms int64) time.Time {
	seconds := float64(ms) / 1000
	whole, frac := math.Modf(seconds)
	micro := math.RoundToEven(frac * 1e6)
	if micro >= 1e6 {
		whole++
		micro -= 1e6
	} else if micro < 0 {
		whole--
		micro += 1e6
	}
	return time.Unix(int64(whole), int64(micro)*1000).UTC()
}

func QueryParams(args [][2]string, detail bool) (query, error) {
	allowed := map[string]bool{"recorder": true}
	if !detail {
		for _, key := range []string{"limit", "cursor", "qname", "qtype", "client_ip", "rcode", "status", "since_ms",
			"until_ms", "matcher_tag"} {
			allowed[key] = true
		}
	}
	var result query
	for _, key := range uniqueKeys(args) {
		values := getList(args, key)
		if !allowed[key] || len(values) != 1 {
			return nil, invalidQuery()
		}
		value := values[0]
		if len(value) > 1024 || strings.ContainsFunc(value, func(r rune) bool { return r < 32 }) {
			return nil, invalidQuery()
		}
		value = trimText(value)
		if value == "" {
			continue
		}
		switch key {
		case "limit", "since_ms", "until_ms":
			if !digits16RE.MatchString(value) {
				return nil, invalidQuery()
			}
			n, _ := strconv.ParseInt(value, 10, 64)
			if n > MaxInteger || key == "limit" && (n < 1 || n > 100) {
				return nil, invalidQuery()
			}
			value = strconv.FormatInt(n, 10)
		case "cursor":
			if !cursorRE.MatchString(value) {
				return nil, invalidQuery()
			}
			for _, part := range strings.Split(value, ":") {
				if n, _ := strconv.ParseInt(part, 10, 64); n > MaxInteger {
					return nil, invalidQuery()
				}
			}
		case "status":
			if !slices.Contains([]string{"all", "error", "has_response", "no_response"}, value) {
				return nil, invalidQuery()
			}
		case "recorder", "matcher_tag":
			if runeLen(value) > 256 || strings.ContainsAny(value, "/?#\\") {
				return nil, invalidQuery()
			}
		case "client_ip":
			if strings.Contains(value, "%") {
				return nil, invalidQuery()
			}
			canonical, _, ok := parseIP(value)
			if !ok {
				return nil, invalidQuery()
			}
			value = canonical
		case "qtype":
			if !qtypeRE.MatchString(value) {
				return nil, invalidQuery()
			}
			value = strings.ToUpper(value)
		case "rcode":
			if !rcodeRE.MatchString(value) {
				return nil, invalidQuery()
			}
			if mapped, ok := RcodeFilterText[strings.ToUpper(value)]; ok {
				value = mapped
			}
		}
		result = result.set(key, value)
	}
	since, okSince := result.get("since_ms")
	until, okUntil := result.get("until_ms")
	if okSince && okUntil {
		s, _ := strconv.ParseInt(since, 10, 64)
		u, _ := strconv.ParseInt(until, 10, 64)
		if s > u {
			return nil, invalidQuery()
		}
	}
	if !detail {
		if _, ok := result.get("limit"); !ok {
			result = result.set("limit", "50")
		}
	}
	return result, nil
}

func ClientParams(args [][2]string) (query, error) {
	var result query
	for _, key := range uniqueKeys(args) {
		values := getList(args, key)
		if (key != "recorder" && key != "since_ms" && key != "until_ms" && key != "search") || len(values) != 1 {
			return nil, invalidQuery()
		}
		value := values[0]
		if hasControl(value) {
			return nil, invalidQuery()
		}
		switch key {
		case "search":
			if len(value) > 64 {
				return nil, invalidQuery()
			}
		case "recorder":
			if !validRecorderTag(value) {
				return nil, invalidQuery()
			}
		default:
			if !digits16RE.MatchString(value) {
				return nil, invalidQuery()
			}
			if n, _ := strconv.ParseInt(value, 10, 64); n > MaxInteger {
				return nil, invalidQuery()
			}
		}
		value = trimText(value)
		if value != "" {
			result = result.set(key, value)
		}
	}
	since, okSince := result.get("since_ms")
	until, okUntil := result.get("until_ms")
	if okSince && okUntil {
		s, _ := strconv.ParseInt(since, 10, 64)
		u, _ := strconv.ParseInt(until, 10, 64)
		if s > u {
			return nil, invalidQuery()
		}
	}
	return result, nil
}

var (
	instanceRE = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,128}$`)
	reasonRE   = regexp.MustCompile(`^[a-z_]{1,64}$`)
	recorderRE = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9_-]*[A-Za-z0-9])?(?:\.[A-Za-z0-9](?:[A-Za-z0-9_-]*[A-Za-z0-9])?)*$`)
	cursorRE   = regexp.MustCompile(`^[0-9]{1,16}:[1-9][0-9]{0,15}$`)
	digits16RE = regexp.MustCompile(`^[0-9]{1,16}$`)
	qtypeRE    = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)
	rcodeRE    = regexp.MustCompile(`^[A-Za-z0-9 _-]{1,64}$`)
	recordIDRE = regexp.MustCompile(`^[1-9][0-9]{0,15}$`)
)

var nowUTC = func() time.Time { return time.Now().UTC() }
var resolutionSources = map[string]bool{"local": true, "remote": true, "fallback": true, "pinned": true, "cache": true,
	"hosts": true, "blocked": true, "rejected": true, "unknown": true, "unrecorded": true, "error": true,
	"no_response": true, "ipv4_preferred": true}

var clockStart = time.Now()
var monotonic = func() float64 { return time.Since(clockStart).Seconds() }

// ResetRankingsCache empties the transient ranking cache.
func ResetRankingsCache() {
	nativeRankingsMu.Lock()
	nativeRankingsCache = map[rankingCacheIdentity]*cachedRanking{}
	nativeRankingsMu.Unlock()
}
func validRecorderTag(tag string) bool {
	return runeLen(tag) > 0 && runeLen(tag) <= 64 && recorderRE.MatchString(tag)
}
