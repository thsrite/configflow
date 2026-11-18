package routers

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strings"
)

var advisoryMetadata = []string{".id", "chain", "action", "comment", "disabled", "invalid", "dynamic", "bytes", "packets", "log", "log-prefix", "reject-with"}
var advisoryScope = []string{"src-address", "dst-address", "src-address-list", "dst-address-list", "in-interface", "out-interface", "in-interface-list", "out-interface-list", "src-address-type", "dst-address-type"}

func advisoryLive(r Row) bool {
	return r.is("chain", "forward") && !rosTruthy(r["disabled"]) && !rosTruthy(r["invalid"])
}
func advisoryStates(r Row) ([]string, bool) {
	s := strings.TrimSpace(r.text("connection-state"))
	neg := strings.HasPrefix(s, "!")
	out := []string{}
	for _, v := range strings.Split(strings.TrimLeft(s, "!"), ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out, neg
}
func advisoryState(r Row, state string) bool {
	v, neg := advisoryStates(r)
	return len(v) == 0 || (slices.Contains(v, state) != neg)
}
func advisoryEstablished(r Row) bool {
	return r.is("action", "accept") && r.set("connection-state") && advisoryState(r, "established") && !advisoryState(r, "invalid") && !advisoryState(r, "new")
}
func advisoryExtra(r Row, protocol bool) bool {
	for k, v := range r {
		if slices.Contains(advisoryMetadata, k) || slices.Contains(advisoryScope, k) || k == "connection-state" {
			continue
		}
		if protocol && k == "protocol" && slices.Contains([]string{"tcp", "6", "all"}, strings.ToLower(r.text(k))) {
			continue
		}
		if rosValueSet(v) {
			return true
		}
	}
	return false
}
func rosValueSet(v any) bool {
	switch v := v.(type) {
	case nil:
		return false
	case bool:
		return v
	case string:
		return v != ""
	default:
		return true
	}
}
func advisoryRemaining(r Row) bool { return advisoryState(r, "invalid") && !advisoryExtra(r, true) }
func advisoryOverlap(values []string, networks []netip.Prefix, neg bool) bool {
	if len(networks) == 0 {
		return true
	}
	parsed := Networks(values)
	if len(parsed) == 0 {
		return len(values) == 0 || neg
	}
	if neg {
		for _, target := range networks {
			covered := false
			for _, n := range parsed {
				covered = covered || (n.Bits() <= target.Bits() && n.Contains(target.Addr()))
			}
			if !covered {
				return true
			}
		}
		return false
	}
	for _, target := range networks {
		for _, n := range parsed {
			if target.Overlaps(n) {
				return true
			}
		}
	}
	return false
}
func advisoryPossible(r Row, iface string, ctx *NatContext, src, dst []netip.Prefix, sets map[string][]string) bool {
	if r.is("src-address-type", "local") || r.is("dst-address-type", "local") {
		return false
	}
	for _, direction := range []struct {
		name string
		nets []netip.Prefix
	}{{"src", src}, {"dst", dst}} {
		value := strings.TrimSpace(r.text(direction.name + "-address"))
		literal, neg := strings.CutPrefix(value, "!")
		if value != "" && len(Networks([]string{literal})) > 0 && !advisoryOverlap([]string{literal}, direction.nets, neg) {
			return false
		}
		value = strings.TrimSpace(r.text(direction.name + "-address-list"))
		name, neg := strings.CutPrefix(value, "!")
		if values, known := sets[name]; value != "" && known {
			if len(values) == 0 {
				if !neg {
					return false
				}
			} else if len(Networks(values)) > 0 && !advisoryOverlap(values, direction.nets, neg) {
				return false
			}
		}
	}
	for _, dir := range []string{"in", "out"} {
		for _, suffix := range []string{"interface", "interface-list"} {
			if selectorMatches(r.text(dir+"-"+suffix), iface, ctx, suffix == "interface-list") == matchNo {
				return false
			}
		}
	}
	return true
}
func advisoryCovers(r Row, iface string, ctx *NatContext, src, dst []netip.Prefix, sets map[string][]string) bool {
	if !advisoryState(r, "invalid") || advisoryExtra(r, false) {
		return false
	}
	for _, dir := range []struct {
		name string
		nets []netip.Prefix
	}{{"src", src}, {"dst", dst}} {
		for _, suffix := range []string{"address", "address-list"} {
			text := strings.TrimSpace(r.text(dir.name + "-" + suffix))
			if text == "" {
				continue
			}
			key, neg := strings.CutPrefix(text, "!")
			values := []string{key}
			if suffix == "address-list" {
				var known bool
				values, known = sets[key]
				if !known {
					return false
				}
			}
			if len(dir.nets) == 0 {
				return false
			}
			parsed := Networks(values)
			if len(values) > 0 && len(parsed) == 0 {
				return false
			}
			if neg {
				for _, net := range parsed {
					for _, target := range dir.nets {
						if net.Overlaps(target) {
							return false
						}
					}
				}
			} else {
				for _, target := range dir.nets {
					covered := false
					for _, net := range parsed {
						covered = covered || (net.Bits() <= target.Bits() && net.Contains(target.Addr()))
					}
					if !covered {
						return false
					}
				}
			}
		}
	}
	if r.set("src-address-type") || r.set("dst-address-type") {
		return false
	}
	for _, dir := range []string{"in", "out"} {
		for _, suffix := range []string{"interface", "interface-list"} {
			if selectorMatches(r.text(dir+"-"+suffix), iface, ctx, suffix == "interface-list") != matchYes {
				return false
			}
		}
	}
	return true
}
func advisoryLabel(r Row, index int) string {
	clip := func(s string, n int) string {
		v := []rune(strings.Join(strings.Fields(s), " "))
		if len(v) > n {
			v = v[:n]
		}
		return string(v)
	}
	identity := r.id()
	if identity == "" {
		identity = fmt.Sprintf(messageAdvisoryRow, index+1)
	}
	identity = clip(identity, 40)
	if comment := clip(r.text("comment"), 100); comment != "" {
		return fmt.Sprintf(messageAdvisoryLabel, comment, identity)
	}
	return fmt.Sprintf(messageAdvisoryID, identity)
}

// ReturnAdvisoryWarnings conservatively observes a one-way LAN state firewall.
// This optional advice never changes operations, checks or fingerprints.
func ReturnAdvisoryWarnings(ctx context.Context, desired *Desired, state *RouterState, d Driver) ([]string, error) {
	out := []string{}
	ros, ok := d.(*RouterOS)
	if !ok || desired == nil || state == nil {
		return out, nil
	}
	var rows []Row
	if state.ROSPolicy != nil {
		rows = (*state.ROSPolicy)[rosFilter]
	} else if state.DNSGuard != nil {
		rows = (*state.DNSGuard)[rosFilter]
	}
	if rows == nil {
		rows = ros.forwardFilterRows
	}
	if rows == nil {
		var err error
		rows, err = ros.list(ctx, rosFilter)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			var re *RouterError
			if errors.As(err, &re) {
				return out, nil
			}
			return nil, err
		}
	}
	candidate, established := false, false
	for _, r := range rows {
		if !advisoryLive(r) {
			continue
		}
		established = established || advisoryEstablished(r)
		values, neg := advisoryStates(r)
		if slices.Contains([]string{"drop", "reject"}, r.text("action")) && ((slices.Contains(values, "invalid") && !neg) || (established && advisoryRemaining(r))) {
			candidate = true
			break
		}
	}
	if !candidate {
		return out, nil
	}
	gateway, err := netip.ParseAddr(desired.ProxyGateway)
	if err != nil || !gateway.Is4() {
		return out, nil
	}
	context := NatContext{}
	if state.NatContext != nil {
		context = *state.NatContext
	}
	network := netip.PrefixFrom(gateway, 24).Masked()
	longest := -1
	for _, r := range context.Routes {
		if rosTruthy(r["connect"]) && rosTruthy(r["active"]) {
			for _, n := range Networks([]string{r.text("dst-address")}) {
				if n.Contains(gateway) && n.Bits() > longest {
					network = n
					longest = n.Bits()
				}
			}
		}
	}
	sources := SubtractNetworks([]netip.Prefix{network}, []netip.Prefix{netip.PrefixFrom(gateway, 32)})
	iface, _ := NewRouteEgressIndex(&context).GatewayEgress(gateway)
	if iface != "" && slices.ContainsFunc(rows, func(r Row) bool { return r.set("in-interface-list") || r.set("out-interface-list") }) {
		for _, item := range []struct {
			path string
			dest *[]Row
		}{{"/interface/list", &context.InterfaceLists}, {"/interface/list/member", &context.InterfaceMembers}} {
			if *item.dest == nil {
				v, e := ros.list(ctx, item.path)
				if e == nil {
					*item.dest = v
				} else if ctx.Err() != nil {
					return nil, ctx.Err()
				}
			}
		}
	}
	values := []string{}
	for _, v := range desired.ProxyCIDRs {
		values = append(values, v.CIDR)
	}
	if desired.ROSPolicy != nil {
		for _, v := range desired.ROSPolicy.CIDRs {
			values = append(values, v.CIDR)
		}
	}
	destinations := Networks(values)
	var accepted Row
	acceptedIndex := 0
	for index, r := range rows {
		if !advisoryLive(r) || !advisoryPossible(r, iface, &context, sources, destinations, state.AddressSets) {
			continue
		}
		if advisoryEstablished(r) {
			accepted = r
			acceptedIndex = index
		}
		states, neg := advisoryStates(r)
		if slices.Contains([]string{"drop", "reject"}, r.text("action")) {
			explicit := slices.Contains(states, "invalid") && !neg
			if explicit || (accepted != nil && advisoryRemaining(r)) {
				reason := fmt.Sprintf(messageAdvisoryRemaining, advisoryLabel(accepted, acceptedIndex), r.text("action"))
				if explicit {
					reason = fmt.Sprintf("%s connection-state=%s", r.text("action"), r.text("connection-state"))
				}
				out = append(out, fmt.Sprintf(messageReturnAdvisory, advisoryLabel(r, index), reason))
			}
		}
		if slices.Contains([]string{"accept", "drop", "reject"}, r.text("action")) && advisoryCovers(r, iface, &context, sources, destinations, state.AddressSets) {
			break
		}
	}
	return out, nil
}
