package oxidns

import (
	"configflow/internal/model"
	"configflow/internal/mosdns"
	"net/url"
	"strconv"
	"strings"
)

// The owned rule-update pipeline (rules_download → rules_reload, called by
// rules_refresh and the rules_cron job) stays valid as its resources change.

var pipeline = map[string]string{"rules_download": "download", "rules_reload": "reload_provider", "rules_refresh": "sequence"}

var pipelineTags = []string{"rules_download", "rules_reload", "rules_refresh"}

func refreshSteps() *Node      { return L(M("exec", "$rules_download"), M("exec", "$rules_reload")) }
func downloadOnlySteps() *Node { return L(M("exec", "$rules_download")) }

func relevant(changes *Node) bool {
	for _, key := range keys(changes) {
		if containsString(RoutingFields, key) || strings.HasPrefix(key, "rule_update.") || strings.HasPrefix(key, "adguard.") {
			return true
		}
	}
	return false
}

func pipelineError() error {
	return fail("规则更新流水线仍被自定义执行链引用或包含自定义步骤，请先在完整参数中调整，不能自动停用")
}

// ReconcileNativeWithdrawals removes downloads of managed resources the
// candidate draft no longer reads (proven by its actual references).
// It returns the number of removed entries and whether candidate changed.
func ReconcileNativeWithdrawals(before, candidate, originalSettings, settings *Node, p *Profile) (int, bool, error) {
	expand := func(document, values *Node) (*Node, error) {
		if !(externalIncludes(document)) {
			return document, nil
		}
		v := shallow(values)
		if err := setNativeConfig(v, document); err != nil {
			return nil, err
		}
		return NativeDocument(v, false, nil)
	}
	previous, err := expand(before, originalSettings)
	var current *Node
	var references []string
	var oldPaths, live map[string]bool
	if err == nil {
		current, err = expand(candidate, settings)
	}
	if err == nil {
		oldPaths, err = referencedPaths(previous)
	}
	if err == nil {
		references, err = ResourceReferences(current)
	}
	if err == nil {
		live, err = referencedPaths(current)
	}
	if err != nil {
		if _, ok := IsSettingsError(err); ok {
			return 0, false, nil // unfinished drafts: leave every download alone
		}
		return 0, false, err
	}
	for _, ref := range references {
		if strings.Contains(ref, "${") {
			return 0, false, nil
		}
	}
	managed := map[string]bool{}
	c := p.catalog()
	for _, r := range c.records {
		if r.suffix == "rulesets" {
			for part := range r.parts {
				managed[strings.TrimPrefix(rulePath(r.item.ID, part), "./")] = true
			}
		}
	}
	for _, u := range readSettingsView(originalSettings).adguardURLs() {
		managed["rules/"+tagForString("adguard", u)+".txt"] = true
	}
	readers := append(append([]string{}, references...), ResourceReaders(current)...)
	withdrawn := map[string]bool{}
	for path := range oldPaths {
		if !live[path] && managed[path] && !ResourceIsRead(path, readers) {
			withdrawn[path] = true
		}
	}
	var downloader *Node
	for _, plugin := range items(get(candidate, "plugins")) {
		if equal(get(plugin, "tag"), S("rules_download")) {
			downloader = plugin
			break
		}
	}
	if len(withdrawn) == 0 || downloader == nil || !equal(get(downloader, "type"), S("download")) {
		return 0, false, nil
	}
	downloads := get(get(downloader, "args"), "downloads")
	if !isMap(get(downloader, "args")) || !isSeq(downloads) {
		return 0, false, nil
	}
	prior := clone(candidate)
	retained := filter(downloads.Content, func(item *Node) bool { return !withdrawn[downloadPath(item)] })
	removed := len(downloads.Content) - len(retained)
	replaceDownloadEntries(downloader, retained)
	ctx, err := newContext(candidate, settings)
	if err == nil {
		err = finishRuleUpdates(ctx, M("direct_rulesets", L()), p, "")
	}
	if err != nil {
		return 0, false, err
	}
	return removed, !equal(candidate, prior), nil
}

