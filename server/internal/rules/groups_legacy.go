package rules

import (
	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/yamlbridge"
)

func legacyGroup(value *jsonx.Object) model.ProxyGroup {
	var group model.ProxyGroup
	_ = decodeRecord(value, &group)
	return group
}
func groupCatalog(config *jsonx.Object) GroupCatalog {
	result := GroupCatalog{}
	// Names, identity and enabled state are all the renderer consumes. Never
	// serialize node params or a full resolved config for a group lookup.
	project := func(collection string, fields ...string) []any {
		var items []any
		for _, entry := range config.GetList(collection) {
			object, ok := entry.(*jsonx.Object)
			if !ok {
				continue
			}
			item := jsonx.NewObject()
			for _, field := range fields {
				if value, present := object.Get(field); present {
					item.Set(field, value)
				}
			}
			items = append(items, item)
		}
		return items
	}
	_ = decodeRecord(project("nodes", "id", "name", "enabled"), &result.Nodes)
	_ = decodeRecord(project("subscriptions", "id", "name", "enabled"), &result.Subscriptions)
	_ = decodeRecord(project("subscription_aggregations", "id", "name", "enabled", "subscriptions"), &result.Aggregations)
	_ = decodeRecord(config.GetList("proxy_groups"), &result.Groups)
	return result
}
func oldObject(value any) *jsonx.Object {
	var object *jsonx.Object
	_ = decodeRecord(value, &object)
	return object
}
func oldStrings(values []string) []any {
	out := make([]any, len(values))
	for i, value := range values {
		out[i] = value
	}
	return out
}
func optionalFilter(value any) (*string, error) {
	if value == nil {
		return nil, nil
	}
	text, ok := value.(string)
	if !ok {
		return nil, candidateError(msgFilterString)
	}
	return &text, nil
}

// FilterCandidates validates the unported request shape before native filtering.
func FilterCandidates(items []any, include, exclude any) ([]any, error) {
	keep, err := optionalFilter(include)
	if err != nil {
		return nil, err
	}
	remove, err := optionalFilter(exclude)
	if err != nil {
		return nil, err
	}
	return Filter(items, keep, remove, func(value any) string { object, _ := value.(*jsonx.Object); return object.GetString("name") })
}
func filterValue(group *jsonx.Object, field string) string {
	value, _ := group.Value(field).(string)
	return trimFilter(value)
}

// Member adapts the historical request/reference representation.
type Member struct{ Kind, ID any }

// Selection is the temporary unported membership DTO.
type Selection struct {
	Nodes, Subscriptions, Aggregations, Groups any
	Members                                    []Member
	Order                                      any
}

// ManagedMembers adapts native source selection to existing converters.
func ManagedMembers(group *jsonx.Object) *Selection {
	value := SelectMembers(legacyGroup(group))
	result := &Selection{Nodes: oldStrings(value.Nodes), Subscriptions: oldStrings(value.Subscriptions), Aggregations: oldStrings(value.Aggregations), Groups: oldStrings(value.Groups)}
	for _, member := range value.Members {
		result.Members = append(result.Members, Member{member.Type, member.ID})
	}
	order := make([]any, len(value.Order))
	for i, entry := range value.Order {
		order[i] = oldObject(entry)
	}
	result.Order = order
	return result
}

// ProviderSelection adapts resource records for existing converters.
type ProviderSelection struct {
	Item             *jsonx.Object
	Include, Exclude string
}

// ManagedProviders retains original resource objects at the legacy boundary.
func ManagedProviders(group, config *jsonx.Object) []ProviderSelection {
	var result []ProviderSelection
	for _, provider := range SelectProviders(legacyGroup(group), groupCatalog(config)) {
		collection := "subscriptions"
		if provider.Kind == "aggregation" {
			collection = "subscription_aggregations"
		}
		for _, entry := range config.GetList(collection) {
			object, ok := entry.(*jsonx.Object)
			if ok && object.GetString("id") == provider.ID {
				result = append(result, ProviderSelection{object, provider.Include, provider.Exclude})
				break
			}
		}
	}
	return result
}

// RenderMembers adapts the native managed membership DTO.
func RenderMembers(group, config *jsonx.Object) *jsonx.Object {
	return oldObject(RenderGroup(legacyGroup(group), groupCatalog(config)))
}

// ValidateFilters adapts a final merged stored record.
func ValidateFilters(group, original *jsonx.Object) error {
	var before *model.ProxyGroup
	if original != nil {
		value := legacyGroup(original)
		before = &value
	}
	return CheckFilters(legacyGroup(group), before)
}

// LowerGroupProviderFilters adapts an unported generated engine document.
func LowerGroupProviderFilters(config, main *jsonx.Object) {
	node, err := yamlbridge.Encode(main)
	if err != nil {
		return
	}
	if err := LowerProviderFilters(groupCatalog(config), node); err != nil {
		return
	}
	value, err := yamlbridge.Value(node)
	if err != nil {
		return
	}
	object, ok := value.(*jsonx.Object)
	if !ok {
		return
	}
	for _, key := range main.Keys() {
		main.Delete(key)
	}
	main.Update(object)
}

// ValidateCandidates projects only consumed resources at the API boundary.
func ValidateCandidates(group, profile, shared, original *jsonx.Object) error {
	catalog := groupCatalog(shared)
	_ = decodeRecord(profile.GetList("proxy_groups"), &catalog.Groups)
	var before *model.ProxyGroup
	if original != nil {
		value := legacyGroup(original)
		before = &value
	}
	return CheckCandidates(legacyGroup(group), catalog, before)
}

// MergeGroupUpdate preserves raw patch members until the native merge validates them.
func MergeGroupUpdate(original, patch *jsonx.Object) (*jsonx.Object, error) {
	var fields model.Fields
	if err := decodeRecord(patch, &fields); err != nil {
		return nil, err
	}
	group, err := MergeGroup(legacyGroup(original), fields)
	if err != nil {
		return nil, err
	}
	return oldObject(group), nil
}
