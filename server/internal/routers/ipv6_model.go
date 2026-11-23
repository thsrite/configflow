package routers

import (
	"context"
	"encoding/json"
	"net/netip"
	"slices"
	"strconv"
	"strings"

	"configflow/internal/model"
	"configflow/internal/netipx"
	"configflow/internal/yamlx"
	"gopkg.in/yaml.v3"
)

const (
	rosIPv6Addresses = "/ipv6/address"
	rosIPv4Addresses = "/ip/address"
)

// IPv6HostAddress is an address on the Agent's detected upstream interface.
type IPv6HostAddress struct {
	Address    string `json:"address"`
	Interface  string `json:"interface,omitempty"`
	Usable     bool   `json:"usable,omitempty"`
	Temporary  bool   `json:"temporary,omitempty"`
	Deprecated bool   `json:"deprecated,omitempty"`
	Tentative  bool   `json:"tentative,omitempty"`
	DADFailed  bool   `json:"dad_failed,omitempty"`
	Invalid    bool   `json:"invalid,omitempty"`
}

// IPv6Host is the read-only Agent report used to choose a stable next hop.
type IPv6Host struct {
	Interface string            `json:"interface,omitempty"`
	Addresses []IPv6HostAddress `json:"addresses,omitempty"`
	Error     string            `json:"error,omitempty"`
}

// IPv6Snapshot contains route and LAN evidence; absent tables are never empty proof.
type IPv6Snapshot map[string][]Row

// SnapshotIPv6 reads only the tables required by an IPv6 route transaction.
func SnapshotIPv6(ctx context.Context, d *RouterOS, network bool) (IPv6Snapshot, error) {
	out := IPv6Snapshot{}
	paths := []struct{ key, path string }{{"routes", rosIPv6Routes}}
	if network {
		paths = append(paths, struct{ key, path string }{"addresses", rosIPv6Addresses}, struct{ key, path string }{"ipv4_addresses", rosIPv4Addresses})
	}
	for _, p := range paths {
		rows, err := d.list(ctx, p.path)
		if err != nil {
			return nil, err
		}
		out[p.key] = rows
	}
	return out, nil
}

var ipv6Runtime = stringSet("active", "inactive", "immediate-gw", "belongs-to", "hw-offloaded", "debug.fwp-ptr", "actual-interface", "bytes", "packets", "uptime", "last-updated", "expires-after", "expires", "valid-lifetime", "preferred-lifetime", "valid", "preferred")

// StableIPv6Row keeps complete configuration, host bits and resolved LAN identity.
// Lease lifetimes are volatile; route activity is added separately by IPv6Evidence.
func StableIPv6Row(row Row) Row {
	out := Row{}
	for k, v := range row {
		if !strings.HasPrefix(k, ".") && !ipv6Runtime[k] {
			out[k] = comparable(k, v)
		}
	}
	if value, ok := row["address"]; ok {
		address := rosText(value)
		addrText, mask, hasMask := strings.Cut(address, "/")
		if a, err := netip.ParseAddr(addrText); err == nil {
			networkText := a.WithZone("").String()
			if hasMask {
				networkText += "/" + mask
			}
			if n, ok := netipx.ParseNetwork(networkText, false); ok {
				address = a.String() + "/" + strconv.Itoa(n.Bits())
			}
		}
		out["address"] = address
		if v, ok := row["actual-interface"]; ok {
			out["actual-interface"] = comparable("actual-interface", v)
		}
	}
	return out
}

// IPv6Evidence returns sorted stable preview evidence, retaining route activity.
func IPv6Evidence(live IPv6Snapshot) IPv6Snapshot {
	out := IPv6Snapshot{}
	for key, rows := range live {
		sorted := slices.Clone(rows)
		slices.SortStableFunc(sorted, func(a, b Row) int { return strings.Compare(a.id(), b.id()) })
		out[key] = []Row{}
		for _, row := range sorted {
			value := StableIPv6Row(row)
			value[".id"] = row.id()
			if key == "routes" {
				value["active"] = policyFlag(row["active"])
			}
			out[key] = append(out[key], value)
		}
	}
	return out
}