// updateProviders is _providers: live provider references for the reload.
func updateProviders(document, existing *Node) *Node {
	_, catalog := schemas()
	available := map[string]bool{}
	for _, item := range items(get(document, "plugins")) {
		kind := str(get(item, "type"))
		if s := catalog[kind]; isMap(item) && (s == nil || s.Kind == "provider") {
			available[str(get(item, "tag"))] = true
		}
	}
	result := L()
	for _, value := range items(existing) {
		if isStr(value) && strings.HasPrefix(value.Value, "$") && available[value.Value[1:]] && !contains(result, value) {
			result.Content = append(result.Content, value)
		}
	}
	for _, item := range items(get(document, "plugins")) {
		if isOneOf(get(item, "type"), "domain_set", "ip_set", "adguard_rule") && (len(items(get(mapOr(get(item, "args")), "files"))) > 0) {
			if value := S("$" + str(get(item, "tag"))); !contains(result, value) {
				result.Content = append(result.Content, value)
			}
		}
	}
	return result
}

func restorePipeline(c *commonContext, tag string) (bool, error) {
	if c.plugin(tag) != nil {
		return false, nil
	}
	saved := get(c.parked, tag)
	if isMap(saved) && equal(get(saved, "tag"), S(tag)) && equal(get(saved, "type"), S(pipeline[tag])) {
		_, err := c.ensure(tag, nil, pipeline[tag])
		return err == nil, err
	}
	return false, nil
}

// prepareRuleUpdate keeps manual updates available for live subscriptions,
// independently of the schedule.
func prepareRuleUpdate(c *commonContext, changes *Node, p *Profile, baseURL string) error {
	if !relevant(changes) {
		return nil
	}
	enable := isTrue(get(changes, "rule_update.enabled"))
	for _, tag := range pipelineTags {
		if c.includedTags[tag] || c.aliasCapability(tag) != nil {
			if enable {
				return fail("规则更新流水线来自 include 文件或已重命名，请在完整参数或附加文件中调整")
			}
			return nil
		}
	}
	document, err := c.expanded()
	if err != nil {
		return err
	}
	desired, err := p.resourceDownloads(document, c.settings, baseURL, nil)
	if err != nil || len(desired) == 0 {
		return err
	}
	custom, complete := false, true
	for _, tag := range pipelineTags {
		plugin := c.plugin(tag)
		custom = custom || plugin != nil && !equal(get(plugin, "type"), S(pipeline[tag]))
		complete = complete && plugin != nil
	}
	if !complete {
		// Imported YAML may rename part of a chain, or reuse both executors.
		kinds := map[string]*Node{}
		for _, item := range items(get(document, "plugins")) {
			if isOneOf(get(item, "type"), "download", "reload_provider") && pipeline[str(get(item, "tag"))] == "" {
				custom = true
			}
			kinds[str(get(item, "tag"))] = get(item, "type")
		}
		for _, item := range items(get(document, "plugins")) {
			steps := get(item, "args")
			if !equal(get(item, "type"), S("sequence")) || equal(get(item, "tag"), S("rules_refresh")) || !isSeq(steps) || len(steps.Content) != 2 {
				continue
			}
			refs := make([]string, 2)
			for i, step := range steps.Content {
				if isMap(step) && mapSize(step) == 1 && has(step, "exec") {
					refs[i] = displayValue(get(step, "exec"))
				}
			}
			if strings.HasPrefix(refs[0], "$") && strings.HasPrefix(refs[1], "$") &&
				equal(kinds[refs[0][1:]], S("download")) && equal(kinds[refs[1][1:]], S("reload_provider")) {
				custom = true
			}
		}
	}
	if custom {
		if enable {
			return fail("已有自定义规则更新插件，请在完整参数中配置其定时任务，不能重复创建更新链")
		}
		return nil
	}
	restoredDownload, err := restorePipeline(c, "rules_download")
	if err != nil {
		return err
	}
	restoredReload, err := restorePipeline(c, "rules_reload")
	if err != nil {
		return err
	}
	restoredRefresh, err := restorePipeline(c, "rules_refresh")
	if err != nil {
		return err
	}
	download := c.plugin("rules_download")
	if restoredDownload {
		args := get(download, "args")
		if !isMap(args) {
			return pipelineError()
		}
		// Parked empty downloads keep the options of surviving entries only.
		historical := get(c.parked, "rules_download_entries")
		old := map[string]*Node{}
		for _, item := range append(append([]*Node{}, items(historical)...), items(get(args, "downloads"))...) {
			if p := downloadPath(item); p != "" {
				old[p] = item
			}
		}
		entries := L()
		for _, item := range desired {
			chosen := item
			if previous := old[downloadPath(item)]; previous != nil {
				chosen = previous
			}
			entries.Content = append(entries.Content, clone(chosen))
		}
		set(args, "downloads", entries)
		if customEntries := get(c.parked, "rules_download_custom_entries"); isSeq(customEntries) || !has(c.parked, "rules_download_custom_entries") {
			present := map[string]bool{}
			for _, item := range entries.Content {
				present[downloadPath(item)] = true
			}
			for _, item := range items(customEntries) {
				if p := downloadPath(item); p != "" && !present[p] {
					entries.Content = append(entries.Content, clone(item))
				}
			}
		}
	}
	reload := c.plugin("rules_reload")
	if restoredReload {
		if !isSeq(get(reload, "args")) {
			return pipelineError()
		}
		set(reload, "args", updateProviders(document, get(reload, "args")))
	}
	refresh := c.plugin("rules_refresh")
	if restoredRefresh && equal(get(refresh, "args"), L()) {
		set(refresh, "args", refreshSteps())
	}
	if refresh != nil && equal(get(refresh, "type"), S("sequence")) && equal(get(refresh, "args"), downloadOnlySteps()) && reload != nil && (len(items(get(reload, "args"))) > 0) {
		set(refresh, "args", refreshSteps())
	}
	defaults := at(defaultSettingsNode, "rule_update", "args")
	if _, err := c.ensure("rules_download", M("timeout", get(defaults, "timeout"), "downloads", seqOf(desired)), "download"); err != nil {
		return err
	}
	if _, err := c.ensure("rules_reload", updateProviders(document, L()), "reload_provider"); err != nil {
		return err
	}
	_, err = c.ensure("rules_refresh", refreshSteps(), "sequence")
	return err
}

