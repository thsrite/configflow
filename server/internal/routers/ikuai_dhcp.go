package routers

// iKuai DHCP DNS (ikuai.py read_dhcp_dns/_dhcp_apply).
//
// The dhcp_server edit form was verified against the vendor UI and backend of
// official firmware 4.0.120 (dhcp_server.sh edit(), config_db.conf; dns2=""
// is allowed). It is a full form: a field left out resets that DHCP setting,
// so only a row with exactly the verified fields and types is ever written,
// and every write is read back whole.

import (
	"configflow/internal/jsondecode"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/netip"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"configflow/internal/devhttp"
	"configflow/internal/netipx"
)

var (
	ikuaiDHCPOptions   = []int{15, 28, 43, 60, 66, 67, 80, 119, 125, 128, 138, 121}
	ikuaiDHCPIntFields = append([]string{"lease", "delay", "check_addr_valid", "check_relay_only"},
		prefixed("opt_type", ikuaiDHCPOptions)...)
	ikuaiDHCPFields = slices.Concat([]string{"id", "enabled", "tagname", "interface", "phy_ifnames", "addr_pool",
		"exclude_pool", "netmask", "gateway", "next_server", "dns1", "dns2", "domain", "wins1", "wins2"},
		ikuaiDHCPIntFields, prefixed("opt", ikuaiDHCPOptions))
)

func prefixed(prefix string, numbers []int) []string {
	out := make([]string, len(numbers))
	for i, n := range numbers {
		out[i] = prefix + strconv.Itoa(n)
	}
	return out
}

func (d *Ikuai) dhcpList(ctx context.Context, funcName, prefix string) ([]ikuaiRow, error) {
	// The vendor's include/sqlite.sh prints a literal [] for an empty query.
	rows, err := d.ListAll(ctx, funcName, prefix, false, fmt.Sprintf(messageIkuaiDHCPIncomplete, funcName))
	if err != nil {
		return nil, err
	}
	return decodeRows(rows), nil
}

func withoutKeys(row ikuaiRow, keys ...string) ikuaiRow {
	out := maps.Clone(row)
	for _, key := range keys {
		delete(out, key)
	}
	return out
}

// ReadDHCPDNS reads every DHCP server and static binding. A network is
// writable only without blockers: unknown or missing fields, a relay-only or
// interface-limited service, an unclear address pool, or static bindings
// that could hand out their own DNS.
func (d *Ikuai) ReadDHCPDNS(ctx context.Context) (*DhcpDnsState, error) {
	rows, err := d.dhcpList(ctx, "dhcp_server", "")
	if err != nil {
		return nil, err
	}
	// The vendor's dhcp_lease.sh writes these DNS fields into ik_dhcpd_static.conf:
	// per-client overrides must not silently defeat a network-level change.
	bindings, err := d.dhcpList(ctx, "dhcp_static", "static_")
	if err != nil {
		return nil, err
	}
	servers, static := make([]ikuaiRow, len(rows)), make([]ikuaiRow, len(bindings))
	for i, row := range rows {
		servers[i] = withoutKeys(row, "available")
	}
	for i, binding := range bindings {
		static[i] = withoutKeys(binding, "termname", "hostname", "ip_addr_int")
	}
	evidence, err := json.Marshal(map[string]any{"servers": servers, "static": static})
	if err != nil {
		return nil, fmt.Errorf("routers: iKuai DHCP evidence: %w", err)
	}
	state := &DhcpDnsState{Entries: []*DhcpDnsEntry{}, Evidence: evidence}
	for _, row := range rows {
		entry, err := ikuaiDHCPEntry(row, bindings)
		if err != nil {
			return nil, err
		}
		state.Entries = append(state.Entries, entry)
	}
	return state, nil
}

