package routers

import (
	"configflow/internal/routers/device"
	"configflow/internal/routers/ros"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/netip"
	"net/url"
	"slices"
	"strings"

	"configflow/internal/ctxx"
	"configflow/internal/model"
)

// IPv6ReadinessConfiguration supplies saved Mihomo flags, independent of DNS mode.
type IPv6ReadinessConfiguration struct {
	PolicyConfiguration
	IPv6          bool  `json:"ipv6"`
	FakeIPv6Route *bool `json:"fake_ipv6_route,omitempty"`
	TUNEnabled    bool  `json:"tun_enabled"`
}

// IPv6ReadinessCheck is a RouterOS diagnostic or static policy result.
type IPv6ReadinessCheck struct {
	Scope    string   `json:"scope,omitempty"`
	ID       string   `json:"id"`
	Label    string   `json:"label"`
	Status   string   `json:"status"`
	Message  string   `json:"message"`
	Details  []string `json:"details,omitempty"`
	Decision string   `json:"decision,omitempty"`
	Command  string   `json:"command,omitempty"`
}

// IPv6Readiness reports RouterOS evidence without modifying device configuration.
type IPv6Readiness struct {
	ID                       string               `json:"id"`
	Name                     string               `json:"name"`
	Checks                   []IPv6ReadinessCheck `json:"checks"`
	Status                   string               `json:"status"`
	CanPrepare               bool                 `json:"can_prepare"`
	RoutePublicationRequired bool                 `json:"route_publication_required"`
	Plan                     Row                  `json:"plan"`
	Fingerprint              string               `json:"fingerprint"`
}
type readinessTable struct{ key, path string }

func readinessTables() []readinessTable {
	out := []readinessTable{{"routes", ros.PathIPv6Routes}, {"addresses", rosIPv6Addresses}, {"nd", ros.PathIPv6ND}, {"dhcp", pdClients}, {"nat", "/ipv6/firewall/nat"}, {"filter", "/ipv6/firewall/filter"}, {"ipv4_addresses", rosIPv4Addresses}}
	paths := PolicyTablePaths()
	for _, key := range []string{"raw", "mangle", "ipv4_routes", "ipv4_filter", "ipv4_nat", "ipv4_raw", "ipv4_mangle", "interface_lists", "interface_members", "interfaces", "routing_rules"} {
		out = append(out, readinessTable{key, paths[key]})
	}
	return out
}

