package rules

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"configflow/internal/lifecycle"
	"configflow/internal/logx"
	"configflow/internal/oxidns"
	"configflow/internal/repo"
	"configflow/internal/rulefetch"
)

var logger = logx.Get("backend.routes.rules")

// Service owns rule-page operations and their transaction boundaries.
type Service struct {
	client *rulefetch.Client
	now    func() time.Time
}

func invalid(message string) error { return repo.NewValidationError(message) }
func normalized(item, previous, profile record) error {
	if item == nil {
		return invalid(msgRuleObject)
	}
	if _, ok := item["itemType"]; !ok {
		kind := "ruleset"
		if _, ok := item["rule_type"]; ok {
			kind = "rule"
		}
		item["itemType"] = stringJSON(kind)
	}
	fallback, fallbackValid := udpChoice(item["udp_fallback"])
	ntp, present := item["ntp_direct"]
	delete(item, "udp_fallback")
	delete(item, "ntp_direct")
	if item.text("itemType") != "ruleset" && item.text("rule_type") == "MATCH" {
		if !fallbackValid || (fallback != "" && fallback != "DIRECT" && fallback != "REJECT") {
			return invalid(msgUDPFallback)
		}
		if present && string(ntp) != "true" && string(ntp) != "false" {
			return invalid(msgNTP)
		}
		if fallback != "" {
			item["udp_fallback"] = stringJSON(fallback)
		}
		if exactTrue(ntp) {
			item["ntp_direct"] = boolJSON(true)
		}
	}
	if item.text("itemType") != "ruleset" {
		changed := previous == nil
		for _, key := range []string{"itemType", "rule_type", "value"} {
			changed = changed || string(item[key]) != string(previous[key])
		}
		if changed {
			return validateSyntax(item)
		}
		return nil
	}
	lib, err := profile.list("rule_library")
	if err != nil {
		return err
	}
	if item.text("library_rule_id") == "" || find(lib, item.text("library_rule_id")) < 0 {
		return invalid(msgLibraryReference)
	}
	for _, key := range []string{"name", "url", "content", "source_type", "behavior", "format", "base_url", "library_enabled"} {
		delete(item, key)
	}
	return nil
}
func (s *Service) pinned(ctx context.Context, id string, ids []string) *bool {
	resolved, err := repo.ResolveProfileID(ctx, id)
	if err != nil {
		logger.Warning(msgWithheldWarning)
		return nil
	}
	value, err := withheld(ctx, resolved, ids)
	if err != nil {
		logger.Warning(msgWithheldWarning)
		return nil
	}
	return value
}

// List reads composition, hydrating display URLs without rewriting stored values.
func (s *Service) List(ctx context.Context, id string, sets bool) ([]record, error) {
	p, err := loadProfile(ctx, id)
	if err != nil {
		return nil, err
	}
	items, err := p.list("rule_configs")
	if err != nil {
		return nil, err
	}
	domain := strings.TrimSpace(p.object("system_config").text("server_domain"))
	out := []record{}
	for _, item := range items {
		if sets && item.text("itemType") != "ruleset" {
			continue
		}
		next := item.clone()
		if next.text("itemType") == "ruleset" && domain != "" && strings.HasPrefix(next.text("url"), "/") {
			next["url"] = stringJSON(domain + next.text("url"))
		}
		out = append(out, next)
	}
	return out, nil
}

