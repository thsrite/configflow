package clientlist

import (
	"configflow/internal/jsondecode"
	"configflow/internal/routers/device"
	"configflow/internal/routers/ros"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/netip"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"configflow/internal/lifecycle"
	"configflow/internal/routerdoc"
)

var clientDurationParts = regexp.MustCompile(`([0-9]+)(w|d|h|ms|m|s)`)
var clientDurationClock = regexp.MustCompile(`^[0-9]+:[0-9]{2}:[0-9]{2}$`)

// ClientDuration parses RouterOS lease ages; nil means unknown, including never.
func ClientDuration(text string) *int64 {
	text = strings.TrimSpace(text)
	var n int64
	if clientDurationClock.MatchString(text) {
		for _, p := range strings.Split(text, ":") {
			v, e := strconv.ParseInt(p, 10, 64)
			if e != nil {
				return nil
			}
			if n > (math.MaxInt64-v)/60 {
				return nil
			}
			n = n*60 + v
		}
		return &n
	}
	if text == "" || clientDurationParts.ReplaceAllString(text, "") != "" {
		return nil
	}
	for _, p := range clientDurationParts.FindAllStringSubmatch(text, -1) {
		v, e := strconv.ParseInt(p[1], 10, 64)
		if e != nil {
			return nil
		}
		unit := map[string]int64{"w": 604800, "d": 86400, "h": 3600, "m": 60, "s": 1, "ms": 0}[p[2]]
		if unit > 0 && v > (math.MaxInt64-n)/unit {
			return nil
		}
		n += v * unit
	}
	return &n
}
func clientEnabled(row ros.Row) bool { return !ros.Flag(row["disabled"]) && !ros.Flag(row["invalid"]) }
func clientLAN(servers, addresses []ros.Row, wan map[string]bool) map[string]bool {
	lan := map[string]bool{}
	for _, s := range servers {
		if name := s["interface"]; name != "" && !ros.Flag(s["disabled"]) {
			lan[name] = true
		}
	}
	if len(lan) > 0 {
		return lan
	}
	for _, s := range addresses {
		name := s["interface"]
		a, err := netip.ParseAddr(strings.Split(s["address"], "/")[0])
		if name != "" && clientEnabled(s) && !ros.Flag(s["dynamic"]) && !wan[name] && err == nil && a.Is4() && a.IsPrivate() {
			lan[name] = true
		}
	}
	return lan
}

type clientARP struct{ status, mac, iface, name string }
type clientLease struct {
	status, mac, iface, name string
	last, period             *int64
	dynamic                  bool
}
type clientProbe struct{ ip, iface string }

