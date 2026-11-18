package rulesync

import (
	"regexp"
	"slices"
	"strings"

	"configflow/internal/deploy"
	"configflow/internal/oxidns"
	"configflow/internal/yamlx"
	"gopkg.in/yaml.v3"
)

var groupTag = regexp.MustCompile(`^rules_(?:domain|ip)_[0-9]+(?:_[0-9]+)*$`)
var helperTag = regexp.MustCompile(`^(?:cf_dns_blackhole|cf_dns_(?:up|fwd|cache)_([0-9a-f]{12}))$`)
var groupMatch = regexp.MustCompile(`^(qname|resp_ip) \$(rules_(?:domain|ip)_[0-9]+(?:_[0-9]+)*)$`)

func copyMap(n *yaml.Node) *yaml.Node {
	out := obj()
	for _, key := range keys(n) {
		yamlx.Set(out, key, get(n, key))
	}
	return out
}
func plugins(doc *yaml.Node) (map[string]*yaml.Node, []string, error) {
	out := map[string]*yaml.Node{}
	order := []string{}
	for _, p := range list(get(doc, "plugins")) {
		tag := text(get(p, "tag"))
		if tag == "" || out[tag] != nil {
			return nil, nil, refuse(msgDuplicateTag)
		}
		out[tag] = p
		order = append(order, tag)
	}
	return out, order, nil
}
func ruleOwned(tag string) bool {
	return groupTag.MatchString(tag) || helperTag.MatchString(tag) || slices.Contains([]string{"sequence_main", "rules_download", "rules_reload"}, tag)
}
func sequenceSkeleton(p *yaml.Node) (*yaml.Node, error) {
	if !mapping(p) || text(get(p, "type")) != "sequence" || yamlx.Resolve(get(p, "args")) == nil || yamlx.Resolve(get(p, "args")).Kind != yaml.SequenceNode {
		return nil, refuse(msgSequenceUnsafe)
	}
	args := list(get(p, "args"))
	remove := map[int]bool{}
	ip := []int{}
	for i, e := range args {
		matches := list(get(e, "matches"))
		if len(matches) != 1 || len(keys(e)) != 2 || get(e, "exec") == nil {
			continue
		}
		parts := groupMatch.FindStringSubmatch(text(matches[0]))
		if parts == nil {
			continue
		}
		part := "domain"
		if parts[1] == "resp_ip" {
			part = "ip"
		}
		if route := oxidns.ParseExec(get(e, "exec"), part, map[string]*yaml.Node{}); route != "" {
			remove[i] = true
			if part == "ip" {
				ip = append(ip, i)
			}
		}
	}
	if len(ip) > 0 {
		start, end := ip[0], ip[len(ip)-1]
		if start == 0 || end+1 >= len(args) || !yamlx.Equal(args[start-1], yamlx.Map(yamlx.Str("exec"), yamlx.Str("jump local_probe"))) || end-start+1 != len(ip) {
			return nil, refuse(msgIPUnsafe)
		}
		after := args[end+1]
		drop := yamlx.Map(yamlx.Str("exec"), yamlx.Str("drop_resp"))
		route := yamlx.Map(yamlx.Str("matches"), stringsNode([]string{"has_resp"}), yamlx.Str("exec"), yamlx.Str("goto default_direct_ip"))
		if !yamlx.Equal(after, drop) && !yamlx.Equal(after, route) {
			return nil, refuse(msgIPUnsafe)
		}
		remove[start-1] = true
		remove[end+1] = true
	}
	kept := yamlx.Seq()
	for i, e := range args {
		if !remove[i] {
			kept.Content = append(kept.Content, e)
		}
	}
	out := copyMap(p)
	yamlx.Set(out, "args", kept)
	return out, nil
}
func canonicalChain(key string, ps map[string]*yaml.Node) bool {
	a := oxidns.PinnedAction(key, ps)
	return a != nil && oxidns.EntryKey(get(a, "upstream"), text(get(a, "source"))) == key
}
func checkHelpers(previous *yaml.Node, left, right map[string]*yaml.Node) error {
	published := map[string]bool{}
	catalog, err := oxidns.UpstreamCatalog(yamlx.Map(yamlx.Str("config_mode"), yamlx.Str("native")), previous, nil)
	if err == nil {
		for _, entry := range catalog {
			published[entry.Key] = true
		}
	}
	for tag := range left {
		match := helperTag.FindStringSubmatch(tag)
		if match != nil && strings.HasPrefix(tag, oxidns.UpstreamPrefix) && canonicalChain(match[1], left) {
			published[match[1]] = true
		}
	}
	for tag, p := range right {
		match := helperTag.FindStringSubmatch(tag)
		if match == nil || yamlx.Equal(left[tag], p) {
			continue
		}
		if tag == oxidns.BlackholeTag {
			if !yamlx.Equal(p, oxidns.BlackholePlugin()) {
				return refuse(msgBlackholeUnsafe)
			}
			continue
		}
		key := match[1]
		if !canonicalChain(key, right) || !published[key] {
			return refuse(msgUnpublishedUpstream)
		}
		if !strings.HasPrefix(tag, oxidns.UpstreamPrefix) {
			found := false
			for _, entry := range list(get(right[oxidns.UpstreamPrefix+key], "args")) {
				if yamlx.Equal(entry, yamlx.Map(yamlx.Str("exec"), yamlx.Str("$"+tag))) {
					found = true
				}
			}
			if !found {
				return refuse(msgUnpublishedUpstream)
			}
		}
	}
	return nil
}
func managedPaths(doc *yaml.Node) (map[string]bool, error) {
	paths := map[string]bool{}
	for _, plugin := range list(get(doc, "plugins")) {
		if !groupTag.MatchString(text(get(plugin, "tag"))) {
			continue
		}
		for _, p := range list(get(get(plugin, "args"), "files")) {
			path, err := deploy.RelativePath(text(p))
			if err != nil {
				return nil, err
			}
			paths[path] = true
		}
	}
	return paths, nil
}
func isolateOxi(previous, candidate *yaml.Node) (*yaml.Node, error) {
	left, leftOrder, err := plugins(previous)
	if err != nil {
		return nil, err
	}
	right, rightOrder, err := plugins(candidate)
	if err != nil {
		return nil, err
	}
	if err = checkHelpers(previous, left, right); err != nil {
		return nil, err
	}
	old, err := sequenceSkeleton(left["sequence_main"])
	if err != nil {
		return nil, err
	}
	next, err := sequenceSkeleton(right["sequence_main"])
	if err != nil {
		return nil, err
	}
	if !yamlx.Equal(old, next) {
		return nil, refuse(msgSequenceChanged)
	}
	otherOld, otherNew := []string{}, []string{}
	for _, tag := range leftOrder {
		if !ruleOwned(tag) {
			otherOld = append(otherOld, tag)
			if !yamlx.Equal(left[tag], right[tag]) {
				return nil, refuse(msgPluginChanged)
			}
		}
	}
	for _, tag := range rightOrder {
		if !ruleOwned(tag) {
			otherNew = append(otherNew, tag)
			if !yamlx.Equal(left[tag], right[tag]) {
				return nil, refuse(msgPluginChanged)
			}
		}
	}
	if !slices.Equal(otherOld, otherNew) {
		return nil, refuse(msgOrderChanged)
	}
	for _, tag := range []string{"rules_download", "rules_reload"} {
		a, b := left[tag], right[tag]
		if (a == nil) != (b == nil) {
			return nil, refuse(msgLifecycleChanged)
		}
		if a == nil {
			continue
		}
		a, b = copyMap(a), copyMap(b)
		if tag == "rules_download" {
			aa, bb := copyMap(get(a, "args")), copyMap(get(b, "args"))
			yamlx.Delete(aa, "downloads")
			yamlx.Delete(bb, "downloads")
			yamlx.Set(a, "args", aa)
			yamlx.Set(b, "args", bb)
		} else {
			yamlx.Delete(a, "args")
			yamlx.Delete(b, "args")
		}
		if !yamlx.Equal(a, b) {
			return nil, refuse(msgParametersChanged)
		}
	}
	oldPaths, err := managedPaths(previous)
	if err != nil {
		return nil, err
	}
	newPaths, err := managedPaths(candidate)
	if err != nil {
		return nil, err
	}
	otherPlugins := yamlx.Seq()
	for _, tag := range leftOrder {
		if !groupTag.MatchString(tag) {
			otherPlugins.Content = append(otherPlugins.Content, left[tag])
		}
	}
	refs, err := oxidns.ResourceReferences(yamlx.Map(yamlx.Str("plugins"), otherPlugins))
	if err != nil {
		return nil, err
	}
	for _, p := range refs {
		r := relativeResource(p)
		if oldPaths[r] || newPaths[r] {
			return nil, refuse(msgSharedFile)
		}
	}
	result := copyMap(previous)
	out := yamlx.Seq()
	resultPlugins := map[string]*yaml.Node{}
	for _, tag := range rightOrder {
		plugin := left[tag]
		if ruleOwned(tag) {
			plugin = right[tag]
		}
		plugin = copyMap(plugin)
		resultPlugins[tag] = plugin
		out.Content = append(out.Content, plugin)
	}
	yamlx.Set(result, "plugins", out)
	if resultPlugins["rules_download"] != nil {
		location := func(entry *yaml.Node) (string, error) {
			if !mapping(entry) {
				return "", refuse(msgDownloadInvalid)
			}
			return deploy.RelativePath(strings.TrimRight(text(get(entry, "dir")), "/") + "/" + text(get(entry, "filename")))
		}
		downloads := yamlx.Seq()
		for _, e := range list(get(get(right["rules_download"], "args"), "downloads")) {
			path, err := location(e)
			if err != nil {
				return nil, err
			}
			if newPaths[path] {
				downloads.Content = append(downloads.Content, e)
			}
		}
		for _, e := range list(get(get(left["rules_download"], "args"), "downloads")) {
			path, err := location(e)
			if err != nil {
				return nil, err
			}
			if !oldPaths[path] {
				downloads.Content = append(downloads.Content, e)
			}
		}
		args := copyMap(get(resultPlugins["rules_download"], "args"))
		yamlx.Set(args, "downloads", downloads)
		yamlx.Set(resultPlugins["rules_download"], "args", args)
		reload := yamlx.Seq()
		for _, e := range list(get(right["rules_reload"], "args")) {
			if groupTag.MatchString(strings.TrimPrefix(text(e), "$")) {
				reload.Content = append(reload.Content, e)
			}
		}
		for _, e := range list(get(left["rules_reload"], "args")) {
			if !groupTag.MatchString(strings.TrimPrefix(text(e), "$")) {
				reload.Content = append(reload.Content, e)
			}
		}
		if resultPlugins["rules_reload"] == nil {
			return nil, refuse(msgLifecycleChanged)
		}
		yamlx.Set(resultPlugins["rules_reload"], "args", reload)
	}
	return result, nil
}

