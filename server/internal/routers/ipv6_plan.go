package routers

import (
	"configflow/internal/jsondecode"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"configflow/internal/agents/diag"
	"configflow/internal/devhttp"
	"configflow/internal/model"
)

// IPv6Resource proves ownership by device ID and complete stable configuration.
type IPv6Resource struct {
	Path   string `json:"path"`
	ID     string `json:"id"`
	Config Row    `json:"config"`
}

// IPv6Borrowed records a correct foreign route that must never be deleted.
type IPv6Borrowed struct {
	ID     string `json:"id"`
	Config Row    `json:"config"`
}

// IPv6FakeIPManifest is the persisted single-route publication.
type IPv6FakeIPManifest struct {
	Version   int            `json:"version"`
	Spec      *IPv6RouteSpec `json:"spec"`
	Resources []IPv6Resource `json:"resources"`
}

// IPv6RoutesManifest is the persisted rule-set publication.
type IPv6RoutesManifest struct {
	Version   int            `json:"version"`
	Gateway   string         `json:"gateway"`
	Resources []IPv6Resource `json:"resources"`
}

// IPv6RouteItem explains a destination's state and the action previewed for it.
type IPv6RouteItem struct {
	IPv6RouteSpec
	Sources *[]string `json:"sources,omitempty"`
	Status  string    `json:"status"`
	Detail  string    `json:"detail"`
}

// IPv6FakeIPCheck is the post-release proof, including a borrowed foreign route.
type IPv6FakeIPCheck struct {
	Spec     *IPv6RouteSpec  `json:"spec"`
	Borrowed *IPv6Borrowed   `json:"borrowed,omitempty"`
	Previous json.RawMessage `json:"previous,omitempty"`
	Removed  *[]string       `json:"removed,omitempty"`
}

// IPv6FakeIPReport is the user-visible portion of a Fake-IP plan.
type IPv6FakeIPReport struct {
	Enabled    bool            `json:"enabled"`
	Status     string          `json:"status"`
	Items      []IPv6RouteItem `json:"items"`
	Blockers   []string        `json:"blockers"`
	Warnings   []string        `json:"warnings"`
	FixCommand string          `json:"fix_command"`
}

// IPv6FakeIPPlan includes the preview evidence and executable typed operations.
type IPv6FakeIPPlan struct {
	IPv6FakeIPReport
	Ops      []Op            `json:"ops"`
	Check    IPv6FakeIPCheck `json:"check"`
	Evidence IPv6Snapshot    `json:"evidence"`
}

// IPv6FakeIPReplace is an exact previewed single-route transaction.
type IPv6FakeIPReplace struct {
	OpHead
	Previous json.RawMessage `json:"previous"`
	Owned    []IPv6Resource  `json:"owned"`
	Spec     *IPv6RouteSpec  `json:"spec"`
	Values   Row             `json:"values"`
	Expected IPv6Snapshot    `json:"expected"`
}

// IPv6FakeIPUndo reverses a completed transaction and restores prior ownership.
type IPv6FakeIPUndo struct {
	Kind     string          `json:"kind"`
	Changes  []ROSChange     `json:"changes"`
	Previous json.RawMessage `json:"previous"`
}

