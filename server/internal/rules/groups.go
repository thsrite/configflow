package rules

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"configflow/internal/model"
	"configflow/internal/yamlx"
	"encoding/json"
	"slices"
)

// Managed proxy-group membership (backend/utils/group_candidates.py)
// shared by write validation and Mihomo rendering. Engine-native
// use/include-all are deliberately not managed sources. Legacy empty records
// remain readable; new writes must have a consumed source.

// CandidateValidationError is a controlled write-validation error, distinct
// from repository conflicts. Error() is the Chinese message.
type CandidateValidationError struct{ Message string }

func (e *CandidateValidationError) Error() string { return e.Message }

func candidateError(format string, args ...any) error {
	return &CandidateValidationError{Message: fmt.Sprintf(format, args...)}
}

// IsCandidateValidationError reports whether err is a CandidateValidationError.
func IsCandidateValidationError(err error) bool {
	var target *CandidateValidationError
	return errors.As(err, &target)
}

// SourceFields decide a group's members; HealthFields may be deleted by an
// update; FilterFields hold name filters.
var (
	SourceFields = []string{"manual_nodes", "subscriptions", "aggregations", "include_groups",
		"follow_group", "source", "proxies", "proxies_order"}
	HealthFields = []string{"url", "interval", "timeout", "lazy", "expected-status", "tolerance"}
	FilterFields = []string{"regex", "aggregation_regex", "exclude_regex", "aggregation_exclude_regex"}
)

func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

// Filter applies name filters without borrowing or replacing records.
func Filter[T any](items []T, include, exclude *string, nameOf func(T) string) ([]T, error) {
	keep, err := compileNameFilter(include)
	if err != nil {
		return nil, err
	}
	remove, err := compileNameFilter(exclude)
	if err != nil {
		return nil, err
	}
	var result []T
	for _, item := range items {
		name := nameOf(item)
		if (keep == nil || keep.Search(name)) && (remove == nil || !remove.Search(name)) {
			result = append(result, item)
		}
	}
	return result, nil
}
func compileNameFilter(value *string) (*Regex, error) {
	text := strings.TrimSpace(valueOr(value, ""))
	if text == "" {
		return nil, nil
	}
	re, err := CompilePyRegex(text, false)
	if err != nil {
		if !IsPyRegexError(err) {
			return nil, nil
		}
		return nil, candidateError(msgInvalidFilter)
	}
	return re, nil
}

// GroupCatalog is the narrow catalog consumed by candidate resolution.
type GroupCatalog struct {
	Nodes         []model.Node
	Subscriptions []model.Subscription
	Aggregations  []model.Aggregation
	Groups        []model.ProxyGroup
}

// GroupMember is the consumed kind/ID pair, independent of stored raw metadata.
type GroupMember struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// GroupSelection is the ordered set of managed source references.
type GroupSelection struct {
	Nodes, Subscriptions, Aggregations, Groups []string
	Members, Order                             []GroupMember
}

func groupList(value *[]string) []string {
	if value == nil {
		return []string{}
	}
	return *value
}

// SelectMembers resolves source precedence and explicit/manual member order.
func SelectMembers(group model.ProxyGroup) GroupSelection {
	result := GroupSelection{Nodes: groupList(group.ManualNodes), Subscriptions: groupList(group.Subscriptions), Aggregations: groupList(group.Aggregations), Groups: groupList(group.IncludeGroups), Order: []GroupMember{}}
	if group.ProxiesOrder != nil {
		for _, entry := range *group.ProxiesOrder {
			result.Order = append(result.Order, GroupMember{entry.Type, entry.ID})
		}
	}
	if len(result.Nodes)+len(result.Subscriptions)+len(result.Aggregations)+len(result.Groups) == 0 {
		switch valueOr(group.Source, "") {
		case "node":
			result.Nodes = groupList(group.Proxies)
		case "strategy":
			result.Groups = groupList(group.Proxies)
		}
	}
	if len(result.Order) > 0 {
		result.Members = append(result.Members, result.Order...)
	} else {
		nodes, groups := []GroupMember{}, []GroupMember{}
		for _, id := range result.Nodes {
			nodes = append(nodes, GroupMember{Type: "node", ID: id})
		}
		for _, id := range result.Groups {
			groups = append(groups, GroupMember{Type: "strategy", ID: id})
		}
		if valueOr(group.ProxyOrder, "") == "strategies_first" {
			result.Members = append(groups, nodes...)
		} else {
			result.Members = append(nodes, groups...)
		}
	}
	return result
}