func ipv6Enabled(row Row) bool { return !policyFlag(row["disabled"]) }
func ipv6Network(value string) (netip.Prefix, bool) {
	p, ok := netipx.ParseNetwork(value, false)
	return p, ok && p.Addr().Is6()
}
func ipv6Key(row Row) string {
	if p, ok := ipv6Network(row.text("dst-address")); ok {
		return p.String()
	}
	return ""
}
func ipv6SameGateway(row Row, gateway string) bool {
	if immediate := row.text("immediate-gw"); strings.Contains(immediate, "%") {
		return immediate == gateway
	}
	return row.text("gateway") == gateway
}

// IPv6LAN returns the unique RouterOS LAN that contains the Agent's IPv4 address.
func IPv6LAN(live IPv6Snapshot, host string) string {
	a, err := netip.ParseAddr(host)
	if err != nil || !a.Is4() {
		return ""
	}
	interfaces := map[string]bool{}
	for _, row := range live["ipv4_addresses"] {
		p, ok := policyNetwork(row.text("address"))
		if ok && p.Addr().Is4() && p.Contains(a) && ipv6Enabled(row) {
			iface := row.text("actual-interface")
			if iface == "" {
				iface = row.text("interface")
			}
			interfaces[iface] = true
		}
	}
	if len(interfaces) == 1 {
		for name := range interfaces {
			return name
		}
	}
	return ""
}

// IPv6Gateway keeps a proven existing gateway, otherwise prefers a unique LLA
// scoped to LAN, falling back to a unique usable ULA on that LAN.
func IPv6Gateway(host IPv6Host, live IPv6Snapshot, lan string, existing []Row) string {
	if lan == "" {
		return ""
	}
	networks := []netip.Prefix{}
	for _, row := range live["addresses"] {
		if p, ok := ipv6Network(row.text("address")); ok && ipv6Enabled(row) && row.is("interface", lan) {
			networks = append(networks, p)
		}
	}
	link, ula := map[string]bool{}, map[string]bool{}
	for _, row := range host.Addresses {
		if row.Interface != "" && row.Interface != host.Interface || row.Temporary || row.Deprecated || row.Tentative || row.DADFailed || row.Invalid {
			continue
		}
		a, err := netip.ParseAddr(strings.SplitN(row.Address, "/", 2)[0])
		if err != nil || !a.Is6() || a.Zone() != "" {
			continue
		}
		candidate := a.String() + "%" + lan
		if a.IsLinkLocalUnicast() {
			link[candidate] = true
		} else if row.Usable && a.IsPrivate() {
			for _, p := range networks {
				if p.Contains(a) {
					ula[candidate] = true
					break
				}
			}
		}
	}
	preserved := map[string]bool{}
	for _, set := range []map[string]bool{link, ula} {
		for candidate := range set {
			for _, row := range existing {
				if ipv6Configured(row, candidate) && policyFlag(row["active"]) {
					preserved[candidate] = true
				}
			}
		}
	}
	if len(preserved) == 1 {
		for candidate := range preserved {
			return candidate
		}
	}
	candidates := link
	if len(link) == 0 {
		candidates = ula
	}
	if len(candidates) == 1 {
		for candidate := range candidates {
			return candidate
		}
	}
	return ""
}

func ipv6Configured(row Row, gateway string) bool {
	if rosTextDefault(row["routing-table"], "main") != "main" || !ipv6Enabled(row) || !ipv6SameGateway(row, gateway) {
		return false
	}
	for _, key := range []string{"blackhole", "unreachable", "prohibit", "invalid", "dynamic", "connect"} {
		if policyFlag(row[key]) {
			return false
		}
	}
	return true
}
func ipv6Correct(row Row, spec IPv6RouteSpec) bool {
	return ipv6Key(row) == spec.CIDR && ipv6Configured(row, spec.Gateway) && policyFlag(row["active"])
}

