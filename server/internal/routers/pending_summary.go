package routers

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// PendingItem describes the changes one enabled release stage would make.
type PendingItem struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Status string `json:"status"`
	Count  int    `json:"count"`
}

// PendingSummary is the read-only reminder for one report and release preview.
type PendingSummary struct {
	State   string        `json:"state"`
	Pending int           `json:"pending"`
	Items   []PendingItem `json:"items"`
	Message string        `json:"message"`
}

// These are boundary projections, not executable plans. The notification
// collaborator sends public report/plan JSON and never executes its operations.
type pendingSection struct {
	Enabled *bool  `json:"enabled"`
	Status  string `json:"status"`
	Items   []struct {
		Status string `json:"status"`
	} `json:"items"`
	Skip     []string `json:"skip"`
	Blockers []string `json:"blockers"`
}
type pendingReport struct {
	InSync bool `json:"in_sync"`
	Routes struct {
		Items []RouteItem `json:"items"`
	} `json:"routes"`
	NAT struct {
		Items []NatItem `json:"items"`
	} `json:"nat_bypass"`
	Fake       *pendingSection `json:"ipv6_fakeip"`
	IPv6Routes *pendingSection `json:"ipv6_routes"`
	DNS        *pendingSection `json:"ipv6_dns"`
	PD         *pendingSection `json:"ipv6_pd_guard"`
	Guard      *pendingSection `json:"dns_guard"`
}
type pendingStage struct {
	Key     string `json:"key"`
	Title   string `json:"title"`
	Enabled *bool  `json:"enabled"`
	Skipped bool   `json:"skipped"`
	Ops     []struct {
		Kind string `json:"kind"`
	} `json:"ops"`
}
type pendingPlan struct {
	Stages   []pendingStage `json:"stages"`
	Blockers []string       `json:"blockers"`
}

func pendingProjection(r *Report, p *Plan) (pendingReport, pendingPlan) {
	var report pendingReport
	var plan pendingPlan
	if r != nil {
		report.InSync = r.InSync
		report.Routes.Items = r.Routes.Items
		report.NAT.Items = r.NatBypass.Items
		for _, part := range r.Parts {
			switch v := part.Section.(type) {
			case IPv6FakeIPReport:
				report.Fake = &pendingSection{Enabled: pointer(v.Enabled), Status: v.Status, Blockers: v.Blockers}
			case IPv6RoutesReport:
				report.IPv6Routes = &pendingSection{Enabled: pointer(v.Enabled), Status: v.Status, Skip: v.Skip}
				for _, i := range v.Items {
					report.IPv6Routes.Items = append(report.IPv6Routes.Items, struct {
						Status string `json:"status"`
					}{i.Status})
				}
			case IPv6DNSReport:
				report.DNS = &pendingSection{Enabled: pointer(v.Enabled), Status: v.Status, Blockers: v.Blockers}
			case IPv6PDGuardReport:
				report.PD = &pendingSection{Enabled: pointer(v.Enabled), Status: v.Status, Blockers: v.Blockers}
				for _, i := range v.Items {
					report.PD.Items = append(report.PD.Items, struct {
						Status string `json:"status"`
					}{i.Status})
				}
			case DNSGuardReport:
				report.Guard = &pendingSection{Enabled: pointer(v.Enabled), Status: v.Status, Blockers: v.Blockers}
			}
		}
	}
	if p != nil {
		plan.Blockers = p.Blockers
		for _, s := range p.Stages {
			stage := pendingStage{Key: s.Key, Title: s.Title, Enabled: s.Enabled}
			if s.Skipped != nil {
				stage.Skipped = *s.Skipped
			}
			for _, op := range s.Ops {
				stage.Ops = append(stage.Ops, struct {
					Kind string `json:"kind"`
				}{op.OpKind()})
			}
			plan.Stages = append(plan.Stages, stage)
		}
	}
	return report, plan
}

// Summarize describes a native report and plan without changing either.
func Summarize(report *Report, plan *Plan) PendingSummary {
	r, p := pendingProjection(report, plan)
	return summarizePending(r, p)
}