func managedJobs(cron *Node) ([]*Node, error) {
	if cron == nil {
		return nil, nil
	}
	if !equal(get(cron, "type"), S("cron")) || !isMap(get(cron, "args")) || !isSeq(get(get(cron, "args"), "jobs")) {
		return nil, pipelineError()
	}
	var jobs []*Node
	for _, job := range get(get(cron, "args"), "jobs").Content {
		if isMap(job) && (equal(get(job, "name"), S("refresh_rules")) || refersTo(get(job, "executors"), "rules_refresh")) {
			jobs = append(jobs, job)
		}
	}
	for _, job := range jobs {
		if len(jobs) > 1 || !equal(get(job, "name"), S("refresh_rules")) || !equal(get(job, "executors"), L("$rules_refresh")) {
			return nil, pipelineError()
		}
	}
	return jobs, nil
}

func externalReferences(c *commonContext, targets map[string]bool, jobs []*Node) (bool, error) {
	document, err := c.expanded()
	if err != nil {
		return false, err
	}
	if len(ReferenceLocations(document, nil, true)) > 0 {
		return true, nil
	}
	top := withoutPlugins(document)
	for tag := range targets {
		if refersTo(top, tag) {
			return true, nil
		}
	}
	for _, item := range items(get(document, "plugins")) {
		if targets[str(get(item, "tag"))] {
			continue
		}
		value := item
		if equal(get(item, "tag"), S("rules_cron")) && isMap(get(item, "args")) {
			value = clone(item)
			kept := filter(items(get(get(item, "args"), "jobs")), func(job *Node) bool { return !contains(seqOf(jobs), job) })
			set(get(value, "args"), "jobs", seqOf(kept))
		}
		for tag := range targets {
			if refersTo(value, tag) {
				return true, nil
			}
		}
	}
	return false, nil
}

