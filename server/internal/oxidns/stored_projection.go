package oxidns

import (
	"bytes"
	"fmt"
	"slices"

	"configflow/internal/jsondecode"
	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/yamlx"
)

var settingsLogger = logx.Get("configflow.oxidns.settings")

// FieldIssues reports value-free diagnostics for the captured dynamic settings.
func (p *Profile) FieldIssues() []model.FieldIssue { return slices.Clone(p.issues) }

type projectionWire struct {
	CustomMatches member[[]member[struct {
		Enabled member[bool] `json:"enabled"`
	}]] `json:"custom_matches"`
	ROSRoute member[struct {
		Args member[struct {
			RoutingTable member[string] `json:"routing_table"`
			Gateway4     member[string] `json:"gateway4"`
			Gateway6     member[string] `json:"gateway6"`
		}] `json:"args"`
	}] `json:"ros_route"`
}

func decodeProjection(source *Node) (projectionWire, bool) {
	if !isMap(source) {
		return projectionWire{}, false
	}
	var w projectionWire
	if err := jsondecode.Unmarshal(ToJSON(source), &w); err != nil {
		return projectionWire{}, false
	}
	return w, true
}

func isJSONNumber(raw []byte) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && (trimmed[0] == '-' || (trimmed[0] >= '0' && trimmed[0] <= '9'))
}

// storedProjection consumes old dynamic settings without modifying their stored
// representation. Only the approved legacy forms have a meaningful fallback;
// malformed route objects are disabled rather than breaking unrelated DNS.
func storedProjection(source *Node) (*Node, []model.FieldIssue) {
	settings := yamlx.CopyGraph(source)
	w, ok := decodeProjection(source)
	if !ok {
		return settings, nil
	}
	var issues []model.FieldIssue
	issue := func(path string) { issues = append(issues, model.FieldIssue{Path: path, Kind: model.IssueType}) }
	if w.CustomMatches.OK {
		matches := items(get(settings, "custom_matches"))
		for i, item := range w.CustomMatches.Value {
			if !item.OK || !item.Value.Enabled.Present() || item.Value.Enabled.OK {
				continue
			}
			issue(fmt.Sprintf("oxidns.custom_matches[%d].enabled", i))
			enabled := false
			if isJSONNumber(item.Value.Enabled.Raw) {
				var n float64
				if jsondecode.Unmarshal(item.Value.Enabled.Raw, &n) == nil && n == 1 {
					enabled = true
				}
			}
			if i < len(matches) {
				set(matches[i], "enabled", B(enabled))
			}
		}
	}
	if w.ROSRoute.OK && w.ROSRoute.Value.Args.OK {
		a := w.ROSRoute.Value.Args.Value
		route := get(settings, "ros_route")
		args := get(route, "args")
		for _, field := range []struct {
			key string
			m   member[string]
		}{
			{"routing_table", a.RoutingTable},
			{"gateway4", a.Gateway4},
			{"gateway6", a.Gateway6},
		} {
			if !field.m.Present() || field.m.OK {
				continue
			}
			issue("oxidns.ros_route.args." + field.key)
			if isJSONNumber(field.m.Raw) {
				set(args, field.key, S(string(bytes.TrimSpace(field.m.Raw))))
				continue
			}
			set(args, field.key, S(""))
			set(route, "enabled", B(false))
		}
	}
	for _, issue := range issues {
		settingsLogger.Warningf(messageStoredFieldIssue, issue.Path, issue.Kind)
	}
	return settings, issues
}

func checkRequestTypes(settings *Node) error {
	w, ok := decodeProjection(settings)
	if !ok {
		return nil
	}
	if w.CustomMatches.OK {
		for _, item := range w.CustomMatches.Value {
			if item.OK && item.Value.Enabled.Present() && !item.Value.Enabled.OK {
				return fail(MatchEnabledTypeMessage)
			}
		}
	}
	if w.ROSRoute.OK && w.ROSRoute.Value.Args.OK {
		a := w.ROSRoute.Value.Args.Value
		for _, field := range []struct {
			key string
			m   member[string]
		}{
			{"routing_table", a.RoutingTable},
			{"gateway4", a.Gateway4},
			{"gateway6", a.Gateway6},
		} {
			if field.m.Present() && !field.m.OK {
				return fail(fmt.Sprintf(RouteFieldTextMessage, field.key))
			}
		}
	}
	return nil
}
