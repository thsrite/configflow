package rules

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"configflow/internal/jsonx"
)

// Port of backend/utils/group_candidates.py: managed proxy-group membership
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

// SourceFields, HealthFields and FilterFields are the module constants.
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

// iterate is Python iteration over a JSON value: a list yields its items, a
// string its characters, an object its keys. Other types (which make Python
// raise TypeError) yield nothing.
func iterate(value any) []any {
	switch v := value.(type) {
	case []any:
		return v
	case string:
		var out []any
		for _, r := range v {
			out = append(out, string(r))
		}
		return out
	case *jsonx.Object:
		var out []any
		for _, key := range v.Keys() {
			out = append(out, key)
		}
		return out
	}
	return nil
}

func containsEqual(list []any, value any) bool {
	for _, item := range list {
		if jsonx.Equal(item, value) {
			return true
		}
	}
	return false
}

// compileFilter compiles one user filter for filter_candidates.
func compileFilter(value any) (*PyRegex, error) {
	if value != nil {
		if _, ok := value.(string); !ok {
			return nil, candidateError("正则筛选必须是字符串或 null")
		}
	}
	text, _ := value.(string)
	if text == "" || pyStrip(text) == "" {
		return nil, nil
	}
	re, err := CompilePyRegex(pyStrip(text), false)
	if err != nil {
		var unsupported *UnsupportedRegexError
		if errors.As(err, &unsupported) {
			// Python accepts and runs this pattern (\N{name}, repeat counts
			// beyond int32); this engine cannot. Domain-level accepted
			// difference: the filter is not applied, the request is accepted.
			return nil, nil
		}
		return nil, candidateError("正则表达式无效")
	}
	return re, nil
}

// FilterCandidates applies name filters without changing source records or
// their ordering. include and exclude are strings or nil (JSON null).
func FilterCandidates(items []any, include, exclude any) ([]any, error) {
	keep, err := compileFilter(include)
	if err != nil {
		return nil, err
	}
	remove, err := compileFilter(exclude)
	if err != nil {
		return nil, err
	}
	var out []any
	for _, entry := range items {
		item, _ := entry.(*jsonx.Object)
		name := item.GetString("name")
		if (keep == nil || keep.Search(name)) && (remove == nil || !remove.Search(name)) {
			out = append(out, entry)
		}
	}
	return out, nil
}

func filterValue(group *jsonx.Object, field string) string {
	if value, ok := group.Value(field).(string); ok {
		return pyStrip(value)
	}
	return ""
}

// ProviderSelection is one provider resource with its source-specific filters.
type ProviderSelection struct {
	Item             *jsonx.Object
	Include, Exclude string
}

// Member is one (type, id) entry of the resolved member order; either may be
// any JSON value (nil for a missing key).
type Member struct{ Kind, ID any }

// Selection is the result of ManagedMembers. The source lists keep their JSON
// type because write validation must reject non-lists.
type Selection struct {
	Nodes, Subscriptions, Aggregations, Groups any
	Members                                    []Member
	Order                                      any
}

// ManagedMembers resolves source precedence and the exact ordering branch,
// without mutation.
func ManagedMembers(group *jsonx.Object) *Selection {
	nodes := group.GetOr("manual_nodes", []any{})
	subs := group.GetOr("subscriptions", []any{})
	aggs := group.GetOr("aggregations", []any{})
	groups := group.GetOr("include_groups", []any{})
	if !jsonx.Truthy(nodes) && !jsonx.Truthy(subs) && !jsonx.Truthy(aggs) && !jsonx.Truthy(groups) {
		switch group.Value("source") {
		case "node":
			nodes = group.GetOr("proxies", []any{})
		case "strategy":
			groups = group.GetOr("proxies", []any{})
		}
	}
	order := group.GetOr("proxies_order", []any{})
	var members []Member
	if jsonx.Truthy(order) {
		for _, item := range iterate(order) {
			if object, ok := item.(*jsonx.Object); ok {
				members = append(members, Member{object.Value("type"), object.Value("id")})
			}
		}
	} else {
		var nodeMembers, strategies []Member
		for _, item := range iterate(nodes) {
			nodeMembers = append(nodeMembers, Member{"node", item})
		}
		for _, item := range iterate(groups) {
			strategies = append(strategies, Member{"strategy", item})
		}
		if group.Value("proxy_order") == "strategies_first" {
			members = append(strategies, nodeMembers...)
		} else {
			members = append(nodeMembers, strategies...)
		}
	}
	return &Selection{Nodes: nodes, Subscriptions: subs, Aggregations: aggs, Groups: groups, Members: members, Order: order}
}

