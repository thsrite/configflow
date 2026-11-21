package routers

import (
	"bytes"
	"configflow/internal/jsondecode"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/netip"
	"slices"
	"strings"
)

// Fingerprint is the stable Go-native revision of the exact previewed stages
// and evidence. Unknown fields remain evidence; presentation warnings are not.
func Fingerprint(stages []Stage, natEvidence, dhcpEvidence, returnEvidence any) (string, error) {
	b, err := canonicalReleaseJSON(struct {
		Stages []Stage `json:"stages"`
		NAT    any     `json:"nat_evidence"`
		DHCP   any     `json:"dhcp_evidence"`
		Return any     `json:"return_evidence"`
	}{stages, natEvidence, dhcpEvidence, returnEvidence})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8]), nil
}
func canonicalReleaseJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	var tree any
	dec := json.NewDecoder(&buf)
	dec.UseNumber()
	if err := dec.Decode(&tree); err != nil {
		return nil, err
	}
	buf.Reset()
	enc = json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(tree); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}
func stageOf(key string, manual bool, ops []Op) Stage {
	return Stage{Key: key, Title: StageTitles[key], Manual: manual, Ops: ops}
}
func pointer[T any](v T) *T { return &v }
func makeReturnOps(status *ReturnStatus, spec *ReturnSpec) []Op {
	ops := []Op{}
	var previous json.RawMessage
	if status != nil && status.Managed != nil && status.Managed.TablePresent {
		previous = status.Managed.Spec
	}
	if effectful(previous) && sameReturnSpec(previous, spec) {
		return ops
	}
	parts := []string{fmt.Sprintf(messageReturnApply, spec.Mark, spec.Table, spec.RouterIP, spec.Priority), returnScope(spec)}
	var old ReturnSpec
	_ = jsondecode.Unmarshal(previous, &old)
	if spec.SafetyVersion == 2 {
		if effectful(previous) && old.SafetyVersion != 2 {
			parts = append(parts, messageReturnUpgradeScope)
		}
	} else if effectful(previous) {
		added := []string{}
		for _, v := range returnDest(spec) {
			if !slices.Contains(returnDest(&old), v) {
				added = append(added, v)
			}
		}
		slices.Sort(added)
		if len(added) > 0 {
			parts = append(parts, fmt.Sprintf(messageReturnAdded, strings.Join(added, ", ")))
		}
	}
	if len(previous) == 0 {
		previous = json.RawMessage("null")
	}
	return append(ops, ReturnApplyOp{OpHead{"return_routing_apply", strings.Join(parts, "；")}, spec, previous})
}
func cleanupReturnOps(status *ReturnStatus, warnings *[]string) []Op {
	ops := []Op{}
	if status == nil || status.Cleanup == nil {
		return ops
	}
	c := status.Cleanup
	if len(c.SharedWith) > 0 {
		*warnings = append(*warnings, fmt.Sprintf(messageReturnShared, strings.Join(c.SharedWith, "、")))
		return ops
	}
	issues := ReturnSafetyBlockers(&ReturnStatus{Safety: c.Safety}, true)
	if !c.Complete {
		issues = append([]string{messageReturnPending}, issues...)
	}
	if len(issues) > 0 {
		*warnings = append(*warnings, fmt.Sprintf(messageReturnCleanupUnsafe, strings.Join(issues, "；")))
		return ops
	}
	var spec ReturnSpec
	if jsondecode.Unmarshal(c.Spec, &spec) != nil {
		*warnings = append(*warnings, fmt.Sprintf(messageReturnCleanupUnsafe, messageReturnIncomplete))
		return ops
	}
	return append(ops, ReturnRemoveOp{OpHead{"return_routing_remove", fmt.Sprintf(messageReturnRemove, spec.Table, spec.RouterIP, returnScope(&spec))}, false, c.Spec})
}
func proxyTargets(d *Desired, skipped []string) []string {
	out := []string{}
	for _, v := range d.ProxyCIDRs {
		if !slices.Contains(skipped, v.CIDR) {
			out = append(out, v.CIDR)
		}
	}
	return out
}
func natChecks(items []NatItem, gateway string) NATChecks {
	c := NATChecks{Gateway: gateway, NATBypass: []string{}, NATNotRequired: []CIDRGateway{}}
	for _, v := range items {
		if v.Status == "not_required" {
			c.NATNotRequired = append(c.NATNotRequired, CIDRGateway{v.CIDR, gateway})
		} else if v.Status != "unknown" {
			c.NATBypass = append(c.NATBypass, v.CIDR)
		}
	}
	return c
}