func classifyClients(arp, leases, servers []ros.Row, lan map[string]bool) (map[string]*routerdoc.Sighting, []clientProbe) {
	entries := map[string]clientARP{}
	byIP := map[string]clientLease{}
	periods := map[string]*int64{}
	ifaces := map[string]string{}
	for _, row := range servers {
		name := row["name"]
		ifaces[name] = row["interface"]
		if n := ClientDuration(row["lease-time"]); n != nil && *n != 0 {
			periods[name] = n
		}
	}
	for _, row := range arp {
		ip := routerdoc.IPv4(row["address"])
		iface := row["interface"]
		if ip == "" || !lan[iface] || !clientEnabled(row) {
			continue
		}
		status := strings.ToLower(row["status"])
		old, exists := entries[ip]
		if !exists || clientARPOnline(status) && !clientARPOnline(old.status) {
			entries[ip] = clientARP{status, routerdoc.NormalizeMAC(row["mac-address"]), iface, strings.TrimSpace(row["comment"])}
		}
	}
	for _, row := range leases {
		ip := routerdoc.IPv4(device.FirstNonEmpty(row["active-address"], row["address"]))
		if ip == "" || ros.Flag(row["disabled"]) {
			continue
		}
		name := row["server"]
		period := ClientDuration(row["lease-time"])
		if period == nil || *period == 0 {
			period = periods[name]
			if period == nil {
				for _, v := range periods {
					if period == nil {
						period = v
					} else if *period != *v {
						period = nil
						break
					}
				}
			}
		}
		l := clientLease{strings.ToLower(row["status"]), routerdoc.NormalizeMAC(device.FirstNonEmpty(row["active-mac-address"], row["mac-address"])), ifaces[name], device.FirstNonEmpty(strings.TrimSpace(row["comment"]), strings.TrimSpace(row["host-name"])), ClientDuration(row["last-seen"]), period, ros.Flag(row["dynamic"])}
		old, exists := byIP[ip]
		if !exists || l.last != nil && (old.last == nil || *l.last < *old.last) {
			byIP[ip] = l
		}
	}
	keys := map[string]bool{}
	for k := range entries {
		keys[k] = true
	}
	for k := range byIP {
		keys[k] = true
	}
	sightings := map[string]*routerdoc.Sighting{}
	candidates := []clientProbe{}
	for ip := range keys {
		e, hasARP := entries[ip]
		l, hasLease := byIP[ip]
		s := routerdoc.NewSighting(ip)
		s.MAC = device.FirstNonEmpty(e.mac, l.mac)
		s.Hostname = device.FirstNonEmpty(l.name, e.name)
		s.LastSeen = l.last
		if hasARP {
			s.AddSource("arp")
		}
		if hasLease {
			s.AddSource("lease")
		}
		fresh := hasLease && l.status == "bound" && l.last != nil && *l.last <= 1200
		missed := hasLease && l.last != nil && l.period != nil && *l.period != 0 && (l.dynamic || l.status == "bound") && *l.last > max(int64(1200), *l.period/2+300)
		switch {
		case clientARPOnline(e.status):
			s.Presence = "online"
			s.Basis = "arp"
		case fresh:
			s.Presence = "online"
			s.Basis = "lease"
		case clientARPGone(e.status):
			s.Presence = "offline"
			s.Basis = "arp"
		case missed:
			s.Presence = "offline"
			s.Basis = "lease"
		case hasARP:
			s.Presence = "recent"
			s.Basis = "stale"
			candidates = append(candidates, clientProbe{ip, e.iface})
		case l.status == "bound":
			s.Presence = "recent"
			s.Basis = "stale"
			iface := ""
			if lan[l.iface] {
				iface = l.iface
			}
			candidates = append(candidates, clientProbe{ip, iface})
		default:
			s.Presence = "offline"
			s.Basis = "lease"
		}
		sightings[ip] = s
	}
	return sightings, candidates
}
func clientARPOnline(s string) bool { return s == "reachable" || s == "delay" || s == "probe" }
func clientARPGone(s string) bool   { return s == "failed" || s == "incomplete" }
func clientBudget(deadline time.Time) time.Duration {
	left := time.Until(deadline)
	if left < time.Second {
		return 0
	}
	return min(4*time.Second, left)
}
func (d rosDevice) clientList(ctx context.Context, path string, timeout time.Duration) ([]ros.Row, bool, error) {
	v, err := d.Get(ctx, path, timeout)
	if err != nil {
		return nil, false, err
	}
	var items []json.RawMessage
	if ros.ReplyKind(v) != '[' || jsondecode.Decode(v, &items) != nil {
		return nil, false, device.Error(fmt.Sprintf(device.MessageROSListFormat, path))
	}
	rows := make([]ros.Row, 0, len(items))
	complete := true
	for _, item := range items {
		r := ros.AsRow(item)
		if r == nil {
			// A corrupt row has no trustworthy identity. Keep healthy peers but
			// do not use this partial scan as evidence that a host is absent.
			complete = false
			continue
		}
		rows = append(rows, r)
	}
	return rows, complete, nil
}
func (d rosDevice) confirmClients(ctx context.Context, r *routerdoc.Reading, candidates []clientProbe, lan map[string]bool, opts ReadOptions) []string {
	if len(candidates) == 0 {
		return []string{}
	}
	slices.SortFunc(candidates, func(a, b clientProbe) int {
		if opts.Priority[a.ip] != opts.Priority[b.ip] {
			if opts.Priority[a.ip] {
				return -1
			}
			return 1
		}
		aa, _ := netip.ParseAddr(a.ip)
		bb, _ := netip.ParseAddr(b.ip)
		return aa.Compare(bb)
	})
	count := min(32, len(candidates))
	var denied atomic.Bool
	type answer struct {
		ip, kind string
		answered *bool
	}
	results := make(chan answer, count)
	jobs := make(chan clientProbe, count)
	for _, c := range candidates[:count] {
		jobs <- c
	}
	close(jobs)
	for range min(6, count) {
		run := func(work context.Context) {
			// Spawn detaches request cancellation. Restore this read's bounded deadline.
			work, cancel := context.WithDeadline(work, opts.Deadline)
			defer cancel()
			for c := range jobs {
				a := answer{ip: c.ip}
				if !denied.Load() && clientBudget(opts.Deadline) > 0 && work.Err() == nil {
					if c.iface != "" {
						v, err := d.Ping(work, c.ip, c.iface, clientBudget(opts.Deadline))
						if err == nil {
							a.answered = &v
							a.kind = "arp"
						} else if device.RouterErrorKind(err) == device.KindPingNotPermitted {
							denied.Store(true)
						}
					}
					if a.answered == nil && !denied.Load() && clientBudget(opts.Deadline) > 0 && work.Err() == nil {
						v, err := d.Ping(work, c.ip, "", clientBudget(opts.Deadline))
						if err == nil {
							a.answered = &v
							a.kind = "icmp"
						} else if device.RouterErrorKind(err) == device.KindPingNotPermitted {
							denied.Store(true)
						}
					}
				}
				results <- a
			}
		}
		if err := lifecycle.Spawn(ctx, "router-client-probe", run); err != nil {
			run(ctx)
		}
	}
	silent := []answer{}
	unsettled := 0
	for range count {
		a := <-results
		if a.answered == nil {
			unsettled++
		} else if *a.answered {
			r.Sightings[a.ip].Presence = "online"
			r.Sightings[a.ip].Basis = "probe"
		} else {
			silent = append(silent, a)
		}
	}
	if len(silent) > 0 {
		now := map[string]string{}
		if budget := clientBudget(opts.Deadline); budget > 0 {
			rows, _, err := d.clientList(ctx, "/ip/arp", budget)
			if err == nil {
				for _, row := range rows {
					if lan[row["interface"]] {
						now[routerdoc.IPv4(row["address"])] = strings.ToLower(row["status"])
					}
				}
			}
		}
		for _, a := range silent {
			s := r.Sightings[a.ip]
			if now[a.ip] == "reachable" {
				s.Presence = "online"
				s.Basis = "probe"
			} else if clientARPGone(now[a.ip]) || a.kind == "arp" {
				s.Presence = "offline"
				s.Basis = "probe"
			}
		}
	}
	notes := []string{}
	if denied.Load() {
		notes = append(notes, messageClientsNoTest)
	}
	if len(candidates) > count {
		notes = append(notes, fmt.Sprintf(messageClientsCap, count, len(candidates)-count))
	}
	if unsettled > 0 && !denied.Load() {
		notes = append(notes, fmt.Sprintf(messageClientsUnsettled, unsettled))
	}
	return notes
}

