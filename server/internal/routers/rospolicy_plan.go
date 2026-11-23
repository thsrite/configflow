package routers

import (
	"configflow/internal/jsondecode"
	"configflow/internal/routers/device"
	"configflow/internal/routers/ros"
	"configflow/internal/routers/routeedit"
	"encoding/json"
	"fmt"
	"net/netip"
	"slices"
	"strings"

	"configflow/internal/netipx"
)

func policyNATMigrations(live ROSPolicySnapshot, exclusions []netip.Prefix, owned map[[2]string]bool) ([]ROSResource, []ROSReplacement, error) {
	migrations, replacements := []ROSResource{}, []ROSReplacement{}
	if len(exclusions) == 0 {
		return migrations, replacements, nil
	}
	exclude := []string{}
	for _, p := range exclusions {
		exclude = append(exclude, p.String())
	}
	for _, row := range live[ros.PathNAT] {
		if !row.Enabled() || !row.Is("chain", "srcnat") || !row.Is("action", "accept") || !device.IsManaged(row.Text("comment")) || strings.HasPrefix(row.Text("comment"), ROSPolicyPrefix) || owned[[2]string{ros.PathNAT, row.ID()}] {
			continue
		}
		if row.Is("comment", NATListComment) && row.Is("src-address-list", NATSrcList) && row.Is("dst-address-list", NATDstList) {
			entries, err := NATPolicyMigrations(row, live[ros.PathAddresses], exclude)
			if err != nil {
				return nil, nil, err
			}
			migrations = append(migrations, entries...)
			continue
		}
		selectors := [][]netip.Prefix{}
		for _, key := range []string{"dst-address", "dst-address-list"} {
			value := row.Text(key)
			if value == "" {
				continue
			}
			if strings.HasPrefix(value, "!") {
				return nil, nil, device.Error(messagePolicyNATInverse + row.ID())
			}
			values := []string{value}
			if key == "dst-address-list" {
				values = []string{}
				for _, entry := range live[ros.PathAddresses] {
					if entry.Enabled() && entry.Is("list", value) {
						values = append(values, entry.Text("address"))
					}
				}
			}
			networks := []netip.Prefix{}
			for _, value := range values {
				parsed := device.Networks([]string{value})
				if len(parsed) == 0 {
					return nil, nil, device.Error(messagePolicyNATInvalid + row.ID())
				}
				networks = append(networks, parsed...)
			}
			selectors = append(selectors, networks)
		}
		destinations := []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0")}
		if len(selectors) > 0 {
			destinations = selectors[0]
		}
		if len(selectors) == 2 {
			destinations = []netip.Prefix{}
			for _, a := range selectors[0] {
				for _, b := range selectors[1] {
					if a.Overlaps(b) {
						if ros.SubnetOf(a, b) {
							destinations = append(destinations, a)
						} else {
							destinations = append(destinations, b)
						}
					}
				}
			}
		}
		remaining := SubtractNetworks(destinations, exclusions)
		collapsed, _ := netipx.Collapse(destinations)
		if slices.Equal(remaining, collapsed) {
			continue
		}
		if row.Set("dst-address-list") {
			return nil, nil, device.Error(messagePolicyNATListSplit + row.ID())
		}
		if row.Flag("dynamic") {
			return nil, nil, device.Error(messagePolicyNATDynamic + row.ID())
		}
		if row.ID() == "" {
			return nil, nil, device.Error(messagePolicyDuplicateID)
		}
		migrations = append(migrations, ros.NewResource(ros.PathNAT, row))
		values := ros.ConfigROSRow(row)
		delete(values, "dynamic")
		for _, prefix := range remaining {
			replacement := ros.CloneRow(values)
			replacement["dst-address"] = prefix.String()
			replacement["place-before"] = row.ID()
			replacements = append(replacements, ROSReplacement{ros.PathNAT, replacement})
		}
	}
	return migrations, replacements, nil
}

