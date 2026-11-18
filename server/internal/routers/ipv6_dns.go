package routers

import (
	"context"
	"fmt"
	"net/netip"
	"reflect"
	"regexp"
	"slices"
	"strings"

	"configflow/internal/ctxx"
	"configflow/internal/netipx"
)

const rosIPv6ND = "/ipv6/nd"

// IPv6DNSItem describes one LAN or shared RA advertisement.
type IPv6DNSItem struct {
	ID              string   `json:"id"`
	Interface       string   `json:"interface"`
	SourceInterface string   `json:"source_interface"`
	AdvertiseDNS    string   `json:"advertise_dns"`
	DNSServers      []string `json:"dns_servers"`
	Status          string   `json:"status"`
	Detail          string   `json:"detail"`
}

// IPv6DNSReport is the public RA DNS checklist, without write evidence.
type IPv6DNSReport struct {
	Enabled    bool          `json:"enabled"`
	Status     string        `json:"status"`
	Interface  string        `json:"interface"`
	TargetDNS  string        `json:"target_dns"`
	Message    string        `json:"message"`
	Items      []IPv6DNSItem `json:"items"`
	Blockers   []string      `json:"blockers"`
	Warnings   []string      `json:"warnings"`
	FixCommand string        `json:"fix_command"`
}

// IPv6DNSEntry binds an ND ID to its complete stable configuration.
type IPv6DNSEntry struct {
	ID         string `json:"id"`
	Attributes Row    `json:"attributes"`
}

// IPv6DNSEvidence binds preview to its DHCP target and all relevant RA entries.
type IPv6DNSEvidence struct {
	Gateway   string         `json:"gateway"`
	Interface string         `json:"interface"`
	Scope     DNSCheck       `json:"scope"`
	Entries   []IPv6DNSEntry `json:"entries"`
}

// IPv6DNSCheck verifies that only RA DNS advertising changed.
type IPv6DNSCheck = IPv6DNSEvidence

// IPv6DNSUpdate disables DNS advertisements on one proven dedicated LAN entry.
type IPv6DNSUpdate struct {
	OpHead
	ID              string         `json:"id"`
	Interface       string         `json:"interface"`
	SourceInterface string         `json:"source_interface"`
	TargetDNS       string         `json:"target_dns"`
	Gateway         string         `json:"gateway"`
	Scope           DNSCheck       `json:"scope"`
	Values          Row            `json:"values"`
	ExpectedRows    []IPv6DNSEntry `json:"expected_rows"`
	Expected        Row            `json:"expected"`
	Check           *IPv6DNSCheck  `json:"check"`
}

// IPv6DNSPlan carries the public checklist and internal release evidence.
type IPv6DNSPlan struct {
	IPv6DNSReport
	Ops      []Op             `json:"ops"`
	Check    *IPv6DNSCheck    `json:"check"`
	Evidence *IPv6DNSEvidence `json:"evidence"`
}

