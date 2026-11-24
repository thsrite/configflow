package discovery

import (
	"configflow/internal/jsondecode"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"configflow/internal/ctxx"
	"configflow/internal/faultprobe"
	"configflow/internal/lifecycle"
	"configflow/internal/rulefetch"
	"configflow/internal/rules"
)

// ApplyEntry is a normalized domain rule and its destination library.
type ApplyEntry struct {
	Value    string `json:"value"`
	RuleType string `json:"rule_type"`
	Target   string `json:"target"`
}

// AddedEntry identifies the library changed by an adoption.
type AddedEntry struct {
	ApplyEntry
	Ruleset   string `json:"ruleset"`
	RulesetID string `json:"ruleset_id"`
}

// SkippedEntry describes a domain already covered in the destination.
type SkippedEntry struct {
	Value  string `json:"value"`
	Target string `json:"target"`
	Reason string `json:"reason"`
}

// ApplyResult lists effective writes, duplicates and ordering warnings.
type ApplyResult struct {
	Added    []AddedEntry   `json:"added"`
	Skipped  []SkippedEntry `json:"skipped"`
	Warnings []string       `json:"warnings"`
}

// ParseApplyItems validates the manual-adoption boundary.
func ParseApplyItems(raw json.RawMessage) ([]ApplyEntry, error) {
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil || len(items) < 1 || len(items) > 500 {
		return nil, invalid(msgItems)
	}
	out := []ApplyEntry{}
	for _, item := range items {
		var fields map[string]json.RawMessage
		if json.Unmarshal(item, &fields) != nil || fields == nil {
			return nil, invalid(msgItemObject)
		}
		if raw, present := fields["rule_type"]; present && string(raw) == "null" {
			return nil, invalid(msgItem)
		}
		e := ApplyEntry{RuleType: "DOMAIN-SUFFIX"}
		if jsondecode.Unmarshal(item, &e) != nil {
			return nil, invalid(msgItem)
		}
		e.Value = CleanDomain(e.Value)
		if e.Value == "" || (e.RuleType != "DOMAIN" && e.RuleType != "DOMAIN-SUFFIX") || (e.Target != "direct" && e.Target != "proxy") {
			return nil, invalid(msgItem)
		}
		out = append(out, e)
	}
	return out, nil
}
func label(target string) string {
	switch target {
	case "direct":
		return msgDirectLabel
	case "proxy":
		return msgProxyLabel
	default:
		return fmt.Sprintf(msgPolicyLabel, strings.TrimPrefix(target, "policy:"))
	}
}
func covered(lines []string, e ApplyEntry) bool {
	for _, line := range lines {
		k, v, ok := rules.ParseRuleLine(line)
		if !ok {
			continue
		}
		if (k == e.RuleType && strings.ToLower(v) == e.Value) || (k == "DOMAIN-SUFFIX" && rules.MatchDomain(e.Value, k, v)) {
			return true
		}
	}
	return false
}
func formatLine(behavior string, e ApplyEntry) string {
	if behavior == "classical" {
		return e.RuleType + "," + e.Value
	}
	if e.RuleType == "DOMAIN-SUFFIX" {
		return "+." + e.Value
	}
	return e.Value
}

// ApplyOptions supplies an optional authorization/precondition check. BeforeWrite
// runs under the shared repository transaction before any library mutation. Its
// context carries the transaction lock; errors abort without caches or history.
type ApplyOptions struct {
	BeforeWrite func(context.Context, map[string]*RulesetInfo) error
}