func ipv6Owned(raw json.RawMessage, live IPv6Snapshot, fake bool) ([]IPv6Resource, error) {
	invalid, entryError, duplicate, dynamic := messageIPv6RoutesManifest, messageIPv6RoutesEntry, messageIPv6RoutesDuplicate, messageIPv6RoutesDynamic
	if fake {
		invalid, entryError, duplicate, dynamic = messageIPv6FakeManifest, messageIPv6FakeEntry, messageIPv6FakeDuplicate, messageIPv6FakeDynamic
	}
	out := []IPv6Resource{}
	if len(raw) == 0 || string(raw) == "null" {
		return out, nil
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return nil, routerError(invalid)
	}
	if len(fields) == 0 {
		return out, nil
	}
	var version int
	var entries []IPv6Resource
	if json.Unmarshal(fields["version"], &version) != nil || version != 1 || !strings.HasPrefix(strings.TrimSpace(string(fields["resources"])), "[") || devhttp.DecodeJSON(fields["resources"], &entries) != nil {
		return nil, routerError(invalid)
	}
	var spec *IPv6RouteSpec
	if fake {
		if len(entries) > 1 || jsondecode.Unmarshal(fields["spec"], &spec) != nil || spec == nil || !ipv6ValidSpec(spec) {
			return nil, routerError(invalid)
		}
	} else {
		var gateway string
		if json.Unmarshal(fields["gateway"], &gateway) != nil || string(fields["gateway"]) == "null" {
			return nil, routerError(invalid)
		}
	}
	rows := map[string][]Row{}
	for _, row := range live["routes"] {
		rows[row.id()] = append(rows[row.id()], row)
	}
	seen := map[string]bool{}
	for _, entry := range entries {
		if entry.Path != rosIPv6Routes || !rosIDPattern.MatchString(entry.ID) || entry.Config == nil || (!fake && ipv6Key(entry.Config) == "") {
			return nil, routerError(entryError)
		}
		if seen[entry.ID] || len(rows[entry.ID]) > 1 {
			return nil, routerError(duplicate)
		}
		seen[entry.ID] = true
		// Check manifest self-consistency even if the recorded object disappeared.
		if fake && (ipv6Key(entry.Config) != spec.CIDR || entry.Config.text("gateway") != spec.Gateway) {
			return nil, routerError(messageIPv6FakeSpec)
		}
		if len(rows[entry.ID]) == 0 {
			continue
		}
		row := rows[entry.ID][0]
		if !sameRow(StableIPv6Row(row), entry.Config) {
			if fake {
				return nil, routerError(messageIPv6FakeExternal)
			}
			return nil, routerError(fmt.Sprintf(messageIPv6RoutesExternal, entry.Config.text("dst-address")))
		}
		if policyFlag(row["dynamic"]) || policyFlag(row["connect"]) {
			return nil, routerError(dynamic)
		}
		out = append(out, entry)
	}
	return out, nil
}

func ipv6Targets(live IPv6Snapshot, cidr string, ignore map[string]bool) []Row {
	target, ok := ipv6Network(cidr)
	if !ok {
		return nil
	}
	out := []Row{}
	for _, row := range live["routes"] {
		if ignore[row.id()] || rosTextDefault(row["routing-table"], "main") != "main" {
			continue
		}
		p, ok := ipv6Network(row.text("dst-address"))
		if ok && (p == target || ipv6Enabled(row) && p.Bits() > target.Bits() && p.Overlaps(target)) {
			out = append(out, row)
		}
	}
	return out
}
func ipv6Hairpin(ctx context.Context, d *RouterOS, lan, cidr string) (string, string) {
	hint := CheckForwardHint(ctx, d, 6, lan, []string{cidr}, nil)
	if hint.Status == "unknown" {
		return messageIPv6HairpinUnread, ""
	}
	if hint.Status == "drop" {
		return ForwardDropWarning(6), hint.Command
	}
	return "", ""
}
func ipv6AgentEnabled(agent *model.Agent) bool {
	return agent != nil && agent.ServiceType == "mihomo" && model.SwitchEnabled(agent, "enabled", agent.Enabled, true)
}
func ipv6AgentHost(agent *model.Agent) string {
	if agent == nil {
		return ""
	}
	return agent.Host
}
func ipv6LocalOverlap(live IPv6Snapshot, cidr string) bool {
	target, ok := ipv6Network(cidr)
	if !ok {
		return true
	}
	for _, row := range live["addresses"] {
		if p, ok := ipv6Network(row.text("address")); ok && p.Overlaps(target) {
			return true
		}
	}
	return false
}
func ipv6FakeValues(r *Router, spec *IPv6RouteSpec) Row {
	if spec == nil {
		return nil
	}
	return Row{"dst-address": spec.CIDR, "gateway": spec.Gateway, "routing-table": "main", "distance": "1", "disabled": "false", "comment": "cf-fakeip6:" + r.ID}
}