// ipv6Routes reports whether row sends spec's destination to its gateway,
// whatever its origin. Inspection uses it to recognise a correct dynamic or
// connected route, which it then refuses to borrow with its own explanation.
func ipv6Routes(row Row, spec IPv6RouteSpec) bool {
	if ipv6Key(row) != spec.CIDR || rosTextDefault(row["routing-table"], "main") != "main" || !ipv6Enabled(row) || !policyFlag(row["active"]) || !ipv6SameGateway(row, spec.Gateway) {
		return false
	}
	for _, key := range []string{"blackhole", "unreachable", "prohibit", "invalid"} {
		if policyFlag(row[key]) {
			return false
		}
	}
	return true
}

// IPv6RouteSpec is the exact destination and scoped next hop of a Fake-IP route.
type IPv6RouteSpec struct {
	CIDR    string `json:"cidr"`
	Gateway string `json:"gateway"`
}

// IPv6Settings is the saved Mihomo route requirement. Error never means disabled.
type IPv6Settings struct {
	Enabled    bool   `json:"enabled"`
	CIDR       string `json:"cidr"`
	TUNEnabled bool   `json:"tun_enabled"`
	Error      string `json:"error"`
}

func ipv6Document(config *model.Profile) (*yamlx.Node, error) {
	raw := ""
	if config != nil {
		raw = config.Mihomo.CustomConfig
	}
	// A stored base that is not text is refused, not read as an empty base
	// (which would withdraw the managed IPv6 settings).
	if raw == model.DamagedText {
		return nil, routerError(messageIPv6YAMLType)
	}
	if strings.TrimSpace(raw) == "" {
		return yamlx.Map(), nil
	}
	n, err := yamlx.Parse([]byte(raw))
	if err != nil {
		return nil, err
	}
	n = yamlx.Resolve(n)
	if n == nil || n.Kind != yaml.MappingNode {
		return nil, routerError(messageIPv6YAMLObject)
	}
	return n, nil
}
func ipv6YAMLBool(n *yamlx.Node, fallback bool) (bool, bool) {
	n = yamlx.Resolve(n)
	if n == nil {
		return fallback, true
	}
	if n.Kind != yaml.ScalarNode || n.ShortTag() != "!!bool" {
		return false, false
	}
	return strings.EqualFold(n.Value, "true"), true
}
func ipv6YAMLString(n *yamlx.Node) string {
	n = yamlx.Resolve(n)
	if n == nil || n.Kind != yaml.ScalarNode || n.ShortTag() != "!!str" {
		return ""
	}
	return n.Value
}
func ipv6TUN(n *yamlx.Node) bool {
	v, ok := ipv6YAMLBool(yamlx.Get(yamlx.Get(n, "tun"), "enable"), false)
	return v && ok
}

// IPv6FakeIPSettings follows both Mihomo IPv6 modes; DNS IPv6 only controls
// whether TUN and a range must already be configured for clients.
func IPv6FakeIPSettings(config *model.Profile) IPv6Settings {
	out := IPv6Settings{}
	fail := func(err error) IPv6Settings { out.Error = messageIPv6FakeSettings + err.Error(); return out }
	n, err := ipv6Document(config)
	if err != nil {
		return fail(err)
	}
	dns := yamlx.Resolve(yamlx.Get(n, "dns"))
	if dns != nil && dns.Kind != yaml.MappingNode {
		return fail(routerError(messageIPv6DNSObject))
	}
	ipv6, ok := ipv6YAMLBool(yamlx.Get(n, "ipv6"), true)
	if !ok {
		return fail(routerError(messageIPv6Bool))
	}
	clients, ok := ipv6YAMLBool(yamlx.Get(dns, "ipv6"), false)
	if !ok {
		return fail(routerError(messageIPv6DNSBool))
	}
	enable, _ := ipv6YAMLBool(yamlx.Get(dns, "enable"), false)
	rangeNode := yamlx.Resolve(yamlx.Get(dns, "fake-ip-range6"))
	hasRange := rangeNode != nil && rangeNode.ShortTag() != "!!null" && !(rangeNode.ShortTag() == "!!str" && rangeNode.Value == "")
	out.Enabled = ipv6 && enable && ipv6YAMLString(yamlx.Get(dns, "enhanced-mode")) == "fake-ip" && (clients || hasRange)
	if !out.Enabled {
		return out
	}
	p, ok := ipv6Network(ipv6YAMLString(rangeNode))
	if !ok {
		return fail(routerError(messageIPv6Range))
	}
	if !p.Addr().IsPrivate() || p.Bits() < 7 {
		return fail(routerError(messageIPv6ULA))
	}
	out.CIDR = p.String()
	out.TUNEnabled = ipv6TUN(n)
	if clients && !out.TUNEnabled {
		return fail(routerError(messageIPv6TUN))
	}
	return out
}

