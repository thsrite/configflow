package oxidns

import (
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
)

// NativeRename is an explicit plugin identity change. Slice order is retained.
type NativeRename struct{ From, To string }

// NativeApplyOptions identifies deliberate renames and standard-entry actions.
type NativeApplyOptions struct {
	Renames     []NativeRename
	StandardAdd []string
}

// NativeApplied is a replacement native draft and its common-control projection.
// Applying a draft performs no persistence and leaves caller inputs unchanged.
type NativeApplied struct {
	Settings *Node
	View     CommonView
	Notices  []string
}

type nativeEdit struct {
	input                                *Node
	current, original, before, candidate *Node
	old, proposed                        map[string]*Node
	removed, added                       map[string]bool
	profile                              *Profile
	baseURL, text                        string
	notices                              []string
}

var nativeFeatures = map[string]string{
	"lazy_cache": "cache_enabled", "local_probe_cache": "probe_cache_enabled",
	"query_recorder": "query_recorder.enabled", "ip_selector": "ip_selector.enabled",
	"ros_address_list": "ros_address_list.enabled", "ros_route": "ros_route.enabled",
}
var nativeFallback = tagSet("forward_fallback", "fallback_dns", "fallback")
var nativeAdguard = tagSet("adguard_rules", "adguard_block", "adguard_reject")
var nativeUpdater = tagSet("rules_download", "rules_reload", "rules_refresh", "rules_cron")
var nativeDynamic = tagSet("dynamic_domains", "learn_classified_domain")

