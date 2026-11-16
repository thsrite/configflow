package oxidns

import "configflow/internal/jsonx"

// Port of backend/common/oxidns_match_targets.py (module C22): executable
// custom-match targets read from the current native draft.

// ReadMatchTargets is read_match_targets(document, settings): the executor
// plugins a custom match may call ({custom_match_targets: [{tag, type, exec,
// source}], custom_match_target_tags: [...]}). Plugins that reach the main
// sequence, depend on a cycle or on an ambiguous tag are left out.
func ReadMatchTargets(document *jsonx.Object, settings *jsonx.Object) (result *jsonx.Object, err error) {
	defer catch(&err)
	return readMatchTargets(document, settings), nil
}

func readMatchTargets(document *jsonx.Object, settings *jsonx.Object) *jsonx.Object {
	var sources *NativeSources
	if truthy(document.Value("include")) {
		sources = &NativeSources{}
		document = nativeDocument(settings, false, sources)
	}
	plugins := iterate(document.GetOr("plugins", []any{}))
	var origins []any
	if sources != nil {
		origins = sources.Plugins
	} else {
		for range plugins {
			origins = append(origins, jsonx.Obj("source", "config.yaml"))
		}
	}
	var tags []string
	for _, plugin := range plugins {
		if isDict(plugin) {
			if tag, ok := get(plugin, "tag", nil).(string); ok {
				tags = append(tags, tag)
			}
		}
	}
	counts := map[string]int{}
	for _, tag := range tags {
		counts[tag]++
	}
	loadSchemas()
	catalog := schemaPlugins
	graph := map[string][]string{}
	var order []string
	collectReferenceIssues(document, catalog, graph, &order)
	reverse := map[string]map[string]bool{}
	for _, tag := range order {
		reverse[tag] = map[string]bool{}
	}
	for _, owner := range order {
		for _, target := range graph[owner] {
			if reverse[target] == nil {
				reverse[target] = map[string]bool{}
			}
			reverse[target][owner] = true
		}
	}
	// Removing leaves leaves exactly the plugins that depend on a cycle.
	pendingCount := map[string]int{}
	var leaves []string
	for _, tag := range order {
		distinct := map[string]bool{}
		for _, target := range graph[tag] {
			distinct[target] = true
		}
		pendingCount[tag] = len(distinct)
		if len(distinct) == 0 {
			leaves = append(leaves, tag)
		}
	}
	for len(leaves) > 0 {
		target := leaves[0]
		leaves = leaves[1:]
		for owner := range reverse[target] {
			pendingCount[owner]--
			if pendingCount[owner] == 0 {
				leaves = append(leaves, owner)
			}
		}
	}
	unsafe := map[string]bool{}
	for tag, count := range pendingCount {
		if count != 0 {
			unsafe[tag] = true
		}
	}
	for _, tag := range tags {
		if counts[tag] != 1 || !validPluginTag(tag) {
			unsafe[tag] = true
		}
	}
	unsafe["sequence_main"] = true
	if parked, ok := settings.Value("_common_disabled").(*jsonx.Object); ok && parked != nil {
		if aliases, ok := parked.Value("renamed_plugins").(*jsonx.Object); ok && aliases != nil {
			if alias, ok := aliases.Value("sequence_main").(string); ok {
				unsafe[alias] = true
			}
		}
	}
	// An executor that reaches the main sequence would create recursion when
	// called by a custom match. Also omit dependencies on ambiguous tag names.
	var queue []string
	for tag := range unsafe {
		queue = append(queue, tag)
	}
	for len(queue) > 0 {
		target := queue[0]
		queue = queue[1:]
		for owner := range reverse[target] {
			if !unsafe[owner] {
				unsafe[owner] = true
				queue = append(queue, owner)
			}
		}
	}
	targets := []any{}
	for index, plugin := range plugins {
		if index >= len(origins) {
			break
		}
		if !isDict(plugin) {
			continue
		}
		tag, tagIsText := get(plugin, "tag", nil).(string)
		kind, kindIsText := get(plugin, "type", nil).(string)
		if !tagIsText || unsafe[tag] || !kindIsText || asObject(catalog.GetOr(kind, jsonx.NewObject())).Value("x-kind") != "executor" {
			continue
		}
		exec := "$" + tag
		if kind == "sequence" {
			exec = "goto " + tag
		}
		targets = append(targets, jsonx.Obj("tag", tag, "type", kind, "exec", exec, "source", item(origins[index], "source")))
	}
	return jsonx.Obj("custom_match_targets", targets, "custom_match_target_tags", jsonx.ListOf(dedupe(tags)))
}