// BuildROSPolicy plans a single guarded replacement and records complete stable
// evidence even for a no-op. The caller owns serialization under the device lock.
func BuildROSPolicy(desired *Desired, state *RouterState) *ROSPolicyPlan {
	request := desired.ROSPolicy
	result := &ROSPolicyPlan{Ops: []Op{}, Blockers: []string{}, Warnings: append([]string{}, request.Warnings...), Items: []ROSPolicyItem{}}
	if request.Error != "" {
		result.Blockers = append(result.Blockers, request.Error)
	}
	if state.ROSPolicy == nil {
		result.Blockers = append(result.Blockers, messagePolicyUnreadable)
		return result
	}
	live := *state.ROSPolicy
	selected := len(request.Selected) > 0
	owned, err := OwnedROSPolicyRows(request.Previous, &live)
	if err == nil {
		err = checkPolicyReferences(owned, live)
	}
	if err == nil && selected {
		err = checkPolicyOrder(live)
		if err == nil {
			err = policyHealth(request.Previous, live)
		}
	}
	if err != nil {
		result.Blockers = append(result.Blockers, err.Error())
		return result
	}
	ownedKeys := policyKeys(owned)
	otherMangle := []Row{}
	for _, row := range live[ros.PathMangle] {
		if !ownedKeys[[2]string{ros.PathMangle, row.ID()}] {
			otherMangle = append(otherMangle, row)
		}
	}
	if selected {
		if slices.ContainsFunc(live[ros.PathFilter], func(row Row) bool { return row.Enabled() && row.Is("action", "fasttrack-connection") }) {
			result.Blockers = append(result.Blockers, messagePolicyFastTrack)
		}
		if slices.ContainsFunc(otherMangle, conflictingPolicyMangle) {
			result.Blockers = append(result.Blockers, messagePolicyMangleConflict)
		}
		if slices.ContainsFunc(live["/ip/vrf"], func(row Row) bool { return row.Enabled() && !row.Is("name", "main") }) {
			result.Blockers = append(result.Blockers, messagePolicyVRF)
		}
	}
	statics, policies := []netip.Prefix{}, []netip.Prefix{}
	for _, item := range desired.ProxyCIDRs {
		if p, ok := netipx.ParseNetwork4(item.CIDR, true); ok {
			statics = append(statics, p)
		}
	}
	for _, item := range request.CIDRs {
		if p, ok := netipx.ParseNetwork4(item.CIDR, true); ok {
			policies = append(policies, p)
		} else {
			result.Blockers = append(result.Blockers, messagePolicyGatewayInvalid)
		}
	}
	staticOverlap, policyOverlap := ROSPolicyNetworkOverlap(statics), ROSPolicyNetworkOverlap(policies)
	policyOnly := SubtractNetworks(policies, statics)
	policyOnlyOverlap := ROSPolicyNetworkOverlap(policyOnly)
	if slices.ContainsFunc(policies, staticOverlap) {
		result.Warnings = append(result.Warnings, messagePolicyStaticOverlap)
	}
	migrations := []ROSResource{}
	preserved := 0
	for _, row := range live[ros.PathRoutes] {
		if device.FirstNonEmpty(row.Text("routing-table"), "main") != "main" || !row.Enabled() {
			continue
		}
		prefix, ok := netipx.ParseNetwork4(row.Text("dst-address"), true)
		if !ok || !policyOverlap(prefix) {
			continue
		}
		if device.IsManaged(row.Text("comment")) && !strings.HasPrefix(row.Text("comment"), ROSPolicyPrefix) && row.Is("gateway", desired.ProxyGateway) && policyOnlyOverlap(prefix) && !row.Flag("dynamic") {
			if routeedit.EditROSEntry(row, nil).Editable {
				migrations = append(migrations, ros.NewResource(ros.PathRoutes, row))
				continue
			}
			result.Blockers = append(result.Blockers, messagePolicyRouteMigration+row.ID())
		}
		if prefix.Bits() > 0 {
			preserved++
		}
	}
	if preserved > 0 {
		result.Warnings = append(result.Warnings, fmt.Sprintf(messagePolicyPreservedRoutes, preserved))
	}
	natMigrations, replacements, err := policyNATMigrations(live, policyOnly, ownedKeys)
	if err != nil {
		result.Blockers = append(result.Blockers, err.Error())
		return result
	}
	migrations = append(migrations, natMigrations...)
	spec := &ROSPolicySpec{Sources: append([]string{}, request.Sources...), Destinations: []string{}, Gateway: desired.ProxyGateway, NamingVersion: 1, PolicyVersion: 2}
	for _, entry := range request.CIDRs {
		spec.Destinations = append(spec.Destinations, entry.CIDR)
	}
	if selected && len(spec.Destinations) == 0 {
		result.Blockers = append(result.Blockers, messagePolicyNoDestinations)
	}
	proxyState := *state
	if state.NatContext != nil {
		context := *state.NatContext
		context.Mangle = append([]Row{}, otherMangle...)
		moving := map[string]bool{}
		for _, item := range migrations {
			if item.Path == ros.PathRoutes {
				moving[item.ID] = true
			}
		}
		context.Routes = slices.DeleteFunc(slices.Clone(context.Routes), func(row Row) bool { return moving[row.ID()] })
		proxyState.NatContext = &context
	}
	index := ros.NewRouteEgressIndex(proxyState.NatContext)
	needNAT := false
	iface := ""
	if selected && len(spec.Destinations) > 0 {
		var reason string
		iface, reason = ros.Egress(spec.Destinations[0], spec.Gateway, proxyState.NatContext, true, index)
		if iface == "" {
			result.Blockers = append(result.Blockers, messagePolicyLANUnknown+reason)
		}
		for _, cidr := range spec.Destinations {
			assessment := ros.AssessNAT(cidr, spec.Gateway, proxyState.NatContext, true, index)
			if assessment.Status == "unknown" {
				result.Blockers = append(result.Blockers, cidr+"："+assessment.Detail)
				break
			}
			needNAT = needNAT || assessment.Status == "missing"
		}
	}
	spec.EgressInterface = iface
	if needNAT {
		spec.NATInterface = iface
	}
	previous, err := policyManifest(request.Previous)
	if err != nil {
		result.Blockers = append(result.Blockers, err.Error())
		return result
	}
	current := false
	if previous != nil {
		// Compare the stored raw spec too: unknown fields cannot silently disappear.
		var raw struct {
			Spec json.RawMessage `json:"spec"`
		}
		_ = jsondecode.Unmarshal(request.Previous, &raw)
		current = ros.JSONValuesEqual(raw.Spec, device.JSON(spec)) && len(owned) == len(previous.Resources)
	}
	unchanged := previous == nil
	if selected {
		unchanged = current && len(migrations) == 0
	}
	for _, entry := range request.CIDRs {
		status := "pending"
		if unchanged {
			status = "synced"
		}
		result.Items = append(result.Items, ROSPolicyItem{entry, status, messagePolicyItemDetail, []string{}, "policy"})
	}
	result.Evidence = device.JSON(stablePolicy(live))
	requestCopy := *request
	requestCopy.Previous = nil
	var req map[string]json.RawMessage
	_ = json.Unmarshal(device.JSON(requestCopy), &req)
	delete(req, "previous")
	result.Request = device.JSON(req)
	result.Check = &ROSPolicyCheck{nil, slices.Clone(request.Previous), migrations, replacements}
	if selected {
		result.Check.Spec = spec
	}
	if unchanged || len(result.Blockers) > 0 {
		return result
	}
	bundle := []ROSReplacement{}
	if selected {
		values := map[string]bool{}
		for _, rows := range live {
			for _, row := range rows {
				for _, value := range row {
					values[strings.TrimLeft(value, "!")] = true
				}
			}
		}
		names := map[string]string{}
		for group := 1; group <= 256; group++ {
			names = map[string]string{"src": fmt.Sprintf("CF_Source_Proxy_IP_%02d", group), "dst": fmt.Sprintf("CF_Destination_Proxy_IP_%02d", group), "bypass": fmt.Sprintf("CF_Bypass_IP_%02d", group), "routing": fmt.Sprintf("CF_Proxy_Routing_%02d", group)}
			occupied := false
			for _, name := range names {
				if values[name] {
					occupied = true
				}
				for value := range values {
					if strings.Contains(value, "@"+name) {
						occupied = true
					}
				}
			}
			if !occupied {
				break
			}
			if group == 256 {
				result.Blockers = append(result.Blockers, messagePolicyNamesExhausted)
				return result
			}
		}
		add := func(path string, row Row) { bundle = append(bundle, ROSReplacement{path, row}) }
		for _, kind := range []string{"src", "dst", "bypass"} {
			addresses := spec.Sources
			if kind == "dst" {
				addresses = spec.Destinations
			}
			if kind == "bypass" {
				addresses = []string{spec.Gateway}
			}
			for _, address := range addresses {
				add(ros.PathAddresses, Row{"list": names[kind], "address": address, "comment": request.Owner, "disabled": "false"})
			}
		}
		name := names["routing"]
		common := func(row Row) Row { row["comment"] = request.Owner; row["disabled"] = "false"; return row }
		add(ros.PathTables, Row{"name": name, "fib": "true"})
		add(ros.PathRoutes, common(Row{"dst-address": "0.0.0.0/0", "gateway": spec.Gateway + "@main", "routing-table": name}))
		if needNAT {
			add(ros.PathNAT, common(Row{"chain": "srcnat", "action": "accept", "src-address-list": names["src"], "dst-address-list": names["dst"], "out-interface": iface}))
		}
		add(ros.PathMangle, common(Row{"chain": name, "action": "return", "src-address-list": names["bypass"]}))
		add(ros.PathMangle, common(Row{"chain": name, "action": "mark-routing", "new-routing-mark": name, "passthrough": "false"}))
		add(ros.PathMangle, common(Row{"chain": "prerouting", "action": "jump", "src-address-list": names["src"], "dst-address-list": names["dst"], "dst-address-type": "!local", "jump-target": name}))
	}
	summary := messagePolicyWithdraw
	if len(bundle) > 0 {
		summary = fmt.Sprintf(messagePolicyPublish, len(spec.Sources), len(spec.Destinations), spec.Gateway)
	}
	if needNAT {
		summary += messagePolicyNATSummary
	}
	if len(migrations) > 0 {
		counts := map[string]int{}
		for _, item := range migrations {
			counts[item.Path]++
		}
		summary += fmt.Sprintf(messagePolicyMigrationSummary, counts[ros.PathRoutes], counts[ros.PathNAT])
		if counts[ros.PathAddresses] > 0 {
			summary += fmt.Sprintf(messagePolicyMemberSummary, counts[ros.PathAddresses])
		}
	}
	op := &ROSPolicyReplace{OpHead: OpHead{Kind: "ros_policy_replace", Summary: summary}, Bundle: bundle, Previous: slices.Clone(request.Previous), Owned: owned, Migrations: migrations, Replacements: replacements, Evidence: stablePolicy(live), StaticChecks: []CIDRGateway{}}
	if len(bundle) > 0 {
		op.Spec = spec
	}
	result.Ops = append(result.Ops, op)
	return result
}