// stopPipeline parks the owned pipeline (only rules_refresh when
// refreshOnly), keeping user jobs and refusing unknown references.
func stopPipeline(c *commonContext, refreshOnly bool) error {
	targets := map[string]bool{}
	for _, tag := range pipelineTags {
		if c.plugin(tag) != nil && (!refreshOnly || tag == "rules_refresh") {
			targets[tag] = true
			if !equal(get(c.plugin(tag), "type"), S(pipeline[tag])) {
				return pipelineError()
			}
		}
	}
	if refresh := c.plugin("rules_refresh"); refresh != nil {
		args := get(refresh, "args")
		if !equal(args, refreshSteps()) && !equal(args, downloadOnlySteps()) && !equal(args, L()) {
			return pipelineError()
		}
	}
	cron := c.plugin("rules_cron")
	jobs, err := managedJobs(cron)
	if err != nil {
		return err
	}
	if external, err := externalReferences(c, targets, jobs); err != nil {
		return err
	} else if external {
		return pipelineError()
	}
	for _, job := range jobs {
		c.park("rules_cron_job", job)
		list := get(get(cron, "args"), "jobs")
		if i := indexOf(list, job); i >= 0 {
			list.Content = append(list.Content[:i:i], list.Content[i+1:]...)
		}
	}
	if targets["rules_download"] {
		// Keep the options of withdrawn entries apart from the empty plugin.
		original, err := readSettingsView(c.settings).loadNative(NativeLabel)
		if err != nil {
			return err
		}
		var entries []*Node
		for _, item := range items(get(original, "plugins")) {
			if equal(get(item, "tag"), S("rules_download")) && equal(get(item, "type"), S("download")) {
				entries = items(get(mapOr(get(item, "args")), "downloads"))
				break
			}
		}
		byPath := map[string]*Node{}
		var order []string
		for _, item := range append(append([]*Node{}, items(get(c.parked, "rules_download_entries"))...), entries...) {
			if p := downloadPath(item); p != "" {
				if byPath[p] == nil {
					order = append(order, p)
				}
				byPath[p] = item
			}
		}
		if len(order) > 0 {
			saved := L()
			for _, p := range order {
				saved.Content = append(saved.Content, byPath[p])
			}
			c.park("rules_download_entries", saved)
		}
	}
	for _, tag := range pipelineTags {
		if targets[tag] {
			c.parkPlugin(tag)
		}
	}
	if cron != nil && equal(get(get(cron, "args"), "jobs"), L()) {
		c.parkPlugin("rules_cron")
	}
	return nil
}

func removeEmptyReload(c *commonContext) error {
	refresh := c.plugin("rules_refresh")
	if refresh != nil && (!equal(get(refresh, "type"), S("sequence")) || !equal(get(refresh, "args"), refreshSteps()) && !equal(get(refresh, "args"), downloadOnlySteps())) {
		return pipelineError()
	}
	document, err := c.expanded()
	if err != nil {
		return err
	}
	for _, item := range items(get(document, "plugins")) {
		if !isOneOf(get(item, "tag"), "rules_reload", "rules_refresh") && refersTo(item, "rules_reload") {
			return pipelineError()
		}
	}
	if refersTo(withoutPlugins(document), "rules_reload") {
		return pipelineError()
	}
	c.parkPlugin("rules_reload")
	if refresh != nil {
		c.park("rules_refresh", refresh)
		set(refresh, "args", downloadOnlySteps())
	}
	return nil
}