// BuildPlan turns one coherent inspection into reviewable operations. It does
// no writes and never mutates the inspection's feature operations.
func BuildPlan(ctx context.Context, r *Router, in *Inspection, d Driver) (*Plan, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil || in == nil || in.Desired == nil || in.State == nil || in.Report == nil {
		return nil, routerError(messageOperationFormat)
	}
	desired, state, report := in.Desired, in.State, in.Report
	status := report.ReturnRouting
	if status == nil {
		status = &ReturnStatus{}
	}
	plan := &Plan{Stages: []Stage{}, Blockers: []string{}, Warnings: append([]string{}, desired.Warnings...), Empty: true}
	warnings, blockers := &plan.Warnings, &plan.Blockers
	gateway := desired.ProxyGateway
	if gateway == "" {
		*blockers = append(*blockers, messageNoGateway)
	} else if a, e := netip.ParseAddr(gateway); e != nil || !a.Is4() {
		*blockers = append(*blockers, messageInvalidGateway)
	}
	upstream := RouterIP(r.Endpoint)
	if r.ManageReturnRouting {
		if upstream == "" {
			*blockers = append(*blockers, messageInvalidUpstream)
		}
		if problem := returnUpstreamProblem(upstream, gateway, state.NatContext); problem != "" {
			*blockers = append(*blockers, problem)
		}
		if status.Status != "ok" {
			*blockers = append(*blockers, fmt.Sprintf(messageReturnUnreadable, orText(status.Message, messagePlanUnknown)))
		} else {
			*blockers = append(*blockers, ReturnSafetyBlockers(status, true)...)
		}
	}
	if len(*blockers) > 0 {
		return plan, nil
	}
	var planner *GroupPlanner
	if state.IkuaiGroups != nil {
		planner = NewGroupPlanner(state.IkuaiGroups, warnings, blockers)
	}
	var routeOps []Op
	var skipped []string
	policyChecks := []PolicyRouteCheck{}
	policy := state.PolicyRoutes != nil
	if policy {
		routeOps, skipped, policyChecks = policyPlanRoutes(report, state, gateway, warnings, planner)
	} else {
		routeOps, skipped = staticPlanRoutes(report, state, gateway, warnings)
	}
	proxy := proxyTargets(desired, skipped)
	restored := []CIDRGateway{}
	if state.RetiredPolicyRoutes != nil {
		var ops []Op
		ops, restored = retiredPlanRoutes(state, proxy, skipped, warnings, blockers)
		routeOps = append(routeOps, ops...)
		if planner != nil {
			for _, v := range state.RetiredPolicyRoutes {
				planner.DeletedRules["policy:"+v.ID] = true
			}
		}
		if slices.ContainsFunc(proxy, func(v string) bool { return v != desired.FakeIPRange }) {
			*warnings = append(*warnings, messageStaticLoop)
		}
	}
	if planner != nil {
		removed := planner.Cleanup("pa", "pm")
		routeOps = append(routeOps, groupOpsInto(removed)...)
		planner.StageChecks[StageRoutes] = planner.Check([]string{"pa", "pm"}, planner.RouteRules, deletedGroupNames(removed))
	}
	rosPolicy := state.ROSPolicyPlan
	policyOps := []*ROSPolicyReplace{}
	if rosPolicy != nil {
		*blockers = append(*blockers, rosPolicy.Blockers...)
		*warnings = append(*warnings, rosPolicy.Warnings...)
		var previous ROSPolicyManifest
		if desired.ROSPolicy != nil {
			_ = jsondecode.Unmarshal(desired.ROSPolicy.Previous, &previous)
		}
		old := []string{}
		if previous.Spec != nil {
			old = previous.Spec.Destinations
		}
		next := []string{}
		if desired.ROSPolicy != nil {
			for _, v := range desired.ROSPolicy.CIDRs {
				next = append(next, v.CIDR)
			}
		}
		replacements := SubtractNetworks(Networks(old), Networks(next))
		if rosPolicy.Check != nil {
			for _, v := range rosPolicy.Check.Migrations {
				if v.Path == rosRoutes {
					replacements = append(replacements, Networks([]string{v.Config.text("dst-address")})...)
				}
			}
		}
		overlaps := ROSPolicyNetworkOverlap(replacements)
		for _, n := range Networks(skipped) {
			if overlaps(n) {
				*blockers = append(*blockers, messagePolicyWithdrawConflict)
				break
			}
		}
		for _, raw := range rosPolicy.Ops {
			op, ok := raw.(*ROSPolicyReplace)
			if !ok {
				return nil, routerError(messageOperationFormat)
			}
			copy := *op
			copy.StaticChecks = []CIDRGateway{}
			for _, cidr := range proxy {
				for _, n := range Networks([]string{cidr}) {
					if overlaps(n) {
						copy.StaticChecks = append(copy.StaticChecks, CIDRGateway{cidr, gateway})
						break
					}
				}
			}
			policyOps = append(policyOps, &copy)
			routeOps = append(routeOps, &copy)
		}
	}
	if stale := StaleRoutes(state, gateway); len(stale) > 0 {
		*warnings = append(*warnings, StaleRouteWarning(stale, gateway))
	}
	settings := DNSSettings{}
	if desired.DNSSettings != nil {
		settings = *desired.DNSSettings
	}
	settings.AgentID = r.DNSAgentID
	dns := BuildDNSReport(settings, gateway, state.DhcpDNS)
	dnsOps := []Op{}
	manageDNS := r.DNSAgentID != ""
	if manageDNS {
		*blockers = append(*blockers, dns.Blockers...)
		if len(skipped) > 0 {
			*blockers = append(*blockers, messageDNSRouteConflict)
		}
		if dns.Status == "pending" && len(dns.Items) > 0 {
			item := dns.Items[0]
			for _, entry := range state.DhcpDNS.Entries {
				if entry.ID != item.ID {
					continue
				}
				replacement := messageDHCPReplace
				if r.Type == "ikuai" {
					replacement = messageDHCPBoth
				}
				summary := fmt.Sprintf(messageDHCPUpdate, entry.Network, orText(strings.Join(entry.DNS, ", "), messageDHCPAuto), dns.TargetDNS, replacement)
				dnsOps = append(dnsOps, DHCPDNSUpdateOp{OpHead{"dhcp_dns_update", summary}, entry.ID, item.DesiredDNS, entry.Attributes, DNSScope{entry.ID, entry.Network, gateway}})
				break
			}
		}
	}
	natItems := []NatItem{}
	for _, v := range report.NatBypass.Items {
		if slices.Contains(skipped, v.CIDR) {
			continue
		}
		natItems = append(natItems, v)
		if v.Status == "unknown" {
			*blockers = append(*blockers, fmt.Sprintf(messagePlanUnknownNAT, v.CIDR, v.Detail))
		}
	}
	natOps, exempt, listed := planNAT(ctx, report, state, gateway, skipped, r.Type, planner)
	if listed != nil {
		*blockers = append(*blockers, listed.Blockers...)
		*warnings = append(*warnings, listed.Warnings...)
	}
	if r.Type == "ikuai" {
		raw := []json.RawMessage{}
		for _, op := range append(slices.Clone(routeOps), natOps...) {
			b, e := json.Marshal(op)
			if e != nil {
				return nil, e
			}
			raw = append(raw, b)
		}
		*blockers = append(*blockers, IkuaiOversizedRequests(raw)...)
	}
	if r.Type == "routeros" && state.NatContext != nil {
		expansions := []BypassExpansion{}
		for _, op := range natOps {
			if v, ok := op.(NATUpdateOp); ok {
				expansions = append(expansions, BypassExpansion{v.ID, v.AddDst, v.Comment})
			}
		}
		*blockers = append(*blockers, PlannedBypassConflicts(expansions, state, gateway)...)
	}
	for _, op := range policyOps {
		if len(op.StaticChecks) == 0 {
			continue
		}
		if len(natOps) > 0 {
			iface, reason := Egress(op.StaticChecks[0].CIDR, gateway, state.NatContext, true, nil)
			if iface == "" {
				*blockers = append(*blockers, fmt.Sprintf(messagePrepareNAT, reason))
			} else {
				for i, preparation := range natOps {
					switch v := preparation.(type) {
					case *NATListSync:
						copy := *v
						if listed == nil || listed.Check == nil {
							return nil, routerError(messageOperationFormat)
						}
						check := *listed.Check
						listed.Check = &check
						if problem := RestrictNATEgress(&copy, listed.Check, iface); problem != "" {
							*blockers = append(*blockers, problem)
						}
						natOps[i] = &copy
					case NATUpdateOp:
						v.OutInterface = &iface
						natOps[i] = v
					case NATAddOp:
						v.OutInterface = &iface
						natOps[i] = v
					}
				}
			}
		}
		op.Summary += messagePrepareStaticSuffix
		op.PrepareStatic = append([]Op{}, natOps...)
		for _, other := range routeOps {
			if other != op {
				op.PrepareStatic = append(op.PrepareStatic, other)
			}
		}
		op.StaticNATChecks = pointer(natChecks(natItems, gateway))
		routeOps = []Op{op}
		natOps = []Op{}
	}
	if listed != nil && listed.Check != nil && len(natOps) > 0 {
		natOps, routeOps = NATRemovalsFirst(natOps, routeOps, gateway)
	}
	returnOps := []Op{}
	var spec *ReturnSpec
	if r.ManageReturnRouting {
		a := ReturnAllocationOf(status)
		spec = &ReturnSpec{RouterIP: upstream, Table: a.Table, Priority: a.Priority, Mark: a.Mark, SafetyVersion: ReturnProtocol(status)}
		if spec.SafetyVersion == 1 {
			values := append(slices.Clone(proxy), exempt...)
			if desired.ROSPolicy != nil {
				for _, v := range desired.ROSPolicy.CIDRs {
					values = append(values, v.CIDR)
				}
			}
			if status.Managed != nil {
				var old ReturnSpec
				_ = jsondecode.Unmarshal(status.Managed.Spec, &old)
				for _, n := range Networks(returnDest(&old)) {
					for _, skip := range Networks(skipped) {
						if n.Overlaps(skip) {
							if n.Bits() > skip.Bits() {
								values = append(values, n.String())
							} else {
								values = append(values, skip.String())
							}
						}
					}
				}
			}
			collapsed, err := collapseRouterNetworks(values)
			if err != nil {
				return nil, err
			}
			spec.ProxyDst = &collapsed
		}
		returnOps = makeReturnOps(status, spec)
	}
	cleanup := []Op{}
	if !r.ManageReturnRouting {
		cleanup = cleanupReturnOps(status, warnings)
	}
	manual := d != nil && !d.SupportsWrite()
	retStage := stageOf(StageReturnRouting, false, returnOps)
	retStage.Enabled = pointer(r.ManageReturnRouting)
	note := messageReturnOffNote
	if r.ManageReturnRouting {
		note = messageReturnOnNote
	} else if len(cleanup) > 0 {
		note = messageReturnCleanupNote
	}
	retStage.Note = &note
	routeStage := stageOf(StageRoutes, manual, routeOps)
	if policy {
		routeStage.Note = pointer(messagePolicyNote)
	}
	dhcpStage := stageOf(StageDHCPDNS, manual, dnsOps)
	dhcpStage.Enabled = &manageDNS
	dhcpStage.Note = pointer(messageDHCPOffNote)
	if manageDNS {
		dhcpStage.Note = pointer(messageDHCPOnNote)
	}
	stages := []Stage{retStage, routeStage}
	checks := &Checks{NATChecks: natChecks(natItems, gateway), Routes: []CIDRGateway{}, RemovedRoutes: []string{}}
	if !policy {
		for _, v := range proxy {
			checks.Routes = append(checks.Routes, CIDRGateway{v, gateway})
		}
	} else {
		checks.PolicyRoutes = &policyChecks
	}
	if len(state.RetiredPolicyRoutes) > 0 {
		checks.RestoredRoutes = &restored
		ids := []string{}
		for _, v := range state.RetiredPolicyRoutes {
			ids = append(ids, v.ID)
		}
		checks.RemovedPolicyRoutes = &ids
	}
	if listed != nil {
		checks.NATList = listed.Check
	}
	if rosPolicy != nil {
		checks.ROSPolicy = rosPolicy.Check
		checks.rosPolicyPresent = true
	}
	checks.ReturnRouting = spec
	for _, op := range routeOps {
		ops := []Op{op}
		if v, ok := op.(*ROSPolicyReplace); ok && v.PrepareStatic != nil {
			ops = v.PrepareStatic
		}
		for _, entry := range ops {
			if v, ok := entry.(RouteDeleteOp); ok {
				checks.RemovedRoutes = append(checks.RemovedRoutes, v.ID)
			}
		}
	}
	if v := state.IPv6FakeIPPlan; v != nil {
		*blockers = append(*blockers, v.Blockers...)
		*warnings = append(*warnings, v.Warnings...)
		stage := stageOf(StageIPv6FakeIP, manual, v.Ops)
		stage.Note = pointer(messageFakeNote)
		stages = append(stages, stage)
		check := v.Check
		checks.IPv6FakeIP = &check
	}
	if v := state.IPv6RoutesPlan; v != nil {
		skip := len(v.Skip) > 0
		for _, s := range v.Skip {
			*warnings = append(*warnings, fmt.Sprintf(messageIPv6RoutesSkip, s))
		}
		for _, w := range v.Warnings {
			if !slices.Contains(*warnings, w) {
				*warnings = append(*warnings, w)
			}
		}
		stage := stageOf(StageIPv6Routes, manual, v.Ops)
		stage.Enabled = pointer(v.Enabled || len(v.Ops) > 0)
		stage.Skipped = &skip
		stage.Note = pointer(messageIPv6RoutesOffNote)
		if v.Enabled {
			stage.Note = pointer(messageIPv6RoutesOnNote)
		}
		if skip {
			stage.Ops = []Op{}
		} else {
			checks.IPv6Routes = v.Check
		}
		stages = append(stages, stage)
	}
	stages = append(stages, stageOf(StageNATBypass, manual, natOps), dhcpStage)
	if manageDNS && (dns.Status == "pending" || dns.Status == "synced") && len(dns.Items) > 0 {
		item := dns.Items[0]
		checks.DHCPDNS = &DNSCheck{ID: item.ID, Network: item.Network, Gateway: gateway, DNS: item.DesiredDNS}
	}
	if v := state.IPv6DNSPlan; v != nil {
		skip := len(v.Blockers) > 0
		for _, s := range v.Blockers {
			*warnings = append(*warnings, fmt.Sprintf(messageIPv6DNSSkip, s))
		}
		*warnings = append(*warnings, v.Warnings...)
		if len(v.Ops) > 0 && (!manageDNS || (dns.Status != "pending" && dns.Status != "synced")) {
			*blockers = append(*blockers, messageIPv6DNSNeedsDHCP)
		}
		stage := stageOf(StageIPv6DNS, manual, v.Ops)
		stage.Enabled = &v.Enabled
		stage.Skipped = &skip
		stage.Note = pointer(messageIPv6DNSNote)
		if skip {
			stage.Ops = []Op{}
		}
		stages = append(stages, stage)
		checks.IPv6DNS = v.Check
		checks.ipv6DNSPresent = true
	}
	if v := state.DNSGuardPlan; v != nil {
		*blockers = append(*blockers, v.Blockers...)
		*warnings = append(*warnings, v.Warnings...)
		if len(skipped) > 0 && effectful(v.Spec) {
			*blockers = append(*blockers, messageGuardRouteConflict)
		}
		stage := stageOf(StageDNSGuard, manual, v.Ops)
		stage.Note = pointer(messageGuardOffNote)
		if effectful(v.Spec) {
			stage.Note = pointer(messageGuardOnNote)
		}
		stages = append(stages, stage)
		checks.DNSGuard = &DNSGuardCheck{Roles: v.Roles}
	}
	if v := state.IPv6PDGuardPlan; v != nil {
		skip := len(v.Blockers) > 0
		for _, s := range v.Blockers {
			*warnings = append(*warnings, fmt.Sprintf(messagePDSkip, s))
		}
		*warnings = append(*warnings, v.Warnings...)
		stage := stageOf(StageIPv6PDGuard, manual, v.Ops)
		stage.Skipped = &skip
		stage.Note = pointer(messagePDOffNote)
		if v.Enabled {
			stage.Note = pointer(messagePDOnNote)
		}
		if skip {
			stage.Ops = []Op{}
			checks.IPv6PDGuard = json.RawMessage(`{}`)
		} else {
			var err error
			checks.IPv6PDGuard, err = json.Marshal(v.Check)
			if err != nil {
				return nil, err
			}
		}
		stages = append(stages, stage)
	}
	if len(cleanup) > 0 {
		stage := stageOf(StageReturnCleanup, false, cleanup)
		stage.Note = pointer(messageCleanupNote)
		stages = append(stages, stage)
	}
	if planner != nil {
		checks.IkuaiGroups = planner.StageChecks
		if w := planner.MigrationWarning(); w != "" {
			prependWarning(warnings, w)
		}
	}
	plan.Stages = stages
	plan.Checks = checks
	if manual {
		raw := []json.RawMessage{}
		for _, stage := range stages {
			if stage.Manual {
				for _, op := range stage.Ops {
					b, e := json.Marshal(op)
					if e != nil {
						return nil, e
					}
					raw = append(raw, b)
				}
			}
		}
		script, e := d.RenderScript(raw)
		if e != nil {
			return nil, e
		}
		plan.Script = script
	}
	natEvidence, dnsEvidence := planEvidence(r, desired, state, listed, dns)
	var returnEvidence any
	if r.ManageReturnRouting {
		returnEvidence = status.Safety
	}
	fp, err := Fingerprint(stages, natEvidence, dnsEvidence, returnEvidence)
	if err != nil {
		return nil, err
	}
	plan.Fingerprint = fp
	plan.Empty = !slices.ContainsFunc(stages, func(s Stage) bool { return len(s.Ops) > 0 })
	if r.Type == "routeros" && !r.ManageReturnRouting {
		hint := report.Routes.ForwardHint
		if hint != nil && hint.Status == "drop" {
			*warnings = append(*warnings, hint.Warning)
		} else {
			advisory, e := ReturnAdvisoryWarnings(ctx, desired, state, d)
			if e != nil {
				return nil, e
			}
			*warnings = append(*warnings, advisory...)
			if hint != nil {
				*warnings = append(*warnings, hint.Warning)
			}
		}
	}
	return plan, nil
}
