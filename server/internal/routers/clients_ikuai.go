package routers

import (
	"context"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"configflow/internal/routerdoc"
)

func clientText(row ikuaiRow, key string) string {
	switch v := row[key].(type) {
	case string:
		return strings.TrimSpace(replyText(DecodeIkuaiValue(v, key)))
	default:
		if n, ok := jsonInt(v); ok {
			return strconv.FormatInt(n, 10)
		}
	}
	return ""
}
func clientName(row ikuaiRow) string {
	for _, key := range []string{"comment", "termname", "hostname"} {
		if name := clientText(row, key); name != "" {
			return name
		}
	}
	return ""
}
func (d *Ikuai) clientRows(ctx context.Context, name string) ([]ikuaiRow, error) {
	result := []ikuaiRow{}
	for page := 0; page < 10; page++ {
		reply, err := d.Call(ctx, name, map[string]any{"TYPE": "data,total", "limit": fmt.Sprintf("%d,1000", page*1000)})
		if err != nil {
			return nil, err
		}
		data, _ := reply["data"].([]any)
		count := 0
		for _, v := range data {
			if row, ok := v.(map[string]any); ok {
				result = append(result, ikuaiRow(row))
				count++
			}
		}
		total, err := strconv.Atoi(replyText(reply["total"]))
		if count < 1000 || err == nil && len(result) >= total {
			break
		}
	}
	return result, nil
}
func ikuaiClient(row ikuaiRow, online bool) *routerdoc.Sighting {
	ip := routerdoc.IPv4(rosText(row["ip_addr"]))
	if ip == "" {
		return nil
	}
	s := routerdoc.NewSighting(ip)
	s.MAC = routerdoc.NormalizeMAC(rosText(row["mac"]))
	s.Hostname = clientName(row)
	if online {
		s.AddSource("online")
		s.Presence = "online"
		s.Basis = "terminal"
	} else {
		s.AddSource("lease")
		s.Presence = "offline"
		s.Basis = "absent"
		start, a := jsonInt(row["start_time"])
		end, b := jsonInt(row["end_time"])
		left, c := jsonInt(row["timeout"])
		if a && b && c && left >= 0 && end-start >= left {
			n := end - start - left
			s.LastSeen = &n
		}
	}
	return s
}

// ReadClients joins the traffic monitor, DHCP leases and LAN interface subnets.
func (d *Ikuai) ReadClients(ctx context.Context, opts ClientReadOptions) (*routerdoc.Reading, error) {
	// iKuai has no active probe phase; its budget is the overall read context,
	// rather than RouterOS's two-second reserved probe/collection margin.
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	r := routerdoc.NewReading()
	r.Notes = []string{messageClientsTraffic}
	terminals, err := d.clientRows(ctx, "monitor_lanip")
	if err != nil {
		return nil, err
	}
	for _, row := range terminals {
		if s := ikuaiClient(row, true); s != nil {
			if old := r.Sightings[s.IP]; old != nil {
				old.MAC = firstNonEmpty(old.MAC, s.MAC)
				old.Hostname = firstNonEmpty(old.Hostname, s.Hostname)
			} else {
				r.Sightings[s.IP] = s
			}
		}
	}
	leases, err := d.clientRows(ctx, "dhcp_lease")
	if err != nil {
		r.Notes = append(r.Notes, fmt.Sprintf(messageClientsDHCP, err))
	}
	for _, row := range leases {
		if s := ikuaiClient(row, false); s != nil {
			r.Sightings[s.IP] = routerdoc.Better(r.Sightings[s.IP], s)
		}
	}
	lan, err := d.Call(ctx, "lan", map[string]any{"TYPE": "data,total", "limit": "0,1000"})
	if err != nil {
		r.Complete = false
		r.Notes = append(r.Notes, messageClientsNoLAN)
		return r, nil
	}
	data, _ := lan["data"].([]any)
	for _, value := range data {
		row, ok := value.(map[string]any)
		if !ok {
			continue
		}
		for _, part := range strings.Fields(strings.ReplaceAll(rosText(row["ip_mask"]), ",", " ")) {
			if p, ok := clientInterface(part); ok {
				r.Own[p.Addr().String()] = true
				r.LAN = append(r.LAN, p.Masked())
			}
		}
	}
	return r, nil
}
func clientInterface(value string) (netip.Prefix, bool) {
	if p, err := netip.ParsePrefix(value); err == nil && p.Addr().Is4() {
		return p, true
	}
	parts := strings.Split(value, "/")
	if len(parts) != 2 {
		return netip.Prefix{}, false
	}
	a, e := netip.ParseAddr(parts[0])
	m, f := netip.ParseAddr(parts[1])
	if e != nil || f != nil || !a.Is4() || !m.Is4() {
		return netip.Prefix{}, false
	}
	mask := m.As4()
	bits := 0
	zero := false
	for _, b := range mask {
		for shift := 7; shift >= 0; shift-- {
			if b&(1<<shift) == 0 {
				zero = true
			} else {
				if zero {
					return netip.Prefix{}, false
				}
				bits++
			}
		}
	}
	return netip.PrefixFrom(a, bits), true
}
