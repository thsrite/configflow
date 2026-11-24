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
	"configflow/internal/model"
	"configflow/internal/oxidns"
	"configflow/internal/refresh"
	"configflow/internal/repo"
	"configflow/internal/rulefetch"
	"configflow/internal/rulesource"
)

var logger = logx.Get("backend.routes.rules")

// Service owns rule-page operations and their transaction boundaries.
type Service struct {
	repo        *repo.Repository
	ruleSource  *rulesource.Service
	refresh     *refresh.Service
	client      *rulefetch.Client
	now         func() time.Time
	pinnedCheck func(ctx context.Context, profileID string, ruleIDs []string) (bool, error)
}

func (s *Service) refreshSvc(r *repo.Repository) *refresh.Service {
	if s != nil && s.refresh != nil {
		return s.refresh
	}
	return refresh.New(refresh.Options{Repo: r})
}

func (s *Service) repository(ctx context.Context) (*repo.Repository, error) {
	if s != nil && s.repo != nil {
		return s.repo, nil
	}
	return repo.GetRepository(ctx)
}

func invalid(message string) error { return repo.NewValidationError(message) }

// normalized validates a rule request (previous is the stored rule of an
// update) and drops what the rule kind does not keep: the UDP fallback and NTP
// switch belong to MATCH rules, the source members of a rule-set reference to
// its shared library entry.
func normalized(in *ruleInput, previous *model.RuleConfig, config *model.Config) error {
	base := model.RuleConfig(in.ruleWire)
	itemType := sentText(in.ItemType, base.ItemType)
	ruleType := sentText(in.RuleType, base.RuleType)
	if !itemType.Present() {
		kind := "ruleset"
		if ruleType.Present() {
			kind = "rule"
		}
		in.ItemType = okMember(kind)
		itemType = in.ItemType
	}
	fallback, fallbackValid := udpChoice(sentOptional(in.UDPFallback, base.UDPFallback))
	ntp := sentOptional(in.NTPDirect, base.NTPDirect)
	in.UDPFallback, in.NTPDirect = member[string]{}, member[bool]{}
	in.ruleWire.UDPFallback, in.ruleWire.NTPDirect = nil, nil
	if itemType.Value != "ruleset" && ruleType.Value == "MATCH" {
		if !fallbackValid || (fallback != "" && fallback != "DIRECT" && fallback != "REJECT") {
			return invalid(msgUDPFallback)
		}
		if ntp.Present() && string(ntp.Raw) != "true" && string(ntp.Raw) != "false" {
			return invalid(msgNTP)
		}
		if fallback != "" {
			in.UDPFallback = okMember(fallback)
		}
		if ntp.OK && ntp.Value {
			in.NTPDirect = okMember(true)
		}
	}
	if itemType.Value != "ruleset" {
		value := sentOptional(in.Value, base.Value)
		changed := previous == nil
		if !changed {
			changed = string(itemType.Raw) != string(sentText(member[string]{}, previous.ItemType).Raw) ||
				string(ruleType.Raw) != string(sentText(member[string]{}, previous.RuleType).Raw) ||
				string(value.Raw) != string(sentOptional(member[string]{}, previous.Value).Raw)
		}
		if changed {
			return validateSyntax(ruleType, value)
		}
		return nil
	}
	library := sentText(in.LibraryRuleID, base.LibraryRuleID).Value
	if library == "" || !slices.ContainsFunc(config.RuleLibrary, func(entry model.RuleLibraryItem) bool { return entry.ID == library }) {
		return invalid(msgLibraryReference)
	}
	for _, sent := range []*member[string]{&in.Name, &in.URL, &in.Content, &in.SourceType, &in.Behavior, &in.Format} {
		*sent = member[string]{}
	}
	in.LibraryEnabled = member[bool]{}
	in.ruleWire.Name = ""
	in.ruleWire.URL, in.ruleWire.Content, in.ruleWire.SourceType, in.ruleWire.Behavior, in.ruleWire.Format = nil, nil, nil, nil, nil
	in.ruleWire.LibraryEnabled = nil
	return nil
}