// InspectIPv6FakeIP plans additions/withdrawal, borrowing correct foreign routes.
// This only reads devices and assigns a private copy of the stored driver manifest.
func InspectIPv6FakeIP(ctx context.Context, config *model.Profile, r *Router, agent *model.Agent, host IPv6Host, d *RouterOS) *IPv6FakeIPPlan {
	s := IPv6FakeIPSettings(config)
	previous := guardRaw(r.Extra["ipv6_fakeip_state"])
	d.SetIPv6FakeIPManifest(previous)
	plan := &IPv6FakeIPPlan{IPv6FakeIPReport: IPv6FakeIPReport{Enabled: s.Enabled, Status: "disabled", Items: []IPv6RouteItem{}, Blockers: []string{}, Warnings: []string{}}, Ops: []Op{}, Evidence: IPv6Snapshot{}}
	if s.Error != "" {
		plan.Blockers = append(plan.Blockers, s.Error)
	}
	live, err := SnapshotIPv6(ctx, d, s.Enabled)
	var owned []IPv6Resource
	if err == nil {
		plan.Evidence = IPv6Evidence(live)
		owned, err = ipv6Owned(previous, live, true)
	}
	if err != nil {
		plan.Blockers = append(plan.Blockers, err.Error())
		plan.Status = "unknown"
		return plan
	}
	if len(plan.Blockers) > 0 {
		plan.Status = "unknown"
		return plan
	}
	var spec *IPv6RouteSpec
	var borrowed *IPv6Borrowed
	good := false
	if s.Enabled {
		if !ipv6AgentEnabled(agent) {
			plan.Blockers = append(plan.Blockers, messageIPv6Agent)
		} else if host.Error != "" {
			plan.Blockers = append(plan.Blockers, messageIPv6Host+ipv6HostError(d, agent, host.Error))
		}
		lan := IPv6LAN(live, ipv6AgentHost(agent))
		existing := []Row{}
		for _, row := range live["routes"] {
			if ipv6Key(row) == s.CIDR {
				existing = append(existing, row)
			}
		}
		gateway := IPv6Gateway(host, live, lan, existing)
		if gateway == "" {
			plan.Blockers = append(plan.Blockers, messageIPv6FakeGateway)
		} else {
			warning, command := ipv6Hairpin(ctx, d, lan, s.CIDR)
			plan.FixCommand = command
			if warning != "" {
				plan.Warnings = append(plan.Warnings, warning)
			}
		}
		if ipv6LocalOverlap(live, s.CIDR) {
			plan.Blockers = append(plan.Blockers, messageIPv6FakeOverlap)
		}
		spec = &IPv6RouteSpec{s.CIDR, gateway}
		ids := map[string]bool{}
		for _, e := range owned {
			ids[e.ID] = true
		}
		candidates := ipv6Targets(live, s.CIDR, ids)
		if len(candidates) > 0 {
			if len(candidates) == 1 && gateway != "" && ipv6Routes(candidates[0], *spec) {
				row := candidates[0]
				borrowed = &IPv6Borrowed{row.id(), StableIPv6Row(row)}
				if !rosIDPattern.MatchString(row.id()) || policyFlag(row["dynamic"]) || policyFlag(row["connect"]) {
					plan.Blockers = append(plan.Blockers, messageIPv6FakeStatic)
				}
			} else {
				plan.Blockers = append(plan.Blockers, messageIPv6FakeConflict)
			}
		}
		good = len(owned) == 1 && ipv6Correct(findRow(live["routes"], owned[0].ID), *spec)
		if len(owned) > 0 && borrowed != nil {
			plan.Blockers = append(plan.Blockers, messageIPv6FakeBoth)
		}
		status, detail := "missing", messageIPv6FakeMissing
		switch {
		case len(plan.Blockers) > 0:
			status, detail = "conflict", strings.Join(plan.Blockers, "；")
		case good:
			status, detail = "synced", messageIPv6FakeSynced
		case borrowed != nil:
			status, detail = "borrowed", messageIPv6FakeBorrowed
		}
		plan.Items = append(plan.Items, IPv6RouteItem{IPv6RouteSpec: *spec, Status: status, Detail: detail})
		plan.Status = status
		// Borrowed null is explicitly present in the enabled check contract.
		plan.Check = IPv6FakeIPCheck{Spec: spec, Borrowed: borrowed, Previous: previous}
	} else {
		removed := []string{}
		for _, e := range owned {
			removed = append(removed, e.ID)
		}
		plan.Check = IPv6FakeIPCheck{Removed: &removed}
		if hasStoredState(previous) {
			plan.Status = "pending"
			var manifest IPv6FakeIPManifest
			_ = jsondecode.Unmarshal(previous, &manifest)
			if manifest.Spec != nil {
				plan.Items = []IPv6RouteItem{{IPv6RouteSpec: *manifest.Spec, Status: "pending", Detail: messageIPv6FakeWithdraw}}
			}
		}
	}
	if len(plan.Blockers) == 0 && (hasStoredState(previous) || spec != nil) && !good && borrowed == nil {
		summary := messageIPv6FakeRemoveSummary
		if spec != nil {
			summary = fmt.Sprintf(messageIPv6FakeAddSummary, spec.CIDR, spec.Gateway)
		}
		plan.Ops = append(plan.Ops, &IPv6FakeIPReplace{OpHead: OpHead{"ipv6_fakeip_replace", summary}, Previous: previous, Owned: owned, Spec: spec, Values: ipv6FakeValues(r, spec), Expected: plan.Evidence})
	}
	return plan
}