// ApplyDomains commits shared rule content, refreshes served caches, and records history.
func (svc *Service) ApplyDomains(ctx context.Context, id string, entries []ApplyEntry, source string, evidence map[string]Probe, options ...ApplyOptions) (ApplyResult, error) {
	ctx = lifecycle.Detach(ctx)
	out := ApplyResult{Added: []AddedEntry{}, Skipped: []SkippedEntry{}, Warnings: []string{}}
	c, err := svc.loadConfig(ctx, id)
	if err != nil {
		return out, err
	}
	infos := map[string]*RulesetInfo{}
	targets := []string{}
	for _, e := range entries {
		if _, ok := infos[e.Target]; ok {
			continue
		}
		v := info(c, c.Settings.target(e.Target))
		if v == nil || v.Problem != nil {
			return out, invalid(fmt.Sprintf(msgWritable, label(e.Target)))
		}
		infos[e.Target] = v
		targets = append(targets, e.Target)
	}
	ids := []string{}
	for _, target := range targets {
		ids = append(ids, infos[target].ID)
	}
	r, err := svc.repo()
	if err != nil {
		return out, err
	}
	ctx, release, err := rulefetch.LockCacheEdits(ctx, r, ids...)
	if err != nil {
		return out, err
	}
	defer release()
	stages := []*rulefetch.CachePublication{}
	updated := map[string]libraryItem{}
	defer func() {
		for _, stage := range stages {
			stage.Discard(ctx)
		}
	}()
	err = svc.editLibraryContext(ctx, func(tx context.Context, items *[]libraryItem) error {
		tx = ctxx.Detach(ctxx.WithParent(tx, ctx))
		for _, option := range options {
			if option.BeforeWrite != nil {
				if err := option.BeforeWrite(tx, infos); err != nil {
					return err
				}
			}
		}
		for _, target := range targets {
			inf := infos[target]
			var item *libraryItem
			for i := range *items {
				if (*items)[i].ID == inf.ID {
					item = &(*items)[i]
				}
			}
			if problem(item) != nil {
				return invalid(msgChanged)
			}
			if err := validateContent(*item); err != nil {
				return err
			}
			lines, payload, err := rulefetch.ReadRulePayload(item.Content)
			if err != nil {
				return err
			}
			newLines := []string{}
			for _, e := range entries {
				if e.Target != target {
					continue
				}
				if covered(lines, e) {
					out.Skipped = append(out.Skipped, SkippedEntry{e.Value, target, "exists"})
					continue
				}
				line := formatLine(item.Behavior, e)
				lines = append(lines, line)
				newLines = append(newLines, line)
				out.Added = append(out.Added, AddedEntry{e, item.Name, item.ID})
			}
			if len(newLines) > 0 {
				item.Content, err = rulefetch.AppendRuleLines(item.Content, newLines, payload)
				if err != nil {
					return err
				}
				updated[item.ID] = *item
			}
		}
		// Several targets can share one library. Stage its final declaration once.
		for _, target := range targets {
			item, ok := updated[infos[target].ID]
			if !ok {
				continue
			}
			stage, err := svc.stageRule(tx, item)
			if err != nil {
				return err
			}
			stages = append(stages, stage)
			delete(updated, item.ID)
		}
		return nil
	})
	if err != nil {
		return out, err
	}
	if len(stages) > 0 {
		faultprobe.Point(ctx, faultprobe.RuleDBCommitted)
	}
	for _, stage := range stages {
		if err = stage.Commit(ctx); err != nil {
			return out, err
		}
	}
	if len(out.Added) > 0 {
		at := timestamp()
		err = svc.editSettings(ctx, id, func(s *Settings) error {
			history := []HistoryEntry{}
			for _, h := range s.History {
				replace := false
				for _, a := range out.Added {
					replace = replace || h.Value == a.Value && h.Target == a.Target
				}
				if !replace {
					history = append(history, h)
				}
			}
			for _, a := range out.Added {
				p, ok := evidence[a.Value]
				h := HistoryEntry{Value: a.Value, RuleType: a.RuleType, Target: a.Target, RulesetID: a.RulesetID, Source: source, AppliedAt: at}
				if ok {
					h.Verdict = new(p.Verdict)
					h.Confidence = new(p.Confidence)
				}
				history = append(history, h)
			}
			if len(history) > 1000 {
				history = history[len(history)-1000:]
			}
			s.History = history
			return nil
		})
		if err != nil {
			return out, err
		}
	}
	for _, target := range targets {
		v := infos[target]
		if !v.Active {
			out.Warnings = append(out.Warnings, fmt.Sprintf(msgInactive, *v.Name))
		}
	}
	match, err := svc.localMatcher(ctx, id)
	if err != nil {
		return out, err
	}
	fresh, err := svc.loadConfig(ctx, id)
	if err != nil {
		return out, err
	}
	details := []string{}
	count := 0
	for _, e := range out.Added {
		inf := infos[e.Target]
		if !inf.Active {
			continue
		}
		m := match(e.Value)
		if m == nil {
			continue
		}
		own := false
		hasOwn := false
		for _, r := range fresh.Rules {
			if r.ItemType == "ruleset" && r.LibraryID == inf.ID {
				hasOwn = true
				own = own || r.ID == m.RuleID
			}
		}
		if hasOwn && !own {
			count++
			if len(details) < 3 {
				details = append(details, fmt.Sprintf(msgShadowDetail, e.Value, m.Priority, m.RuleName, m.Policy))
			}
		}
	}
	if count > 0 {
		more := ""
		if count > 3 {
			more = fmt.Sprintf(msgShadowMore, count)
		}
		out.Warnings = append(out.Warnings, fmt.Sprintf(msgShadow, more, strings.Join(details, "；")))
	}
	return out, nil
}