func objects(value any) []*jsonx.Object {
	var out []*jsonx.Object
	for _, entry := range iterate(value) {
		if item, ok := entry.(*jsonx.Object); ok {
			out = append(out, item)
		}
	}
	return out
}

// ManagedProviders returns provider resources together with their
// source-specific filters.
func ManagedProviders(group, config *jsonx.Object) []ProviderSelection {
	selection := ManagedMembers(group)
	aggregationIDs := append([]any(nil), iterate(selection.Aggregations)...)
	for _, member := range selection.Members {
		if member.Kind == "aggregation" && !containsEqual(aggregationIDs, member.ID) {
			aggregationIDs = append(aggregationIDs, member.ID)
		}
	}
	var aggregations []*jsonx.Object
	for _, id := range aggregationIDs {
		for _, item := range objects(config.Value("subscription_aggregations")) {
			if jsonx.Equal(item.Value("id"), id) && enabled(item, "enabled") {
				aggregations = append(aggregations, item)
			}
		}
	}
	var covered []any
	for _, agg := range aggregations {
		covered = append(covered, iterate(agg.GetOr("subscriptions", []any{}))...)
	}
	var subscriptions []*jsonx.Object
	for _, id := range iterate(selection.Subscriptions) {
		if containsEqual(covered, id) {
			continue
		}
		for _, item := range objects(config.Value("subscriptions")) {
			if jsonx.Equal(item.Value("id"), id) {
				subscriptions = append(subscriptions, item)
			}
		}
	}
	var out []ProviderSelection
	for _, item := range subscriptions {
		out = append(out, ProviderSelection{item, filterValue(group, "regex"), filterValue(group, "exclude_regex")})
	}
	for _, item := range aggregations {
		out = append(out, ProviderSelection{item, filterValue(group, "aggregation_regex"), filterValue(group, "aggregation_exclude_regex")})
	}
	return out
}

// RenderMembers returns exactly the consumed managed proxies/providers for
// one source group: an object with optional "proxies", "use" and "filter".
func RenderMembers(group, config *jsonx.Object) *jsonx.Object {
	selection := ManagedMembers(group)
	var proxies []any
	aggregations := append([]any(nil), iterate(selection.Aggregations)...)
	for _, member := range selection.Members {
		kind, _ := member.Kind.(string)
		switch {
		case kind == "aggregation":
			if !containsEqual(aggregations, member.ID) {
				aggregations = append(aggregations, member.ID)
			}
		case kind == "node" && (member.ID == "DIRECT" || member.ID == "REJECT"):
			proxies = append(proxies, member.ID)
		case kind == "node" || kind == "strategy":
			key := "proxy_groups"
			if kind == "node" {
				key = "nodes"
			}
			for _, item := range objects(config.Value(key)) {
				if jsonx.Equal(item.Value("id"), member.ID) {
					if jsonx.Truthy(item) {
						proxies = append(proxies, item.Value("name"))
					}
					break
				}
			}
		}
	}
	var aggItems []*jsonx.Object
	for _, item := range objects(config.Value("subscription_aggregations")) {
		if containsEqual(aggregations, item.Value("id")) && enabled(item, "enabled") {
			aggItems = append(aggItems, item)
		}
	}
	var covered []any
	for _, agg := range aggItems {
		covered = append(covered, iterate(agg.GetOr("subscriptions", []any{}))...)
	}
	var use []any
	for _, id := range iterate(selection.Subscriptions) {
		if containsEqual(covered, id) {
			continue
		}
		for _, item := range objects(config.Value("subscriptions")) {
			if jsonx.Equal(item.Value("id"), id) {
				use = append(use, item.Value("name"))
			}
		}
	}
	for _, id := range aggregations {
		for _, item := range aggItems {
			if jsonx.Equal(item.Value("id"), id) {
				use = append(use, item.Value("name"))
			}
		}
	}
	emitted := jsonx.NewObject()
	if len(proxies) > 0 {
		emitted.Set("proxies", proxies)
	}
	if len(use) > 0 {
		var unique []any
		for _, name := range use {
			if !containsEqual(unique, name) {
				unique = append(unique, name)
			}
		}
		emitted.Set("use", unique)
		var filters []string
		for _, key := range []string{"regex", "aggregation_regex"} {
			if value, ok := group.Value(key).(string); ok && pyStrip(value) != "" {
				filters = append(filters, pyStrip(value))
			}
		}
		if len(filters) == 1 {
			emitted.Set("filter", filters[0])
		} else if len(filters) > 1 {
			wrapped := make([]string, len(filters))
			for i, item := range filters {
				wrapped[i] = "(" + item + ")"
			}
			emitted.Set("filter", strings.Join(wrapped, "|"))
		}
	}
	return emitted
}