// finishRuleUpdates suspends an empty owned pipeline without deleting
// independent user jobs, and restores the reload when resources return.
func finishRuleUpdates(c *commonContext, changes *Node, p *Profile, baseURL string) error {
	if !relevant(changes) {
		return nil
	}
	download, reload, refresh := c.plugin("rules_download"), c.plugin("rules_reload"), c.plugin("rules_refresh")
	downloads := get(get(download, "args"), "downloads")
	if !has(get(download, "args"), "downloads") {
		downloads = L()
	}
	emptyDownload := download != nil && equal(get(download, "type"), S("download")) && isMap(get(download, "args")) && equal(downloads, L())
	emptyRefresh := refresh != nil && equal(get(refresh, "type"), S("sequence")) && equal(get(refresh, "args"), L())
	if emptyDownload || emptyRefresh {
		return stopPipeline(c, emptyRefresh && !emptyDownload && reload != nil && (len(items(get(reload, "args"))) > 0))
	}
	if reload != nil && equal(get(reload, "type"), S("reload_provider")) && equal(get(reload, "args"), L()) {
		if download != nil && equal(get(download, "type"), S("download")) && isMap(get(download, "args")) && (len(items(get(get(download, "args"), "downloads"))) > 0) {
			return removeEmptyReload(c)
		}
		return stopPipeline(c, false)
	}
	// A custom download may outlive its last managed provider.
	saved := get(c.parked, "rules_reload")
	if download != nil && reload == nil && isMap(saved) && equal(get(saved, "tag"), S("rules_reload")) &&
		equal(get(saved, "type"), S("reload_provider")) && isSeq(get(saved, "args")) {
		document, err := c.expanded()
		if err != nil {
			return err
		}
		if providers := updateProviders(document, get(saved, "args")); len(providers.Content) > 0 {
			if _, err := restorePipeline(c, "rules_reload"); err != nil {
				return err
			}
			set(c.plugin("rules_reload"), "args", providers)
			if refresh != nil && equal(get(refresh, "type"), S("sequence")) && equal(get(refresh, "args"), downloadOnlySteps()) {
				set(refresh, "args", refreshSteps())
			}
		}
	}
	return nil
}

func managedDownloads(document *nativeDoc) *Node {
	for _, item := range items(get(document, "plugins")) {
		if equal(get(item, "tag"), S("rules_download")) {
			if !equal(get(item, "type"), S("download")) || !isSeq(get(mapOr(get(item, "args")), "downloads")) || !isMap(get(item, "args")) {
				return nil
			}
			return get(get(item, "args"), "downloads")
		}
	}
	return nil
}

// proxyIdentity: the known capability tokens and the profile's rule-proxy
// endpoint (ok false without a token).
func (p *Profile) proxyIdentity() (map[string]bool, string, bool) {
	credentials := mosdns.ProxyCredentials(p.source.SystemConfig)
	if credentials.Current == "" {
		return nil, "", false
	}
	tokens := map[string]bool{credentials.Current: true}
	for _, t := range credentials.Retired {
		tokens[t] = true
	}
	endpoint := "/api/mosdns/rule-proxy"
	if id := p.profileID(); id != "" {
		endpoint = "/api/profiles/" + id + "/mosdns/rule-proxy"
	}
	return tokens, endpoint, true
}

type sourcePart struct{ source, part string }

// rulesetSources maps generated resource paths to (source, part).
func (p *Profile) rulesetSources(referenced map[string]bool) map[string]sourcePart {
	out := map[string]sourcePart{}
	c := p.catalog()
	for _, id := range c.order {
		r := c.records[id]
		source := model.TextValue(r.item.URL)
		if r.suffix != "rulesets" || source == "" {
			continue
		}
		for part := range r.parts {
			path, _, _ := relativeResource(rulePath(r.item.ID, part))
			if referenced == nil || referenced[path] {
				out[path] = sourcePart{source, part}
			}
		}
	}
	return out
}

// generatedOrigin is the origin of a rule-proxy URL ConfigFlow rendered from
// source ("" for a manual override): endpoint, exact query, part, a known
// token and the nested source must all match.
func generatedOrigin(entry *Node, tokens map[string]bool, endpoint, source, part string) string {
	raw := get(entry, "url")
	if !isStr(raw) {
		return ""
	}
	u, err := url.Parse(raw.Value)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.Fragment != "" ||
		u.Opaque != "" || !strings.HasSuffix(u.EscapedPath(), endpoint) {
		return ""
	}
	if port := u.Port(); port != "" {
		if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
			return ""
		}
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(strings.Split(u.RawQuery, "&")) != 3 || len(query) != 3 ||
		len(query["url"]) != 1 || len(query["part"]) != 1 || len(query["token"]) != 1 ||
		!tokens[query.Get("token")] || query.Get("part") != part {
		return ""
	}
	path := u.EscapedPath()
	origin := u.Scheme + "://" + u.Host + path[:len(path)-len(endpoint)]
	expected := source
	if strings.HasPrefix(source, "/") {
		expected = origin + source
	}
	if query.Get("url") != expected {
		return ""
	}
	return origin
}

