package subs

// Legacy entry points transcode the old API; native owners never call them.
import (
	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/yamlx"
	"encoding/json"
)

func decodeLegacy[T any](value any) (T, error) {
	var result T
	data, err := json.Marshal(value)
	if err == nil {
		err = json.Unmarshal(data, &result)
	}
	return result, err
}
func encodeLegacy(value any) (*jsonx.Object, error) {
	data, err := model.Marshal(value)
	if err != nil {
		return nil, err
	}
	decoded, err := jsonx.Parse(data)
	if err != nil {
		return nil, err
	}
	result, _ := decoded.(*jsonx.Object)
	return result, nil
}
func legacyNode(value any) (*yamlx.Node, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return yamlx.FromJSON(data)
}

// RelinkGroupListeners adapts a profile once and copies back only the owned member.
func RelinkGroupListeners(profile *jsonx.Object, oldName string, newName *string) ([]string, error) {
	native, err := decodeLegacy[model.Profile](profile)
	if err != nil {
		return nil, err
	}
	changes, err := RelinkListeners(&native, oldName, newName)
	if err != nil || len(changes) == 0 {
		return changes, err
	}
	settings := profile.GetObject("mihomo")
	if settings == nil {
		settings = jsonx.NewObject()
		profile.Set("mihomo", settings)
	}
	settings.Set("custom_config", native.Mihomo.CustomConfig)
	return changes, nil
}

// ValidateListenerPolicies adapts the open engine graph once.
func ValidateListenerPolicies(document *jsonx.Object) error {
	native, err := legacyNode(document)
	if err != nil {
		return err
	}
	return ValidateListenerNode(native)
}

// ValidateRegexFilter retains the strict request token boundary before native validation.
func ValidateRegexFilter(value any) string {
	if value == nil {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		return msgFilterType
	}
	return ValidateFilter(text)
}

// CompileSubscriptionFilter adapts one stored subscription record.
func CompileSubscriptionFilter(value any) Matcher {
	sub, err := decodeLegacy[model.Subscription](value)
	if err != nil {
		return nil
	}
	return CompileFilter(sub)
}

// FilterSubscriptionItems transcodes the subscription and complete proxy batch once.
func FilterSubscriptionItems(value any, items []any) []any {
	sub, err := decodeLegacy[model.Subscription](value)
	if err != nil {
		sub = model.Subscription{}
	}
	graph, err := legacyNode(items)
	if err != nil {
		return nil
	}
	selected := FilterProxies(sub, graph.Content)
	if selected == nil {
		return nil
	}
	data, err := yamlx.ToJSON(yamlx.Seq(selected...))
	if err != nil {
		return nil
	}
	decoded, err := jsonx.Parse(data)
	if err != nil {
		return nil
	}
	out, _ := decoded.([]any)
	return out
}

// ParseURIList transcodes a parsed batch for unported node consumers.
func ParseURIList(source string) []*jsonx.Object {
	parsed := ParseList(source)
	result := make([]*jsonx.Object, 0, len(parsed))
	for _, item := range parsed {
		if item.Raw != nil {
			data, err := yamlx.ToJSON(item.Raw)
			if err != nil {
				continue
			}
			decoded, err := jsonx.Parse(data)
			if err != nil {
				continue
			}
			if object, ok := decoded.(*jsonx.Object); ok {
				object.Set("_raw_object", true)
				result = append(result, object)
			}
		} else {
			value, err := encodeLegacy(item.Node)
			if err == nil {
				result = append(result, value)
			}
		}
	}
	return result
}

// ParseYAMLObject adapts one open structured node at the old boundary.
func ParseYAMLObject(source string) *jsonx.Object {
	graph := ParseObject(source)
	if graph == nil {
		return nil
	}
	data, err := yamlx.ToJSON(graph)
	if err != nil {
		return nil
	}
	value, err := jsonx.Parse(data)
	if err != nil {
		return nil
	}
	object, _ := value.(*jsonx.Object)
	if object != nil {
		object.Set("_raw_object", true)
	}
	return object
}