func conflictingPolicyMangle(row Row) bool {
	return row.Enabled() && row.Is("chain", "prerouting") && slices.Contains([]string{"mark-routing", "route", "jump", "accept", "return"}, row.Text("action"))
}

// natPolicyEvidence derives shared migration proofs from the policy plan;
// there is no independently populated NAT copy of the policy evidence.
func (state *RouterState) natPolicyEvidence() *NATPolicyEvidence {
	if state.ROSPolicyPlan == nil || state.ROSPolicyPlan.Check == nil {
		return nil
	}
	check := state.ROSPolicyPlan.Check
	proof := &NATPolicyEvidence{Migrations: check.Migrations, Replacements: check.Replacements}
	if check.Spec != nil {
		proof.Destinations = check.Spec.Destinations
	}
	if previous, err := policyManifest(check.Previous); err == nil && previous != nil {
		proof.PreviousResources = previous.Resources
		if previous.Spec != nil {
			proof.PreviousDestinations = previous.Spec.Destinations
		}
	}
	return proof
}

func nestedNATMigration(op Op) (merged, stale int) {
	policy, ok := op.(*ROSPolicyReplace)
	if !ok {
		return
	}
	for _, inner := range policy.PrepareStatic {
		if sync, ok := inner.(*NATListSync); ok {
			merged += len(sync.Legacy)
			stale += len(sync.Stale)
		} else {
			a, b := nestedNATMigration(inner)
			merged += a
			stale += b
		}
	}
	return
}