// Create inserts a validated rule or shared-library reference at the top.
func (s *Service) Create(ctx context.Context, id string, item record, sets bool) (mutationResult, error) {
	ctx = lifecycle.Detach(ctx)
	result := mutationResult{Success: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if item == nil {
		if sets {
			return result, errors.New("rule-set request must be an object")
		}
		return result, invalid(msgRuleObject)
	}
	next := item.clone()
	if sets {
		next["itemType"] = stringJSON("ruleset")
	}
	err := editProfile(ctx, id, func(p record) error {
		if err := normalized(next, nil, p); err != nil {
			return err
		}
		items, err := p.list("rule_configs")
		if err != nil {
			return err
		}
		return p.set("rule_configs", append([]record{next}, items...))
	})
	result.Data = next
	return result, err
}

// Mutate updates or deletes one rule and its DNS selections atomically.
func (s *Service) Mutate(ctx context.Context, profileID, id string, payload record, remove, sets bool, base string) (mutationResult, error) {
	ctx = lifecycle.Detach(ctx)
	result := mutationResult{Success: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	dns := ""
	dnsPresent := false
	var dnsErr error
	if !remove {
		if payload == nil {
			return result, invalid(msgRuleObject)
		}
		payload = payload.clone()
		raw, ok := payload["dns"]
		dnsPresent = ok
		if ok {
			dns, dnsErr = dnsChoice(raw)
			delete(payload, "dns")
		}
	}
	err := editProfile(ctx, profileID, func(p record) error {
		before := p.clone()
		items, err := p.list("rule_configs")
		if err != nil {
			return err
		}
		index := find(items, id)
		if index < 0 || (sets && items[index].text("itemType") != "ruleset") {
			return missing("Rule not found")
		}
		original := items[index]
		if remove {
			native, err := nativeWithout(before, id, base)
			if err != nil {
				return err
			}
			if native != nil {
				p["oxidns"] = native
			}
			items = slices.Delete(items, index, index+1)
			if err := p.set("rule_configs", items); err != nil {
				return err
			}
			for _, engine := range []string{"mosdns", "oxidns"} {
				settings := p.object(engine)
				for _, key := range []string{"direct_rulesets", "proxy_rulesets", "direct_rules", "proxy_rules"} {
					var ids []json.RawMessage
					if json.Unmarshal(settings[key], &ids) != nil {
						continue
					}
					kept := []json.RawMessage{}
					for _, v := range ids {
						value, ok := textValue(v)
						if !ok || value != id {
							kept = append(kept, v)
						}
					}
					if err := settings.set(key, kept); err != nil {
						return err
					}
				}
				if err := p.set(engine, settings); err != nil {
					return err
				}
			}
			settings := p.object("oxidns")
			for _, key := range oxidns.RuleDNSMetadataFields() {
				var values map[string]json.RawMessage
				if json.Unmarshal(settings[key], &values) == nil && values != nil {
					delete(values, id)
					if err := settings.set(key, values); err != nil {
						return err
					}
					continue
				}
				var ids []string
				if json.Unmarshal(settings[key], &ids) == nil {
					ids = slices.DeleteFunc(ids, func(value string) bool { return value == id })
					if err := settings.set(key, ids); err != nil {
						return err
					}
				}
			}
			return p.set("oxidns", settings)
		}
		updated := original.clone()
		for key, value := range payload {
			updated[key] = value
		}
		updated["id"] = stringJSON(id)
		if sets {
			updated["itemType"] = stringJSON("ruleset")
		}
		if err := normalized(updated, original, p); err != nil {
			return err
		}
		result.Data = updated.clone()
		updated, err = hydrated(p, updated)
		if err != nil {
			return err
		}
		if updated.text("itemType") == "rule" && updated.text("rule_type") != original.text("rule_type") {
			if err := ensureDNSKind(before, updated, dns); err != nil {
				return err
			}
		}
		items[index] = updated
		if err := p.set("rule_configs", items); err != nil {
			return err
		}
		assignments := []oxidns.DNSAssignment{}
		if dnsPresent {
			assignments = append(assignments, oxidns.DNSAssignment{RuleID: id, Choice: dns})
		}
		if dnsErr != nil {
			return dnsErr
		}
		result.Warnings, err = syncDNS(before, p, assignments, base, dnsPresent)
		return err
	})
	if err == nil && !remove {
		result.PinnedWithheld = s.pinned(ctx, profileID, []string{id})
	}
	return result, err
}
func parseIDs(raw json.RawMessage) ([]string, error) {
	var ids []string
	if json.Unmarshal(raw, &ids) != nil || len(ids) == 0 {
		return nil, invalid(msgIDs)
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if id == "" || seen[id] {
			return nil, invalid(msgIDs)
		}
		seen[id] = true
	}
	return ids, nil
}
func knownPolicy(p record, policy string) bool {
	if slices.Contains([]string{"DIRECT", "REJECT", "REJECT-DROP", "PASS"}, policy) {
		return true
	}
	groups, _ := p.list("proxy_groups")
	for _, group := range groups {
		if group.text("name") == policy {
			return true
		}
	}
	return false
}

// Rename updates only selected group labels, preserving concurrent rule fields.
func (s *Service) Rename(ctx context.Context, id string, body record) error {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	name, ok := textValue(body["group_name"])
	if body == nil || !ok {
		return invalid(msgGroupBody)
	}
	ids, err := parseIDs(body["ids"])
	if err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	return editProfile(ctx, id, func(p record) error {
		items, err := p.list("rule_configs")
		if err != nil {
			return err
		}
		for _, id := range ids {
			index := find(items, id)
			if index < 0 {
				return missing(msgGroupMissing)
			}
			items[index]["group_name"] = stringJSON(name)
		}
		return p.set("rule_configs", items)
	})
}

// SetPolicy moves selected rules and their follow-DNS routing in one transaction.
func (s *Service) SetPolicy(ctx context.Context, id string, body record, base string) (mutationResult, error) {
	ctx = lifecycle.Detach(ctx)
	out := mutationResult{Success: true}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if body == nil {
		return out, invalid(msgPolicyBody)
	}
	ids, err := parseIDs(body["ids"])
	if err != nil {
		return out, err
	}
	policy, ok := textValue(body["policy"])
	if !ok || policy == "" {
		return out, invalid(msgPolicy)
	}
	group, present := body["group_name"]
	name := ""
	if present {
		name, ok = textValue(group)
		if !ok {
			return out, invalid(msgGroupText)
		}
	}
	err = editProfile(ctx, id, func(p record) error {
		if !knownPolicy(p, policy) {
			return invalid(fmt.Sprintf(msgPolicyUnknown, policy))
		}
		before := p.clone()
		items, err := p.list("rule_configs")
		if err != nil {
			return err
		}
		for _, id := range ids {
			index := find(items, id)
			if index < 0 {
				return missing(msgPolicyMissing)
			}
			items[index]["policy"] = stringJSON(policy)
			if present {
				items[index]["group_name"] = stringJSON(strings.TrimSpace(name))
			}
		}
		if err := p.set("rule_configs", items); err != nil {
			return err
		}
		out.Warnings, err = syncDNS(before, p, nil, base, false)
		return err
	})
	if err == nil {
		out.PinnedWithheld = s.pinned(ctx, id, ids)
	}
	return out, err
}

// Reorder moves IDs using the current transaction snapshot, including legacy input.
func (s *Service) Reorder(ctx context.Context, id string, body record, base string) ([]string, []string, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if len(body) == 0 {
		return nil, nil, invalid(msgNoBody)
	}
	ids, err := reorderRequest(body)
	if err != nil {
		return nil, nil, invalid(err.Error())
	}
	order, warnings := []string{}, []string{}
	err = editProfile(ctx, id, func(p record) error {
		before := p.clone()
		items, err := p.list("rule_configs")
		if err != nil {
			return err
		}
		items, missingIDs, err := reorderSelected(items, ids, defaultText(body, "position", "top"))
		if err != nil {
			return err
		}
		if len(missingIDs) > 0 {
			return missing(missingSelected(missingIDs))
		}
		if err := p.set("rule_configs", items); err != nil {
			return err
		}
		for _, item := range items {
			order = append(order, item.text("id"))
		}
		warnings, err = syncDNS(before, p, nil, base, false)
		return err
	})
	return order, warnings, err
}

// Batch validates the whole input before allocating collision-free IDs under lock.
func (s *Service) Batch(ctx context.Context, id string, body record) ([]record, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if body == nil {
		return nil, invalid(msgRuleObject)
	}
	var inputs []*string
	if json.Unmarshal(body["domains"], &inputs) != nil || len(inputs) == 0 {
		return nil, invalid(msgBatchValues)
	}
	values := make([]string, len(inputs))
	for i, value := range inputs {
		if value == nil {
			return nil, invalid(msgBatchValues)
		}
		values[i] = *value
	}
	for i, value := range values {
		item := record{"rule_type": body["rule_type"], "value": stringJSON(strings.TrimSpace(value))}
		if err := validateSyntax(item); err != nil {
			return nil, invalid(fmt.Sprintf(msgLineError, i+1, err.Error()))
		}
	}
	added := []record{}
	err := editProfile(ctx, id, func(p record) error {
		items, err := p.list("rule_configs")
		if err != nil {
			return err
		}
		used := map[string]bool{}
		for _, item := range items {
			used[item.text("id")] = true
		}
		next := 1
		for _, value := range values {
			for used[fmt.Sprintf("rule_%d", next)] {
				next++
			}
			identity := fmt.Sprintf("rule_%d", next)
			used[identity] = true
			policy := body["policy"]
			if len(policy) == 0 {
				policy = json.RawMessage("null")
			}
			added = append(added, record{"id": stringJSON(identity), "rule_type": body["rule_type"], "value": stringJSON(strings.TrimSpace(value)), "policy": policy, "enabled": boolJSON(true), "itemType": stringJSON("rule")})
		}
		return p.set("rule_configs", append(added, items...))
	})
	return added, err
}