// Provider is a consumed subscription or aggregation and its name filters.
type Provider struct{ ID, Name, Kind, Include, Exclude string }

func enabledResource(record model.SwitchMember, value *bool) bool {
	return model.SwitchEnabled(record, "enabled", value, true)
}
func selectedAggregationIDs(selection GroupSelection) []string {
	ids := append([]string(nil), selection.Aggregations...)
	for _, member := range selection.Members {
		if member.Type == "aggregation" && !slices.Contains(ids, member.ID) {
			ids = append(ids, member.ID)
		}
	}
	return ids
}
func groupAggregations(ids []string, catalog GroupCatalog) []model.Aggregation {
	var result []model.Aggregation
	for _, id := range ids {
		for _, item := range catalog.Aggregations {
			if item.ID == id && enabledResource(&item, item.Enabled) {
				result = append(result, item)
			}
		}
	}
	return result
}
func coveredSubscriptions(aggregations []model.Aggregation) []string {
	var result []string
	for _, item := range aggregations {
		result = append(result, item.Subscriptions...)
	}
	return result
}

// SelectProviders resolves provider coverage and source-specific filters.
func SelectProviders(group model.ProxyGroup, catalog GroupCatalog) []Provider {
	selection := SelectMembers(group)
	aggregations := groupAggregations(selectedAggregationIDs(selection), catalog)
	covered := coveredSubscriptions(aggregations)
	var result []Provider
	for _, id := range selection.Subscriptions {
		if slices.Contains(covered, id) {
			continue
		}
		for _, item := range catalog.Subscriptions {
			if item.ID == id {
				result = append(result, Provider{item.ID, item.Name, "subscription", strings.TrimSpace(valueOr(group.Regex, "")), strings.TrimSpace(valueOr(group.ExcludeRegex, ""))})
			}
		}
	}
	for _, item := range aggregations {
		result = append(result, Provider{item.ID, item.Name, "aggregation", strings.TrimSpace(valueOr(group.AggregationRegex, "")), strings.TrimSpace(valueOr(group.AggregationExcludeRegex, ""))})
	}
	return result
}

// RenderedMembers is the engine-facing managed membership projection.
type RenderedMembers struct {
	Proxies []string `json:"proxies,omitempty"`
	Use     []string `json:"use,omitempty"`
	Filter  string   `json:"filter,omitempty"`
}

// RenderGroup returns exactly the proxies/providers consumed by the source group.
func RenderGroup(group model.ProxyGroup, catalog GroupCatalog) RenderedMembers {
	selection := SelectMembers(group)
	result := RenderedMembers{}
	for _, member := range selection.Members {
		switch member.Type {
		case "node":
			if member.ID == "DIRECT" || member.ID == "REJECT" {
				result.Proxies = append(result.Proxies, member.ID)
				continue
			}
			for _, item := range catalog.Nodes {
				if item.ID == member.ID {
					result.Proxies = append(result.Proxies, item.Name)
					break
				}
			}
		case "strategy":
			for _, item := range catalog.Groups {
				if item.ID == member.ID {
					result.Proxies = append(result.Proxies, item.Name)
					break
				}
			}
		}
	}
	// Rendering preserves catalog order for coverage, then selection order for use.
	aggregationIDs := selectedAggregationIDs(selection)
	var aggregations []model.Aggregation
	for _, item := range catalog.Aggregations {
		if slices.Contains(aggregationIDs, item.ID) && enabledResource(&item, item.Enabled) {
			aggregations = append(aggregations, item)
		}
	}
	covered := coveredSubscriptions(aggregations)
	add := func(name string) {
		if !slices.Contains(result.Use, name) {
			result.Use = append(result.Use, name)
		}
	}
	for _, id := range selection.Subscriptions {
		if slices.Contains(covered, id) {
			continue
		}
		for _, item := range catalog.Subscriptions {
			if item.ID == id {
				add(item.Name)
			}
		}
	}
	for _, id := range aggregationIDs {
		for _, item := range aggregations {
			if item.ID == id {
				add(item.Name)
			}
		}
	}
	if len(result.Use) > 0 {
		var filters []string
		for _, value := range []*string{group.Regex, group.AggregationRegex} {
			if text := strings.TrimSpace(valueOr(value, "")); text != "" {
				filters = append(filters, text)
			}
		}
		if len(filters) == 1 {
			result.Filter = filters[0]
		} else if len(filters) > 1 {
			for i := range filters {
				filters[i] = "(" + filters[i] + ")"
			}
			result.Filter = strings.Join(filters, "|")
		}
	}
	return result
}
func recordMembers[T any](value T) map[string]json.RawMessage {
	data, _ := model.Marshal(value)
	var result map[string]json.RawMessage
	_ = json.Unmarshal(data, &result)
	return result
}
func equalMembers(a, b map[string]json.RawMessage, fields []string) bool {
	for _, field := range fields {
		left, right := a[field], b[field]
		if string(left) == "null" {
			left = nil
		}
		if string(right) == "null" {
			right = nil
		}
		if !bytes.Equal(left, right) {
			return false
		}
	}
	return true
}