// profileView is the DNS view of config as it is before an edit. Rule edits
// replace the profile's members (new slices, new settings) instead of writing
// into them, and the view reads the rest when it is built, so a copy of the
// profile record is enough.
func profileView(config *model.Config) (*oxidns.Profile, error) {
	copy := *config
	profile := *config.Profile
	copy.Profile = &profile
	return dnsProfile(&copy)
}
func (s *Service) pinned(ctx context.Context, id string, ids []string) *bool {
	resolved, err := repo.ResolveProfileID(ctx, id)
	if err != nil {
		logger.Warning(msgWithheldWarning)
		return nil
	}
	value, err := s.withheld(ctx, resolved, ids, s.pinnedCheck)
	if err != nil {
		logger.Warning(msgWithheldWarning)
		return nil
	}
	return value
}

// List reads composition, hydrating display URLs without rewriting stored values.
func (s *Service) List(ctx context.Context, id string, sets bool) ([]model.RuleConfig, error) {
	config, err := s.loadProfile(ctx, id)
	if err != nil {
		return nil, err
	}
	domain := strings.TrimSpace(config.SystemConfig.ServerDomain)
	out := []model.RuleConfig{}
	for _, item := range config.Profile.RuleConfigs {
		if sets && item.ItemType != "ruleset" {
			continue
		}
		if item.ItemType == "ruleset" && domain != "" && strings.HasPrefix(text(item.URL), "/") {
			item.URL = new(domain + text(item.URL))
		}
		out = append(out, item)
	}
	return out, nil
}