// ReadClients reads required ARP evidence, optional LAN/DHCP tables and bounded probes.
func (d rosDevice) ReadClients(ctx context.Context, opts ReadOptions) (*routerdoc.Reading, error) {
	if opts.Deadline.IsZero() {
		opts.Deadline = time.Now().Add(15 * time.Second)
	}
	ctx, cancel := context.WithDeadline(ctx, opts.Deadline)
	defer cancel()
	r := routerdoc.NewReading()
	r.Notes = []string{}
	listing := func(path string) ([]ros.Row, error) {
		left := time.Until(opts.Deadline)
		if left < time.Second {
			return nil, device.Error(messageClientsSlow)
		}
		rows, complete, err := d.clientList(ctx, path, min(ros.Timeout, left))
		if !complete {
			r.Complete = false
		}
		return rows, err
	}
	arp, err := listing("/ip/arp")
	if err != nil {
		return nil, err
	}
	optional := func(path, label string) ([]ros.Row, bool) {
		rows, err := listing(path)
		if err != nil {
			r.Notes = append(r.Notes, fmt.Sprintf(messageClientsOptional, label, err))
			return nil, false
		}
		return rows, true
	}
	addresses, a := optional("/ip/address", " IP 地址")
	servers, b := optional("/ip/dhcp-server", " DHCP 服务")
	leases, c := optional("/ip/dhcp-server/lease", " DHCP 租约")
	r.Complete = r.Complete && a && b && c
	lan := clientLAN(servers, nil, nil)
	if len(lan) == 0 {
		members, _ := listing("/interface/list/member")
		wan := map[string]bool{}
		for _, row := range members {
			if strings.EqualFold(row["list"], "WAN") {
				wan[row["interface"]] = true
			}
		}
		lan = clientLAN(nil, addresses, wan)
		if len(lan) > 0 {
			names := []string{}
			for name := range lan {
				names = append(names, name)
			}
			slices.Sort(names)
			r.Notes = append(r.Notes, fmt.Sprintf(messageClientsFallback, strings.Join(names, "、")))
		}
	}
	if len(lan) == 0 {
		return nil, device.Error(messageClientsNoInterface)
	}
	for _, row := range addresses {
		value := row["address"]
		if ip := routerdoc.IPv4(strings.Split(value, "/")[0]); ip != "" {
			r.Own[ip] = true
		}
		if lan[row["interface"]] && clientEnabled(row) {
			if p, ok := clientInterface(value); ok {
				r.LAN = append(r.LAN, p.Masked())
			}
		}
	}
	if !r.Complete {
		r.Notes = append(r.Notes, messageClientsIncomplete)
	}
	var candidates []clientProbe
	r.Sightings, candidates = classifyClients(arp, leases, servers, lan)
	r.Notes = append(r.Notes, d.confirmClients(ctx, r, candidates, lan, opts)...)
	return r, nil
}
