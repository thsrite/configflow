package mosdns

import (
	"strings"

	"configflow/internal/convert"
	"configflow/internal/jsonx"
	"configflow/internal/pystr"
)

// builtinTags is _BUILTIN_TAGS.
var builtinTags = []string{
	"lazy_cache", "forward_local", "forward_remote", "forward_remote_fallback",
	"custom_hosts", "remote_dns", "remote_dns_fallback", "proxy_dns", "proxy_dns_seq",
	"china_dns", "sequence_main", "ip_requery_proxy",
}

// normalizeRulesetID is _normalize_ruleset_id: the id without a "ruleset_"
// prefix (AttributeError for a truthy id that is not a string).
func normalizeRulesetID(id any) string {
	if !truthy(id) {
		return ""
	}
	s, ok := id.(string)
	if !ok {
		raise(attributeError(id, "startswith"))
	}
	return strings.TrimPrefix(s, "ruleset_")
}

// SplitRulesAndRulesets is split_rules_and_rulesets: the enabled rules and
// rule sets of rule_configs, or the legacy rules/rule_sets fields when
// rule_configs is empty. The legacy values are returned as they are stored.
func SplitRulesAndRulesets(config *jsonx.Object) (rules, ruleSets any, err error) {
	defer catch(&err)
	rules, ruleSets = splitRulesAndRulesets(config)
	return rules, ruleSets, nil
}

func splitRulesAndRulesets(config *jsonx.Object) (any, any) {
	allRules := get(config, "rule_configs", []any{})
	var rules, ruleSets any = []any{}, []any{}
	ruleList, setList := []any{}, []any{}
	for _, entry := range iterate(allRules) {
		if !truthy(get(entry, "enabled", true)) || !truthy(get(entry, "library_enabled", true)) {
			continue
		}
		itemType := get(entry, "itemType", "")
		if itemType == "rule" {
			ruleList = append(ruleList, entry)
		} else if itemType == "ruleset" {
			setList = append(setList, entry)
		}
	}
	rules, ruleSets = ruleList, setList
	if !truthy(allRules) {
		rules = get(config, "rules", []any{})
		ruleSets = get(config, "rule_sets", []any{})
	}
	return rules, ruleSets
}

// rulesetMap is {rule_set.get('id'): rule_set for rule_set in rule_sets_list
// if rule_set.get('id')} (later entries win; first position kept).
type rulesetMap struct {
	values map[string]any
}

func buildRulesetMap(ruleSets any, onlyTruthy bool) rulesetMap {
	m := rulesetMap{values: map[string]any{}}
	for _, rs := range iterate(ruleSets) {
		id := get(rs, "id", nil)
		if onlyTruthy && !truthy(get(rs, "id", nil)) {
			continue
		}
		m.values[hashKey(id)] = rs
	}
	return m
}

func (m rulesetMap) get(id any, def any) any {
	if v, ok := m.values[hashKey(id)]; ok {
		return v
	}
	return def
}

// rulesetExecutionOrder is _ruleset_execution_order: rule set ids in
// rule_configs order, then the remaining declared rule sets.
func rulesetExecutionOrder(config *jsonx.Object, ruleSets any) []any {
	allRules := get(config, "rule_configs", []any{})
	ordered := []any{}
	for _, entry := range iterate(allRules) {
		if get(entry, "itemType", nil) == "ruleset" && truthy(get(entry, "id", nil)) {
			ordered = append(ordered, get(entry, "id", nil))
		}
	}
	if len(ordered) == 0 {
		out := []any{}
		for _, rs := range iterate(ruleSets) {
			if truthy(get(rs, "id", nil)) {
				out = append(out, get(rs, "id", nil))
			}
		}
		return out
	}
	known := pySet{}
	for _, id := range ordered {
		known[hashKey(id)] = true
	}
	for _, rs := range iterate(ruleSets) {
		id := get(rs, "id", nil)
		if truthy(id) && !known.has(id) {
			ordered = append(ordered, id)
			known[hashKey(id)] = true
		}
	}
	return ordered
}

// tagMap is a dict from rule set id to tag.
type tagMap map[string]string

func (t tagMap) lookup(id any) (string, bool) {
	v, ok := t[hashKey(id)]
	return v, ok
}