func ikuaiDHCPEntry(row ikuaiRow, bindings []ikuaiRow) (*DhcpDnsEntry, error) {
	var blockers []string
	var missing, unknown []string
	for _, key := range ikuaiDHCPFields {
		if !row.has(key) {
			missing = append(missing, key)
		}
	}
	for key := range row {
		if !slices.Contains(ikuaiDHCPFields, key) && key != "status" && key != "available" {
			unknown = append(unknown, key)
		}
	}
	if len(missing) > 0 || len(unknown) > 0 {
		list := func(keys []string) string {
			slices.Sort(keys)
			return cmpOrNone(strings.Join(keys, ","))
		}
		blockers = append(blockers, fmt.Sprintf(messageIkuaiDHCPFields, list(missing), list(unknown)))
	}
	attributes := ikuaiRow{}
	for _, key := range ikuaiDHCPFields {
		value, ok := row[key]
		if !ok {
			continue
		}
		attributes[key] = value
		valid := false
		if key == "id" || slices.Contains(ikuaiDHCPIntFields, key) {
			_, valid = jsonInt(value)
		} else {
			_, valid = value.(string)
		}
		if !valid {
			blockers = append(blockers, fmt.Sprintf(messageIkuaiDHCPFieldType, key))
		}
	}
	if !row.is("enabled", "yes") && !row.is("enabled", "no") {
		blockers = append(blockers, messageIkuaiDHCPEnabled)
	}
	if status, ok := jsonInt(row["status"]); !ok || status != 1 {
		blockers = append(blockers, messageIkuaiDHCPStatus)
	}
	if !numberEquals(row["check_relay_only"], 0) {
		blockers = append(blockers, messageIkuaiDHCPRelay)
	}
	if !row.is("phy_ifnames", "all") {
		blockers = append(blockers, messageIkuaiDHCPInterfaces)
	}
	subnet, network := ikuaiDHCPSubnet(row)
	if !network {
		blockers = append(blockers, messageIkuaiDHCPNetwork)
	}
	dns := []string{}
	for _, field := range []string{"dns1", "dns2"} {
		if row.is(field, "") {
			continue
		}
		text, _ := row[field].(string)
		address, ok := netipx.ParseAddr4(text)
		if !ok {
			blockers = append(blockers, messageIkuaiDHCPDNSInherited)
		} else if !slices.Contains(dns, address.String()) {
			dns = append(dns, address.String())
		}
	}
	for _, binding := range bindings {
		blockers = append(blockers, ikuaiBindingBlockers(binding, subnet, network)...)
	}
	encoded, err := json.Marshal(attributes)
	if err != nil {
		return nil, fmt.Errorf("routers: iKuai DHCP attributes: %w", err)
	}
	entry := &DhcpDnsEntry{ID: row.text("id"), Network: "", DNS: dns, Enabled: !row.is("enabled", "no"),
		DNSExplicit: row.filled("dns1") && row.filled("dns2"), Attributes: encoded,
		Blockers: uniqueStrings(blockers)}
	if network {
		entry.Network = subnet.String()
	}
	return entry, nil
}

func cmpOrNone(text string) string {
	if text == "" {
		return messageIkuaiDHCPNone
	}
	return text
}

// ikuaiDHCPSubnet is the one network the address pool, netmask and gateway
// agree on.
func ikuaiDHCPSubnet(row ikuaiRow) (netip.Prefix, bool) {
	first, last, ok := strings.Cut(row.text("addr_pool"), "-")
	if !ok || strings.Contains(last, "-") {
		return netip.Prefix{}, false
	}
	start, ok1 := netipx.ParseAddr4(first)
	end, ok2 := netipx.ParseAddr4(last)
	gateway, ok3 := netipx.ParseAddr4(row.text("gateway"))
	if !ok1 || !ok2 || !ok3 {
		return netip.Prefix{}, false
	}
	subnet, ok := netipx.ParseNetwork4(start.String()+"/"+row.text("netmask"), false)
	if !ok || !subnet.Contains(end) || start.Compare(end) > 0 || !subnet.Contains(gateway) {
		return netip.Prefix{}, false
	}
	return subnet, true
}

// ikuaiBindingBlockers are the reasons a static binding inside the network
// (or anywhere, when the network is unknown) could defeat the DHCP DNS.
func ikuaiBindingBlockers(binding ikuaiRow, subnet netip.Prefix, network bool) []string {
	if binding.is("enabled", "no") {
		return nil
	}
	text, isString := binding["ip_addr"].(string)
	address, ok := netipx.ParseAddr4(text)
	if !isString || !ok {
		return []string{messageIkuaiDHCPStaticAddress}
	}
	if network && !subnet.Contains(address) {
		return nil
	}
	var blockers []string
	complete := binding.is("enabled", "yes")
	for _, key := range IkuaiDHCPStaticFields {
		if !binding.has(key) {
			complete = false
		} else if _, ok := binding[key].(string); !ok && key != "id" {
			complete = false
		}
	}
	if !complete {
		blockers = append(blockers, messageIkuaiDHCPStaticPartial)
	} else if !binding.is("dns1", "") || !binding.is("dns2", "") {
		blockers = append(blockers, messageIkuaiDHCPStaticDNS)
	}
	for key := range binding {
		if !slices.Contains(IkuaiDHCPStaticFields, key) && key != "termname" && key != "hostname" && key != "ip_addr_int" {
			blockers = append(blockers, messageIkuaiDHCPStaticFields)
			break
		}
	}
	return blockers
}

// jsonEqual compares two JSON-shaped values structurally (decoded numbers as
// written; arrays and objects compare deeply, never panicking).
func jsonEqual(a, b any) bool {
	normalize := func(v any) (any, bool) {
		data, err := json.Marshal(v)
		if err != nil {
			return nil, false
		}
		var out any
		return out, devhttp.DecodeJSON(data, &out) == nil
	}
	left, ok1 := normalize(a)
	right, ok2 := normalize(b)
	return ok1 && ok2 && reflect.DeepEqual(left, right)
}