func policyReportPart(desired *Desired, state *RouterState) ReportPart {
	plan := BuildROSPolicy(desired, state)
	state.ROSPolicyPlan = plan
	// Report's summary counts a withdrawal with no destination items as pending.
	statuses := []string{}
	for _, item := range plan.Items {
		statuses = append(statuses, item.Status)
	}
	for range plan.Blockers {
		statuses = append(statuses, "unknown")
	}
	if len(plan.Ops) > 0 && len(plan.Items) == 0 {
		statuses = append(statuses, "pending")
	}
	retiring := []string{}
	state.PolicyNATRetiring = nil
	if plan.Check != nil {
		for _, item := range plan.Check.Migrations {
			if item.Path == ros.PathRoutes {
				retiring = append(retiring, item.ID)
			}
		}
	}
	for _, operation := range plan.Ops {
		if op, ok := operation.(*ROSPolicyReplace); ok {
			for _, item := range op.Owned {
				if item.Path == ros.PathNAT {
					state.PolicyNATRetiring = append(state.PolicyNATRetiring, item.ID)
				}
			}
		}
	}
	section := struct {
		Enabled  bool            `json:"enabled"`
		Sources  []string        `json:"sources"`
		Items    []ROSPolicyItem `json:"items"`
		Blockers []string        `json:"blockers"`
		Warnings []string        `json:"warnings"`
	}{len(desired.ROSPolicy.Selected) > 0, desired.ROSPolicy.Sources, plan.Items, plan.Blockers, plan.Warnings}
	return ReportPart{Key: "policy_routing", Section: section, Statuses: statuses, InSync: len(plan.Ops) == 0 && len(plan.Blockers) == 0, RetireExtra: retiring}
}