// buildRulesetTagMap is _build_ruleset_tag_map: familiar tags where
// possible, with room for the "_ip" tag of classical rule sets.
func buildRulesetTagMap(sets rulesetMap, order []any, reserved []string) tagMap {
	tags := tagMap{}
	used := map[string]bool{}
	for _, t := range builtinTags {
		used[t] = true
	}
	for _, t := range reserved {
		used[t] = true
	}
	for _, id := range order {
		rs := sets.get(id, jsonx.NewObject())
		nameValue := get(rs, "name", nil)
		if !truthy(nameValue) {
			nameValue = ""
		}
		name := strip(nameValue)
		if name == "" {
			name = normalizeRulesetID(id)
		}
		suffixes := []string{""}
		if get(rs, "behavior", nil) == "classical" {
			suffixes = []string{"", "_ip"}
		}
		tag, index := name, 1
		for {
			taken := false
			for _, suffix := range suffixes {
				if used[tag+suffix] {
					taken = true
					break
				}
			}
			if !taken {
				break
			}
			index++
			tag = name + "_" + itoa(index)
		}
		for _, suffix := range suffixes {
			used[tag+suffix] = true
		}
		tags[hashKey(id)] = tag
	}
	return tags
}

// ruleGroup is one entry of _single_rule_groups.
type ruleGroup struct {
	tag, part, path string
	ids             []any
	lines           []any
}

// singleRuleGroups is _single_rule_groups: only adjacent rules with the same
// route and matcher share a file.
func singleRuleGroups(config *jsonx.Object) []*ruleGroup {
	settings := get(config, "mosdns", jsonx.NewObject())
	direct := newSet(get(settings, "direct_rules", []any{}))
	proxy := newSet(get(settings, "proxy_rules", []any{}))
	selected := pySet{}
	for k := range direct {
		selected[k] = true
	}
	for k := range proxy {
		selected[k] = true
	}
	var groups []*ruleGroup
	counts := map[string]int{}
	previous := ""
	items := get(config, "rule_configs", nil)
	if !truthy(items) {
		items = get(config, "rules", []any{})
	}
	for _, entry := range iterate(items) {
		ident, kind := get(entry, "id", nil), get(entry, "rule_type", nil)
		if get(entry, "itemType", "rule") != "rule" || !truthy(get(entry, "enabled", true)) ||
			!truthy(get(entry, "library_enabled", true)) || !selected.has(ident) {
			previous = ""
			continue
		}
		var part string
		var value any
		hashKey(kind) // `kind in _CLASH_DOMAIN_PREFIXES` hashes kind
		kindText, _ := kind.(string)
		if prefix, ok := clashDomainPrefix(kindText); ok && kind != nil {
			part = PartDomain
			value = prefix + ":" + str(get(entry, "value", ""))
		} else if _, isStr := kind.(string); isStr && inList(kindText, clashIPTypes) {
			part, value = PartIP, get(entry, "value", "")
		} else {
			previous = ""
			continue
		}
		base := "proxy"
		if direct.has(ident) {
			base = "direct"
		}
		if part == PartIP {
			base += "_ip_rules"
		} else {
			base += "_rules"
		}
		if previous != base {
			counts[base]++
			tag := base
			if counts[base] != 1 {
				tag = base + "_" + itoa(counts[base])
			}
			groups = append(groups, &ruleGroup{tag: tag, part: part, path: "./rules/" + tag + ".txt"})
		}
		last := groups[len(groups)-1]
		last.ids = append(last.ids, ident)
		last.lines = append(last.lines, value)
		previous = base
	}
	return groups
}

// rulesetLayout is _ruleset_layout: one namespace for generated tags and
// files (preview, ZIP and Agent paths); references to one library share the
// downloaded file.
func rulesetLayout(config *jsonx.Object, sets rulesetMap, order []any) (tagMap, tagMap) {
	groups := singleRuleGroups(config)
	reserved := make([]string, len(groups))
	for i, g := range groups {
		reserved[i] = g.tag
	}
	tags := buildRulesetTagMap(sets, order, reserved)
	stems := tagMap{}
	sources := map[string]string{}
	for _, id := range order {
		rs := sets.get(id, jsonx.NewObject())
		source := hashKey(get(rs, "library_rule_id", nil)) + "\x00" + hashKey(get(rs, "name", nil)) + "\x00" +
			hashKey(get(rs, "behavior", "domain")) + "\x00" + hashKey(get(rs, "url", nil))
		tag, _ := tags.lookup(id)
		if existing, ok := sources[source]; ok {
			tag = existing
		} else {
			sources[source] = tag
		}
		stems[hashKey(id)] = tag
	}
	return tags, stems
}

// rulesetLocalPath is _ruleset_local_path.
func rulesetLocalPath(rs any, part, fileStem string) string {
	suffix := ""
	if part == PartIP && get(rs, "behavior", nil) == "classical" {
		suffix = "_ip"
	}
	stem := fileStem
	if stem == "" {
		stem = str(item(rs, "name"))
	}
	return "./rules/" + stem + suffix + ".txt"
}

// rulesetIPTag is _ruleset_ip_tag.
func rulesetIPTag(rs any, tag string) string {
	if get(rs, "behavior", nil) == "classical" {
		return tag + "_ip"
	}
	return tag
}

