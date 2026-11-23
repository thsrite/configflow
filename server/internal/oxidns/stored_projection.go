package oxidns

import (
	"fmt"
	"slices"

	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/yamlx"
)

var settingsLogger = logx.Get("configflow.oxidns.settings")

// FieldIssues reports value-free diagnostics for the captured dynamic settings.
func (p *Profile) FieldIssues() []model.FieldIssue { return slices.Clone(p.issues) }

// storedProjection consumes old dynamic settings without modifying their stored
// representation. Only the approved legacy forms have a meaningful fallback;
// malformed route objects are disabled rather than breaking unrelated DNS.
func storedProjection(source *Node) (*Node, []model.FieldIssue) {
	settings := yamlx.CopyGraph(source)
	var issues []model.FieldIssue
	issue := func(path string) { issues = append(issues, model.FieldIssue{Path: path, Kind: model.IssueType}) }
	for i, item := range items(get(settings, "custom_matches")) {
		value := get(item, "enabled")
		if value == nil || isBool(value) {
			continue
		}
		issue(fmt.Sprintf("oxidns.custom_matches[%d].enabled", i))
		enabled := false
		if isNum(value) {
			var n float64
			if yamlx.Decode(value, &n) == nil && n == 1 {
				enabled = true
			}
		}
		set(item, "enabled", B(enabled))
	}
	route := get(settings, "ros_route")
	args := get(route, "args")
	for _, key := range []string{"routing_table", "gateway4", "gateway6"} {
		value := get(args, key)
		if value == nil || isStr(value) {
			continue
		}
		issue("oxidns.ros_route.args." + key)
		if isNum(value) {
			data, err := yamlx.ToJSON(value)
			if err == nil {
				set(args, key, S(string(data)))
				continue
			}
		}
		set(args, key, S(""))
		set(route, "enabled", B(false))
	}
	for _, issue := range issues {
		settingsLogger.Warningf(messageStoredFieldIssue, issue.Path, issue.Kind)
	}
	return settings, issues
}

func checkRequestTypes(settings *Node) error {
	for _, item := range items(get(settings, "custom_matches")) {
		if value := get(item, "enabled"); value != nil && !isBool(value) {
			return fail(MatchEnabledTypeMessage)
		}
	}
	args := at(settings, "ros_route", "args")
	for _, key := range []string{"routing_table", "gateway4", "gateway6"} {
		if value := get(args, key); value != nil && !isStr(value) {
			return fail(fmt.Sprintf(RouteFieldTextMessage, key))
		}
	}
	return nil
}