// ProbeEvidence picks the strongest actionable probe or latest observation.
func (svc *Service) ProbeEvidence(ctx context.Context, id string, entries []ApplyEntry) (map[string]Probe, error) {
	ctx = lifecycle.Detach(ctx)
	as, err := svc.profileAgents(ctx, id)
	if err != nil {
		return nil, err
	}
	ps, err := svc.probes(ctx, as)
	if err != nil {
		return nil, err
	}
	out := map[string]Probe{}
	for _, e := range entries {
		found := false
		var best Probe
		for _, host := range sortedKeys(ps) {
			p := ps[host]
			if host != e.Value && !strings.HasSuffix(host, "."+e.Value) {
				continue
			}
			if !found || betterProbe(p, best) {
				best = p
				found = true
			}
		}
		if found {
			out[e.Value] = best
		}
	}
	return out, nil
}
func betterProbe(a, b Probe) bool {
	aa, ba := a.Target != nil && *a.Target != "", b.Target != nil && *b.Target != ""
	if aa != ba {
		return aa
	}
	if aa {
		return a.Confidence > b.Confidence
	}
	return a.CheckedAt > b.CheckedAt
}

// UndoDomain removes matching domain lines from the historically selected library.
func (svc *Service) UndoDomain(ctx context.Context, id, value, target string) ([]string, error) {
	ctx = lifecycle.Detach(ctx)
	value = CleanDomain(value)
	if value == "" || (target != "direct" && target != "proxy" && !strings.HasPrefix(target, "policy:")) {
		return nil, invalid(msgUndo)
	}
	c, err := svc.loadConfig(ctx, id)
	if err != nil {
		return nil, err
	}
	rid := c.Settings.target(target)
	for _, h := range c.Settings.History {
		if h.Value == value && h.Target == target {
			if h.RulesetID != "" {
				rid = new(h.RulesetID)
			}
			break
		}
	}
	removed := []string{}
	var stage *rulefetch.CachePublication
	ids := []string{}
	if rid != nil {
		ids = append(ids, *rid)
	}
	r, err := svc.repo()
	if err != nil {
		return nil, err
	}
	ctx, release, err := rulefetch.LockCacheEdits(ctx, r, ids...)
	if err != nil {
		return nil, err
	}
	defer release()
	defer func() { stage.Discard(ctx) }()
	err = svc.editLibraryContext(ctx, func(tx context.Context, items *[]libraryItem) error {
		tx = ctxx.Detach(ctxx.WithParent(tx, ctx))
		if rid == nil {
			return nil
		}
		for i := range *items {
			item := &(*items)[i]
			if item.ID != *rid || item.SourceType != "content" {
				continue
			}
			content, lines, err := rulefetch.RemoveRulePayload(item.Content, func(line string) bool {
				k, v, ok := rules.ParseRuleLine(line)
				return ok && (k == "DOMAIN" || k == "DOMAIN-SUFFIX") && strings.ToLower(v) == value
			})
			if err != nil {
				return err
			}
			for _, line := range lines {
				removed = append(removed, strings.TrimSpace(line))
			}
			if len(lines) > 0 {
				item.Content = content
				stage, err = svc.stageRule(tx, *item)
				if err != nil {
					return err
				}
			}
			break
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if stage != nil {
		faultprobe.Point(ctx, faultprobe.RuleDBCommitted)
		if err = stage.Commit(ctx); err != nil {
			return nil, err
		}
	}
	err = svc.editSettings(ctx, id, func(s *Settings) error {
		h := []HistoryEntry{}
		for _, e := range s.History {
			if e.Value != value || e.Target != target {
				h = append(h, e)
			}
		}
		s.History = h
		return nil
	})
	return removed, err
}

// HistoryPayload adds current library names to the newest-first history.
func (svc *Service) HistoryPayload(ctx context.Context, id string) ([]map[string]json.RawMessage, error) {
	c, err := svc.loadConfig(ctx, id)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(c.Settings.History, func(i, j int) bool { return c.Settings.History[i].AppliedAt > c.Settings.History[j].AppliedAt })
	out := []map[string]json.RawMessage{}
	for _, h := range c.Settings.History {
		b, err := json.Marshal(h)
		if err != nil {
			return nil, err
		}
		var item map[string]json.RawMessage
		if err = json.Unmarshal(b, &item); err != nil {
			return nil, err
		}
		item["ruleset"] = json.RawMessage("null")
		if lib := c.library(h.RulesetID); lib != nil {
			item["ruleset"], err = json.Marshal(lib.Name)
			if err != nil {
				return nil, err
			}
		}
		out = append(out, item)
	}
	return out, nil
}