// InspectIPv6Readiness exports the RouterOS reader used by Agent readiness.
// It never resolves a hostname, writes the router or reads unreferenced lists.
func InspectIPv6Readiness(ctx context.Context, d *RouterOS, r *Router, agent *model.Agent, host IPv6Host, configuration IPv6ReadinessConfiguration, dnsTarget string, policyContext PolicyContext) (*IPv6Readiness, error) {
	if err := r.ValidateForDevice(); err != nil {
		return nil, err
	}
	snapshot := IPv6Snapshot{}
	failed := []string{}
	checks := []IPv6ReadinessCheck{}
	for _, t := range readinessTables() {
		rows, e := d.List(ctx, t.path)
		if e != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			failed = append(failed, t.key)
			checks = append(checks, IPv6ReadinessCheck{ID: "read_" + t.key, Label: t.key, Status: "unknown", Message: messageReadinessRead})
		} else {
			snapshot[t.key] = rows
		}
	}
	paths := PolicyTablePaths()
	for _, prefix := range []string{"", "ipv4_"} {
		key := prefix + "address_lists"
		snapshot[key] = []Row{}
		names := []string{}
		for _, table := range []string{"filter", "nat", "raw", "mangle"} {
			for _, row := range snapshot[prefix+table] {
				for _, field := range []string{"src-address-list", "dst-address-list"} {
					if row.Set(field) {
						names = append(names, strings.TrimLeft(row.Text(field), "!"))
					}
				}
			}
		}
		names = device.Unique(names)
		slices.Sort(names)
		path := paths[key]
		if prefix == "" {
			path = ros.PathIPv6AddressLists
		}
		for _, name := range names {
			rows, e := d.List(ctx, path+"?list="+strings.ReplaceAll(url.QueryEscape(name), "+", "%20"))
			if e != nil {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				failed = append(failed, key)
				checks = append(checks, IPv6ReadinessCheck{ID: "read_" + key, Label: key, Status: "unknown", Message: messageReadinessLists})
				break
			}
			snapshot[key] = append(snapshot[key], rows...)
		}
	}
	dns := Row{}
	value, e := d.Get(ctx, "/ip/dns", ros.Timeout)
	if e == nil {
		if m := ros.AsRow(value); m != nil {
			dns = m
		} else {
			e = device.Error(messageReadinessDNS)
		}
	}
	if e != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		failed = append(failed, "dns")
		checks = append(checks, IPv6ReadinessCheck{ID: "read_dns", Label: "dns", Status: "unknown", Message: messageReadinessDNS})
	}
	agentHost := ""
	if agent != nil {
		agentHost = agent.Host
	}
	lan := IPv6LAN(snapshot, agentHost)
	fake := ""
	route := configuration.FakeIPv6Enabled
	if configuration.FakeIPv6Route != nil {
		route = *configuration.FakeIPv6Route
	}
	if route {
		fake = configuration.FakeIPv6Range
	}
	existing := []Row{}
	for _, row := range snapshot["routes"] {
		if fake != "" && ipv6Key(row) == fake {
			existing = append(existing, row)
		}
	}
	gateway := IPv6Gateway(host, snapshot, lan, existing)
	add := func(id, label, status, message string, details []string) {
		checks = append(checks, IPv6ReadinessCheck{ID: id, Label: label, Status: status, Message: message, Details: details})
	}
	defaults := []Row{}
	for _, row := range snapshot["routes"] {
		if row.Is("dst-address", "::/0") && row.TextOr("routing-table", "main") == "main" && ipv6Enabled(row) && ros.PolicyFlag(row["active"]) && !ros.PolicyFlag(row["blackhole"]) && !ros.PolicyFlag(row["unreachable"]) && !ros.PolicyFlag(row["prohibit"]) {
			defaults = append(defaults, row)
		}
	}
	status, message := "warning", messageReadinessWANMissing
	if len(defaults) > 0 {
		status, message = "ok", messageReadinessWANOK
	}
	add("wan", messageReadinessWANLabel, status, message, nil)
	nd := []Row{}
	for _, row := range snapshot["nd"] {
		if ipv6Enabled(row) && (row.Is("interface", lan) || row.Is("interface", "all")) {
			nd = append(nd, row)
		}
	}
	status, message = "warning", messageReadinessRAMissing
	if lan != "" && len(nd) > 0 {
		status, message = "ok", fmt.Sprintf(messageReadinessRAOK, lan)
	}
	add("ra", messageReadinessRALabel, status, message, nil)
	details := []string{}
	for _, row := range snapshot["dhcp"] {
		if ipv6Enabled(row) && ros.PolicyFlag(row["use-peer-dns"]) {
			details = append(details, messageReadinessPeerDNS)
			break
		}
	}
	for _, row := range nd {
		if ros.PolicyFlag(row["advertise-dns"]) {
			details = append(details, messageReadinessRADNS)
			break
		}
	}
	if dns.Set("dynamic-servers") {
		details = append(details, messageReadinessDynamicDNS)
	}
	if dnsTarget != "" && dns.Text("servers") != dnsTarget {
		details = append(details, messageReadinessSelectedDNS)
	}
	status, message = "ok", messageReadinessDNSOK
	if len(details) > 0 {
		status, message = "warning", messageReadinessDNSWarning
	}
	add("dns", messageReadinessDNSLabel, status, message, details)
	env := PolicyEnvironmentFromSnapshot(snapshot, failed)
	for _, c := range PolicyChecks(env, agent, host, configuration.PolicyConfiguration, lan, policyContext) {
		checks = append(checks, IPv6ReadinessCheck{ID: c.ID, Label: c.Label, Status: c.Status, Message: c.Message, Details: c.Details, Decision: c.Decision})
	}
	if lan != "" && configuration.IPv6 {
		target := fake
		if target == "" {
			target = "2000::/3"
		}
		hint := checkForwardSnapshot(&env.IPv6, 6, lan, []string{target})
		if hint.Status == "drop" {
			checks = append(checks, IPv6ReadinessCheck{ID: "forward_hairpin", Label: messageReadinessForwardLabel, Status: "warning", Message: ForwardDropWarning(6), Command: hint.Command})
		} else if hint.Status == "unknown" {
			add("forward_hairpin", messageReadinessForwardLabel, "unknown", messageForwardIPv6Unknown, nil)
		}
	}
	var plan Row
	if fake != "" {
		target, valid := ipv6Network(fake)
		if !valid {
			return nil, device.Error(messageIPv6Range)
		}
		rows, conflicts := []Row{}, []Row{}
		for _, row := range snapshot["routes"] {
			p, ok := ipv6Network(row.Text("dst-address"))
			if row.TextOr("routing-table", "main") != "main" || !ok {
				continue
			}
			if p == target {
				rows = append(rows, row)
			} else if ipv6Enabled(row) && p.Bits() >= target.Bits() && p.Overlaps(target) {
				conflicts = append(conflicts, row)
			}
		}
		overlap := false
		for _, row := range snapshot["addresses"] {
			p, ok := ipv6Network(row.Text("address"))
			overlap = overlap || ok && p.Overlaps(target)
		}
		idle := ""
		if !configuration.FakeIPv6Enabled {
			idle = messageReadinessIdle
			if !configuration.TUNEnabled {
				idle += messageReadinessIdleTUN
			}
		}
		label := messageReadinessFakeLabel
		status = "warning"
		switch {
		case configuration.FakeIPv6Enabled && !configuration.TUNEnabled:
			label, status, message = messageReadinessReceiveLabel, "error", messageReadinessTUN
		case overlap:
			status, message = "error", messageReadinessFakeOverlap
		case len(rows) == 1 && gateway != "" && ipv6Correct(rows[0], IPv6RouteSpec{fake, gateway}) && len(conflicts) == 0:
			status, message = "ok", fmt.Sprintf(messageReadinessFakeOK, fake, idle)
		case len(rows) > 0 || len(conflicts) > 0:
			status, message = "error", messageReadinessFakeConflict
		case gateway == "" || slices.ContainsFunc(failed, func(k string) bool {
			return slices.Contains([]string{"routes", "addresses", "nd", "dhcp", "nat", "filter", "ipv4_addresses", "dns"}, k)
		}):
			message = messageReadinessFakeUnknown + idle
		default:
			plan = Row{"dst-address": fake, "gateway": gateway, "routing-table": "main", "disabled": "false", "distance": "1"}
			message = fmt.Sprintf(messageReadinessFakeMissing, fake, gateway, idle)
		}
		add("fake_ipv6", label, status, message, nil)
	} else {
		add("fake_ipv6", messageReadinessFakeLabel, "ok", messageReadinessFakeOff, nil)
	}
	deps := map[string][]string{"wan": {"routes"}, "ra": {"nd", "ipv4_addresses"}, "dns": {"dns", "dhcp", "nd"}}
	result := &IPv6Readiness{Checks: checks, Status: "ok", Plan: plan, RoutePublicationRequired: plan != nil}
	if r != nil {
		result.ID, result.Name = r.ID, r.Name
		if result.Name == "" {
			result.Name = r.ID
		}
	}
	for i := range checks {
		c := &checks[i]
		if slices.ContainsFunc(deps[c.ID], func(k string) bool { return slices.Contains(failed, k) }) {
			c.Status, c.Message = "unknown", messageReadinessIncomplete
		}
		if c.Status == "error" {
			result.Status = "error"
		} else if result.Status != "error" && (c.Status == "warning" || c.Status == "unknown") {
			result.Status = "warning"
		}
	}
	// Hash only stable, sorted route evidence; clocks and read ordering are not changes.
	evidence := []Row{}
	for _, row := range snapshot["routes"] {
		x := Row{}
		for _, k := range []string{".id", "dst-address", "gateway", "immediate-gw", "routing-table", "disabled", "active", "blackhole", "unreachable", "prohibit", "comment"} {
			x[k] = row[k]
		}
		evidence = append(evidence, x)
	}
	slices.SortFunc(evidence, func(a, b Row) int {
		x, _ := json.Marshal(a)
		y, _ := json.Marshal(b)
		return strings.Compare(string(x), string(y))
	})
	slices.Sort(failed)
	// The fingerprint reaches the browser; an unsalted hash over the device
	// password would let anyone holding it test password guesses offline.
	var router *Router
	if r != nil {
		c := *r
		c.Password = ""
		router = &c
	}
	raw, _ := json.Marshal(struct {
		Router       *Router
		Routes       []Row
		LAN, Gateway string
		Plan         Row
		Errors       []string
	}{router, evidence, lan, gateway, plan, failed})
	hash := sha256.Sum256(raw)
	result.Fingerprint = hex.EncodeToString(hash[:])
	return result, nil
}

