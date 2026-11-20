package rules

import (
	"encoding/json"

	"configflow/internal/jsondecode"
	"configflow/internal/jsonx"
	"configflow/internal/model"
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

// RenderMembers adapts the native managed membership DTO.
func RenderMembers(group, config *jsonx.Object) *jsonx.Object {
	return oldObject(RenderGroup(legacyGroup(group), groupCatalog(config)))
}

func decodeRecord(source, target any) error {
	data, err := json.Marshal(source)
	if err != nil {
		return err
	}
	return jsondecode.Unmarshal(data, target)
}