// IPv6RouteSettings is the saved rule-set route requirement and TUN capability.
type IPv6RouteSettings struct {
	Enabled    bool   `json:"enabled"`
	TUNEnabled bool   `json:"tun_enabled"`
	Error      string `json:"error"`
}

// IPv6RoutesSettings follows Mihomo defaults without altering its YAML text.
func IPv6RoutesSettings(config *model.Profile) IPv6RouteSettings {
	out := IPv6RouteSettings{}
	n, err := ipv6Document(config)
	if err == nil {
		var ok bool
		out.Enabled, ok = ipv6YAMLBool(yamlx.Get(n, "ipv6"), true)
		if !ok {
			err = routerError(messageIPv6Bool)
		} else {
			out.TUNEnabled = ipv6TUN(n)
		}
	}
	if err != nil {
		out.Error = messageIPv6RoutesSettings + err.Error()
	}
	return out
}

// IPv6FakeIPRelevant reports whether inspection has settings or ownership to check.
func IPv6FakeIPRelevant(config *model.Profile, r *Router) bool {
	return r != nil && r.Type == "routeros" && (IPv6FakeIPSettings(config).Enabled || hasStoredState(r.Extra["ipv6_fakeip_state"]))
}

// IPv6RoutesRelevant reports whether a rule-set destination or prior manifest exists.
func IPv6RoutesRelevant(config *model.Profile, r *Router, desired *Desired) bool {
	if r == nil || r.Type != "routeros" {
		return false
	}
	if hasStoredState(r.Extra["ipv6_routes_state"]) {
		return true
	}
	s := IPv6RoutesSettings(config)
	return desired != nil && len(desired.IPv6CIDRs) > 0 && (s.Enabled || s.Error != "")
}

// IPv6RoutesNeedsHost avoids calling an offline host during pure withdrawal.
func IPv6RoutesNeedsHost(config *model.Profile, r *Router, desired *Desired) bool {
	return IPv6RoutesRelevant(config, r, desired) && IPv6RoutesSettings(config).Enabled && desired != nil && len(desired.IPv6CIDRs) > 0
}

func (d *RouterOS) setIPv6Manifest(fake bool, raw json.RawMessage) {
	if fake {
		d.ipv6FakeIPManifest = guardRaw(raw)
		d.ipv6FakeIPManifestSet = true
	} else {
		d.ipv6RoutesManifest = guardRaw(raw)
		d.ipv6RoutesManifestSet = true
	}
}

// SetIPv6FakeIPManifest supplies the stored ownership record without aliasing it.
func (d *RouterOS) SetIPv6FakeIPManifest(raw json.RawMessage) { d.setIPv6Manifest(true, raw) }

// SetIPv6RoutesManifest supplies the stored ownership record without aliasing it.
func (d *RouterOS) SetIPv6RoutesManifest(raw json.RawMessage) { d.setIPv6Manifest(false, raw) }
