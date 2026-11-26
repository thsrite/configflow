package oxidns

// MatchTarget is an executor a custom match may call.
type MatchTarget struct {
	Tag    string `json:"tag"`
	Type   string `json:"type"`
	Exec   string `json:"exec"`
	Source string `json:"source"`
}

// MatchTargets are the executable custom-match targets of a native draft.
type MatchTargets struct {
	Targets []MatchTarget `json:"custom_match_targets"`
	Tags    []string      `json:"custom_match_target_tags"`
}

// ReadMatchTargets lists the executors a custom match can safely call: not
// ambiguous, not reaching sequence_main (or its recorded alias), not on a
// dependency cycle.
func ReadMatchTargets(document *nativeDoc, settings *Node) (MatchTargets, error) {
	var sources *Sources
	if externalIncludes(document) {
		sources = &Sources{}
		expanded, err := NativeDocument(settings, false, sources)
		if err != nil {
			return MatchTargets{}, err
		}
		document = expanded
	}
	plugins := items(get(document, "plugins"))
	var tags []string
	counts := map[string]int{}
	for _, plugin := range plugins {
		if tag, ok := tagOf(plugin); ok {
			tags = append(tags, tag)
			counts[tag]++
		}
	}
	_, catalog := schemas()
	var graph depGraph
	collectReferenceIssues(document, &graph)
	reverse := map[string][]string{}
	pending := map[string]int{}
	var leaves []string
	for _, owner := range graph.order {
		distinct := map[string]bool{}
		for _, e := range graph.edges[owner] {
			if !containsString(reverse[e.target], owner) {
				reverse[e.target] = append(reverse[e.target], owner)
			}
			distinct[e.target] = true
		}
		pending[owner] = len(distinct)
		if len(distinct) == 0 {
			leaves = append(leaves, owner)
		}
	}
	// Removing leaves leaves exactly the plugins that depend on a cycle.
	for len(leaves) > 0 {
		target := leaves[0]
		leaves = leaves[1:]
		for _, owner := range reverse[target] {
			if pending[owner]--; pending[owner] == 0 {
				leaves = append(leaves, owner)
			}
		}
	}
	unsafe := map[string]bool{"sequence_main": true}
	for tag, count := range pending {
		if count != 0 {
			unsafe[tag] = true
		}
	}
	for _, tag := range tags {
		if counts[tag] != 1 || !validTag(tag) {
			unsafe[tag] = true
		}
	}
	if alias, ok := readSettingsView(settings).renamedPlugin("sequence_main"); ok {
		unsafe[alias] = true
	}
	// An executor reaching the main sequence would recurse from a custom match.
	var queue []string
	for tag := range unsafe {
		queue = append(queue, tag)
	}
	for len(queue) > 0 {
		target := queue[0]
		queue = queue[1:]
		for _, owner := range reverse[target] {
			if !unsafe[owner] {
				unsafe[owner] = true
				queue = append(queue, owner)
			}
		}
	}
	result := MatchTargets{Targets: []MatchTarget{}, Tags: []string{}}
	for index, plugin := range plugins {
		tag, ok := tagOf(plugin)
		kind := get(plugin, "type")
		if !isMap(plugin) || !ok || unsafe[tag] || !isStr(kind) || catalog[kind.Value] == nil || catalog[kind.Value].Kind != "executor" {
			continue
		}
		exec := "$" + tag
		if kind.Value == "sequence" {
			exec = "goto " + tag
		}
		source := "config.yaml"
		if sources != nil {
			source = sources.Plugins[index].Source
		}
		result.Targets = append(result.Targets, MatchTarget{tag, kind.Value, exec, source})
	}
	for _, tag := range tags {
		if !containsString(result.Tags, tag) {
			result.Tags = append(result.Tags, tag)
		}
	}
	return result, nil
}