// IsolateRuleBundle pins deployed non-rule bytes and independently verifies the
// candidate main configuration contains only permitted rule changes.
func IsolateRuleBundle(baseline, candidate *deploy.Bundle, service string) (*deploy.Bundle, error) {
	oldM, newM := baseline.Manifest(), candidate.Manifest()
	if oldM.AgentID != newM.AgentID || oldM.ProfileID != newM.ProfileID || oldM.ServiceType != newM.ServiceType {
		return nil, refuse(msgIdentityChanged)
	}
	if newM.ServiceType != service {
		return nil, refuse(msgServiceMismatch)
	}
	oldFiles, err := ReadBundleFiles(baseline)
	if err != nil {
		return nil, err
	}
	newFiles, err := ReadBundleFiles(candidate)
	if err != nil {
		return nil, err
	}
	previous, err := parseMapping(oldFiles[oldM.ConfigPath].Data)
	if err != nil {
		return nil, err
	}
	proposed, err := parseMapping(newFiles[newM.ConfigPath].Data)
	if err != nil {
		return nil, err
	}
	var document *yaml.Node
	var oldPaths, paths map[string]bool
	switch service {
	case "mihomo":
		document = copyMap(previous)
		for _, key := range []string{"rules", "rule-providers"} {
			if value := get(proposed, key); value != nil {
				yamlx.Set(document, key, value)
			} else {
				yamlx.Delete(document, key)
			}
		}
		oldPaths, err = providerPaths(previous)
		if err == nil {
			paths, err = providerPaths(document)
		}
	case "oxidns":
		document, err = isolateOxi(previous, proposed)
		if err == nil {
			oldPaths, err = managedPaths(previous)
		}
		if err == nil {
			paths, err = managedPaths(document)
		}
	default:
		return nil, refuse(msgProjectionService)
	}
	if err != nil {
		return nil, err
	}
	files := map[string]deploy.Content{}
	for path, c := range oldFiles {
		if path != oldM.ConfigPath && !oldPaths[path] {
			files[path] = c
		}
	}
	for path := range paths {
		if _, ok := files[path]; ok {
			return nil, refuse(msgFileConflict)
		}
		file, ok := newFiles[path]
		if !ok || file.Role != deploy.RoleRule {
			return nil, refuse(msgRuleMissing)
		}
		files[path] = file
	}
	raw, err := yamlx.Marshal(document)
	if err != nil {
		return nil, err
	}
	files[oldM.ConfigPath] = deploy.Content{Data: raw, Role: deploy.RoleConfig}
	newM.ConfigPath = oldM.ConfigPath
	newM.Files = manifestFiles(files)
	return deploy.Pack(newM, files, digest(raw)[:8], nil)
}
func manifestFiles(files map[string]deploy.Content) []deploy.File {
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	out := make([]deploy.File, 0, len(paths))
	for _, path := range paths {
		f := files[path]
		out = append(out, deploy.File{Path: path, Role: f.Role, Size: int64(len(f.Data)), SHA256: digest(f.Data)})
	}
	return out
}
func providerPaths(doc *yaml.Node) (map[string]bool, error) {
	out := map[string]bool{}
	providers := get(doc, "rule-providers")
	for _, key := range keys(providers) {
		path := text(get(get(providers, key), "path"))
		if path != "" {
			p, err := deploy.RelativePath(path)
			if err != nil {
				return nil, err
			}
			out[p] = true
		}
	}
	return out, nil
}