func tagSet(tags ...string) map[string]bool {
	out := map[string]bool{}
	for _, tag := range tags {
		out[tag] = true
	}
	return out
}
func tagNames[T any](values map[string]T) []string {
	out := make([]string, 0, len(values))
	for key := range values {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
func intersects(a, b map[string]bool) bool {
	for key := range a {
		if b[key] {
			return true
		}
	}
	return false
}
func includeTags(target, source map[string]bool) {
	for key := range source {
		target[key] = true
	}
}
func withoutTags(a, b map[string]bool) map[string]bool {
	out := maps.Clone(a)
	for key := range b {
		delete(out, key)
	}
	return out
}
func nativeAdditions() map[string]bool {
	out := tagSet("custom_hosts")
	for key := range nativeFeatures {
		out[key] = true
	}
	for _, group := range []map[string]bool{nativeFallback, nativeAdguard, nativeUpdater, nativeDynamic} {
		includeTags(out, group)
	}
	return out
}
func nativeStandard() map[string]bool {
	out := tagSet("adguard_rules", "rules_download", "custom_hosts", "forward_fallback")
	for key := range nativeFeatures {
		out[key] = true
	}
	includeTags(out, nativeDynamic)
	return out
}
func nativeKind(tag string) string {
	if kind := tagTypes[tag]; kind != "" {
		return kind
	}
	return "sequence"
}
func nativeManaged(tag string) bool {
	return tagTypes[tag] != "" || slices.Contains(wrapperChains, tag) || slices.Contains([]string{"sequence_main", "remote_dns", "local_probe", "proxy_dns_seq", "ip_requery_proxy", "fallback_dns", "adguard_reject", "rules_refresh"}, tag)
}

func nativePlugins(document *nativeDoc) (map[string]*Node, error) {
	list := get(document, "plugins")
	if !has(document, "plugins") {
		list = L()
	}
	if !isSeq(list) {
		return nil, fail(messageNativePlugins)
	}
	result := map[string]*Node{}
	for _, plugin := range list.Content {
		if !isMap(plugin) || !isStr(get(plugin, "tag")) {
			return nil, fail(messageNativePlugins)
		}
		tag := str(get(plugin, "tag"))
		if result[tag] != nil {
			return nil, fail(messageNativeDuplicateTags)
		}
		result[tag] = plugin
	}
	return result, nil
}

func prepareNativeEdit(settings *Node, text string, p *Profile, baseURL string) (*nativeEdit, error) {
	if p == nil {
		p = emptyProfile()
	}
	current, err := PrepareEditor(settings, p, baseURL)
	if err != nil {
		return nil, err
	}
	before, err := readSettingsView(current).loadNative(NativeLabel)
	if err != nil {
		return nil, err
	}
	candidate, err := LoadNativeYAML(text, NativeLabel)
	if err != nil {
		return nil, err
	}
	old, err := nativePlugins(before)
	if err != nil {
		return nil, err
	}
	proposed, err := nativePlugins(candidate)
	if err != nil {
		return nil, err
	}
	edit := &nativeEdit{input: clone(settings), current: current, original: clone(current), before: before, candidate: candidate, old: old, proposed: proposed, removed: map[string]bool{}, added: map[string]bool{}, profile: p, baseURL: baseURL, text: text, notices: []string{}}
	for tag := range old {
		if proposed[tag] == nil {
			edit.removed[tag] = true
		}
	}
	for tag := range proposed {
		if old[tag] == nil {
			edit.added[tag] = true
		}
	}
	return edit, nil
}

func nativeWithoutTag(plugin *Node) *Node { out := clone(plugin); del(out, "tag"); return out }
func renameNativeReferences(document *nativeDoc, old, next string) {
	var walk func(*Node, string)
	walk = func(value *Node, field string) {
		switch {
		case isMap(value):
			pairs(value, func(key string, item *Node) bool {
				if key != "tag" && key != "type" {
					walk(item, key)
				}
				return true
			})
		case isSeq(value):
			for _, item := range value.Content {
				walk(item, field)
			}
		case isStr(value) && slices.Contains([]string{"exec", "matches", "entry", "primary", "secondary", "provider", "executors", "args"}, field):
			if slices.Contains([]string{"entry", "primary", "secondary", "provider"}, field) && value.Value == old {
				value.Value = next
				return
			}
			if field == "exec" && (value.Value == "goto "+old || value.Value == "jump "+old) {
				value.Value = strings.SplitN(value.Value, " ", 2)[0] + " " + next
				return
			}
			value.Value = replaceToken(value.Value, "$"+old, "$"+next)
		}
	}
	walk(document, "")
}

func nativeMaskOwned(document *nativeDoc, targets map[string]bool) *Node {
	out := clone(document)
	for _, item := range items(get(out, "plugins")) {
		tag := str(get(item, "tag"))
		if targets[tag] {
			item.Content = nil
			continue
		}
		args := get(item, "args")
		if tag == "rules_cron" && isMap(args) && isSeq(get(args, "jobs")) && intersects(targets, nativeUpdater) {
			for i, job := range get(args, "jobs").Content {
				if nativeRefreshJob(job) {
					get(args, "jobs").Content[i] = M()
				}
			}
		}
		if tag == "rules_reload" && isSeq(args) && targets["adguard_rules"] {
			for i, value := range args.Content {
				if equal(value, S("$adguard_rules")) {
					args.Content[i] = Null()
				}
			}
		}
		if !isSeq(args) || !equal(get(item, "type"), S("sequence")) {
			continue
		}
		expected := []*Node{}
		for target := range targets {
			chains := []string{}
			switch {
			case target == "lazy_cache":
				chains = []string{"china_dns"}
			case target == "local_probe_cache":
				chains = []string{"local_probe"}
			case target == "query_recorder" || target == "custom_hosts":
				chains = []string{"sequence_main"}
			case target == "learn_classified_domain":
				chains = []string{"direct_ip", "ip_requery_proxy"}
			case nativeFeatures[target] != "":
				chains = wrapperChains
			}
			if slices.Contains(chains, tag) {
				expected = append(expected, M("exec", "$"+target))
			}
		}
		if tag == "sequence_main" && targets["dynamic_domains"] {
			for _, route := range []string{"china_dns", "proxy_dns_seq"} {
				expected = append(expected, M("matches", L("qname $dynamic_domains"), "exec", "goto "+route))
			}
		}
		if tag == "sequence_main" && intersects(targets, nativeAdguard) {
			expected = append(expected, M("matches", L("question $adguard_rules"), "exec", "goto adguard_reject"))
		}
		if tag == "proxy_dns_seq" && intersects(targets, nativeFallback) && equal(args, L(M("exec", "$fallback"), M("exec", "accept"))) {
			set(item, "args", L())
		} else {
			for i, entry := range args.Content {
				for _, owned := range expected {
					if equal(entry, owned) {
						args.Content[i] = M()
						break
					}
				}
			}
		}
	}
	return out
}
func nativeRefreshJob(job *Node) bool {
	return isMap(job) && equal(get(job, "name"), S("refresh_rules")) && equal(get(job, "executors"), L("$rules_refresh"))
}
func nativeShapeReferences(candidate *Node, targets map[string]bool, message string) []Reference {
	for i, item := range items(get(candidate, "plugins")) {
		tag := str(get(item, "tag"))
		if targets[tag] || tag == "proxy_dns_seq" || tag == "sequence_main" {
			return []Reference{{Path: Path{}.with("plugins").with(i).with("args"), Tag: tag, Reason: message, tagSet: true}}
		}
	}
	return []Reference{}
}
func nativeRejectReferences(candidate *Node, targets map[string]bool) error {
	refs := ReferenceLocations(nativeMaskOwned(candidate, targets), tagNames(targets), true)
	if len(refs) > 0 {
		return NewReferenceError(messageNativeSharedDelete, refs)
	}
	return nil
}
func nativeAdditionTargets(added map[string]bool) map[string]bool {
	targets := maps.Clone(added)
	for _, group := range []map[string]bool{nativeDynamic, nativeAdguard, nativeUpdater, nativeFallback} {
		if intersects(added, group) {
			includeTags(targets, group)
		}
	}
	return targets
}
func mergeNativeDefaults(defaults, explicit *Node) *Node {
	if isMap(defaults) && isMap(explicit) {
		out := clone(defaults)
		pairs(explicit, func(key string, value *Node) bool {
			set(out, key, mergeNativeDefaults(get(defaults, key), value))
			return true
		})
		return out
	}
	return clone(explicit)
}
func upstreamFormState(value *Node) string {
	if isNone(value) || isSeq(value) && len(value.Content) == 0 {
		return "empty"
	}
	if !isSeq(value) {
		return "draft"
	}
	empty, ready := true, true
	for _, entry := range value.Content {
		address := get(entry, "addr")
		empty = empty && isMap(entry) && (isNone(address) || isStr(address) && strings.TrimSpace(address.Value) == "")
		ready = ready && isMap(entry) && isStr(address) && strings.TrimSpace(address.Value) != ""
	}
	if empty {
		return "empty"
	}
	if ready {
		return "ready"
	}
	return "draft"
}
func (e *nativeEdit) common(document *nativeDoc, changes *Node) (*Node, error) {
	if err := setNativeConfig(e.current, document); err != nil {
		return nil, err
	}
	applied, err := ApplyCommonSettings(e.current, changes, e.profile, e.baseURL)
	if err != nil {
		return nil, err
	}
	e.current = applied.Settings
	return readSettingsView(e.current).loadNative(NativeLabel)
}
func (e *nativeEdit) apply(settings *Node, options NativeApplyOptions) (NativeApplied, error) {
	renames := append([]NativeRename{}, options.Renames...)
	targetsUsed := map[string]bool{}
	fromUsed := map[string]bool{}
	badRenameMapping := false
	for _, rename := range renames {
		badRenameMapping = badRenameMapping || fromUsed[rename.From] || targetsUsed[rename.To] || !e.removed[rename.From] || !e.added[rename.To]
		fromUsed[rename.From] = true
		targetsUsed[rename.To] = true
	}
	standard := map[string]bool{}
	allowedStandard := nativeStandard()
	for _, tag := range options.StandardAdd {
		if standard[tag] {
			return NativeApplied{}, fail(messageNativeStandardList)
		}
		standard[tag] = true
	}
	for tag := range standard {
		if !allowedStandard[tag] || !e.added[tag] || !equal(get(e.proposed[tag], "type"), S(nativeKind(tag))) {
			return NativeApplied{}, fail(messageNativeStandardEntry)
		}
	}
	if badRenameMapping {
		return NativeApplied{}, fail(messageNativeRenameMapping)
	}
	for _, tag := range tagNames(e.removed) {
		if fromUsed[tag] {
			continue
		}
		matches := []string{}
		for _, name := range tagNames(e.added) {
			if !targetsUsed[name] && !standard[name] && same(nativeWithoutTag(e.old[tag]), nativeWithoutTag(e.proposed[name])) {
				matches = append(matches, name)
			}
		}
		if len(matches) != 1 {
			continue
		}
		count := 0
		for other := range e.removed {
			if !fromUsed[other] && same(nativeWithoutTag(e.old[other]), nativeWithoutTag(e.proposed[matches[0]])) {
				count++
			}
		}
		if count == 1 {
			renames = append(renames, NativeRename{tag, matches[0]})
			fromUsed[tag] = true
			targetsUsed[matches[0]] = true
		}
	}
	for _, rename := range renames {
		if standard[rename.To] {
			return NativeApplied{}, fail(messageNativeStandardRename)
		}
		if !validTag(rename.To) || !same(get(e.old[rename.From], "type"), get(e.proposed[rename.To], "type")) {
			return NativeApplied{}, fail(messageNativeRenameTag)
		}
	}
	if len(e.removed) > 0 {
		for tag := range e.added {
			if strings.TrimSpace(tag) == "" {
				return NativeApplied{}, fail(messageNativeEmptyTag)
			}
		}
	}
	if len(renames) > 0 {
		refs := ReferenceLocations(e.candidate, nil, true)
		if len(refs) > 0 || (externalIncludes(e.candidate)) {
			if len(refs) == 0 {
				refs = []Reference{{Path: Path{}.with("include"), Reason: messageNativeIncludeRenameReason}}
			}
			return NativeApplied{}, NewReferenceError(messageNativeExternalRename, refs)
		}
		for _, rename := range renames {
			renameNativeReferences(e.candidate, rename.From, rename.To)
			if left := ReferenceLocations(e.candidate, []string{rename.From}, false); len(left) > 0 {
				return NativeApplied{}, NewReferenceError(messageNativeCustomRename, left)
			}
			delete(e.removed, rename.From)
			e.notices = append(e.notices, fmt.Sprintf(messageNativeRenamed, rename.From, rename.To))
		}
		parked := setDefault(e.current, "_common_disabled", M())
		if !isMap(parked) {
			return NativeApplied{}, fail(messageNativeParkedRename)
		}
		aliases := setDefault(parked, "renamed_plugins", M())
		if !isMap(aliases) {
			return NativeApplied{}, fail(messageNativeAliasRecord)
		}
		for _, rename := range renames {
			originals := []string{}
			pairs(aliases, func(original string, value *Node) bool {
				if equal(value, S(rename.From)) {
					originals = append(originals, original)
				}
				return true
			})
			for _, original := range originals {
				if rename.To == original {
					del(aliases, original)
				} else {
					set(aliases, original, S(rename.To))
				}
			}
			if nativeManaged(rename.From) && len(originals) == 0 {
				set(aliases, rename.From, S(rename.To))
			}
		}
		e.proposed, _ = nativePlugins(e.candidate)
	}
	additions := withoutTags(e.added, targetsUsed)
	allowed := nativeAdditions()
	for tag := range additions {
		if !allowed[tag] || !equal(get(e.proposed[tag], "type"), S(nativeKind(tag))) {
			delete(additions, tag)
		}
	}
	changes, targets := M(), map[string]bool{}
	for _, tag := range tagNames(e.removed) {
		switch {
		case nativeFeatures[tag] != "":
			set(changes, nativeFeatures[tag], B(false))
			targets[tag] = true
		case tag == "dynamic_domains":
			set(changes, "dynamic_domain_set.enabled", B(false))
			includeTags(targets, nativeDynamic)
		case tag == "learn_classified_domain":
			set(changes, "dynamic_domain_set.learn", B(false))
			targets[tag] = true
		case tag == "custom_hosts":
			set(changes, "custom_hosts", S(""))
			targets[tag] = true
		case nativeAdguard[tag]:
			set(changes, "adguard.enabled", B(false))
			includeTags(targets, nativeAdguard)
		case nativeFallback[tag]:
			set(changes, "fallback_dns", S(""))
			includeTags(targets, nativeFallback)
		case nativeUpdater[tag]:
			if tag == "rules_cron" || len(ReferenceLocations(e.candidate, []string{tag}, false)) > 0 {
				set(changes, "rule_update.enabled", B(false))
				if tag == "rules_cron" {
					targets[tag] = true
				} else {
					includeTags(targets, withoutTags(nativeUpdater, tagSet("rules_cron")))
				}
			}
		}
	}
	backup := e.proposed["forward_fallback"]
	if equal(get(backup, "type"), S("forward")) && isMap(get(backup, "args")) {
		previous := get(e.old["forward_fallback"], "args")
		previousState := "draft"
		if isMap(previous) {
			previousState = upstreamFormState(get(previous, "upstreams"))
		}
		next := upstreamFormState(at(backup, "args", "upstreams"))
		if previousState == "ready" && next == "empty" {
			set(changes, "fallback_dns", S(""))
			includeTags(targets, nativeFallback)
		} else if previousState != "ready" && next == "ready" && !targetsUsed["forward_fallback"] {
			additions["forward_fallback"] = true
		}
	}
	if intersects(e.removed, nativeAdditionTargets(additions)) {
		return NativeApplied{}, NewReferenceError(messageNativeDeleteAndEnable, nativeShapeReferences(e.candidate, additions, messageNativeDependencyRemoved))
	}
	cron, previousCron := e.proposed["rules_cron"], e.old["rules_cron"]
	if isMap(get(cron, "args")) && isMap(get(previousCron, "args")) {
		jobs, previous := get(get(cron, "args"), "jobs"), get(get(previousCron, "args"), "jobs")
		if jobs == nil {
			jobs = L()
		}
		if previous == nil {
			previous = L()
		}
		if isSeq(jobs) && isSeq(previous) {
			oldJob, newJob := false, false
			for _, job := range previous.Content {
				oldJob = oldJob || isMap(job) && equal(get(job, "name"), S("refresh_rules"))
			}
			for _, job := range jobs.Content {
				newJob = newJob || isMap(job) && (equal(get(job, "name"), S("refresh_rules")) || equal(get(job, "executors"), L("$rules_refresh")))
			}
			if oldJob && !newJob {
				set(changes, "rule_update.enabled", B(false))
				targets["rules_cron"] = true
			}
		}
	}
	originalCandidate := clone(e.candidate)
	if len(targets) > 0 {
		if externalIncludes(e.candidate) {
			return NativeApplied{}, NewReferenceError(messageNativeIncludeDelete, []Reference{{Path: Path{}.with("include"), Reason: messageNativeIncludeDeleteReason}})
		}
		if err := nativeRejectReferences(e.candidate, targets); err != nil {
			return NativeApplied{}, err
		}
		working := clone(e.candidate)
		for _, tag := range tagNames(e.removed) {
			if targets[tag] && e.old[tag] != nil {
				restored := clone(e.old[tag])
				if tag == "custom_hosts" && equal(get(restored, "type"), S("hosts")) && isMap(get(restored, "args")) {
					set(get(restored, "args"), "files", L())
					set(get(restored, "args"), "entries", L())
				}
				list := setDefault(working, "plugins", L())
				list.Content = append(list.Content, restored)
			}
		}
		result, err := e.common(working, changes)
		if err != nil {
			if len(e.removed) > 0 && len(additions) == 0 && len(ReferenceLocations(e.candidate, tagNames(e.removed), true)) == 0 {
				if err = setNativeConfig(e.current, e.candidate); err != nil {
					return NativeApplied{}, err
				}
			} else {
				return NativeApplied{}, NewReferenceError(err.Error(), nativeShapeReferences(e.candidate, targets, err.Error()))
			}
		} else {
			e.candidate = result
			if intersects(e.removed, withoutTags(nativeUpdater, tagSet("rules_cron"))) && intersects(targets, withoutTags(nativeUpdater, tagSet("rules_cron"))) {
				ctx, err := newContext(e.candidate, e.current)
				if err != nil {
					return NativeApplied{}, err
				}
				if err = stopPipeline(ctx, false); err != nil {
					return NativeApplied{}, err
				}
			}
		}
		set(e.candidate, "plugins", seqOf(filter(items(get(e.candidate, "plugins")), func(item *Node) bool { return !e.removed[str(get(item, "tag"))] })))
		if e.removed["custom_hosts"] {
			parked := setDefault(e.current, "_common_disabled", M())
			set(parked, "custom_hosts", clone(e.old["custom_hosts"]))
			if hosts := readSettingsView(settings).CustomHosts; hosts.OK && strings.TrimSpace(hosts.Value) != "" {
				set(parked, "native_hosts_source", S(hosts.Value))
			}
		}
		e.notices = append(e.notices, messageNativeRemoved)
	} else {
		set(e.current, "native_config", S(e.text))
	}
	if len(additions) > 0 {
		if err := e.activate(additions, standard); err != nil {
			return NativeApplied{}, err
		}
	}
	if err := e.stopCacheDump(); err != nil {
		return NativeApplied{}, err
	}
	if err := e.stopEmptyHosts(); err != nil {
		return NativeApplied{}, err
	}
	pruned, withdrawal, err := ReconcileNativeWithdrawals(e.before, e.candidate, e.original, e.current, e.profile)
	if err != nil {
		return NativeApplied{}, err
	}
	if pruned > 0 {
		e.notices = append(e.notices, fmt.Sprintf(messageNativePruned, pruned))
	}
	if len(e.removed) > 0 {
		refs := ReferenceLocations(e.candidate, tagNames(e.removed), true)
		if len(refs) > 0 {
			original := ReferenceLocations(nativeMaskOwned(originalCandidate, targets), tagNames(e.removed), true)
			if len(original) > 0 {
				refs = original
			}
			return NativeApplied{}, NewReferenceError(messageNativeStillReferenced, refs)
		}
	}
	if mapSize(changes) > 0 || len(renames) > 0 || len(additions) > 0 || withdrawal {
		if err := setNativeConfig(e.current, e.candidate); err != nil {
			return NativeApplied{}, err
		}
	}
	view, err := ReadCommonSettings(e.current, e.profile, e.baseURL)
	if err != nil {
		return NativeApplied{}, err
	}
	return NativeApplied{Settings: e.current, View: view, Notices: e.notices}, nil
}