func ikuaiDHCPTarget(state *DhcpDnsState, id string) (*DhcpDnsEntry, error) {
	var found []*DhcpDnsEntry
	for _, e := range state.Entries {
		if e.ID == id {
			found = append(found, e)
		}
	}
	if len(found) != 1 {
		return nil, routerError(messageIkuaiDHCPTarget)
	}
	return found[0], nil
}

// dhcpEditVerified writes a complete dhcp_server form and proves the
// read-back holds exactly it, without blockers.
func (d *Ikuai) dhcpEditVerified(ctx context.Context, payload ikuaiRow) error {
	if _, err := d.Write(ctx, "dhcp_server", "edit", payload, 0); err != nil {
		return err
	}
	state, err := d.ReadDHCPDNS(ctx)
	if err != nil {
		return err
	}
	actual, err := ikuaiDHCPTarget(state, payload.text("id"))
	if err != nil {
		return err
	}
	if !jsonEqual(json.RawMessage(actual.Attributes), payload) || len(actual.Blockers) > 0 {
		return routerError(messageIkuaiDHCPReadback)
	}
	return nil
}

type ikuaiDHCPScope struct {
	ID      any `json:"id"`
	Network any `json:"network"`
	Gateway any `json:"gateway"`
}

// dhcpApply runs dhcp_dns_update (the selected DNS into both slots of the
// previewed network) and dhcp_dns_restore (a complete before-image). A failed
// write is read back and, when anything changed, restored at once.
func (d *Ikuai) dhcpApply(ctx context.Context, op ikuaiOp) (any, error) {
	if op.Kind == "dhcp_dns_restore" {
		var keys []string
		for key := range op.Record {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		if op.Record == nil || !slices.Equal(keys, slices.Sorted(slices.Values(ikuaiDHCPFields))) {
			return nil, routerError(messageIkuaiDHCPUndoRecord)
		}
		// A rollback has a complete before-image and a known ID; it must work
		// even when the failed release cannot read state.
		if err := d.dhcpEditVerified(ctx, op.Record); err != nil {
			return nil, err
		}
		return ikuaiRecord{Kind: "dhcp_dns_restore", Record: op.Record}, nil
	}
	state, err := d.ReadDHCPDNS(ctx)
	if err != nil {
		return nil, err
	}
	var scope ikuaiDHCPScope
	scopeOK := len(op.Scope) > 0 && jsondecode.Unmarshal(op.Scope, &scope) == nil && strings.HasPrefix(strings.TrimSpace(string(op.Scope)), "{")
	text := func(v any) string { s, _ := v.(string); return s }
	if !scopeOK || text(scope.ID) == "" || text(scope.Network) == "" || text(scope.Gateway) == "" {
		return nil, routerError(messageIkuaiDHCPScope)
	}
	entry, err := CheckedDNSTarget(DNSCheck{ID: text(scope.ID), Network: text(scope.Network), Gateway: text(scope.Gateway)}, state)
	if err != nil {
		return nil, err
	}
	// A missing "expected" never matches: the write needs the previewed form.
	if entry.ID != op.ID.String() || op.Expected == nil || !jsonEqual(json.RawMessage(entry.Attributes), op.Expected) {
		return nil, routerError(messageIkuaiDHCPDrift)
	}
	var target []any
	if json.Unmarshal(op.DNS, &target) != nil || len(target) != 1 {
		return nil, routerError(messageIkuaiDHCPAddress)
	}
	server, _ := target[0].(string)
	address, ok := netipx.ParseAddr4(server)
	if !ok || address.IsUnspecified() || address.IsMulticast() || address.IsLoopback() ||
		address == netip.AddrFrom4([4]byte{255, 255, 255, 255}) {
		return nil, routerError(messageIkuaiDHCPAddress)
	}
	// The stored iKuai record keeps its numbers (decoded as float64, as the
	// before-image has always been).
	var before ikuaiRow
	if entry != nil {
		_ = json.Unmarshal(entry.Attributes, &before)
	}
	// The UI requires both fields. Both are set to the same DNS so no blank
	// value can inherit a firmware-specific fallback resolver; the read-back
	// lists the server once.
	payload := maps.Clone(before)
	payload["dns1"], payload["dns2"] = address.String(), address.String()
	if err := d.dhcpEditVerified(ctx, payload); err != nil {
		changed := true
		if state, readErr := d.ReadDHCPDNS(ctx); readErr == nil {
			// An unreachable read does not prove the edit failed; the known
			// ID and complete before-image still permit the restore.
			if actual, findErr := ikuaiDHCPTarget(state, payload.text("id")); findErr == nil {
				changed = !jsonEqual(json.RawMessage(actual.Attributes), before)
			}
		}
		if changed {
			if rollbackErr := d.dhcpEditVerified(ctx, before); rollbackErr != nil {
				return nil, &RouterError{Kind: KindRollback, Message: fmt.Sprintf(messageIkuaiDHCPRollback, err, rollbackErr)}
			}
		}
		return nil, err
	}
	return ikuaiRecord{Kind: "dhcp_dns_restore", Record: before}, nil
}
