package oxidns

import (
	"slices"
	"strings"
)

func replaceNativeMap(target, value *Node) { copy := clone(value); *target = *copy }

func (e *nativeEdit) activate(added, standard map[string]bool) error {
	proposed, err := nativePlugins(e.candidate)
	if err != nil {
		return err
	}
	additions := map[string]bool{}
	allowed := nativeAdditions()
	for tag := range added {
		if allowed[tag] && equal(get(proposed[tag], "type"), S(nativeKind(tag))) {
			additions[tag] = true
		}
	}
	if len(additions) == 0 {
		return nil
	}
	targets := nativeAdditionTargets(additions)
	if externalIncludes(e.candidate) {
		return NewReferenceError(messageNativeIncludeAdd, []Reference{{Path: Path{}.with("include"), Reason: messageNativeIncludeAddReason}})
	}
	refs := ReferenceLocations(nativeMaskOwned(e.candidate, targets), tagNames(targets), true)
	for _, ref := range ReferenceLocations(e.candidate, nil, true) {
		found := false
		for _, old := range refs {
			if old.Tag == ref.Tag && old.Reason == ref.Reason && slices.Equal(old.Path, ref.Path) {
				found = true
				break
			}
		}
		if !found {
			refs = append(refs, ref)
		}
	}
	if len(refs) > 0 {
		return NewReferenceError(messageNativeSharedAdd, refs)
	}
	explicit := map[string]*Node{}
	for tag := range additions {
		explicit[tag] = clone(proposed[tag])
	}
	parked := setDefault(e.current, "_common_disabled", M())
	if !isMap(parked) {
		return fail(messageNativeParked)
	}
	if standard["forward_fallback"] && additions["forward_fallback"] {
		saved := get(parked, "forward_fallback")
		if !isMap(saved) || !equal(get(saved, "type"), S("forward")) {
			saved = M("tag", "forward_fallback", "type", "forward", "args", M())
		} else {
			saved = clone(saved)
		}
		args := get(saved, "args")
		if !isMap(args) {
			args = M()
			set(saved, "args", args)
		}
		set(args, "upstreams", L())
		replaceNativeMap(proposed["forward_fallback"], mergeNativeDefaults(saved, explicit["forward_fallback"]))
	}
	working := clone(e.candidate)
	set(working, "plugins", seqOf(filter(items(get(working, "plugins")), func(plugin *Node) bool {
		tag := str(get(plugin, "tag"))
		return !standard[tag] || tag == "forward_fallback"
	})))
	changes := M()
	for _, tag := range tagNames(additions) {
		if field := nativeFeatures[tag]; field != "" {
			set(changes, field, B(true))
		}
	}
	if intersects(additions, nativeDynamic) {
		set(changes, "dynamic_domain_set.enabled", B(true))
		if additions["learn_classified_domain"] {
			set(changes, "dynamic_domain_set.learn", B(true))
		}
	}
	if intersects(additions, nativeAdguard) {
		set(changes, "adguard.enabled", B(true))
		if intersects(additions, nativeUpdater) || standard["adguard_rules"] {
			working, err = e.common(working, M("adguard.enabled", true))
			if err != nil {
				return NewReferenceError(err.Error(), nativeShapeReferences(e.candidate, nativeAdguard, err.Error()))
			}
			if standard["adguard_rules"] {
				index, err := nativePlugins(working)
				if err != nil {
					return err
				}
				provider := index["adguard_rules"]
				if provider == nil {
					return errMalformed
				}
				replaceNativeMap(provider, mergeNativeDefaults(provider, explicit["adguard_rules"]))
			}
		}
	}
	if intersects(additions, nativeUpdater) {
		available, err := ResourceDownloads(working, e.current, e.profile, e.baseURL)
		if err != nil {
			return err
		}
		if len(available) == 0 {
			updaters := map[string]bool{}
			for tag := range additions {
				if nativeUpdater[tag] {
					updaters[tag] = true
				}
			}
			return NewReferenceError(messageNativeUpdateNeedsSource, nativeShapeReferences(e.candidate, updaters, messageNativeAddSourceReason))
		}
		refresh := proposed["rules_refresh"]
		if refresh != nil && !equal(get(refresh, "args"), refreshSteps()) && !equal(get(refresh, "args"), downloadOnlySteps()) {
			return NewReferenceError(messageNativeCustomUpdate, nativeShapeReferences(e.candidate, tagSet("rules_refresh"), messageNativeCustomUpdateReason))
		}
		desired := map[string]bool{}
		for _, item := range available {
			desired[downloadPath(item)] = true
		}
		historical := map[string]bool{}
		for _, value := range items(get(parked, "rules_download_managed_paths")) {
			if isStr(value) {
				historical[value.Value] = true
			}
		}
		download := proposed["rules_download"]
		if isMap(get(download, "args")) && isSeq(at(download, "args", "downloads")) {
			for _, item := range at(download, "args", "downloads").Content {
				path := downloadPath(item)
				if historical[path] && !desired[path] {
					return NewReferenceError(messageNativeWithdrawnDownload, nativeShapeReferences(e.candidate, tagSet("rules_download"), messageNativeWithdrawnReason))
				}
			}
		}
		if additions["rules_cron"] {
			index, err := nativePlugins(working)
			if err != nil {
				return err
			}
			cron := index["rules_cron"]
			jobs := at(cron, "args", "jobs")
			if isMap(get(cron, "args")) && isSeq(jobs) {
				hasJob := false
				for _, job := range jobs.Content {
					hasJob = hasJob || nativeRefreshJob(job)
				}
				if hasJob {
					set(get(cron, "args"), "jobs", seqOf(filter(jobs.Content, func(job *Node) bool { return !isMap(job) || !equal(get(job, "name"), S("refresh_rules")) })))
				}
			}
		}
		set(changes, "rule_update.enabled", B(true))
	}
	if intersects(additions, nativeFallback) {
		index, err := nativePlugins(working)
		if err != nil {
			return err
		}
		args := get(index["forward_fallback"], "args")
		if isMap(args) && upstreamFormState(get(args, "upstreams")) == "ready" {
			text, err := dump(get(args, "upstreams"))
			if err != nil {
				return err
			}
			set(changes, "fallback_dns", S(text))
		} else {
			e.notices = append(e.notices, messageNativeFallbackDraft)
		}
	}
	if err = setNativeConfig(e.current, working); err != nil {
		return err
	}
	if mapSize(changes) > 0 {
		working, err = e.common(working, changes)
		if err != nil {
			return NewReferenceError(err.Error(), nativeShapeReferences(e.candidate, targets, err.Error()))
		}
	}
	active, err := nativePlugins(working)
	if err != nil {
		return err
	}
	for _, tag := range tagNames(explicit) {
		if tag == "custom_hosts" || active[tag] == nil {
			continue
		}
		restored := explicit[tag]
		if standard[tag] {
			restored = mergeNativeDefaults(active[tag], restored)
		}
		if tag == "forward_fallback" && standard[tag] {
			restored = proposed[tag]
		}
		replaceNativeMap(active[tag], restored)
	}
	if additions["custom_hosts"] {
		ctx, err := newContext(working, e.current)
		if err != nil {
			return err
		}
		_, capability := ctx.callState("custom_hosts", []string{"sequence_main"})
		if !capability.Editable {
			return NewReferenceError(capability.Reason, nativeShapeReferences(e.candidate, tagSet("custom_hosts"), capability.Reason))
		}
		defaults := get(ctx.parked, "custom_hosts")
		if defaults == nil {
			defaults = M("tag", "custom_hosts", "type", "hosts", "args", M("files", L()))
		}
		value := explicit["custom_hosts"]
		if standard["custom_hosts"] {
			value = mergeNativeDefaults(defaults, value)
		}
		host, err := ctx.ensure("custom_hosts", M(), "hosts")
		if err != nil {
			return err
		}
		replaceNativeMap(host, value)
		source := get(ctx.parked, "native_hosts_source")
		if isStr(source) {
			for _, path := range items(at(host, "args", "files")) {
				if isStr(path) && (path.Value == "./rules/custom_hosts.txt" || path.Value == "rules/custom_hosts.txt") {
					set(e.current, "custom_hosts", clone(source))
					break
				}
			}
		}
		del(ctx.parked, "native_hosts_source")
		main := ctx.sequence("sequence_main")
		call, accept := M("exec", "$custom_hosts"), M("matches", L("has_resp"), "exec", "accept")
		index := -1
		for i, item := range main.Content {
			if equal(item, call) {
				index = i
				break
			}
		}
		if index >= 0 {
			if index+1 >= len(main.Content) || !equal(main.Content[index+1], accept) {
				return NewReferenceError(messageNativeHostsResponse, nativeShapeReferences(e.candidate, tagSet("custom_hosts"), messageNativeHostsResponseReason))
			}
		} else {
			index = 0
			for i, item := range main.Content {
				exec := str(get(item, "exec"))
				matches := items(get(item, "matches"))
				first := ""
				if len(matches) > 0 {
					first = str(matches[0])
				}
				if exec != "metrics_collector metrics" && exec != "$query_recorder" && exec != "prefer_ipv4" && !strings.HasPrefix(first, "qtype ") {
					index = i
					break
				}
			}
			next := make([]*Node, 0, len(main.Content)+2)
			next = append(next, main.Content[:index]...)
			next = append(next, call, accept)
			next = append(next, main.Content[index:]...)
			main.Content = next
		}
	}
	if err = setNativeConfig(e.current, working); err != nil {
		return err
	}
	if mapSize(changes) > 0 || additions["custom_hosts"] {
		e.notices = append(e.notices, messageNativeAdded)
	}
	e.candidate = working
	return nil
}