// MarshalJSON preserves borrowed:null in an enabled check and removed:[] on withdrawal.
func (c IPv6FakeIPCheck) MarshalJSON() ([]byte, error) {
	type plain IPv6FakeIPCheck
	if c.Spec != nil {
		return json.Marshal(struct {
			plain
			Borrowed *IPv6Borrowed `json:"borrowed"`
		}{plain(c), c.Borrowed})
	}
	return json.Marshal(plain(c))
}

// IPv6RoutesCheck proves active owned destinations and immutable borrowed rows.
type IPv6RoutesCheck struct {
	Gateway  string         `json:"gateway"`
	Active   []string       `json:"active"`
	Borrowed []IPv6Borrowed `json:"borrowed"`
	Removed  []string       `json:"removed"`
}

// IPv6RoutesReport is the visible rule-set route status; skips do not block IPv4.
type IPv6RoutesReport struct {
	Enabled    bool            `json:"enabled"`
	Status     string          `json:"status"`
	Gateway    string          `json:"gateway"`
	Items      []IPv6RouteItem `json:"items"`
	Skip       []string        `json:"skip"`
	Warnings   []string        `json:"warnings"`
	FixCommand string          `json:"fix_command"`
}

// IPv6RoutesPlan contains the scoped preview and automatic-sync eligibility reason.
type IPv6RoutesPlan struct {
	IPv6RoutesReport
	Ops      []Op             `json:"ops"`
	Check    *IPv6RoutesCheck `json:"check"`
	Evidence IPv6Snapshot     `json:"evidence"`
	Auto     string           `json:"auto"`
}

// IPv6RouteAdd adds an ordinary IPv6 static route through the shared batch driver.
type IPv6RouteAdd struct {
	Kind    string `json:"kind"`
	CIDR    string `json:"cidr"`
	Gateway string `json:"gateway"`
	Comment string `json:"comment"`
}

// IPv6RouteDelete removes one manifest-proven static route.
type IPv6RouteDelete struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
	CIDR string `json:"cidr"`
}