func pyTypeName(value any) string {
	switch v := value.(type) {
	case nil:
		return "NoneType"
	case bool:
		return "bool"
	case string:
		return "str"
	case jsonx.Number:
		if v.IsInt() {
			return "int"
		}
		return "float"
	case []any:
		return "list"
	case *jsonx.Object:
		return "dict"
	}
	return fmt.Sprintf("%T", value)
}

// ValidateFilters checks the four filter fields of a group. original, when
// not nil, lets an unchanged legacy non-string value through.
func ValidateFilters(group, original *jsonx.Object) error {
	for _, field := range FilterFields {
		if value := group.Value(field); value != nil {
			if _, ok := value.(string); !ok {
				// Unchanged legacy values are allowed only for final merged records.
				if original == nil || !original.Has(field) || pyTypeName(original.Value(field)) != pyTypeName(value) ||
					!jsonx.Equal(original.Value(field), value) {
					return candidateError("%s 必须是字符串或 null", field)
				}
			}
		}
		if (field == "exclude_regex" || field == "aggregation_exclude_regex") && filterValue(group, field) != "" {
			if _, err := parsePyRegex(filterValue(group, field), 0); err != nil && IsPyRegexError(err) {
				return candidateError("%s 正则表达式无效", field)
			}
		}
	}
	return nil
}

// embedded is _embedded: a user pattern as one group that keeps its meaning
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

// literalBackticks is _literal_backticks: backticks separate Mihomo filters;
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

