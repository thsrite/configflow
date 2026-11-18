package ipv6

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"configflow/internal/lifecycle"
	"configflow/internal/routers"
)

type observed map[string][]routers.Row

func ip(value string) string {
	a, e := netip.ParseAddr(value)
	if e != nil {
		return ""
	}
	return a.String()
}
func number(v any) int {
	switch n := v.(type) {
	case string:
		i, _ := strconv.Atoi(n)
		return i
	case float64:
		return int(n)
	case json.Number:
		i, _ := strconv.Atoi(n.String())
		return i
	case int:
		return n
	}
	return 0
}
func port(v any) int {
	n := number(v)
	if n < 1 || n > 65535 {
		return 0
	}
	return n
}
func rowText(v any) string { s, _ := v.(string); return s }
func endpoint(row routers.Row, dir string) (string, int) {
	address := rowText(row[dir+"-address"])
	if host := ip(address); host != "" {
		return host, port(row[dir+"-port"])
	}
	host, p, e := net.SplitHostPort(address)
	if e == nil {
		return ip(host), port(p)
	}
	return "", 0
}
func rowMatches(row routers.Row, source string, sport int, destination string, dport int) bool {
	if strings.ToLower(rowText(row["protocol"])) != "tcp" {
		return false
	}
	src, sp := endpoint(row, "src")
	dst, dp := endpoint(row, "dst")
	return source != "" && destination != "" && sport > 0 && dport > 0 && src == source && sp == sport && dst == destination && dp == dport && hasPackets(row)
}

// Packet counters are non-negative uint64 values. Invalid/missing-type
// counters are unknown, and overflow can never manufacture positive evidence.
func hasPackets(row routers.Row) bool {
	positive := false
	for _, key := range []string{"orig-packets", "orig-fasttrack-packets"} {
		v, present := row[key]
		if !present {
			continue
		}
		var n uint64
		var err error
		switch value := v.(type) {
		case string:
			n, err = strconv.ParseUint(strings.TrimSpace(value), 10, 64)
		case json.Number:
			n, err = strconv.ParseUint(value.String(), 10, 64)
		case float64:
			if value < 0 || value >= 18446744073709551616 || math.IsNaN(value) {
				return false
			}
			n = uint64(value)
		case int:
			if value < 0 {
				return false
			}
			n = uint64(value)
		default:
			return false
		}
		if err != nil {
			return false
		}
		positive = positive || n > 0
	}
	return positive
}

func targetURL(testURL string) (string, int) {
	u, e := url.Parse(testURL)
	if e != nil {
		return "", 0
	}
	p := port(u.Port())
	if p == 0 {
		if u.Scheme == "https" {
			p = 443
		} else {
			p = 80
		}
	}
	return strings.TrimRight(strings.ToLower(u.Hostname()), "."), p
}
func documents(b json.RawMessage) []Document {
	var rows []json.RawMessage
	if json.Unmarshal(b, &rows) != nil {
		return nil
	}
	out := []Document{}
	for _, b := range rows {
		if d := object(b); d != nil {
			out = append(out, d)
		}
	}
	return out
}
func rawPort(b json.RawMessage) int { var v any; _ = json.Unmarshal(b, &v); return port(v) }