// RulesetFile is one entry of _ruleset_files; URL nil is None.
type RulesetFile struct {
	Part      string
	URL       any
	LocalPath string
}

// RulesetFiles is _ruleset_files(config, effective_base_url, rule_set,
// file_stem): the local files a rule set needs and their rule-proxy
// download URLs (rule-proxy converts them to MosDNS text). fileStem ""
// stands for None.
func RulesetFiles(config *jsonx.Object, effectiveBaseURL string, ruleSet any, fileStem string) (files []RulesetFile, err error) {
	defer catch(&err)
	return rulesetFiles(config, effectiveBaseURL, ruleSet, fileStem), nil
}

func rulesetFiles(config *jsonx.Object, effectiveBaseURL string, rs any, fileStem string) []RulesetFile {
	behavior := get(rs, "behavior", "domain")
	url := get(rs, "url", "")
	if truthy(url) {
		s, ok := url.(string)
		if !ok {
			raise(attributeError(url, "startswith"))
		}
		if strings.HasPrefix(s, "/") && effectiveBaseURL != "" {
			url = effectiveBaseURL + s
		}
	}
	var files []RulesetFile
	for _, part := range RulesetParts(behavior) {
		var download any
		if truthy(url) {
			var partArg any = part
			if behavior != "ipcidr" && behavior != "classical" {
				partArg = nil
			}
			sourceID := get(rs, "library_rule_id", nil)
			if !truthy(sourceID) {
				sourceID = get(rs, "id", nil)
			}
			download = buildRuleProxyURL(config, effectiveBaseURL, url.(string), partArg, sourceID)
		}
		files = append(files, RulesetFile{Part: part, URL: download, LocalPath: rulesetLocalPath(rs, part, fileStem)})
	}
	return files
}

// BuildRuleProxyURL is _build_rule_proxy_url(config_data,
// effective_base_url, url, part, source_id); part and sourceID are added
// when truthy (nil is None). Errors: Python's ValueError without a rule
// proxy token, or the ValueError of urlsplit.
func BuildRuleProxyURL(config *jsonx.Object, effectiveBaseURL, url string, part, sourceID any) (out string, err error) {
	defer catch(&err)
	return buildRuleProxyURL(config, effectiveBaseURL, url, part, sourceID), nil
}

func buildRuleProxyURL(config *jsonx.Object, effectiveBaseURL, url string, part, sourceID any) string {
	endpoint := "/mosdns/rule-proxy"
	if profileID := config.Value("profile_id"); truthy(profileID) {
		endpoint = "/api/profiles/" + str(profileID) + endpoint
	}
	if endpoint == "/mosdns/rule-proxy" {
		endpoint = "/api" + endpoint
	}
	query := [][2]any{{"url", url}}
	if truthy(sourceID) {
		query = append(query, [2]any{"source_id", sourceID})
	}
	if truthy(part) {
		query = append(query, [2]any{"part", part})
	}
	token := get(get(config, "system_config", jsonx.NewObject()), "rule_proxy_token", "")
	if s, ok := token.(string); !ok || s == "" {
		raise(pyErr("ValueError", "MosDNS rule proxy requires a rule proxy token"))
	}
	query = append(query, [2]any{"token", token})
	out, err := convert.AppendURLQuery(effectiveBaseURL+endpoint, query)
	if err != nil {
		raise(err)
	}
	return out
}

// ParseCustomMatches is _parse_custom_matches: custom match definitions as
// MosDNS sequence entries ({matches, exec}).
func ParseCustomMatches(customMatches any) (out []any, err error) {
	defer catch(&err)
	return parseCustomMatches(customMatches), nil
}

func parseCustomMatches(customMatches any) []any {
	parsed := []any{}
	list, ok := customMatches.([]any)
	if !ok {
		return parsed
	}
	for _, entry := range list {
		if _, isDict := entry.(*jsonx.Object); !isDict || entry.(*jsonx.Object) == nil {
			continue
		}
		if !truthy(get(entry, "enabled", true)) {
			continue
		}
		execValue := get(entry, "exec", nil)
		if !truthy(execValue) {
			execValue = ""
		}
		execAction := strip(execValue)
		matchesRaw := get(entry, "matches", []any{})
		matches := []any{}
		switch x := matchesRaw.(type) {
		case string:
			for _, line := range convert.PySplitlines(x) {
				if s := pystr.Strip(line); s != "" {
					matches = append(matches, s)
				}
			}
		case []any:
			for _, m := range x {
				if s := pystr.Strip(str(m)); s != "" {
					matches = append(matches, s)
				}
			}
		}
		if execAction == "" || len(matches) == 0 {
			continue
		}
		parsed = append(parsed, jsonx.Obj("matches", matches, "exec", execAction))
	}
	return parsed
}