// LowerGroupProviderFilters keeps managed exclusions scoped to each live
// provider, never manual nodes. Private definitions retain the original URL,
// so refreshes and bundled provider delivery use the same source bytes.
// Existing inclusion-only groups retain their historical rendering. main (the
// Mihomo document) is modified in place.
func LowerGroupProviderFilters(config, main *jsonx.Object) {
	providers := main.GetObject("proxy-providers")
	if providers == nil {
		providers = jsonx.NewObject()
	}
	groups := map[string]*jsonx.Object{}
	occupied := map[string]struct{}{}
	for _, name := range providers.Keys() {
		occupied[name] = struct{}{}
	}
	for _, group := range objects(main.Value("proxy-groups")) {
		name := group.GetString("name")
		groups[name] = group
		occupied[name] = struct{}{}
	}
	for _, node := range objects(main.Value("proxies")) {
		occupied[node.GetString("name")] = struct{}{}
	}
	sources := map[string]*jsonx.Object{}
	for _, group := range objects(config.Value("proxy_groups")) {
		sources[jsonx.Repr(group.Value("id"))] = group
	}
	clones := map[cloneKey]string{}
	for _, record := range objects(config.Value("proxy_groups")) {
		group := groups[record.GetString("name")]
		if group == nil {
			continue
		}
		source := record
		if found, ok := sources[jsonx.Repr(record.Value("follow_group"))]; ok {
			source = found
		}
		if filterValue(source, "exclude_regex") == "" && filterValue(source, "aggregation_exclude_regex") == "" {
			continue
		}
		selections := map[string]filterRule{}
		for _, provider := range ManagedProviders(source, config) {
			selections[provider.Item.GetString("name")] = filterRule{provider.Include, provider.Exclude}
		}
		type chosen struct {
			name string
			rule filterRule
		}
		var selected []chosen
		useList := iterate(group.GetOr("use", []any{}))
		for _, name := range useList {
			text, _ := name.(string)
			if rule, ok := selections[text]; ok && providers.Has(text) {
				selected = append(selected, chosen{text, rule})
			}
		}
		if len(selected) == 0 {
			continue
		}
		rules := map[filterRule]struct{}{}
		includes := map[string]struct{}{}
		for _, item := range selected {
			rules[item.rule] = struct{}{}
			includes[item.rule.include] = struct{}{}
		}
		if len(rules) == 1 && len(selected) == len(useList) && !strings.Contains(selected[0].rule.include, "`") {
			// Every provider shares one rule: the group filter expresses it, so
			// the providers stay shared and no node is listed twice.
			filterText := ExclusionFilter(selected[0].rule.include, selected[0].rule.exclude)
			group.Delete("filter")
			if filterText != "" {
				group.Set("filter", filterText)
			}
			continue
		}
		// A shared include stays at group level to preserve Mihomo's backtick
		// bucket ordering, including when the group becomes a chain exit.
		sharedInclude, hasShared := "", false
		if len(includes) == 1 {
			sharedInclude, hasShared = selected[0].rule.include, true
		}
		group.Delete("filter")
		if hasShared && sharedInclude != "" {
			group.Set("filter", sharedInclude)
		}
		var use []any
		for _, item := range selected {
			providerInclude := item.rule.include
			if hasShared {
				providerInclude = ""
			}
			if providerInclude == "" && item.rule.exclude == "" {
				use = append(use, item.name)
				continue
			}
			key := cloneKey{item.name, providerInclude, item.rule.exclude}
			if _, ok := clones[key]; !ok {
				base := source.GetString("name") + "_" + item.name + "_筛选"
				name, suffix := base, 2
				for {
					if _, taken := occupied[name]; !taken {
						break
					}
					name = fmt.Sprintf("%s (%d)", base, suffix)
					suffix++
				}
				occupied[name] = struct{}{}
				original, _ := providers.Value(item.name).(*jsonx.Object)
				provider := original.DeepCopy()
				sum := sha256.Sum256([]byte("(" + jsonx.Repr(key.original) + ", " + jsonx.Repr(key.include) + ", " + jsonx.Repr(key.exclude) + ")"))
				provider.Set("path", "./providers/group_filter_"+hex.EncodeToString(sum[:])[:20]+".yaml")
				if providerInclude != "" {
					provider.Set("filter", providerInclude)
				}
				if item.rule.exclude != "" {
					provider.Set("exclude-filter", literalBackticks(item.rule.exclude))
				}
				providers.Set(name, provider)
				clones[key] = name
			}
			use = append(use, clones[key])
		}
		group.Set("use", use)
	}
}