// observe retains at most 256 fresh tuples per router. An incomplete baseline
// is unknown and never used as proof that an observed connection is new.
func observe(ctx context.Context, config, source Document, testURL string, ready chan<- struct{}) observed {
	defer close(ready)
	_, p := targetURL(testURL)
	deadline := lifecycle.ClockFrom(ctx).Now().Add(30 * time.Second)
	type scan struct {
		id       string
		driver   *routers.RouterOS
		baseline [2]map[string]bool
		rows     map[string]routers.Row
	}
	scans := []*scan{}
	defer func() {
		for _, s := range scans {
			s.driver.Close()
		}
	}()
	rs := linked(config, text(source["id"]))
	if len(rs) > 4 {
		rs = rs[:4]
	}
	for _, r := range rs {
		if ctx.Err() != nil {
			break
		}
		if r.Type != "routeros" {
			continue
		}
		s := &scan{id: r.ID, driver: routers.NewRouterOS(r.Endpoint, r.Username, r.Password, r.VerifyTLS), rows: map[string]routers.Row{}}
		scans = append(scans, s)
		for family := range 2 {
			rows, complete, e := s.driver.ReadConnections(ctx, routers.ConnectionQuery{IPv6: family == 1, DestinationPort: p}, 2*time.Second)
			if e != nil || !complete {
				continue
			}
			s.baseline[family] = map[string]bool{}
			for _, row := range rows {
				s.baseline[family][rowText(row[".id"])] = true
			}
		}
	}
	// Signal only after all baselines; the caller starts the fresh request next.
	select {
	case ready <- struct{}{}:
	case <-ctx.Done():
	}
	for ctx.Err() == nil && lifecycle.ClockFrom(ctx).Now().Before(deadline) {
		for _, s := range scans {
			for family := range 2 {
				baseline := s.baseline[family]
				if baseline == nil {
					continue
				}
				rows, _, e := s.driver.ReadConnections(ctx, routers.ConnectionQuery{IPv6: family == 1, DestinationPort: p}, 2*time.Second)
				if e != nil {
					continue
				}
				for _, row := range rows {
					id := rowText(row[".id"])
					if id != "" && !baseline[id] && len(s.rows) < 256 {
						s.rows[strconv.Itoa(family)+"\n"+id] = row
					}
				}
			}
		}
		if !wait(ctx, 400*time.Millisecond) {
			break
		}
	}
	out := observed{}
	for _, s := range scans {
		for _, row := range s.rows {
			out[s.id] = append(out[s.id], row)
		}
	}
	return out
}
func wait(ctx context.Context, d time.Duration) bool {
	timer := lifecycle.ClockFrom(ctx).NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C():
		return true
	}
}
func tracked(ctx context.Context, d *routers.RouterOS, request Document, p int, rows []routers.Row) bool {
	source, destination, sport := ip(text(request["source_ip"])), ip(text(request["address"])), rawPort(request["source_port"])
	if source == "" || destination == "" || sport == 0 {
		return false
	}
	for _, row := range rows {
		if rowMatches(row, source, sport, destination, p) {
			return true
		}
	}
	a, _ := netip.ParseAddr(source)
	for _, legacy := range []bool{false, true} {
		rows, _, e := d.ReadConnections(ctx, routers.ConnectionQuery{IPv6: a.Is6(), SourceAddress: source, SourcePort: sport, LegacySource: legacy}, 0)
		if e != nil {
			continue
		}
		for _, row := range rows {
			if rowMatches(row, source, sport, destination, p) {
				return true
			}
		}
	}
	return false
}
func evidenceCheck(id, label string, confirmed bool, success, missing string, details ...string) Check {
	status, message := "unknown", missing
	if confirmed {
		status, message = "ok", success
	}
	return check(id, label, status, message, details...)
}
func evaluate(ctx context.Context, config, source, report, trace Document, testURL string, seen observed) []Check {
	expected, p := targetURL(testURL)
	host := strings.TrimRight(strings.ToLower(text(report["target_host"])), ".")
	var all []json.RawMessage
	_ = json.Unmarshal(report["requests"], &all)
	requests := []Document{}
	for _, b := range all {
		r := object(b)
		if text(r["status"]) == "ok" && len(requests) < 8 {
			requests = append(requests, r)
		}
	}
	complete := len(requests) > 0 && len(requests) == len(all)
	proxied, transport := complete, complete
	matches := documents(trace["matches"])
	for _, r := range requests {
		found, ipv6 := false, false
		for _, m := range matches {
			if ip(text(r["source_ip"])) == "" || ip(text(m["source_ip"])) != ip(text(r["source_ip"])) || rawPort(r["source_port"]) == 0 || rawPort(m["source_port"]) != rawPort(r["source_port"]) {
				continue
			}
			if strings.TrimRight(strings.ToLower(text(m["host"])), ".") != host || host != expected || strings.TrimRight(strings.ToLower(text(trace["target_host"])), ".") != expected || text(m["network"]) != "tcp" || rawPort(m["destination_port"]) != p || text(m["proxy_name"]) == "" || text(m["proxy_type"]) == "" {
				continue
			}
			var chains []string
			if json.Unmarshal(m["chains"], &chains) != nil || len(chains) == 0 {
				continue
			}
			blocked := false
			for _, chain := range chains {
				switch strings.ToUpper(chain) {
				case "DIRECT", "REJECT", "REJECT-DROP", "PASS":
					blocked = true
				}
			}
			if blocked {
				continue
			}
			found = true
			ipv6 = ipv6 || yes(m["ipv6_transport_verified"])
		}
		proxied = proxied && found
		transport = transport && found && ipv6
	}
	checks := []Check{evidenceCheck("client_mihomo_path", msgClientMihomo, proxied, msgClientMihomoOK, msgClientMihomoMissing), evidenceCheck("client_ipv6_transport", msgClientTransport, transport, msgClientTransportOK, msgClientTransportMissing)}
	confirmed := map[int]bool{}
	details := []string{}
	rs := linked(config, text(source["id"]))
	if len(rs) > 8 {
		rs = rs[:8]
	}
	for _, r := range rs {
		if ctx.Err() != nil {
			break
		}
		if r.Type != "routeros" {
			continue
		}
		d := routers.NewRouterOS(r.Endpoint, r.Username, r.Password, r.VerifyTLS)
		for i, request := range requests {
			if tracked(ctx, d, request, p, seen[r.ID]) {
				confirmed[i] = true
				name := r.Name
				if name == "" {
					name = r.ID
				}
				details = append(details, fmt.Sprintf(msgClientROSDetail, name, text(request["family"])))
			}
		}
		d.Close()
	}
	return append(checks, evidenceCheck("client_ros_path", msgClientROS, complete && len(confirmed) == len(requests), msgClientROSOK, msgClientROSMissing, details...))
}
