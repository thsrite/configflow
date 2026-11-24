package routers

import (
	"configflow/internal/routers/device"
	"configflow/internal/routers/ikuai"
	"fmt"
	"slices"
	"strings"
)

func staticPlanRoutes(report *Report, state *RouterState, gateway string, warnings *[]string) ([]device.Op, []string) {
	ops := []device.Op{}
	skipped := []string{}
	ix := device.NewStaticRouteIndex(state.Routes)
	for _, item := range report.Routes.Items {
		switch item.Status {
		case "missing":
			comment := device.ManagedComment(strings.Join(item.Sources, ","))
			ops = append(ops, RouteAddOp{device.OpHead{Kind: "route_add", Summary: fmt.Sprintf(messagePlanRouteAdd, item.CIDR, gateway, comment)}, item.CIDR, gateway, comment})
		case "adoptable":
			sel, _ := ix.Select(item.CIDR, gateway)
			for _, r := range sel.Candidates {
				if !device.IsManaged(r.Comment) {
					comment := device.ManagedComment(r.Comment)
					ops = append(ops, RouteCommentOp{device.OpHead{Kind: "route_comment", Summary: fmt.Sprintf(messagePlanRouteAdopt, r.Dst, device.OrText(r.Comment, messageNone), comment)}, r.ID, r.Dst, comment})
				}
			}
		case "conflict":
			skipped = append(skipped, item.CIDR)
			*warnings = append(*warnings, fmt.Sprintf(messagePlanSkip, item.CIDR, item.Detail))
		}
	}
	for _, e := range report.Routes.Extra {
		ops = append(ops, RouteDeleteOp{device.OpHead{Kind: "route_delete", Summary: fmt.Sprintf(messagePlanRouteDelete, e.CIDR, e.Gateway)}, e.RouteID, e.CIDR})
	}
	return ops, skipped
}
func prependWarning(warnings *[]string, s string) { *warnings = append([]string{s}, *warnings...) }
func groupOpsInto(ops []ikuai.GroupOp) []device.Op {
	out := make([]device.Op, len(ops))
	for i, v := range ops {
		out[i] = v
	}
	return out
}
func deletedGroupNames(ops []ikuai.GroupOp) []string {
	out := []string{}
	for _, op := range ops {
		if op.Kind == ikuai.OpGroupDelete {
			out = append(out, op.Name)
		}
	}
	return out
}
func groupTotal(p *ikuai.GroupPlanner, role string) int {
	n := 0
	for _, v := range p.Contents[role] {
		n += len(v)
	}
	return n
}
func makePolicyOp(p *ikuai.GroupPlanner, role string, rule *device.PolicyRouteEntry, gateway string, want, dst []string, comment string, keep bool, groups *[]device.Op) device.Op {
	kind := "policy_route_add"
	id := ""
	if rule != nil {
		kind = "policy_route_update"
		id = rule.ID
	}
	scope := fmt.Sprintf(messagePlanPolicyScope, gateway, strings.Join(dst, ", "), gateway)
	summary := fmt.Sprintf(messagePolicyAdd, comment, scope)
	if rule != nil {
		summary = fmt.Sprintf(messagePolicyUpdate, comment, scope)
	}
	op := PolicyRouteOp{OpHead: device.OpHead{Kind: kind, Summary: summary}, ID: id, Gateway: gateway, Dst: dst, Comment: comment}
	if p == nil {
		if rule != nil && device.PolicyRouteOK(rule, gateway) && sameStrings(rule.Dst, dst) {
			return nil
		}
		return op
	}
	key := ""
	if rule != nil {
		key = "policy:" + rule.ID
	}
	final, changes := p.Family(role, want, key)
	*groups = append(*groups, groupOpsInto(changes)...)
	p.RouteRules = append(p.RouteRules, ikuai.GroupRuleCheck{Func: "stream_ipport", Comment: comment, Groups: final, KeepForeign: keep})
	if rule != nil && device.PolicyRouteOK(rule, gateway) && !p.NeedsRewrite(key, role, keep) {
		return nil
	}
	op.Groups = &final
	op.KeepForeign = keep
	targets := fmt.Sprintf(messageGroupTargets, ikuai.GroupSpan(final), groupTotal(p, role))
	scope = fmt.Sprintf(messagePlanPolicyScope, gateway, targets, gateway)
	op.Summary = fmt.Sprintf(messagePolicyAdd, comment, scope)
	if rule != nil {
		p.Rewritten[key] = final
		op.Summary = fmt.Sprintf(messagePolicyUpdate, comment, scope)
		if p.Migration(key, messagePortPolicy, comment) > 0 {
			op.Summary = fmt.Sprintf(messagePolicyMigrate, comment, ikuai.GroupSpan(final), groupTotal(p, role))
		}
		if !keep {
			if dropped := p.DroppedForeign(key); len(dropped) > 0 {
				op.Summary += fmt.Sprintf(messageDropForeign, strings.Join(dropped, "、"))
			}
		}
	}
	return op
}
func policyPlanRoutes(report *Report, state *RouterState, gateway string, warnings *[]string, p *ikuai.GroupPlanner) ([]device.Op, []string, []PolicyRouteCheck) {
	ops, groups := []device.Op{}, []device.Op{}
	skipped, target := []string{}, []string{}
	checks := []PolicyRouteCheck{}
	for _, item := range report.Routes.Items {
		if item.Status == "conflict" {
			skipped = append(skipped, item.CIDR)
			*warnings = append(*warnings, fmt.Sprintf(messagePlanSkip, item.CIDR, item.Detail))
		} else {
			target = append(target, item.CIDR)
		}
	}
	covered := device.NewSupernetIndex(device.Networks(target))
	inside := func(cidr string) bool {
		for _, net := range device.Networks([]string{cidr}) {
			if covered.Covers(net) {
				return true
			}
		}
		return false
	}
	auto := device.PolicyRoute(state.PolicyRoutes, device.PolicyAutoComment)
	if len(target) > 0 {
		if op := makePolicyOp(p, "pa", auto, gateway, target, target, device.PolicyAutoComment, false, &groups); op != nil {
			ops = append(ops, op)
		}
		checks = append(checks, PolicyRouteCheck{device.PolicyAutoComment, gateway, target})
	} else if auto != nil {
		ops = append(ops, PolicyRouteDeleteOp{device.OpHead{Kind: "policy_route_delete", Summary: fmt.Sprintf(messagePolicyDelete, device.PolicyAutoComment)}, auto.ID})
		if p != nil {
			p.DeletedRules["policy:"+auto.ID] = true
		}
	}
	migrate := []string{}
	for _, item := range report.Routes.UnmanagedOther {
		if item.Migrate != nil && *item.Migrate && !inside(item.CIDR) {
			migrate = append(migrate, item.CIDR)
		}
	}
	manual := device.PolicyRoute(state.PolicyRoutes, device.PolicyManualComment)
	if (p != nil && (len(migrate) > 0 || manual != nil)) || (p == nil && (len(migrate) > 0 || (manual != nil && !device.PolicyRouteOK(manual, gateway)))) {
		current := []string{}
		key := ""
		if manual != nil {
			current = append(current, manual.Dst...)
			key = "policy:" + manual.ID
		}
		added := []string{}
		for _, cidr := range migrate {
			if !slices.Contains(current, cidr) {
				added = append(added, cidr)
			}
		}
		dst := append(slices.Clone(current), added...)
		want := dst
		if p != nil {
			want = append(append(slices.Clone(p.Refs(key).Custom), p.ReferencedValues(key)...), added...)
		}
		if op := makePolicyOp(p, "pm", manual, gateway, want, dst, device.PolicyManualComment, true, &groups); op != nil {
			ops = append(ops, op)
		}
		checks = append(checks, PolicyRouteCheck{device.PolicyManualComment, gateway, dst})
	}
	migrated := []string{}
	deleting := map[string]bool{}
	for _, r := range state.Routes {
		if r.Enabled && r.Gateway == gateway && (slices.Contains(migrate, r.Dst) || inside(r.Dst)) {
			ops = append(ops, RouteDeleteOp{device.OpHead{Kind: "route_delete", Summary: fmt.Sprintf(messagePolicyStaticDelete, r.Dst, gateway, device.OrText(r.Comment, messageNoComment))}, r.ID, r.Dst})
			migrated = append(migrated, fmt.Sprintf("%s（%s）", r.Dst, device.OrText(r.Comment, messageNoComment)))
			deleting[r.ID] = true
		}
	}
	if len(migrated) > 0 {
		prependWarning(warnings, fmt.Sprintf(messagePolicyMigrationWarning, len(migrated), gateway, device.PolicyAutoComment, device.PolicyManualComment, strings.Join(migrated, "、")))
	}
	for _, e := range report.Routes.Extra {
		if !strings.HasPrefix(e.RouteID, "policy:") && !deleting[e.RouteID] {
			ops = append(ops, RouteDeleteOp{device.OpHead{Kind: "route_delete", Summary: fmt.Sprintf(messagePlanRouteDelete, e.CIDR, e.Gateway)}, e.RouteID, e.CIDR})
		}
	}
	return append(groups, ops...), skipped, checks
}
func retiredPlanRoutes(state *RouterState, proxy, skipped []string, warnings, blockers *[]string) ([]device.Op, []device.CIDRGateway) {
	ops := []device.Op{}
	restored := []device.CIDRGateway{}
	covered := device.NewSupernetIndex(device.Networks(proxy))
	existing := map[string]bool{}
	for _, r := range state.Routes {
		existing[r.Dst] = true
	}
	kept := []string{}
	for _, r := range state.RetiredPolicyRoutes {
		if r.Comment != device.PolicyManualComment {
			continue
		}
		for _, net := range device.Networks(r.Dst) {
			if covered.Covers(net) {
				continue
			}
			cidr := net.String()
			if existing[cidr] || r.Gateway == "" {
				kept = append(kept, cidr)
				continue
			}
			existing[cidr] = true
			ops = append(ops, RouteAddOp{device.OpHead{Kind: "route_add", Summary: fmt.Sprintf(messageRestoreManual, cidr, r.Gateway, device.RestoredComment)}, cidr, r.Gateway, device.RestoredComment})
			restored = append(restored, device.CIDRGateway{CIDR: cidr, Gateway: r.Gateway})
		}
	}
	lost := []string{}
	for _, cidr := range skipped {
		hit := false
		for _, net := range device.Networks([]string{cidr}) {
			for _, r := range state.RetiredPolicyRoutes {
				for _, old := range device.Networks(r.Dst) {
					hit = hit || net.Overlaps(old)
				}
			}
		}
		if hit {
			lost = append(lost, cidr)
		}
	}
	if len(lost) > 0 {
		slices.Sort(lost)
		*blockers = append(*blockers, fmt.Sprintf(messageRetireConflict, strings.Join(lost, "、")))
	}
	for _, r := range state.RetiredPolicyRoutes {
		ops = append(ops, PolicyRouteDeleteOp{device.OpHead{Kind: "policy_route_delete", Summary: fmt.Sprintf(messageRetirePolicy, r.Comment)}, r.ID})
	}
	if len(state.RetiredPolicyRoutes) > 0 {
		suffix := ""
		if len(kept) > 0 {
			suffix = fmt.Sprintf(messageKeptStatic, strings.Join(kept, "、"))
		}
		prependWarning(warnings, messageRetireWarning+suffix)
	}
	return ops, restored
}