// Create inserts a validated rule or shared-library reference at the top.
func (s *Service) Create(ctx context.Context, id string, body json.RawMessage, sets bool) (mutationResult, error) {
	ctx = lifecycle.Detach(ctx)
	result := mutationResult{Success: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if body == nil {
		if sets {
			return result, errors.New("rule-set request must be an object")
		}
		return result, invalid(msgRuleObject)
	}
	err := editProfile(ctx, id, func(config *model.Config) error {
		in := decodeRule(body, model.RuleConfig{})
		if sets {
			in.ItemType = okMember("ruleset")
		}
		if err := normalized(in, nil, config); err != nil {
			return err
		}
		rule, typeErr := in.rule()
		result.Data = &rule
		items := append([]model.RuleConfig{rule}, config.Profile.RuleConfigs...)
		if typeErr != nil {
			return ruleTypeError(items, typeErr)
		}
		config.Profile.RuleConfigs = items
		return nil
	})
	return result, err
}

// dnsRequest is the DNS choice an update may carry next to the rule members.
type dnsRequest struct {
	DNS member[string] `json:"dns"`
}

// Mutate updates or deletes one rule and its DNS selections atomically.
func (s *Service) Mutate(ctx context.Context, profileID, id string, body json.RawMessage, remove, sets bool, base string) (mutationResult, error) {
	ctx = lifecycle.Detach(ctx)
	result := mutationResult{Success: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	dns := ""
	dnsPresent := false
	var dnsErr error
	if !remove {
		request, err := decodeRequest[dnsRequest](body)
		if err != nil {
			return result, err
		}
		if request == nil {
			return result, invalid(msgRuleObject)
		}
		if dnsPresent = request.DNS.Present(); dnsPresent {
			dns, dnsErr = dnsChoice(request.DNS)
		}
	}
	err := editProfile(ctx, profileID, func(config *model.Config) error {
		items := slices.Clone(config.Profile.RuleConfigs)
		index := find(items, id)
		if index < 0 || (sets && items[index].ItemType != "ruleset") {
			return missing("Rule not found")
		}
		original := items[index]
		if remove {
			return removeRule(config, items, index, base)
		}
		before, err := profileView(config)
		if err != nil {
			return err
		}
		in := decodeRule(body, original)
		in.ID = okMember(id)
		if sets {
			in.ItemType = okMember("ruleset")
		}
		if err := normalized(in, &original, config); err != nil {
			return err
		}
		updated, typeErr := in.rule()
		result.Data = new(updated)
		updated, err = hydrated(config, updated)
		if err != nil {
			return err
		}
		if updated.ItemType == "rule" && updated.RuleType != original.RuleType {
			if err := ensureDNSKind(before, config, updated, dns); err != nil {
				return err
			}
		}
		items[index] = updated
		if typeErr != nil {
			return ruleTypeError(items, typeErr)
		}
		config.Profile.RuleConfigs = items
		assignments := []oxidns.DNSAssignment{}
		if dnsPresent {
			assignments = append(assignments, oxidns.DNSAssignment{RuleID: id, Choice: dns})
		}
		if dnsErr != nil {
			return dnsErr
		}
		result.Warnings, err = syncDNS(before, config, assignments, base, dnsPresent)
		return err
	})
	if err == nil && !remove {
		result.PinnedWithheld = s.pinned(ctx, profileID, []string{id})
	}
	return result, err
}

// removeRule deletes items[index] and every DNS selection of it: native
// routing first, then the MosDNS and OxiDNS selectors and per-rule metadata.
func removeRule(config *model.Config, items []model.RuleConfig, index int, base string) error {
	id := items[index].ID
	view, err := dnsProfile(config)
	if err != nil {
		return err
	}
	settings, err := config.Profile.OxiDNS.Node()
	if err != nil {
		return err
	}
	head, err := oxidns.ReadSettingsHead(config.Profile.OxiDNS)
	if err != nil {
		return err
	}
	native, err := nativeWithout(view, head, settings, id, base)
	if err != nil {
		return err
	}
	if native != nil {
		settings = native
	}
	config.Profile.RuleConfigs = slices.Delete(items, index, index+1)
	selectors := &config.Profile.MosDNS
	for _, list := range []*[]string{&selectors.DirectRulesets, &selectors.ProxyRulesets, &selectors.DirectRules, &selectors.ProxyRules} {
		kept := []string{}
		for _, value := range *list {
			if value != id {
				kept = append(kept, value)
			}
		}
		*list = kept
	}
	return config.Profile.OxiDNS.SetNode(oxidns.WithoutRuleReferences(settings, id))
}

// idsRequest selects rules by distinct, non-empty ids.
type idsRequest struct {
	IDs member[[]string] `json:"ids"`
}

func (r *idsRequest) ids() ([]string, error) {
	ids := r.IDs.Value
	if !r.IDs.OK || len(ids) == 0 {
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
func knownPolicy(config *model.Config, policy string) bool {
	if slices.Contains([]string{"DIRECT", "REJECT", "REJECT-DROP", "PASS"}, policy) {
		return true
	}
	return slices.ContainsFunc(config.Profile.ProxyGroups, func(group model.ProxyGroup) bool { return group.Name == policy })
}

// renameRequest is the body of a group rename.
type renameRequest struct {
	idsRequest
	GroupName member[string] `json:"group_name"`
}

// Rename updates only selected group labels, preserving concurrent rule fields.
func (s *Service) Rename(ctx context.Context, id string, body *renameRequest) error {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	if body == nil || !body.GroupName.OK {
		return invalid(msgGroupBody)
	}
	ids, err := body.ids()
	if err != nil {
		return err
	}
	name := strings.TrimSpace(body.GroupName.Value)
	return editProfile(ctx, id, func(config *model.Config) error {
		items := slices.Clone(config.Profile.RuleConfigs)
		for _, id := range ids {
			index := find(items, id)
			if index < 0 {
				return missing(msgGroupMissing)
			}
			items[index].GroupName = new(name)
		}
		config.Profile.RuleConfigs = items
		return nil
	})
}

// policyRequest is the body of a policy change.
type policyRequest struct {
	idsRequest
	Policy    member[string] `json:"policy"`
	GroupName member[string] `json:"group_name"`
	BaseURL   member[string] `json:"base_url"`
}

// SetPolicy moves selected rules and their follow-DNS routing in one transaction.
func (s *Service) SetPolicy(ctx context.Context, id string, body *policyRequest, base string) (mutationResult, error) {
	ctx = lifecycle.Detach(ctx)
	out := mutationResult{Success: true}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if body == nil {
		return out, invalid(msgPolicyBody)
	}
	ids, err := body.ids()
	if err != nil {
		return out, err
	}
	policy := body.Policy.Value
	if !body.Policy.OK || policy == "" {
		return out, invalid(msgPolicy)
	}
	present := body.GroupName.Present()
	if present && !body.GroupName.OK {
		return out, invalid(msgGroupText)
	}
	err = editProfile(ctx, id, func(config *model.Config) error {
		if !knownPolicy(config, policy) {
			return invalid(fmt.Sprintf(msgPolicyUnknown, policy))
		}
		before, err := profileView(config)
		if err != nil {
			return err
		}
		items := slices.Clone(config.Profile.RuleConfigs)
		for _, id := range ids {
			index := find(items, id)
			if index < 0 {
				return missing(msgPolicyMissing)
			}
			items[index].Policy = new(policy)
			if present {
				items[index].GroupName = new(strings.TrimSpace(body.GroupName.Value))
			}
		}
		config.Profile.RuleConfigs = items
		out.Warnings, err = syncDNS(before, config, nil, base, false)
		return err
	})
	if err == nil {
		out.PinnedWithheld = s.pinned(ctx, id, ids)
	}
	return out, err
}

// Reorder moves IDs using the current transaction snapshot, including legacy input.
func (s *Service) Reorder(ctx context.Context, id string, raw json.RawMessage, base string) ([]string, []string, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if emptyObject(raw) {
		return nil, nil, invalid(msgNoBody)
	}
	body, err := decodeRequest[orderRequest](raw)
	if err != nil {
		return nil, nil, err
	}
	ids, err := body.selection()
	if err != nil {
		return nil, nil, err
	}
	order, warnings := []string{}, []string{}
	err = editProfile(ctx, id, func(config *model.Config) error {
		before, err := profileView(config)
		if err != nil {
			return err
		}
		items, missingIDs, err := reorderSelected(config.Profile.RuleConfigs, ids, body.Position.Or("top"))
		if err != nil {
			return err
		}
		if len(missingIDs) > 0 {
			return missing(missingSelected(missingIDs))
		}
		config.Profile.RuleConfigs = items
		for _, item := range items {
			order = append(order, item.ID)
		}
		warnings, err = syncDNS(before, config, nil, base, false)
		return err
	})
	return order, warnings, err
}

// batchRequest adds one rule per value.
type batchRequest struct {
	Domains  member[[]*string] `json:"domains"`
	RuleType member[string]    `json:"rule_type"`
	Policy   member[string]    `json:"policy"`
}

// Batch validates the whole input before allocating collision-free IDs under lock.
func (s *Service) Batch(ctx context.Context, id string, body *batchRequest) ([]model.RuleConfig, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if body == nil {
		return nil, invalid(msgRuleObject)
	}
	if !body.Domains.OK || len(body.Domains.Value) == 0 {
		return nil, invalid(msgBatchValues)
	}
	values := make([]string, len(body.Domains.Value))
	for i, value := range body.Domains.Value {
		if value == nil {
			return nil, invalid(msgBatchValues)
		}
		values[i] = strings.TrimSpace(*value)
	}
	for i, value := range values {
		if err := validateSyntax(body.RuleType, okMember(value)); err != nil {
			return nil, invalid(fmt.Sprintf(msgLineError, i+1, err.Error()))
		}
	}
	// An absent or null policy is stored as an empty reference.
	policy := new(body.Policy.Value)
	policyErr := !body.Policy.OK && !body.Policy.Null()
	added := []model.RuleConfig{}
	err := editProfile(ctx, id, func(config *model.Config) error {
		items := config.Profile.RuleConfigs
		used := map[string]bool{}
		for _, item := range items {
			used[item.ID] = true
		}
		added = added[:0]
		next := 1
		for _, value := range values {
			for used[fmt.Sprintf("rule_%d", next)] {
				next++
			}
			identity := fmt.Sprintf("rule_%d", next)
			used[identity] = true
			added = append(added, model.RuleConfig{ID: identity, RuleType: body.RuleType.Value, Value: new(value), Policy: policy, Enabled: new(true), ItemType: "rule"})
		}
		all := append(slices.Clone(added), items...)
		if policyErr {
			return ruleTypeError(all, errMemberType)
		}
		config.Profile.RuleConfigs = all
		return nil
	})
	return added, err
}
