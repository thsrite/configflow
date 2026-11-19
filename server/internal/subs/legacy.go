package subs

// Legacy entry points transcode the old API; native owners never call them.
import (
	"encoding/json"

	"configflow/internal/jsondecode"
	"configflow/internal/jsonresp"
	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/yamlx"
)

func decodeLegacy[T any](value any) (T, error) {
	var result T
	data, err := json.Marshal(value)
	if err == nil {
		err = jsondecode.Unmarshal(data, &result)
	}
	return result, err
}
func encodeLegacy(value any) (*jsonx.Object, error) {
	data, err := jsonresp.Encode(value)
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
	// Preserve the native selector's source identities at this codec boundary.
	// Callers may add owned cache fields through the selected original objects.
	// Selection stays entirely in FilterProxies; this only maps its references.
	out := make([]any, 0, len(selected))
	next := 0
	for index, node := range graph.Content {
		if next < len(selected) && node == selected[next] {
			out = append(out, items[index])
			next++
		}
	}

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

// StatusLoader is the unported traffic store boundary.
type StatusLoader func(agentID string) *jsonx.Object

func legacyStatusLoader(load StatusLoader) CoreStatusLoader {
	if load == nil {
		return nil
	}
	return func(id string) CoreStatus { value, _ := decodeLegacy[CoreStatus](load(id)); return value }
}

// ReportedCoreVersion adapts one legacy agent and its owning status loader.
func ReportedCoreVersion(agent *jsonx.Object, load StatusLoader) string {
	native, err := decodeLegacy[CoreAgent](agent)
	if err != nil {
		return ""
	}
	return AgentCoreVersion(native, legacyStatusLoader(load))
}

// OutdatedAgents adapts a legacy batch and returns preview DTOs.
func OutdatedAgents(agents []*jsonx.Object, profileID, defaultID string, load StatusLoader) []any {
	native, err := decodeLegacy[[]CoreAgent](agents)
	if err != nil {
		return []any{}
	}
	reports := OldCoreAgents(native, profileID, defaultID, legacyStatusLoader(load))
	out := make([]any, 0, len(reports))
	for _, report := range reports {
		item, err := encodeLegacy(report)
		if err == nil {
			out = append(out, item)
		}
	}
	return out
}

// ProjectHostsYAMLKeys is the old graph validator name.
func ProjectHostsYAMLKeys(n *yamlx.Node) error { return ValidateHostsGraph(n) }