// IPv6ReadinessReceipt proves one additive legacy preparation route.
type IPv6ReadinessReceipt struct {
	ID     string `json:"id"`
	Values Row    `json:"values"`
	Config Row    `json:"config,omitempty"`
}

func readinessOwned(row, values Row) bool {
	if !ipv6Enabled(row) || ros.PolicyFlag(row["dynamic"]) || ros.PolicyFlag(row["connect"]) {
		return false
	}
	for k, v := range values {
		if k != "disabled" && ros.Comparable(k, row[k]) != ros.Comparable(k, v) {
			return false
		}
	}
	return true
}

// AddMissingIPv6Route preserves the legacy Agent preparation boundary. New
// readiness reports direct users to normal publication and never call it.
func (d *RouterOS) AddMissingIPv6Route(ctx context.Context, plan Row) (*IPv6ReadinessReceipt, error) {
	ctx = ctxx.Detach(ctx)
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	p, ok := ipv6Network(plan.Text("dst-address"))
	g := plan.Text("gateway")
	a, _, scoped := strings.Cut(g, "%")
	addr, e := netip.ParseAddr(a)
	if !ok || p.String() != plan.Text("dst-address") || e != nil || !addr.Is6() || !scoped || !ipv6ValidSpec(&IPv6RouteSpec{p.String(), g}) || len(plan) != 5 || !plan.Is("routing-table", "main") || !plan.Is("distance", "1") || !plan.Is("disabled", "false") {
		return nil, device.Error(device.MessageOperationFormat)
	}
	rows, e := d.List(ctx, ros.PathIPv6Routes)
	if e != nil {
		return nil, e
	}
	for _, r := range rows {
		if ipv6Key(r) == p.String() && r.TextOr("routing-table", "main") == "main" {
			return nil, device.Error(messageReadinessRouteChanged)
		}
	}
	token := make([]byte, 12)
	if _, e = rand.Read(token); e != nil {
		return nil, e
	}
	values := ros.CloneRow(plan)
	values["comment"] = "cf-ipv6-readiness:" + hex.EncodeToString(token)
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	_, writeErr := d.Request(ctx, "PUT", ros.PathIPv6Routes, values, ros.Timeout)
	rows, readErr := d.List(ctx, ros.PathIPv6Routes)
	matched := []Row{}
	for _, r := range rows {
		if r.Is("comment", values.Text("comment")) {
			matched = append(matched, r)
		}
	}
	if writeErr == nil && readErr == nil && len(matched) == 1 && readinessOwned(matched[0], values) && ros.IDPattern.MatchString(matched[0].ID()) && ros.PolicyFlag(matched[0]["active"]) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return &IPv6ReadinessReceipt{matched[0].ID(), values, d.OwnershipSnapshot(ros.PathIPv6Routes, matched[0])}, nil
	}
	if readErr != nil {
		return nil, device.Error(messageReadinessUncertain)
	}
	if len(matched) == 1 && readinessOwned(matched[0], values) {
		if e := d.UndoIPv6ReadinessRoute(ctx, IPv6ReadinessReceipt{matched[0].ID(), values, d.OwnershipSnapshot(ros.PathIPv6Routes, matched[0])}); e != nil {
			return nil, device.Error(messageReadinessUncertain)
		}
	} else if len(matched) > 0 {
		return nil, device.Error(messageReadinessUncertain)
	}
	if writeErr != nil {
		return nil, writeErr
	}
	return nil, device.Error(messageReadinessNewRoute)
}

