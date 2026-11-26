package routers

import (
	"configflow/internal/routers/device"
	"configflow/internal/routers/ikuai"
	"configflow/internal/routers/ros"
	"configflow/internal/routers/rospolicy"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"maps"
	"net/netip"
	"slices"
	"strings"
)

func lanExcept(gateway string) []string {
	a, err := netip.ParseAddr(gateway)
	if err != nil || !a.Is4() {
		return []string{}
	}
	v := a.As4()
	n := binary.BigEndian.Uint32(v[:])
	first, last := n&0xffffff00|1, n&0xffffff00|254
	addr := func(n uint32) string {
		var b [4]byte
		binary.BigEndian.PutUint32(b[:], n)
		return netip.AddrFrom4(b).String()
	}
	out := []string{}
	if n > first {
		out = append(out, addr(first)+"-"+addr(n-1))
	}
	if n < last {
		out = append(out, addr(n+1)+"-"+addr(last))
	}
	return out
}
func groupedNATOps(p *ikuai.GroupPlanner, rules map[string]*device.NatBypassEntry, target *device.NatBypassEntry, adopt, missing []string, gateway string) []device.Op {
	key := ""
	if target != nil {
		key = "nat:" + target.ID
	}
	final := []string{}
	ops := []device.Op{}
	if target != nil || len(missing) > 0 {
		want := append(append(slices.Clone(p.Refs(key).Custom), p.ReferencedValues(key)...), missing...)
		var changes []ikuai.GroupOp
		final, changes = p.Family("nat", want, key)
		ops = append(ops, groupOpsInto(changes)...)
	}
	total := groupTotal(p, "nat")
	for _, id := range adopt {
		if target != nil && target.ID == id {
			continue
		}
		r := rules[id]
		comment := device.ManagedComment(r.Comment)
		ops = append(ops, device.NATUpdateOp{OpHead: device.OpHead{Kind: "nat_update", Summary: fmt.Sprintf(messagePlanNATAdopt, device.OrText(r.Comment, id), comment, "")}, ID: id, AddDst: []string{}, Comment: comment})
	}
	checks := []ikuai.GroupRuleCheck{}
	if target != nil {
		adopting := slices.Contains(adopt, target.ID)
		comment := target.Comment
		if adopting {
			comment = device.ManagedComment(comment)
		}
		checks = append(checks, ikuai.GroupRuleCheck{Func: "nat_rule", ID: target.ID, Comment: comment, Groups: final, KeepForeign: true})
		if adopting || p.NeedsRewrite(key, "nat", true) {
			p.Rewritten[key] = final
			added := ""
			if len(missing) > 0 {
				added = fmt.Sprintf(messageNATGroupAdded, len(missing))
			}
			summary := fmt.Sprintf(messageNATGroupUpdate, comment, ikuai.GroupSpan(final), total, added)
			if adopting {
				summary = fmt.Sprintf(messageNATGroupAdopt, device.OrText(target.Comment, target.ID), comment, ikuai.GroupSpan(final), total, added)
			} else if p.Migration(key, messageNATLabel, comment) > 0 {
				summary = fmt.Sprintf(messageNATGroupMigrate, comment, ikuai.GroupSpan(final), total, added)
			}
			ops = append(ops, device.NATUpdateOp{OpHead: device.OpHead{Kind: "nat_update", Summary: summary}, ID: target.ID, AddDst: missing, Groups: &final, KeepForeign: true, Comment: comment})
		}
	} else if len(missing) > 0 {
		src := lanExcept(gateway)
		checks = append(checks, ikuai.GroupRuleCheck{Func: "nat_rule", Comment: device.PolicyAutoComment, Groups: final, KeepForeign: true})
		ops = append(ops, NATAddOp{OpHead: device.OpHead{Kind: "nat_add", Summary: fmt.Sprintf(messageNATGroupAdd, strings.Join(src, ", "), ikuai.GroupSpan(final), total)}, Src: src, Dst: missing, Groups: &final, Comment: device.PolicyAutoComment})
	}
	removed := p.Cleanup("nat")
	p.StageChecks[device.StageNATBypass] = p.Check([]string{"nat"}, checks, deletedGroupNames(removed))
	return append(ops, groupOpsInto(removed)...)
}
func planNAT(ctx context.Context, report *Report, state *RouterState, gateway string, skipped []string, vendor string, p *ikuai.GroupPlanner) ([]device.Op, []string, *rospolicy.NATListPlan) {
	if vendor == "routeros" && state.NatContext != nil {
		if listed := rospolicy.PlanNATLists(ctx, report.Routes, report.NatBypass, &state.State, state.NatContext, &state.NATLists, &state.NATListsReader, state.ROSPolicyPlan, gateway, skipped); listed != nil {
			return listed.Ops, listed.Exempt, listed
		}
	}
	items := []device.NatItem{}
	for _, v := range report.NatBypass.Items {
		if !slices.Contains(skipped, v.CIDR) {
			items = append(items, v)
		}
	}
	retiring := []string{}
	if state.ROSPolicyPlan != nil && state.ROSPolicyPlan.Check != nil {
		for _, v := range state.ROSPolicyPlan.Check.Migrations {
			if v.Path == ros.PathNAT {
				retiring = append(retiring, v.ID)
			}
		}
	}
	rules := map[string]*device.NatBypassEntry{}
	ordered := []*device.NatBypassEntry{}
	managed := []*device.NatBypassEntry{}
	for _, r := range state.NatBypass {
		if r.Enabled && !slices.Contains(retiring, r.ID) {
			rules[r.ID] = r
			ordered = append(ordered, r)
			if device.IsManaged(r.Comment) {
				managed = append(managed, r)
			}
		}
	}
	missing, adopt := []string{}, []string{}
	for _, item := range items {
		if item.Status == "missing" {
			missing = append(missing, item.CIDR)
		}
		if item.Status == "adoptable" {
			for _, id := range item.RuleIDs {
				if r := rules[id]; r != nil && !device.IsManaged(r.Comment) && !slices.Contains(adopt, id) {
					adopt = append(adopt, id)
				}
			}
		}
	}
	var target *device.NatBypassEntry
	if len(managed) > 0 {
		target = managed[0]
	} else if len(adopt) > 0 {
		target = rules[adopt[0]]
	}
	exempt := []string{}
	for _, r := range ordered {
		exempt = append(exempt, r.Dst...)
	}
	if p != nil {
		target = ikuai.NATTarget(p.State, managed)
		if target == nil && len(adopt) > 0 {
			target = rules[adopt[0]]
		}
		ops := groupedNATOps(p, rules, target, adopt, missing, gateway)
		exempt = append(exempt, missing...)
		for _, op := range ops {
			switch op := op.(type) {
			case device.NATUpdateOp:
				exempt = append(exempt, op.AddDst...)
			case NATAddOp:
				exempt = append(exempt, op.Dst...)
			}
		}
		return ops, exempt, nil
	}
	targets := []*device.NatBypassEntry{}
	if target != nil {
		targets = append(targets, target)
	}
	if target != nil && effectful(target.Attributes) && vendor == "routeros" {
		group := func(raw json.RawMessage) map[string]json.RawMessage {
			var v map[string]json.RawMessage
			_ = json.Unmarshal(raw, &v)
			delete(v, "src-address")
			return v
		}
		want := group(target.Attributes)
		candidates := managed
		if len(candidates) == 0 {
			for _, id := range adopt {
				candidates = append(candidates, rules[id])
			}
		}
		targets = nil
		for _, r := range candidates {
			if maps.EqualFunc(want, group(r.Attributes), func(a, b json.RawMessage) bool { return device.JSONEqual(a, b) }) {
				targets = append(targets, r)
			}
		}
	}
	ops := []device.Op{}
	for _, id := range adopt {
		r := rules[id]
		add := []string{}
		if slices.ContainsFunc(targets, func(t *device.NatBypassEntry) bool { return t.ID == id }) {
			add = missing
		}
		comment := device.ManagedComment(r.Comment)
		extra := ""
		if len(add) > 0 {
			extra = fmt.Sprintf(messagePlanNATAdoptAdd, strings.Join(add, ", "))
		}
		ops = append(ops, device.NATUpdateOp{OpHead: device.OpHead{Kind: "nat_update", Summary: fmt.Sprintf(messagePlanNATAdopt, device.OrText(r.Comment, r.ID), comment, extra)}, ID: id, AddDst: add, Comment: comment})
	}
	for _, r := range targets {
		if device.IsManaged(r.Comment) && len(missing) > 0 {
			summary := fmt.Sprintf(messageNATExtend, r.Comment, strings.Join(missing, ", "))
			if len(targets) > 1 {
				summary += fmt.Sprintf(messageNATSource, strings.Join(r.Src, ", "))
			}
			ops = append(ops, device.NATUpdateOp{OpHead: device.OpHead{Kind: "nat_update", Summary: summary}, ID: r.ID, AddDst: missing, Comment: r.Comment})
		}
	}
	if target == nil && len(missing) > 0 {
		src := lanExcept(gateway)
		ops = append(ops, NATAddOp{OpHead: device.OpHead{Kind: "nat_add", Summary: fmt.Sprintf(messagePlanNATAdd, strings.Join(src, ", "), strings.Join(missing, ", "))}, Src: src, Dst: missing, Comment: device.PolicyAutoComment})
	}
	for i, op := range ops {
		switch v := op.(type) {
		case device.NATUpdateOp:
			if vendor == "routeros" {
				v.Expected = rules[v.ID].Attributes
				ops[i] = v
			}
			exempt = append(exempt, v.AddDst...)
		case NATAddOp:
			exempt = append(exempt, v.Dst...)
		}
	}
	if state.ROSPolicyPlan != nil && state.ROSPolicyPlan.Check != nil {
		for _, r := range state.ROSPolicyPlan.Check.Replacements {
			if r.Path == ros.PathNAT {
				exempt = append(exempt, r.Values.Text("dst-address"))
			}
		}
	}
	return ops, exempt, nil
}