func ipv6DNSFlag(row Row, key string) (bool, error) {
	text := "false"
	if v, ok := row[key]; ok {
		text = rosText(v)
	}
	switch strings.ToLower(text) {
	case "false", "no", "0":
		return false, nil
	case "true", "yes", "1":
		return true, nil
	default:
		return false, routerError(fmt.Sprintf(messageIPv6DNSFlag, key))
	}
}
func ipv6DNSServers(row Row) ([]string, error) {
	out := []string{}
	for _, key := range []string{"dns", "dns-servers"} {
		var values []string
		switch v := row[key].(type) {
		case nil:
			if _, exists := row[key]; exists {
				return nil, routerError(messageIPv6DNSServerList)
			}
		case string:
			for _, part := range strings.Split(v, ",") {
				if part = strings.TrimSpace(part); part != "" {
					values = append(values, part)
				}
			}
		case []any:
			for _, x := range v {
				s, ok := x.(string)
				if !ok {
					return nil, routerError(messageIPv6DNSServerList)
				}
				values = append(values, s)
			}
		default:
			return nil, routerError(messageIPv6DNSServerList)
		}
		for _, s := range values {
			a, e := netip.ParseAddr(s)
			if e != nil || !a.Is6() {
				return nil, routerError(messageIPv6DNSServerAddress)
			}
			out = append(out, a.String())
		}
	}
	return uniqueStrings(out), nil
}
func ipv6DNSScope(desired *Desired, scope DNSCheck) (DNSCheck, error) {
	scope.DNS = slices.Clone(scope.DNS)
	if desired == nil || desired.DNSSettings == nil {
		return scope, routerError(messageIPv6DNSSelection)
	}
	target, gateway := clientIPv4(desired.Server), clientIPv4(desired.ProxyGateway)
	if desired.Error != "" || target == "" || gateway == "" {
		return scope, routerError(messageIPv6DNSSelection)
	}
	if scope.ID == "" || scope.Network == "" || scope.Gateway != gateway || !slices.Equal(scope.DNS, []string{target}) {
		return scope, routerError(messageIPv6DNSScope)
	}
	p, ok := netipx.ParseNetwork4(scope.Network, false)
	a, _ := netip.ParseAddr(gateway)
	if !ok || !p.Contains(a) {
		return scope, routerError(messageIPv6DNSNetwork)
	}
	return scope, nil
}
func readIPv6DNS(ctx context.Context, d *RouterOS, gateway string) (string, []Row, error) {
	addresses, err := d.list(ctx, rosIPv4Addresses)
	if err != nil {
		return "", nil, err
	}
	a, _ := netip.ParseAddr(gateway)
	for _, r := range addresses {
		disabled, e := ipv6DNSFlag(r, "disabled")
		if e != nil {
			return "", nil, e
		}
		if disabled {
			continue
		}
		p, ok := netipx.ParseNetwork4(r.text("address"), false)
		if !ok {
			return "", nil, routerError(messageIPv6DNSLANAddress)
		}
		if !p.Contains(a) {
			continue
		}
		invalid, e := ipv6DNSFlag(r, "invalid")
		if e != nil {
			return "", nil, e
		}
		if invalid {
			return "", nil, routerError(messageIPv6DNSLANInvalid)
		}
		iface, valid := r.str("interface")
		if actual, exists := r["actual-interface"]; exists && actual != nil && actual != "" {
			iface, valid = actual.(string)
		}
		if !valid || iface == "" {
			return "", nil, routerError(messageIPv6DNSLANInterface)
		}
	}
	iface := IPv6LAN(IPv6Snapshot{"ipv4_addresses": addresses}, gateway)
	if iface == "" || iface == "all" {
		return "", nil, routerError(messageIPv6DNSLANUnique)
	}
	rows, err := d.list(ctx, rosIPv6ND)
	if err != nil {
		return "", nil, err
	}
	selected := []Row{}
	seen := map[string]bool{}
	for _, r := range rows {
		if iface, ok := r.str("interface"); !ok || iface == "" {
			return "", nil, routerError(messageIPv6DNSNDInterface)
		}
		if r.text("interface") != iface && !r.is("interface", "all") {
			continue
		}
		if id, ok := r.str(".id"); !ok || id == "" || seen[id] {
			return "", nil, routerError(messageIPv6DNSNDID)
		}
		seen[r.id()] = true
		for _, key := range []string{"dynamic", "invalid", "disabled"} {
			flag, e := ipv6DNSFlag(r, key)
			if e != nil {
				return "", nil, e
			}
			if flag && key != "disabled" {
				return "", nil, routerError(messageIPv6DNSNDDynamic)
			}
		}
		if !slices.Contains([]string{"yes", "self", "no"}, r.text("advertise-dns")) {
			return "", nil, routerError(messageIPv6DNSNDMode)
		}
		if _, e := ipv6DNSServers(r); e != nil {
			return "", nil, e
		}
		selected = append(selected, r)
	}
	slices.SortFunc(selected, func(a, b Row) int { return strings.Compare(a.id(), b.id()) })
	return iface, selected, nil
}
func ipv6DNSEntries(d *RouterOS, rows []Row) []IPv6DNSEntry {
	out := []IPv6DNSEntry{}
	for _, r := range rows {
		out = append(out, IPv6DNSEntry{r.id(), d.ownershipSnapshot(rosIPv6ND, r)})
	}
	return out
}
func ipv6DNSEffective(rows []Row, iface string) []Row {
	out := []Row{}
	for _, r := range rows {
		if r.is("interface", iface) && ipv6Enabled(r) {
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		for _, r := range rows {
			if r.is("interface", "all") && ipv6Enabled(r) {
				out = append(out, r)
			}
		}
	}
	return out
}
func ipv6DNSConstraints(rows []Row, iface string) error {
	for _, scope := range []string{iface, "all"} {
		n := 0
		for _, r := range rows {
			if r.is("interface", scope) {
				n++
			}
		}
		if n > 1 {
			if scope == "all" {
				return routerError(messageIPv6DNSDuplicateAll)
			}
			return routerError(messageIPv6DNSDuplicateLAN)
		}
	}
	for _, r := range ipv6DNSEffective(rows, iface) {
		if r.is("interface", "all") && !r.is("advertise-dns", "no") {
			return routerError(messageIPv6DNSShared)
		}
	}
	return nil
}

var ipv6DNSBare = regexp.MustCompile(`^[A-Za-z0-9_.:/,%+\-]+$`)
var ipv6DNSKey = regexp.MustCompile(`^[a-z0-9-]+$`)

func ipv6DNSCLI(value any) string {
	text := rosText(value)
	if v, ok := value.([]any); ok {
		parts := []string{}
		for _, x := range v {
			parts = append(parts, rosText(x))
		}
		text = strings.Join(parts, ",")
	}
	if text == "true" {
		text = "yes"
	}
	if text == "false" {
		text = "no"
	}
	if ipv6DNSBare.MatchString(text) {
		return text
	}
	return ScriptString(text)
}
func ipv6DNSOverride(rows []Row, iface string) string {
	shared := []Row{}
	for _, r := range rows {
		if r.is("interface", iface) {
			return ""
		}
		if r.is("interface", "all") {
			shared = append(shared, r)
		}
	}
	if len(shared) != 1 || !ipv6Enabled(shared[0]) || shared[0].is("advertise-dns", "no") {
		return ""
	}
	skip := stringSet("interface", "advertise-dns", "disabled", "comment", "dynamic", "invalid", "default", "running")
	keys := []string{}
	for k := range shared[0] {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	parts := []string{fmt.Sprintf(messageIPv6DNSOverride, ipv6DNSCLI(iface))}
	for _, k := range keys {
		v := shared[0][k]
		if skip[k] || !ipv6DNSKey.MatchString(k) || v == nil || reflect.DeepEqual(v, "") || reflect.DeepEqual(v, []any{}) {
			continue
		}
		parts = append(parts, k+"="+ipv6DNSCLI(v))
	}
	return strings.Join(parts, " ")
}

// InspectIPv6DNS plans RA DNS changes only for the selected DHCP LAN. Pending
// DHCP DNS is allowed in preview; ApplyIPv6DNS requires it to be effective.
func InspectIPv6DNS(ctx context.Context, desired *Desired, scope DNSCheck, d *RouterOS) (*IPv6DNSPlan, error) {
	p := &IPv6DNSPlan{IPv6DNSReport: IPv6DNSReport{Status: "disabled", Message: messageIPv6DNSDisabled, Items: []IPv6DNSItem{}, Blockers: []string{}, Warnings: []string{}}, Ops: []Op{}}
	if desired != nil && desired.DNSSettings != nil {
		p.Enabled = desired.AgentID != ""
		p.TargetDNS = desired.Server
	}
	if !p.Enabled {
		return p, nil
	}
	err := func() error {
		scope, e := ipv6DNSScope(desired, scope)
		if e != nil {
			return e
		}
		state, e := d.ReadDHCPDNS(ctx)
		if e != nil {
			return e
		}
		if _, e = CheckedDNSTarget(scope, state); e != nil {
			return e
		}
		iface, rows, e := readIPv6DNS(ctx, d, scope.Gateway)
		if e != nil {
			return e
		}
		p.Interface = iface
		entries := ipv6DNSEntries(d, rows)
		p.Evidence = &IPv6DNSEvidence{scope.Gateway, iface, scope, entries}
		effective := rowIDs(ipv6DNSEffective(rows, iface))
		for _, r := range rows {
			pending := effective[r.id()] && !r.is("advertise-dns", "no")
			detail, status := messageIPv6DNSNoChange, "synced"
			if pending {
				detail, status = messageIPv6DNSPendingItem, "pending"
			} else if r.is("interface", "all") && !effective[r.id()] && ipv6Enabled(r) {
				detail = messageIPv6DNSOverridden
			}
			servers, _ := ipv6DNSServers(r)
			p.Items = append(p.Items, IPv6DNSItem{r.id(), iface, r.text("interface"), r.text("advertise-dns"), servers, status, detail})
		}
		p.FixCommand = ipv6DNSOverride(rows, iface)
		if e := ipv6DNSConstraints(rows, iface); e != nil {
			return e
		}
		after := cloneRows(rows)
		pending := []Row{}
		for i, r := range rows {
			if r.is("interface", iface) && ipv6Enabled(r) && !r.is("advertise-dns", "no") {
				pending = append(pending, r)
				after[i]["advertise-dns"] = "no"
			}
		}
		p.Check = &IPv6DNSCheck{scope.Gateway, iface, scope, ipv6DNSEntries(d, after)}
		p.Status, p.Message = "synced", messageIPv6DNSSynced
		if len(pending) > 0 {
			p.Status, p.Message = "pending", fmt.Sprintf(messageIPv6DNSPending, iface)
		}
		for _, r := range pending {
			p.Ops = append(p.Ops, &IPv6DNSUpdate{OpHead: OpHead{Kind: "ipv6_dns_update", Summary: fmt.Sprintf(messageIPv6DNSSummary, iface)}, ID: r.id(), Interface: iface, SourceInterface: r.text("interface"), TargetDNS: desired.Server, Gateway: scope.Gateway, Scope: scope, Values: Row{"advertise-dns": "no"}, ExpectedRows: entries, Expected: d.ownershipSnapshot(rosIPv6ND, r), Check: p.Check})
		}
		return nil
	}()
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		p.Status, p.Message, p.Blockers, p.Ops, p.Check = "blocked", err.Error(), []string{err.Error()}, []Op{}, nil
		for i := range p.Items {
			p.Items[i].Status, p.Items[i].Detail = "blocked", err.Error()
		}
	}
	return p, nil
}

// ApplyIPv6DNS disables one proven RA DNS advertisement after verifying DHCP.
func (d *RouterOS) ApplyIPv6DNS(ctx context.Context, op IPv6DNSUpdate) (ROSUndo, error) {
	ctx = ctxx.Detach(ctx)
	if e := ctx.Err(); e != nil {
		return ROSUndo{}, e
	}
	scope, e := ipv6DNSScope(&Desired{DNSSettings: &DNSSettings{Server: op.TargetDNS}, ProxyGateway: op.Gateway}, op.Scope)
	if e != nil {
		return ROSUndo{}, e
	}
	state, e := d.ReadDHCPDNS(ctx)
	if e != nil {
		return ROSUndo{}, e
	}
	if e = VerifyDNS(scope, state); e != nil {
		return ROSUndo{}, e
	}
	iface, rows, e := readIPv6DNS(ctx, d, scope.Gateway)
	if e != nil {
		return ROSUndo{}, e
	}
	if e = ipv6DNSConstraints(rows, iface); e != nil {
		return ROSUndo{}, e
	}
	if iface != op.Interface || !reflect.DeepEqual(ipv6DNSEntries(d, rows), op.ExpectedRows) {
		return ROSUndo{}, routerError(messageIPv6DNSPreviewChanged)
	}
	r := findRow(rows, op.ID)
	if r == nil || !r.is("interface", iface) || !ipv6Enabled(r) || !slices.Contains([]string{"yes", "self"}, r.text("advertise-dns")) || !reflect.DeepEqual(op.Values, Row{"advertise-dns": "no"}) || !reflect.DeepEqual(d.ownershipSnapshot(rosIPv6ND, r), op.Expected) {
		return ROSUndo{}, routerError(messageIPv6DNSTargetChanged)
	}
	if e := ctx.Err(); e != nil {
		return ROSUndo{}, e
	}
	undo, err := d.transaction(ctx, []ROSChange{{Method: "PATCH", Path: rosIPv6ND, ID: op.ID, Values: Row{"advertise-dns": "no"}, Expected: marshalRow(op.Expected)}})
	if err == nil {
		err = ctx.Err()
	}
	return undo, err
}

// VerifyIPv6DNS checks DHCP and every relevant RA attribute, including no-op plans.
func VerifyIPv6DNS(ctx context.Context, d *RouterOS, check *IPv6DNSCheck) error {
	if check == nil {
		return nil
	}
	state, e := d.ReadDHCPDNS(ctx)
	if e != nil {
		return e
	}
	if e = VerifyDNS(check.Scope, state); e != nil {
		return e
	}
	iface, rows, e := readIPv6DNS(ctx, d, check.Gateway)
	if e != nil {
		return e
	}
	if e = ipv6DNSConstraints(rows, iface); e != nil {
		return e
	}
	if iface != check.Interface || !reflect.DeepEqual(ipv6DNSEntries(d, rows), check.Entries) {
		return routerError(messageIPv6DNSReadback)
	}
	for _, r := range ipv6DNSEffective(rows, iface) {
		if !r.is("advertise-dns", "no") {
			return routerError(messageIPv6DNSStillOn)
		}
	}
	return nil
}