// CheckFilters validates edited filter types, grandfathering only unchanged
// stored values. Raw evidence is inspected without converting to dynamic values.
func CheckFilters(group model.ProxyGroup, original *model.ProxyGroup) error {
	members := recordMembers(group)
	var before map[string]json.RawMessage
	if original != nil {
		before = recordMembers(*original)
	}
	for _, field := range FilterFields {
		raw := members[field]
		if len(raw) == 0 || string(raw) == "null" {
			continue
		}
		var value string
		if json.Unmarshal(raw, &value) != nil {
			if original == nil || len(before[field]) == 0 || !bytes.Equal(before[field], raw) {
				return candidateError(msgFilterType, field)
			}
			continue
		}
		if (field == "exclude_regex" || field == "aggregation_exclude_regex") && strings.TrimSpace(value) != "" {
			if _, err := CompilePyRegex(strings.TrimSpace(value), false); IsPyRegexError(err) {
				return candidateError(msgFilterExpression, field)
			}
		}
	}
	return nil
}

// embedded is a user pattern as one group that keeps its meaning
// inside a larger one. Leading global flags such as (?i) become scoped flags;
// the group does not capture, so the user's own group numbers stay the same.
func embedded(pattern string) string {
	flags := ""
	for strings.HasPrefix(pattern, "(?") {
		end := strings.IndexByte(pattern, ')')
		if end < 3 {
			break
		}
		body := pattern[2:end]
		valid := body != ""
		for i := 0; i < len(body); i++ {
			if !strings.ContainsRune("aiLmsux", rune(body[i])) {
				valid = false
				break
			}
		}
		if !valid {
			break
		}
		flags += body
		pattern = pattern[end+1:]
	}
	if flags != "" {
		return "(?" + flags + ":" + pattern + ")"
	}
	return "(?:" + pattern + ")"
}