func (e *nativeEdit) stopCacheDump() error {
	previous := e.old["lazy_cache"]
	index, err := nativePlugins(e.candidate)
	if err != nil {
		return err
	}
	cache := index["lazy_cache"]
	if previous == nil || cache == nil || !equal(get(previous, "type"), S("cache")) || !equal(get(cache, "type"), S("cache")) {
		return nil
	}
	oldArgs, args := get(previous, "args"), get(cache, "args")
	if !isMap(oldArgs) || !isMap(args) {
		return nil
	}
	oldPath, path := get(oldArgs, "dump_file"), get(args, "dump_file")
	if !isStr(oldPath) || strings.TrimSpace(oldPath.Value) == "" || !(isNone(path) || isStr(path) && strings.TrimSpace(path.Value) == "") {
		return nil
	}
	working := clone(e.candidate)
	plugins, _ := nativePlugins(working)
	holder := get(plugins["lazy_cache"], "args")
	set(holder, "dump_file", clone(oldPath))
	if !has(holder, "dump_interval") && has(oldArgs, "dump_interval") {
		set(holder, "dump_interval", clone(get(oldArgs, "dump_interval")))
	}
	candidate, err := e.common(working, M("cache_dump_enabled", false))
	if err != nil {
		return NewReferenceError(err.Error(), nativeShapeReferences(e.candidate, tagSet("lazy_cache"), err.Error()))
	}
	e.notices = append(e.notices, messageNativeCacheDumpOff)
	e.candidate = candidate
	return nil
}
func (e *nativeEdit) stopEmptyHosts() error {
	previous := e.old["custom_hosts"]
	index, err := nativePlugins(e.candidate)
	if err != nil {
		return err
	}
	host := index["custom_hosts"]
	if previous == nil || host == nil || !equal(get(previous, "type"), S("hosts")) || !equal(get(host, "type"), S("hosts")) {
		return nil
	}
	oldArgs, args := get(previous, "args"), get(host, "args")
	if !isMap(oldArgs) || !isNone(args) && !isMap(args) {
		return nil
	}
	oldSource := false
	for _, field := range []string{"files", "entries"} {
		if value := get(oldArgs, field); isSeq(value) && len(value.Content) > 0 {
			oldSource = true
		}
		value := get(args, field)
		if !isNone(value) && !equal(value, L()) {
			return nil
		}
	}
	if !oldSource {
		return nil
	}
	if externalIncludes(e.candidate) {
		return NewReferenceError(messageNativeIncludeHosts, []Reference{{Path: Path{}.with("include"), Tag: "custom_hosts", Reason: messageNativeIncludeHostsReason}})
	}
	if err = nativeRejectReferences(e.candidate, tagSet("custom_hosts")); err != nil {
		return err
	}
	working := clone(e.candidate)
	plugins, _ := nativePlugins(working)
	holder := plugins["custom_hosts"]
	if isNone(get(holder, "args")) {
		set(holder, "args", M())
	}
	for _, field := range []string{"files", "entries"} {
		if isNone(get(get(holder, "args"), field)) {
			del(get(holder, "args"), field)
		}
	}
	candidate, err := e.common(working, M("custom_hosts", ""))
	if err != nil {
		return NewReferenceError(err.Error(), nativeShapeReferences(e.candidate, tagSet("custom_hosts"), err.Error()))
	}
	e.notices = append(e.notices, messageNativeHostsOff)
	e.candidate = candidate
	return nil
}
