package oxidns

// N8-10: delete NewProfile and ToJSONX after agents/rulesync/adapter.go and
// api/agents/client_tools_adapter.go migrate. All functions here only transcode
// external legacy collaborators; package business logic uses native documents.
import (
	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/mosdns"
	"configflow/internal/rules"
	"configflow/internal/yamlx"
	"context"
	"encoding/json"
)

// NewProfile transcodes a legacy snapshot; new owners use NewTypedProfile.
func NewProfile(raw *jsonx.Object) *Profile {
	if raw == nil {
		raw = jsonx.NewObject()
	}
	data, err := jsonx.Compact(raw)
	if err != nil {
		return &Profile{doc: M(), source: &model.Config{Profile: &model.Profile{}}}
	}
	p, err := ProfileFromJSON(data)
	if err != nil {
		return &Profile{doc: M(), source: &model.Config{Profile: &model.Profile{}}}
	}
	p.notesChanged = func(notes *Node) { raw.Set("device_notes", ToJSONX(notes)) }
	return p
}

// ToJSONX transcodes a validated document for the two remaining Agent owners.
func ToJSONX(node *Node) any {
	data, err := yamlx.ToJSON(node)
	if err != nil {
		return nil
	}
	value, err := jsonx.Parse(data)
	if err != nil {
		return nil
	}
	return value
}
func fromJSONX(value any) *Node {
	node, err := convertJSONX(value)
	if err != nil {
		return Null()
	}
	return node
}
func convertJSONX(value any) (*Node, error) {
	data, err := jsonx.Compact(value)
	if err != nil {
		return nil, err
	}
	return yamlx.FromJSON(data)
}

func legacyMembers(group *Node, config *Node) *Node {
	g, _ := ToJSONX(group).(*jsonx.Object)
	selected := M("nodes", get(config, "nodes"), "subscriptions", get(config, "subscriptions"), "subscription_aggregations", get(config, "subscription_aggregations"), "proxy_groups", get(config, "proxy_groups"))
	c, _ := ToJSONX(selected).(*jsonx.Object)
	return fromJSONX(rules.RenderMembers(g, c))
}

// ClassicalIPLinesFunc is the pre-N8 router hook. SetNativeClassicalIPLines
// provides the typed entry point; this signature remains for foreign owners.
type ClassicalIPLinesFunc func(context.Context, any, any) (any, error)

// SetClassicalIPLines transcodes old registered router/test callbacks.
func SetClassicalIPLines(fn ClassicalIPLinesFunc) func() {
	if fn == nil {
		return SetNativeClassicalIPLines(nil)
	}
	return SetNativeClassicalIPLines(func(ctx context.Context, item model.RuleConfig, library []model.RuleLibraryItem) (*bool, error) {
		rule := ruleNode(item)
		entries := L()
		for _, entry := range library {
			entries.Content = append(entries.Content, libraryNode(entry))
		}
		value, err := fn(ctx, ToJSONX(rule), ToJSONX(M("rule_library", entries)))
		result, known := value.(bool)
		if err != nil || !known {
			return nil, err
		}
		return &result, nil
	})
}

func legacyRule(node *Node) (model.RuleConfig, error) {
	var item model.RuleConfig
	return item, json.Unmarshal(ToJSON(node), &item)
}

// BuildZIP transcodes the pre-N8 configgen captured snapshot.
func BuildZIP(ctx context.Context, config *jsonx.Object, content string, downloads []mosdns.Download, files []File, baseURL string) ([]byte, error) {
	data, err := jsonx.Compact(config)
	if err != nil {
		return nil, err
	}
	profile, err := ProfileFromJSON(data)
	if err != nil {
		return nil, err
	}
	return BuildTypedZIP(ctx, profile.source, content, downloads, files, baseURL)
}
