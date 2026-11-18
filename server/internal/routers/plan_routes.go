package routers

import (
	"fmt"
	"slices"
	"strings"
)

func staticPlanRoutes(report *Report, state *RouterState, gateway string, warnings *[]string) ([]Op, []string) {
	ops := []Op{}
	skipped := []string{}
	ix := NewStaticRouteIndex(state.Routes)
	for _, item := range report.Routes.Items {
		switch item.Status {
		case "missing":
			comment := ManagedComment(strings.Join(item.Sources, ","))
			ops = append(ops, RouteAddOp{OpHead{"route_add", fmt.Sprintf(messagePlanRouteAdd, item.CIDR, gateway, comment)}, item.CIDR, gateway, comment})
		case "adoptable":
			sel, _ := ix.Select(item.CIDR, gateway)
			for _, r := range sel.Candidates {
				if !IsManaged(r.Comment) {
					comment := ManagedComment(r.Comment)
					ops = append(ops, RouteCommentOp{OpHead{"route_comment", fmt.Sprintf(messagePlanRouteAdopt, r.Dst, orText(r.Comment, messageNone), comment)}, r.ID, r.Dst, comment})
				}
			}
		case "conflict":
			skipped = append(skipped, item.CIDR)
			*warnings = append(*warnings, fmt.Sprintf(messagePlanSkip, item.CIDR, item.Detail))
		}
	}
	for _, e := range report.Routes.Extra {
		ops = append(ops, RouteDeleteOp{OpHead{"route_delete", fmt.Sprintf(messagePlanRouteDelete, e.CIDR, e.Gateway)}, e.RouteID, e.CIDR})
	}
	return ops, skipped
}
func orText(s, fallback string) string {
	if s != "" {
		return s
	}
	return fallback
}
func prependWarning(warnings *[]string, s string) { *warnings = append([]string{s}, *warnings...) }
func groupOpsInto(ops []GroupOp) []Op {
	out := make([]Op, len(ops))
	for i, v := range ops {
		out[i] = v
	}
	return out
}
func deletedGroupNames(ops []GroupOp) []string {
	out := []string{}
	for _, op := range ops {
		if op.Kind == OpGroupDelete {
			out = append(out, op.Name)
		}
	}
	return out
}
func groupTotal(p *GroupPlanner, role string) int {
	n := 0
	for _, v := range p.Contents[role] {
		n += len(v)
	}
	return n
}
func makePolicyOp(p *GroupPlanner, role string, rule *PolicyRouteEntry, gateway string, want, dst []string, comment string, keep bool, groups *[]Op) Op {
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
	op := PolicyRouteOp{OpHead: OpHead{kind, summary}, ID: id, Gateway: gateway, Dst: dst, Comment: comment}
	if p == nil {
		if rule != nil && PolicyRouteOK(rule, gateway) && sameStrings(rule.Dst, dst) {
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
	p.RouteRules = append(p.RouteRules, GroupRuleCheck{Func: "stream_ipport", Comment: comment, Groups: final, KeepForeign: keep})
	if rule != nil && PolicyRouteOK(rule, gateway) && !p.NeedsRewrite(key, role, keep) {
		return nil
	}
	op.Groups = &final
	op.KeepForeign = keep
	targets := fmt.Sprintf(messageGroupTargets, GroupSpan(final), groupTotal(p, role))
	scope = fmt.Sprintf(messagePlanPolicyScope, gateway, targets, gateway)
	op.Summary = fmt.Sprintf(messagePolicyAdd, comment, scope)
	if rule != nil {
		p.Rewritten[key] = final
		op.Summary = fmt.Sprintf(messagePolicyUpdate, comment, scope)
		if p.Migration(key, messagePortPolicy, comment) > 0 {
			op.Summary = fmt.Sprintf(messagePolicyMigrate, comment, GroupSpan(final), groupTotal(p, role))
		}
		if !keep {
			if dropped := p.DroppedForeign(key); len(dropped) > 0 {
				op.Summary += fmt.Sprintf(messageDropForeign, strings.Join(dropped, "、"))
			}
		}
	}
	return op
}
func policyPlanRoutes(report *Report, state *RouterState, gateway string, warnings *[]string, p *GroupPlanner) ([]Op, []string, []PolicyRouteCheck) {
	ops, groups := []Op{}, []Op{}
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
	covered := NewSupernetIndex(Networks(target))
	inside := func(cidr string) bool {
		for _, net := range Networks([]string{cidr}) {
			if covered.Covers(net) {
				return true
			}
		}
		return false
	}
	auto := PolicyRoute(state, PolicyAutoComment)
	if len(target) > 0 {
		if op := makePolicyOp(p, "pa", auto, gateway, target, target, PolicyAutoComment, false, &groups); op != nil {
			ops = append(ops, op)
		}
		checks = append(checks, PolicyRouteCheck{PolicyAutoComment, gateway, target})
	} else if auto != nil {
		ops = append(ops, PolicyRouteDeleteOp{OpHead{"policy_route_delete", fmt.Sprintf(messagePolicyDelete, PolicyAutoComment)}, auto.ID})
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
	manual := PolicyRoute(state, PolicyManualComment)
	if (p != nil && (len(migrate) > 0 || manual != nil)) || (p == nil && (len(migrate) > 0 || (manual != nil && !PolicyRouteOK(manual, gateway)))) {
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
		if op := makePolicyOp(p, "pm", manual, gateway, want, dst, PolicyManualComment, true, &groups); op != nil {
			ops = append(ops, op)
		}
		checks = append(checks, PolicyRouteCheck{PolicyManualComment, gateway, dst})
	}
	migrated := []string{}
	deleting := map[string]bool{}
	for _, r := range state.Routes {
		if r.Enabled && r.Gateway == gateway && (slices.Contains(migrate, r.Dst) || inside(r.Dst)) {
			ops = append(ops, RouteDeleteOp{OpHead{"route_delete", fmt.Sprintf(messagePolicyStaticDelete, r.Dst, gateway, orText(r.Comment, messageNoComment))}, r.ID, r.Dst})
			migrated = append(migrated, fmt.Sprintf("%s（%s）", r.Dst, orText(r.Comment, messageNoComment)))
			deleting[r.ID] = true
		}
	}
	if len(migrated) > 0 {
		prependWarning(warnings, fmt.Sprintf(messagePolicyMigrationWarning, len(migrated), gateway, PolicyAutoComment, PolicyManualComment, strings.Join(migrated, "、")))
	}
	for _, e := range report.Routes.Extra {
		if !strings.HasPrefix(e.RouteID, "policy:") && !deleting[e.RouteID] {
			ops = append(ops, RouteDeleteOp{OpHead{"route_delete", fmt.Sprintf(messagePlanRouteDelete, e.CIDR, e.Gateway)}, e.RouteID, e.CIDR})
		}
	}
	return append(groups, ops...), skipped, checks
}
func retiredPlanRoutes(state *RouterState, proxy, skipped []string, warnings, blockers *[]string) ([]Op, []CIDRGateway) {
	ops := []Op{}
	restored := []CIDRGateway{}
	covered := NewSupernetIndex(Networks(proxy))
	existing := map[string]bool{}
	for _, r := range state.Routes {
		existing[r.Dst] = true
	}
	kept := []string{}
	for _, r := range state.RetiredPolicyRoutes {
		if r.Comment != PolicyManualComment {
			continue
		}
		for _, net := range Networks(r.Dst) {
			if covered.Covers(net) {
				continue
			}
			cidr := net.String()
			if existing[cidr] || r.Gateway == "" {
				kept = append(kept, cidr)
				continue
			}
			existing[cidr] = true
			ops = append(ops, RouteAddOp{OpHead{"route_add", fmt.Sprintf(messageRestoreManual, cidr, r.Gateway, RestoredComment)}, cidr, r.Gateway, RestoredComment})
			restored = append(restored, CIDRGateway{cidr, r.Gateway})
		}
	}
	lost := []string{}
	for _, cidr := range skipped {
		hit := false
		for _, net := range Networks([]string{cidr}) {
			for _, r := range state.RetiredPolicyRoutes {
				for _, old := range Networks(r.Dst) {
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
		ops = append(ops, PolicyRouteDeleteOp{OpHead{"policy_route_delete", fmt.Sprintf(messageRetirePolicy, r.Comment)}, r.ID})
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