// IPv6RoutesReplace is the immutable scoped transaction prepared by inspection.
type IPv6RoutesReplace struct {
	OpHead
	Previous json.RawMessage   `json:"previous"`
	Owned    []IPv6Resource    `json:"owned"`
	Adds     []IPv6RouteAdd    `json:"adds"`
	Deletes  []IPv6RouteDelete `json:"deletes"`
	Retained []string          `json:"retained"`
	Gateway  string            `json:"gateway"`
	Scope    []string          `json:"scope"`
	Expected IPv6Snapshot      `json:"expected"`
}

// IPv6RoutesUndo reverses the shared batch journals and restores prior ownership.
type IPv6RoutesUndo struct {
	Kind     string            `json:"kind"`
	Undos    []json.RawMessage `json:"undos"`
	Previous json.RawMessage   `json:"previous"`
}

func ipv6Scoped(live IPv6Snapshot, cidrs, ids map[string]bool) IPv6Snapshot {
	out := IPv6Snapshot{"routes": {}, "addresses": append([]Row{}, live["addresses"]...), "ipv4_addresses": append([]Row{}, live["ipv4_addresses"]...)}
	for _, row := range live["routes"] {
		if cidrs[ipv6Key(row)] || ids[row.id()] {
			out["routes"] = append(out["routes"], row)
		}
	}
	return out
}
func ipv6ResourcesEqual(a, b []IPv6Resource) bool { return reflect.DeepEqual(a, b) }