// literalBackticks: backticks separate Mihomo filters;
// in an exclusion they are name characters. A run of backslashes before a
// backtick keeps an even number of them and the backtick becomes \x60.
func literalBackticks(pattern string) string {
	var out strings.Builder
	for i := 0; i < len(pattern); {
		j := i
		for j < len(pattern) && pattern[j] == '\\' {
			j++
		}
		if j < len(pattern) && pattern[j] == '`' {
			out.WriteString(strings.Repeat(`\`, (j-i)/2*2))
			out.WriteString(`\x60`)
			i = j + 1
			continue
		}
		if j > i {
			out.WriteString(pattern[i:j])
			i = j
			continue
		}
		out.WriteByte(pattern[i])
		i++
	}
	return out.String()
}

// ExclusionFilter is one Mihomo group filter that keeps include and drops
// exclude. A group filter leaves the group's own proxies and groups alone, so
// this excludes provider nodes only. Only for a single include: with backtick
// buckets Mihomo reorders every member by the buckets they match.
func ExclusionFilter(include, exclude string) string {
	if exclude == "" {
		return include
	}
	without := `^(?![\s\S]*?` + embedded(literalBackticks(exclude)) + `)`
	if include != "" {
		return without + `(?=[\s\S]*?` + embedded(include) + `)`
	}
	return without
}

type filterRule struct{ include, exclude string }
type cloneKey struct{ original, include, exclude string }

// LowerProviderFilters scopes exclusions to provider copies in an exclusively
// owned engine document. Copies pass through budgeted yamlx public APIs.
func LowerProviderFilters(catalog GroupCatalog, main *yamlx.Node) error {
	providers := yamlx.Get(main, "proxy-providers")
	if providers == nil {
		providers = yamlx.Map()
	}
	groups := map[string]*yamlx.Node{}
	occupied := map[string]bool{}
	for name := range yamlx.Pairs(providers) {
		occupied[name] = true
	}
	if sequence := yamlx.Get(main, "proxy-groups"); sequence != nil {
		for _, group := range sequence.Content {
			name := nodeText(yamlx.Get(group, "name"))
			groups[name] = group
			occupied[name] = true
		}
	}
	if sequence := yamlx.Get(main, "proxies"); sequence != nil {
		for _, node := range sequence.Content {
			occupied[nodeText(yamlx.Get(node, "name"))] = true
		}
	}
	sources := map[string]model.ProxyGroup{}
	for _, group := range catalog.Groups {
		sources[group.ID] = group
	}
	clones := map[cloneKey]string{}
	for _, record := range catalog.Groups {
		group := groups[record.Name]
		if group == nil {
			continue
		}
		source := record
		if followed, ok := sources[valueOr(record.FollowGroup, "")]; ok {
			source = followed
		}
		if strings.TrimSpace(valueOr(source.ExcludeRegex, "")) == "" && strings.TrimSpace(valueOr(source.AggregationExcludeRegex, "")) == "" {
			continue
		}
		selections := map[string]filterRule{}
		for _, provider := range SelectProviders(source, catalog) {
			selections[provider.Name] = filterRule{provider.Include, provider.Exclude}
		}
		type chosen struct {
			name string
			rule filterRule
		}
		var selected []chosen
		useList := yamlx.Get(group, "use")
		if useList == nil {
			continue
		}
		for _, entry := range useList.Content {
			name := nodeText(entry)
			if rule, ok := selections[name]; ok && yamlx.Get(providers, name) != nil {
				selected = append(selected, chosen{name, rule})
			}
		}
		if len(selected) == 0 {
			continue
		}
		rules := map[filterRule]bool{}
		includes := map[string]bool{}
		for _, item := range selected {
			rules[item.rule] = true
			includes[item.rule.include] = true
		}
		if len(rules) == 1 && len(selected) == len(useList.Content) && !strings.Contains(selected[0].rule.include, "`") {
			text := ExclusionFilter(selected[0].rule.include, selected[0].rule.exclude)
			yamlx.Delete(group, "filter")
			if text != "" {
				yamlx.Set(group, "filter", yamlx.Str(text))
			}
			continue
		}
		sharedInclude, hasShared := "", false
		if len(includes) == 1 {
			sharedInclude, hasShared = selected[0].rule.include, true
		}
		yamlx.Delete(group, "filter")
		if hasShared && sharedInclude != "" {
			yamlx.Set(group, "filter", yamlx.Str(sharedInclude))
		}
		use := yamlx.Seq()
		for _, item := range selected {
			include := item.rule.include
			if hasShared {
				include = ""
			}
			if include == "" && item.rule.exclude == "" {
				use.Content = append(use.Content, yamlx.Str(item.name))
				continue
			}
			key := cloneKey{item.name, include, item.rule.exclude}
			if clones[key] == "" {
				base := fmt.Sprintf(msgPrivateProviderName, source.Name, item.name)
				name := base
				for suffix := 2; occupied[name]; suffix++ {
					name = fmt.Sprintf("%s (%d)", base, suffix)
				}
				occupied[name] = true
				provider, err := yamlx.Encode(yamlx.Get(providers, item.name))
				if err != nil {
					return err
				}
				encoded, _ := json.Marshal([3]string{key.original, key.include, key.exclude})
				sum := sha256.Sum256(encoded)
				yamlx.Set(provider, "path", yamlx.Str("./providers/group_filter_"+hex.EncodeToString(sum[:])[:20]+".yaml"))
				if include != "" {
					yamlx.Set(provider, "filter", yamlx.Str(include))
				}
				if item.rule.exclude != "" {
					yamlx.Set(provider, "exclude-filter", yamlx.Str(literalBackticks(item.rule.exclude)))
				}
				yamlx.Set(providers, name, provider)
				clones[key] = name
			}
			use.Content = append(use.Content, yamlx.Str(clones[key]))
		}
		yamlx.Set(group, "use", use)
	}
	if len(clones) > 0 {
		// A provider may refer to an anchor elsewhere in the input document.
		// Encode copied that bounded graph; materialize its first consumer
		// and make copied anchor names unique in the complete owned document.
		yamlx.MaterializeAliases(main)
	}
	return nil
}
func nodeText(node *yamlx.Node) string {
	if node == nil {
		return ""
	}
	return node.Value
}

// CheckCandidates validates the final merged group under the write owner's
// transaction. Malformed source arrays cannot gain validity through R5 fallback.
func CheckCandidates(group model.ProxyGroup, catalog GroupCatalog, original *model.ProxyGroup) error {
	if group.Type == "chain" {
		return nil
	}
	members := recordMembers(group)
	for _, field := range []string{"manual_nodes", "subscriptions", "aggregations", "include_groups", "proxies", "proxies_order"} {
		if raw, present := members[field]; present {
			var list []json.RawMessage
			if string(raw) == "null" || json.Unmarshal(raw, &list) != nil {
				return candidateError(msgSourceArray, field)
			}
		}
	}
	if raw := members["follow_group"]; len(raw) > 0 && string(raw) != "null" {
		var text string
		if json.Unmarshal(raw, &text) != nil {
			return candidateError(msgFollowID)
		}
	}
	if err := CheckFilters(group, original); err != nil {
		return err
	}
	source := group
	follow := valueOr(group.FollowGroup, "")
	if follow != "" {
		found := false
		for _, item := range catalog.Groups {
			if item.ID == follow && enabledResource(&item, item.Enabled) {
				source = item
				found = true
				break
			}
		}
		if !found || source.Type == "chain" || valueOr(source.FollowGroup, "") != "" {
			return candidateError(msgFollowSource)
		}
	}
	selection := SelectMembers(source)
	sourceMembers := recordMembers(source)
	// Requests require string IDs even when a stored codec can read number text.
	for _, field := range []string{"manual_nodes", "subscriptions", "aggregations", "include_groups", "proxies"} {
		if raw, present := sourceMembers[field]; present {
			var entries []json.RawMessage
			if string(raw) == "null" || json.Unmarshal(raw, &entries) != nil {
				return candidateError(msgCandidateIDs)
			}
			for _, entry := range entries {
				var text string
				if string(entry) == "null" || json.Unmarshal(entry, &text) != nil {
					return candidateError(msgCandidateResource)
				}
			}
		}
	}
	available := map[string]map[string]bool{"nodes": {"DIRECT": true, "REJECT": true}, "subscriptions": {}, "aggregations": {}, "groups": {}}
	for _, item := range catalog.Nodes {
		if enabledResource(&item, item.Enabled) {
			available["nodes"][item.ID] = true
		}
	}
	for _, item := range catalog.Subscriptions {
		if enabledResource(&item, item.Enabled) {
			available["subscriptions"][item.ID] = true
		}
	}
	for _, item := range catalog.Aggregations {
		if enabledResource(&item, item.Enabled) {
			available["aggregations"][item.ID] = true
		}
	}
	for _, item := range catalog.Groups {
		if enabledResource(&item, item.Enabled) {
			available["groups"][item.ID] = true
		}
	}
	for kind, ids := range map[string][]string{"nodes": selection.Nodes, "subscriptions": selection.Subscriptions, "aggregations": selection.Aggregations, "groups": selection.Groups} {
		for _, id := range ids {
			if !available[kind][id] {
				return candidateError(msgCandidateResource)
			}
		}
	}
	if raw, present := sourceMembers["proxies_order"]; present {
		var entries []json.RawMessage
		if string(raw) == "null" || json.Unmarshal(raw, &entries) != nil {
			return candidateError(msgGroupOrderArray)
		}
		for _, entry := range entries {
			var members map[string]json.RawMessage
			if json.Unmarshal(entry, &members) != nil || len(members) != 2 || members["type"] == nil || members["id"] == nil {
				return candidateError(msgOrderPair)
			}
			var kind, id string
			if string(members["type"]) == "null" || string(members["id"]) == "null" || json.Unmarshal(members["type"], &kind) != nil || json.Unmarshal(members["id"], &id) != nil {
				return candidateError(msgOrderSource)
			}
		}
	}
	if len(selection.Order) > 0 {
		allowed := map[string][]string{"node": selection.Nodes, "strategy": selection.Groups, "aggregation": selection.Aggregations}
		seen := map[[2]string]bool{}
		for _, entry := range selection.Order {
			kind, id := entry.Type, entry.ID
			if !slices.Contains(allowed[kind], id) {
				return candidateError(msgOrderSource)
			}
			key := [2]string{kind, id}
			if seen[key] {
				return candidateError(msgOrderRepeated)
			}
			seen[key] = true
		}
		for _, id := range selection.Nodes {
			if !seen[[2]string{"node", id}] {
				return candidateError(msgOrderComplete)
			}
		}
		for _, id := range selection.Groups {
			if !seen[[2]string{"strategy", id}] {
				return candidateError(msgOrderComplete)
			}
		}
	}
	rendered := RenderGroup(source, catalog)
	if len(rendered.Proxies)+len(rendered.Use) == 0 {
		if original != nil && equalMembers(recordMembers(*original), members, SourceFields) && follow == "" {
			old := RenderGroup(*original, catalog)
			if len(old.Proxies)+len(old.Use) == 0 {
				return nil
			}
		}
		return candidateError(msgNoGroupCandidates)
	}
	return nil
}

// MergeGroup applies a raw request patch while preserving untouched field order
// and explicit health-field deletion. Core membership remains typed.
func MergeGroup(original model.ProxyGroup, patch model.Fields) (model.ProxyGroup, error) {
	var zero model.ProxyGroup
	data, err := patch.MarshalJSON()
	if err != nil {
		return zero, err
	}
	var requested model.ProxyGroup
	if err := json.Unmarshal(data, &requested); err != nil {
		return zero, err
	}
	if err := CheckFilters(requested, nil); err != nil {
		return zero, err
	}
	var deletes []string
	if raw, present := patch.Get("delete_fields"); present {
		if string(raw) == "null" || json.Unmarshal(raw, &deletes) != nil {
			return zero, candidateError(msgDeleteFields)
		}
	}
	seen := map[string]bool{}
	for _, field := range deletes {
		if !slices.Contains(HealthFields, field) || seen[field] {
			return zero, candidateError(msgDeleteFields)
		}
		seen[field] = true
	}
	for _, field := range deletes {
		if _, present := patch.Get(field); present {
			return zero, candidateError(msgAssignDelete)
		}
	}
	data, err = model.Marshal(original)
	if err != nil {
		return zero, err
	}
	var members model.Fields
	if err := members.UnmarshalJSON(data); err != nil {
		return zero, err
	}
	for field, value := range patch.All() {
		if field != "delete_fields" {
			if err := members.Set(field, value); err != nil {
				return zero, err
			}
		}
	}
	for _, field := range deletes {
		members.Delete(field)
	}
	data, err = members.MarshalJSON()
	if err != nil {
		return zero, err
	}
	var merged model.ProxyGroup
	err = json.Unmarshal(data, &merged)
	return merged, err
}

func trimFilter(value string) string { return strings.TrimSpace(value) }

// GroupResources borrows only the consumed catalogs of a resolved snapshot.
// The returned slices remain read-only and must not outlive that snapshot.
func GroupResources(config *model.Config) GroupCatalog {
	if config == nil {
		return GroupCatalog{}
	}
	result := GroupCatalog{Nodes: config.Nodes, Subscriptions: config.Subscriptions, Aggregations: config.Aggregations}
	if config.Profile != nil {
		result.Groups = config.Profile.ProxyGroups
	}
	return result
}

// RenderGroupDocument returns an owned engine mapping of managed members.
func RenderGroupDocument(group model.ProxyGroup, catalog GroupCatalog) *yamlx.Node {
	members := RenderGroup(group, catalog)
	result := yamlx.Map()
	if len(members.Proxies) > 0 {
		sequence := yamlx.Seq()
		for _, name := range members.Proxies {
			sequence.Content = append(sequence.Content, yamlx.Str(name))
		}
		yamlx.Set(result, "proxies", sequence)
	}
	if len(members.Use) > 0 {
		sequence := yamlx.Seq()
		for _, name := range members.Use {
			sequence.Content = append(sequence.Content, yamlx.Str(name))
		}
		yamlx.Set(result, "use", sequence)
	}
	if members.Filter != "" {
		yamlx.Set(result, "filter", yamlx.Str(members.Filter))
	}
	return result
}