// ValidateCandidates validates the final merged group, not raw incidental
// source flags. profile and shared are the profile and shared documents;
// original is the stored record being replaced, or nil.
func ValidateCandidates(group, profile, shared, original *jsonx.Object) error {
	if group.Value("type") == "chain" {
		return nil
	}
	for _, field := range []string{"manual_nodes", "subscriptions", "aggregations", "include_groups", "proxies", "proxies_order"} {
		if group.Has(field) {
			if _, ok := group.Value(field).([]any); !ok {
				return candidateError("%s 必须是数组", field)
			}
		}
	}
	if follow := group.Value("follow_group"); follow != nil {
		if _, ok := follow.(string); !ok {
			return candidateError("follow_group 必须是 ID 字符串")
		}
	}
	if err := ValidateFilters(group, original); err != nil {
		return err
	}
	catalog := shared.Clone()
	catalog.Set("proxy_groups", profile.GetOr("proxy_groups", []any{}))
	source := group
	follow := group.Value("follow_group")
	if jsonx.Truthy(follow) {
		source = nil
		for _, item := range objects(catalog.Value("proxy_groups")) {
			if jsonx.Equal(item.Value("id"), follow) && enabled(item, "enabled") {
				source = item
				break
			}
		}
		if source == nil || source.Value("type") == "chain" || jsonx.Truthy(source.Value("follow_group")) {
			return candidateError("跟随来源必须是有效启用的普通来源组")
		}
	}
	selection := ManagedMembers(source)
	type check struct {
		values any
		kind   string
	}
	for _, c := range []check{
		{selection.Nodes, "nodes"}, {selection.Subscriptions, "subscriptions"},
		{selection.Aggregations, "subscription_aggregations"}, {selection.Groups, "proxy_groups"},
	} {
		values, ok := c.values.([]any)
		if !ok {
			return candidateError("候选来源必须是 ID 数组")
		}
		available := map[string]struct{}{}
		for _, item := range objects(catalog.Value(c.kind)) {
			if enabled(item, "enabled") {
				if id, ok := item.Value("id").(string); ok {
					available[id] = struct{}{}
				}
			}
		}
		if c.kind == "nodes" {
			available["DIRECT"], available["REJECT"] = struct{}{}, struct{}{}
		}
		for _, value := range values {
			text, isStr := value.(string)
			if _, found := available[text]; !isStr || !found {
				return candidateError("候选来源必须引用有效启用资源")
			}
		}
	}
	order, ok := selection.Order.([]any)
	if !ok {
		return candidateError("proxies_order 必须是数组")
	}
	if len(order) > 0 {
		allowed := map[string]map[string]struct{}{"node": {}, "strategy": {}, "aggregation": {}}
		for kind, values := range map[string]any{"node": selection.Nodes, "strategy": selection.Groups, "aggregation": selection.Aggregations} {
			for _, value := range values.([]any) {
				allowed[kind][value.(string)] = struct{}{}
			}
		}
		seen := map[[2]string]struct{}{}
		for _, entry := range order {
			item, isObject := entry.(*jsonx.Object)
			if !isObject || item.Len() != 2 || !item.Has("type") || !item.Has("id") {
				return candidateError("排序项必须包含且仅包含 type 和 id")
			}
			kind, kindOK := item.Value("type").(string)
			ident, identOK := item.Value("id").(string)
			if !kindOK || !identOK {
				return candidateError("排序项必须与已选择的受支持来源一致")
			}
			set, known := allowed[kind]
			if _, in := set[ident]; !known || !in {
				return candidateError("排序项必须与已选择的受支持来源一致")
			}
			if _, dup := seen[[2]string{kind, ident}]; dup {
				return candidateError("排序项不能重复")
			}
			seen[[2]string{kind, ident}] = struct{}{}
		}
		for _, value := range selection.Nodes.([]any) {
			if _, ok := seen[[2]string{"node", value.(string)}]; !ok {
				return candidateError("排序必须包含所有手动节点和引用策略组")
			}
		}
		for _, value := range selection.Groups.([]any) {
			if _, ok := seen[[2]string{"strategy", value.(string)}]; !ok {
				return candidateError("排序必须包含所有手动节点和引用策略组")
			}
		}
	}
	emitted := RenderMembers(source, catalog)
	if !jsonx.Truthy(emitted.Value("proxies")) && !jsonx.Truthy(emitted.Value("use")) {
		// Only an already empty record with unchanged source fields is grandfathered.
		if original != nil {
			same := true
			for _, key := range SourceFields {
				if !jsonx.Equal(original.Value(key), group.Value(key)) {
					same = false
					break
				}
			}
			if same {
				old := RenderMembers(original, catalog)
				if !jsonx.Truthy(old.Value("proxies")) && !jsonx.Truthy(old.Value("use")) && !jsonx.Truthy(follow) {
					return nil
				}
			}
		}
		return candidateError("策略组必须选择实际消费的候选来源，不能隐式使用 DIRECT")
	}
	return nil
}

// MergeGroupUpdate is a partial merge; optional health removal is an explicit,
// validated operation (delete_fields).
func MergeGroupUpdate(original, patch *jsonx.Object) (*jsonx.Object, error) {
	if err := ValidateFilters(patch, nil); err != nil {
		return nil, err
	}
	var deletes []string
	valid := true
	value := patch.GetOr("delete_fields", []any{})
	list, isList := value.([]any)
	if !isList {
		valid = false
	}
	seen := map[string]struct{}{}
	for _, entry := range list {
		key, isStr := entry.(string)
		if !isStr || !contains(HealthFields, key) {
			valid = false
			break
		}
		if _, dup := seen[key]; dup {
			valid = false
			break
		}
		seen[key] = struct{}{}
		deletes = append(deletes, key)
	}
	if !valid {
		return nil, candidateError("delete_fields 必须是无重复的健康检查字段数组")
	}
	for _, key := range deletes {
		if patch.Has(key) {
			return nil, candidateError("同一字段不能同时赋值和删除")
		}
	}
	merged := original.DeepCopy()
	merged.Update(patch.DeepCopy())
	merged.Delete("delete_fields")
	for _, key := range deletes {
		merged.Delete(key)
	}
	return merged, nil
}
