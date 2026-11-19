package monitor

import (
	"configflow/internal/weburl"
	"strconv"
	"strings"
)

func displayAddress(value string) string {
	s := pySlice(value, 2048)
	if hasControl(s) {
		return ""
	}
	if strings.Contains(s, "://") {
		// The label drops credentials: they are cut before parsing, so
		// any user name net/url would refuse does not matter.
		scheme, rest, _ := strings.Cut(s, "://")
		authority, tail := rest, ""
		if i := strings.IndexAny(rest, "/?#"); i >= 0 {
			authority, tail = rest[:i], rest[i:]
		}
		if i := strings.LastIndex(authority, "@"); i >= 0 {
			authority = authority[i+1:]
		}
		parsed, err := weburl.Parse(scheme + "://" + authority + tail)
		if err != nil {
			return "上游服务器"
		}
		host := weburl.Hostname(parsed)
		if host == "" {
			return "上游服务器"
		}
		if strings.Contains(host, ":") {
			host = "[" + host + "]"
		}
		port, hasPort, err := weburl.Port(parsed)
		if err != nil {
			return "上游服务器"
		}
		netloc := host
		if hasPort {
			netloc += ":" + strconv.Itoa(port)
		}
		return parsed.Scheme + "://" + netloc + weburl.RawPath(parsed)
	}
	s, _, _ = strings.Cut(s, "?")
	if i := strings.LastIndex(s, "@"); i >= 0 {
		s = s[i+1:]
	}
	return s
}