func withDownloads(document *nativeDoc, output []*Node) *Node {
	rendered := clone(document)
	for _, item := range items(get(rendered, "plugins")) {
		if equal(get(item, "tag"), S("rules_download")) {
			replaceDownloadEntries(item, output)
			break
		}
	}
	return rendered
}

// refreshManagedRuleURLs rebases proven generated downloads onto the current
// origin without changing custom native state.
func (p *Profile) refreshManagedRuleURLs(document *nativeDoc, baseURL string) (*Node, error) {
	downloads := managedDownloads(document)
	tokens, endpoint, ok := p.proxyIdentity()
	effective := p.base(baseURL)
	if downloads == nil || !ok || effective == "" {
		return document, nil
	}
	referenced, err := referencedPaths(document)
	if err != nil {
		return nil, err
	}
	sources := p.rulesetSources(referenced)
	var output []*Node
	changed := false
	for _, entry := range downloads.Content {
		output = append(output, entry)
		sp, found := sources[downloadPath(entry)]
		if !found || generatedOrigin(entry, tokens, endpoint, sp.source, sp.part) == "" {
			continue
		}
		current := sp.source
		if strings.HasPrefix(current, "/") {
			current = effective + current
		}
		link, err := p.ruleProxyURL(effective, current, sp.part)
		if err != nil {
			return nil, err
		}
		if !equal(get(entry, "url"), S(link)) {
			updated := shallow(entry)
			set(updated, "url", S(link))
			output[len(output)-1] = updated
			changed = true
		}
	}
	if changed {
		return withDownloads(document, output), nil
	}
	return document, nil
}

// RebaseNativeRuleSources follows a rule-library source edit in after's
// native draft (in place): only URLs generated from the previous source move.
func RebaseNativeRuleSources(before, after *Profile) (bool, error) {
	settings := after.Settings()
	view := readSettingsView(settings)
	if !view.isNative() {
		return false, nil
	}
	document, err := view.loadNativeOrEmpty(NativeLabel)
	if err != nil {
		return false, nil
	}
	downloads := managedDownloads(document)
	tokens, endpoint, ok := before.proxyIdentity()
	if downloads == nil || !ok {
		return false, nil
	}
	previous, current := before.rulesetSources(nil), after.rulesetSources(nil)
	var output []*Node
	changed := false
	for _, entry := range downloads.Content {
		output = append(output, entry)
		path := downloadPath(entry)
		old, okOld := previous[path]
		next, okNew := current[path]
		if !okOld || !okNew || old == next || old.part != next.part {
			continue
		}
		origin := generatedOrigin(entry, tokens, endpoint, old.source, old.part)
		if origin == "" {
			continue
		}
		source := next.source
		if strings.HasPrefix(source, "/") {
			source = origin + source
		}
		link, err := after.ruleProxyURL(origin, source, old.part)
		if err != nil {
			return false, err
		}
		updated := shallow(entry)
		set(updated, "url", S(link))
		output[len(output)-1] = updated
		changed = true
	}
	if changed {
		if err := setNativeConfig(settings, withDownloads(document, output)); err != nil {
			return false, err
		}
	}
	return changed, nil
}

// PrepareNativeRuleUpdates renders current routing and manual updates
// without changing the saved draft.
func PrepareNativeRuleUpdates(document *nativeDoc, settings *Node, p *Profile, baseURL string) (*Node, error) {
	document, settings, err := MigrateDomainOnlyRouting(document, settings, p, baseURL)
	if err != nil {
		return nil, err
	}
	if document, err = p.refreshManagedRuleURLs(document, baseURL); err != nil {
		return nil, err
	}
	for _, item := range items(get(document, "plugins")) {
		if isOneOf(get(item, "type"), "download", "reload_provider") || pipeline[str(get(item, "tag"))] != "" {
			return document, nil
		}
	}
	rendered, current := clone(document), clone(settings)
	if err := setNativeConfig(current, rendered); err != nil {
		return nil, err
	}
	c, err := newContext(rendered, current)
	if err != nil {
		return nil, err
	}
	if err := prepareRuleUpdate(c, M("rule_update.enabled", false), p, baseURL); err != nil {
		return nil, err
	}
	return rendered, nil
}