// UndoIPv6ReadinessRoute removes only the receipt's original stable object.
func (d *RouterOS) UndoIPv6ReadinessRoute(ctx context.Context, receipt IPv6ReadinessReceipt) error {
	ctx = ctxx.Detach(ctx)
	if e := ctx.Err(); e != nil {
		return e
	}
	if !ros.IDPattern.MatchString(receipt.ID) || !strings.HasPrefix(receipt.Values.Text("comment"), "cf-ipv6-readiness:") || len(receipt.Values.Text("comment")) != len("cf-ipv6-readiness:")+24 {
		return device.Error(device.MessageOperationFormat)
	}
	row, e := d.Find(ctx, ros.PathIPv6Routes, receipt.ID)
	if e != nil {
		return e
	}
	if row == nil {
		return nil
	}
	if !readinessOwned(row, receipt.Values) || receipt.Config != nil && !ros.SameRow(d.OwnershipSnapshot(ros.PathIPv6Routes, row), receipt.Config) {
		return device.Error(messageReadinessRouteEdited)
	}
	if e := ctx.Err(); e != nil {
		return e
	}
	_, e = d.Request(ctx, "DELETE", ros.PathIPv6Routes+"/"+ros.EscapePathID(receipt.ID), nil, ros.Timeout)
	if e != nil && d.pdGone(ctx, ros.PathIPv6Routes, receipt.ID) {
		return nil
	}
	return e
}