// SummarizeJSON is the typed public-JSON boundary used by the C07 callback.
// Malformed data is an error, never an invented synced reminder.
func SummarizeJSON(report, plan json.RawMessage) (PendingSummary, error) {
	var r pendingReport
	var p pendingPlan
	if err := json.Unmarshal(report, &r); err != nil {
		return PendingSummary{}, err
	}
	if err := json.Unmarshal(plan, &p); err != nil {
		return PendingSummary{}, err
	}
	return summarizePending(r, p), nil
}
func summarizePending(r pendingReport, p pendingPlan) PendingSummary {
	out := PendingSummary{Items: []PendingItem{}}
	skipped := false
	for _, s := range p.Stages {
		if s.Enabled != nil && !*s.Enabled {
			continue
		}
		label := orText(s.Title, s.Key)
		if s.Skipped {
			skipped = true
			out.Items = append(out.Items, PendingItem{s.Key, label, messagePendingSkipped, 0})
		} else if len(s.Ops) > 0 {
			out.Items = append(out.Items, PendingItem{s.Key, label, pendingStageWord(s, r), len(s.Ops)})
			out.Pending += len(s.Ops)
		}
	}
	blockers := pendingReasons(p.Blockers)
	switch {
	case len(blockers) > 0:
		out.State = "blocked"
		out.Message = pendingFirst(blockers)
	case out.Pending > 0:
		out.State = "pending"
	case skipped || !r.InSync:
		out.State = "blocked"
		reasons := pendingBlockedReasons(r)
		if len(reasons) == 0 {
			reasons = []string{messagePendingGenericBlocked}
		}
		out.Message = pendingFirst(reasons)
	default:
		out.State = "synced"
	}
	return out
}
func pendingReasons(values []string) []string {
	out := []string{}
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			out = append(out, v)
		}
	}
	return out
}
func pendingFirst(values []string) string {
	if len(values) > 1 {
		return fmt.Sprintf(messagePendingMore, values[0], len(values)-1)
	}
	return values[0]
}
func pendingBlockedReasons(r pendingReport) []string {
	reasons := []string{}
	if r.IPv6Routes != nil {
		reasons = append(reasons, r.IPv6Routes.Skip...)
	}
	if r.DNS != nil {
		reasons = append(reasons, r.DNS.Blockers...)
	}
	if r.PD != nil {
		reasons = append(reasons, r.PD.Blockers...)
	}
	add := func(cidr, status, detail string) {
		if status == "conflict" {
			if detail != "" {
				reasons = append(reasons, fmt.Sprintf(messagePendingConflictDetail, cidr, detail))
			} else {
				reasons = append(reasons, fmt.Sprintf(messagePendingConflict, cidr))
			}
		}
	}
	for _, i := range r.Routes.Items {
		add(i.CIDR, i.Status, i.Detail)
	}
	for _, i := range r.NAT.Items {
		add(i.CIDR, i.Status, i.Detail)
	}
	return pendingReasons(reasons)
}
func pendingStageWord(s pendingStage, r pendingReport) string {
	var section *pendingSection
	switch s.Key {
	case StageIPv6FakeIP:
		section = r.Fake
	case StageIPv6Routes:
		section = r.IPv6Routes
	case StageDNSGuard:
		section = r.Guard
	case StageIPv6PDGuard:
		section = r.PD
	}
	if section != nil && section.Enabled != nil && !*section.Enabled {
		return messagePendingWithdraw
	}
	if s.Key == StageIPv6FakeIP && section != nil {
		if word := pendingSectionWords[section.Status]; word != "" {
			return word
		}
	}
	if s.Key == StageIPv6Routes || s.Key == StageIPv6PDGuard {
		statuses := []string{}
		if section != nil {
			for _, item := range section.Items {
				if item.Status != "synced" && item.Status != "borrowed" && !slices.Contains(statuses, item.Status) {
					statuses = append(statuses, item.Status)
				}
			}
		}
		if slices.Equal(statuses, []string{"missing"}) {
			if s.Key == StageIPv6PDGuard {
				return messagePendingInstall
			}
			return messagePendingAdd
		}
		if s.Key == StageIPv6PDGuard && slices.Equal(statuses, []string{"adopt"}) {
			return messagePendingAdopt
		}
		return messagePendingUpdate
	}
	if s.Key == StageIPv6DNS {
		return messagePendingClose
	}
	if s.Key == StageReturnCleanup {
		return messagePendingRemove
	}
	kinds := []string{}
	for _, op := range s.Ops {
		if !slices.Contains(kinds, op.Kind) {
			kinds = append(kinds, op.Kind)
		}
	}
	if slices.Equal(kinds, []string{"route_comment"}) {
		return messagePendingAdopt
	}
	if len(kinds) > 0 && !slices.ContainsFunc(kinds, func(k string) bool { return k != "add" && !strings.HasSuffix(k, "_add") }) {
		return messagePendingAdd
	}
	if len(kinds) > 0 && !slices.ContainsFunc(kinds, func(k string) bool {
		return k != "delete" && !strings.HasSuffix(k, "_delete") && !strings.HasSuffix(k, "_remove")
	}) {
		return messagePendingRemove
	}
	return messagePendingUpdate
}