// InspectIPv6Routes plans public rule-set destinations. Unsafe/incomplete host
// information skips this stage and preserves its ownership instead of blocking IPv4.
func InspectIPv6Routes(ctx context.Context, config *model.Profile, r *Router, desired *Desired, agent *model.Agent, host IPv6Host, d *RouterOS) *IPv6RoutesPlan {
	s := IPv6RoutesSettings(config)
	previous := guardRaw(r.Extra["ipv6_routes_state"])
	d.SetIPv6RoutesManifest(previous)
	plan := &IPv6RoutesPlan{IPv6RoutesReport: IPv6RoutesReport{Enabled: s.Enabled, Status: "disabled", Items: []IPv6RouteItem{}, Skip: []string{}, Warnings: []string{}}, Ops: []Op{}, Evidence: IPv6Snapshot{}}
	wanted := []ProxyCIDR{}
	if desired != nil {
		wanted = desired.IPv6CIDRs
	}
	item := func(cidr string, sources []string, gateway, status, detail string) {
		src := append([]string{}, sources...)
		plan.Items = append(plan.Items, IPv6RouteItem{IPv6RouteSpec: IPv6RouteSpec{cidr, gateway}, Sources: &src, Status: status, Detail: detail})
	}
	skip := func(reason string) *IPv6RoutesPlan {
		plan.Skip = append(plan.Skip, reason)
		plan.Status = "unknown"
		for _, v := range wanted {
			item(v.CIDR, v.Sources, "", "unknown", messageIPv6RoutesSkipDetail)
		}
		return plan
	}
	if s.Error != "" {
		return skip(s.Error)
	}
	// Desired normally comes from ComputeDesired; refuse malformed caller input.
	seenWanted := map[string]bool{}
	for _, v := range wanted {
		p, ok := ipv6Network(v.CIDR)
		if !ok || p.String() != v.CIDR || seenWanted[v.CIDR] {
			return skip(messageOperationFormat)
		}
		seenWanted[v.CIDR] = true
	}
	if s.Enabled && len(wanted) > 0 {
		if !s.TUNEnabled {
			return skip(messageIPv6RoutesTUN)
		}
		if !ipv6AgentEnabled(agent) {
			return skip(messageIPv6RoutesAgent)
		}
		if host.Error != "" {
			return skip(messageIPv6Host + ipv6HostError(d, agent, host.Error))
		}
	}
	live, err := SnapshotIPv6(ctx, d, true)
	var owned []IPv6Resource
	if err == nil {
		owned, err = ipv6Owned(previous, live, false)
	}
	if err != nil {
		return skip(err.Error())
	}
	rowsByID := map[string]Row{}
	for _, row := range live["routes"] {
		rowsByID[row.id()] = row
	}
	mine := map[string][]IPv6Resource{}
	mineOrder := []string{}
	ids := map[string]bool{}
	for _, e := range owned {
		key := ipv6Key(e.Config)
		if _, ok := mine[key]; !ok {
			mineOrder = append(mineOrder, key)
		}
		mine[key] = append(mine[key], e)
		ids[e.ID] = true
	}
	gateway := ""
	if s.Enabled && len(wanted) > 0 {
		lan := IPv6LAN(live, ipv6AgentHost(agent))
		existing := []Row{}
		for _, e := range owned {
			existing = append(existing, rowsByID[e.ID])
		}
		gateway = IPv6Gateway(host, live, lan, existing)
		if gateway == "" {
			return skip(messageIPv6RoutesGateway)
		}
		warning, command := ipv6Hairpin(ctx, d, lan, wanted[0].CIDR)
		plan.FixCommand = command
		if warning != "" {
			plan.Warnings = append(plan.Warnings, warning)
		}
	}
	plan.Gateway = gateway
	adds := []IPv6RouteAdd{}
	deletes, retained := []IPv6Resource{}, []IPv6Resource{}
	active := []string{}
	borrowed := []IPv6Borrowed{}
	replaced, yielded := false, false
	foreignByCIDR := map[string][]Row{}
	for _, row := range live["routes"] {
		if !ids[row.id()] && rosTextDefault(row["routing-table"], "main") == "main" {
			key := ipv6Key(row)
			foreignByCIDR[key] = append(foreignByCIDR[key], row)
		}
	}
	if gateway != "" {
		for _, v := range wanted {
			ours := mine[v.CIDR]
			delete(mine, v.CIDR)
			foreign := foreignByCIDR[v.CIDR]
			switch {
			case ipv6LocalOverlap(live, v.CIDR):
				retained = append(retained, ours...)
				item(v.CIDR, v.Sources, gateway, "conflict", messageIPv6RoutesOverlap)
			case len(foreign) > 0:
				if len(foreign) == 1 && rosIDPattern.MatchString(foreign[0].id()) && ipv6Correct(foreign[0], IPv6RouteSpec{v.CIDR, gateway}) {
					deletes = append(deletes, ours...)
					yielded = yielded || len(ours) > 0
					borrowed = append(borrowed, IPv6Borrowed{foreign[0].id(), StableIPv6Row(foreign[0])})
					item(v.CIDR, v.Sources, gateway, "borrowed", messageIPv6RoutesBorrowed)
				} else {
					retained = append(retained, ours...)
					item(v.CIDR, v.Sources, gateway, "conflict", messageIPv6RoutesConflict)
				}
			case len(ours) == 1 && ipv6Configured(rowsByID[ours[0].ID], gateway):
				retained = append(retained, ours...)
				if policyFlag(rowsByID[ours[0].ID]["active"]) {
					active = append(active, v.CIDR)
					item(v.CIDR, v.Sources, gateway, "synced", messageIPv6RoutesSynced)
				} else {
					item(v.CIDR, v.Sources, gateway, "unknown", messageIPv6RoutesInactive)
				}
			default:
				replaced = replaced || len(ours) > 0
				deletes = append(deletes, ours...)
				adds = append(adds, IPv6RouteAdd{"route_add", v.CIDR, gateway, "cf-rule6:" + r.ID})
				active = append(active, v.CIDR)
				status, detail := "missing", messageIPv6RoutesAdd
				if len(ours) > 0 {
					status, detail = "pending", messageIPv6RoutesReplace
				}
				item(v.CIDR, v.Sources, gateway, status, detail)
			}
		}
	}
	incomplete := s.Enabled && desired != nil && len(desired.BlockingWarnings) > 0
	for _, cidr := range mineOrder {
		entries, ok := mine[cidr]
		if !ok {
			continue
		}
		detail := messageIPv6RoutesWithdraw
		if !s.Enabled {
			detail = messageIPv6RoutesOff
		}
		status := "pending"
		if incomplete {
			retained = append(retained, entries...)
			detail, status = messageIPv6RoutesIncomplete, "unknown"
		} else {
			deletes = append(deletes, entries...)
		}
		item(cidr, nil, entries[0].Config.text("gateway"), status, detail)
	}
	if incomplete && len(mine) > 0 {
		plan.Warnings = append(plan.Warnings, messageIPv6RoutesIncompleteWarning)
	}
	removed, keep := []string{}, []string{}
	for _, e := range deletes {
		removed = append(removed, e.ID)
	}
	for _, e := range retained {
		keep = append(keep, e.ID)
	}
	var prev IPv6RoutesManifest
	_ = jsondecode.Unmarshal(previous, &prev)
	manifestGateway := gateway
	if manifestGateway == "" {
		manifestGateway = prev.Gateway
	}
	scope := map[string]bool{}
	for _, v := range wanted {
		scope[v.CIDR] = true
	}
	for _, e := range owned {
		scope[ipv6Key(e.Config)] = true
	}
	plan.Evidence = IPv6Evidence(ipv6Scoped(live, scope, ids))
	plan.Check = &IPv6RoutesCheck{gateway, active, borrowed, removed}
	statuses := map[string]bool{}
	for _, v := range plan.Items {
		statuses[v.Status] = true
	}
	if len(statuses) > 0 {
		plan.Status = "synced"
	}
	for _, status := range []string{"conflict", "unknown", "missing", "pending"} {
		if statuses[status] {
			plan.Status = status
			break
		}
	}
	remains := len(retained) > 0 || len(adds) > 0
	stale := hasStoredState(previous) && (len(owned) != len(prev.Resources) || !remains)
	if len(adds) > 0 || len(deletes) > 0 || stale || (hasStoredState(previous) && remains && prev.Gateway != manifestGateway) {
		switch {
		case !hasStoredState(previous):
			plan.Auto = messageIPv6RoutesAutoFirst
		case !s.Enabled:
			plan.Auto = messageIPv6RoutesAutoOff
		case replaced || gateway != "" && prev.Gateway != gateway:
			plan.Auto = messageIPv6RoutesAutoGateway
		case len(owned) != len(prev.Resources):
			plan.Auto = messageIPv6RoutesAutoMissing
		case yielded:
			plan.Auto = messageIPv6RoutesAutoYield
		}
		summary := []string{}
		if len(adds) > 0 {
			summary = append(summary, fmt.Sprintf(messageIPv6RoutesSummaryAdd, len(adds)))
		}
		if len(deletes) > 0 {
			summary = append(summary, fmt.Sprintf(messageIPv6RoutesSummaryDelete, len(deletes)))
		}
		if len(summary) == 0 {
			summary = append(summary, messageIPv6RoutesSummaryUpdate)
		}
		text := messageIPv6RoutesSummary + strings.Join(summary, "，")
		if len(adds) > 0 {
			text += fmt.Sprintf(messageIPv6RoutesSummaryGateway, gateway)
		}
		del := []IPv6RouteDelete{}
		for _, e := range deletes {
			del = append(del, IPv6RouteDelete{"route_delete", e.ID, e.Config.text("dst-address")})
		}
		scoped := []string{}
		for cidr := range scope {
			scoped = append(scoped, cidr)
		}
		slices.Sort(scoped)
		plan.Ops = append(plan.Ops, &IPv6RoutesReplace{OpHead: OpHead{"ipv6_routes_replace", text}, Previous: previous, Owned: owned, Adds: adds, Deletes: del, Retained: keep, Gateway: manifestGateway, Scope: scoped, Expected: plan.Evidence})
	}
	return plan
}

func ipv6HostError(d *RouterOS, agent *model.Agent, text string) string {
	secrets := []any{d.password}
	if agent != nil {
		secrets = append(secrets, agent.Token)
	}
	return diag.FailureText(text, secrets, len(text)+128)
}
