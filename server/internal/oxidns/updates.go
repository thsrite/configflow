package oxidns

import (
	"net/url"
	"strconv"
	"strings"

	"configflow/internal/jsonx"
	"configflow/internal/mosdns"
	"configflow/internal/repo"
	"configflow/internal/yamlx"
)

// Port of backend/common/oxidns_common_updates.py (module C26): keep the
// owned rule-update pipeline (rules_download -> rules_reload ->
// rules_refresh, called by the rules_cron job refresh_rules) valid as the
// resource set of a native draft changes.

// pipelineTags is _PIPELINE in Python's dict order; pipelineKind maps each
// tag to its plugin type.
var (
	pipelineTags = []string{"rules_download", "rules_reload", "rules_refresh"}
	pipelineKind = map[string]string{"rules_download": "download", "rules_reload": "reload_provider", "rules_refresh": "sequence"}
)

// refreshSteps is _REFRESH, downloadOnlySteps _DOWNLOAD_ONLY (fresh copies).
func refreshSteps() []any {
	return []any{jsonx.Obj("exec", "$rules_download"), jsonx.Obj("exec", "$rules_reload")}
}

func downloadOnlySteps() []any { return []any{jsonx.Obj("exec", "$rules_download")} }

const pipelineErrorMessage = "规则更新流水线仍被自定义执行链引用或包含自定义步骤，请先在完整参数中调整，不能自动停用"

// updatesDocument is _document(ctx): the document, expanded when it has
// includes.
func updatesDocument(ctx *commonContext) *jsonx.Object {
	if !truthy(ctx.doc.Value("include")) {
		return ctx.doc
	}
	settings := ctx.settings.Clone()
	settings.Set("native_config", dump(ctx.doc))
	return nativeDocument(settings, false, nil)
}

// downloadPath is _path(entry): "dir/filename" without "./", ok false for
// None.
func downloadPath(entry any) (string, bool) {
	obj, isObj := entry.(*jsonx.Object)
	if !isObj || obj == nil {
		return "", false
	}
	dir, ok1 := obj.Value("dir").(string)
	name, ok2 := obj.Value("filename").(string)
	if !ok1 || !ok2 {
		return "", false
	}
	return strings.TrimPrefix(strings.TrimRight(dir, "/")+"/"+name, "./"), true
}

// pathKey is _path(entry) as a set/dict key (None has its own key).
func pathKey(entry any) string {
	if p, ok := downloadPath(entry); ok {
		return p
	}
	return "\x00None"
}

// pluginsOf is document.get('plugins', []) iterated.
func pluginsOf(document any) []any {
	return pyIter(get(document, "plugins", []any{}))
}

// ReconcileNativeWithdrawals is reconcile_native_withdrawals(before,
// candidate, original_settings, settings, config): withdraw proven managed
// download targets from candidate (in place); returns (removed entries,
// whether candidate changed).
func ReconcileNativeWithdrawals(before, candidate, originalSettings, settings, config *jsonx.Object) (removed int, changed bool, err error) {
	defer catch(&err)
	removed, changed = reconcileNativeWithdrawals(before, candidate, originalSettings, settings, config)
	return removed, changed, nil
}

func reconcileNativeWithdrawals(before, candidate, originalSettings, settings *jsonx.Object, configValue any) (int, bool) {
	expanded := func(document, values *jsonx.Object) *jsonx.Object {
		if !truthy(document.Value("include")) {
			return document
		}
		merged := values.Clone()
		text, err := yamlx.SafeDump(document)
		must(err)
		merged.Set("native_config", text)
		return nativeDocument(merged, false, nil)
	}
	var current *jsonx.Object
	var references []string
	oldPaths, live := map[string]bool{}, map[string]bool{}
	if tryExcept(func() {
		previous := expanded(before, originalSettings)
		current = expanded(candidate, settings)
		for _, path := range resourceReferences(previous) {
			oldPaths[resourceKey(relativeResource(path))] = true
		}
		references = resourceReferences(current)
		for _, path := range references {
			live[resourceKey(relativeResource(path))] = true
		}
	}, ClassSettings) != nil {
		// Complete-form drafts may contain unfinished unrelated resource fields.
		return 0, false
	}
	// Unexpanded paths could still consume one of the withdrawn files.
	for _, path := range references {
		if strings.Contains(path, "${") {
			return 0, false
		}
	}
	config := configOf(configValue)
	paths := map[string]bool{}
	catalog := buildCatalog(config)
	for _, identity := range catalog.order {
		record := catalog.records[identity]
		if record.suffix != "rulesets" {
			continue
		}
		for part := range record.parts {
			paths[strings.TrimPrefix(rulePath(record.item, part), "./")] = true
		}
	}
	adguard := get(originalSettings, "adguard", jsonx.NewObject())
	for _, value := range pyIter(get(adguard, "urls", []any{})) {
		if text, ok := value.(string); ok {
			paths["rules/"+TagFor("adguard", text)+".txt"] = true
		}
	}
	readers := append(append([]string{}, references...), resourceReaders(current)...)
	withdrawn := map[string]bool{}
	for path := range oldPaths {
		if !live[path] && paths[path] && !ResourceIsRead(path, readers) {
			withdrawn[path] = true
		}
	}
	var downloader any
	for _, plugin := range pluginsOf(candidate) {
		if get(plugin, "tag", nil) == "rules_download" {
			downloader = plugin
			break
		}
	}
	if len(withdrawn) == 0 || !truthy(downloader) || get(downloader, "type", nil) != "download" {
		return 0, false
	}
	args, ok := get(downloader, "args", nil).(*jsonx.Object)
	if !ok || args == nil {
		return 0, false
	}
	downloads, ok := args.Value("downloads").([]any)
	if !ok {
		return 0, false // Leave unfinished/custom native values to their editor.
	}
	prior := copyValue(candidate)
	retained := []any{}
	for _, entry := range downloads {
		if p, ok := downloadPath(entry); !ok || !withdrawn[p] {
			retained = append(retained, entry)
		}
	}
	removed := len(downloads) - len(retained)
	replaceDownloadEntries(downloader.(*jsonx.Object), retained)
	// Reuse the common editor's conservative empty-pipeline lifecycle.
	finishRuleUpdates(newCommonContext(candidate, settings), jsonx.Obj("direct_rulesets", []any{}), configValue, "")
	return removed, !jsonx.Equal(candidate, prior)
}

// updateProviders is _providers(document, existing).
func updateProviders(document any, existing any) []any {
	_, pluginSchemas, err := Schemas()
	must(err)
	available := map[string]bool{}
	for _, plugin := range pluginsOf(document) {
		if !isDict(plugin) {
			continue
		}
		kind := get(plugin, "type", nil)
		hashable(kind)
		include := true
		if text, ok := kind.(string); ok && pluginSchemas.Has(text) {
			include = get(pluginSchemas.Value(text), "x-kind", nil) == "provider"
		}
		if include {
			tag := item(plugin, "tag")
			hashable(tag)
			if text, ok := tag.(string); ok {
				available[text] = true
			}
		}
	}
	result := []any{}
	contains := func(value string) bool {
		for _, element := range result {
			if element == value {
				return true
			}
		}
		return false
	}
	if list, ok := existing.([]any); ok {
		for _, value := range list {
			if text, ok := value.(string); ok && strings.HasPrefix(text, "$") && available[text[1:]] && !contains(text) {
				result = append(result, text)
			}
		}
	}
	for _, plugin := range pluginsOf(document) {
		if !strIn(get(plugin, "type", nil), "domain_set", "ip_set", "adguard_rule") {
			continue
		}
		args, ok := get(plugin, "args", nil).(*jsonx.Object)
		if !ok || args == nil || !truthy(args.Value("files")) {
			continue
		}
		tag, ok := item(plugin, "tag").(string)
		if !ok {
			raise(pyErr("TypeError", `can only concatenate str (not "`+typeName(item(plugin, "tag"))+`") to str`))
		}
		if !contains("$" + tag) {
			result = append(result, "$"+tag)
		}
	}
	return result
}

// restorePipeline is _restore(ctx, tag).
func restorePipeline(ctx *commonContext, tag string) bool {
	if ctx.plugin(tag) != nil {
		return false
	}
	saved, ok := get(ctx.parked, tag, nil).(*jsonx.Object)
	if ok && saved != nil && saved.Value("tag") == tag && saved.Value("type") == pipelineKind[tag] {
		ctx.ensure(tag, nil, pipelineKind[tag])
		return true
	}
	return false
}

// prepareRuleUpdateImpl is prepare_rule_update(ctx, changes, config,
// base_url): keep manual updates available for live subscriptions,
// independently of cron.
func prepareRuleUpdateImpl(ctx *commonContext, changes *jsonx.Object, config any, baseURL string) {
	if !ruleUpdateRelevant(changes) {
		return
	}
	enabling := changes.Value("rule_update.enabled") == true
	for _, tag := range pipelineTags {
		if ctx.includedTags[tag] || ctx.aliasCapability(tag) != nil {
			if enabling {
				fail("规则更新流水线来自 include 文件或已重命名，请在完整参数或附加文件中调整")
			}
			return
		}
	}
	document := updatesDocument(ctx)
	desired := resourceDownloadsImpl(document, ctx.settings, config, baseURL, nil)
	if len(desired) == 0 {
		return
	}
	// Do not introduce a second update plan alongside custom native executors.
	custom := false
	for _, tag := range pipelineTags {
		if p := ctx.plugin(tag); p != nil && p.Value("type") != pipelineKind[tag] {
			custom = true
			break
		}
	}
	all := true
	for _, tag := range pipelineTags {
		if ctx.plugin(tag) == nil {
			all = false
		}
	}
	if !all {
		// Imported YAML may rename only part of a chain, without editor alias
		// metadata. A custom sequence can also reuse both standard executors.
		if !custom {
			for _, plugin := range pluginsOf(document) {
				if strIn(get(plugin, "type", nil), "download", "reload_provider") {
					tag := get(plugin, "tag", nil)
					hashable(tag)
					if text, ok := tag.(string); !ok || pipelineKind[text] == "" {
						custom = true
						break
					}
				}
			}
		}
		kinds := map[string]any{}
		for _, plugin := range pluginsOf(document) {
			tag := get(plugin, "tag", nil)
			hashable(tag)
			kind := get(plugin, "type", nil)
			if text, ok := tag.(string); ok {
				kinds[text] = kind
			}
		}
		for _, plugin := range pluginsOf(document) {
			steps := get(plugin, "args", nil)
			list, isList := steps.([]any)
			if get(plugin, "type", nil) != "sequence" || get(plugin, "tag", nil) == "rules_refresh" || !isList || len(list) != 2 {
				continue
			}
			refs := make([]any, 2)
			for i, step := range list {
				refs[i] = ""
				if obj, ok := step.(*jsonx.Object); ok && obj != nil && obj.Len() == 1 && obj.Has("exec") {
					refs[i] = obj.Value("exec")
				}
			}
			allRefs := true
			for _, ref := range refs {
				if text, ok := ref.(string); !ok || !strings.HasPrefix(text, "$") {
					allRefs = false
				}
			}
			if allRefs && kinds[refs[0].(string)[1:]] == "download" && kinds[refs[1].(string)[1:]] == "reload_provider" {
				custom = true
			}
		}
	}
	if custom {
		if enabling {
			fail("已有自定义规则更新插件，请在完整参数中配置其定时任务，不能重复创建更新链")
		}
		return
	}
	restoredDownload := restorePipeline(ctx, "rules_download")
	restoredReload := restorePipeline(ctx, "rules_reload")
	restoredRefresh := restorePipeline(ctx, "rules_refresh")
	download := ctx.plugin("rules_download")
	if restoredDownload {
		args, ok := download.Value("args").(*jsonx.Object)
		if !ok || args == nil {
			fail(pipelineErrorMessage)
		}
		// Empty owned downloads were parked only after resource reconciliation.
		// Preserve any surviving entry's options, and never restore old resources.
		historical, ok := get(ctx.parked, "rules_download_entries", []any{}).([]any)
		if !ok {
			historical = []any{}
		}
		old := map[string]any{}
		for _, entry := range append(append([]any{}, historical...), pyIter(args.GetOr("downloads", []any{}))...) {
			if p, ok := downloadPath(entry); ok {
				old[p] = entry
			}
		}
		downloads := make([]any, 0, len(desired))
		for _, entry := range desired {
			value := entry
			if previous, ok := old[pathKey(entry)]; ok {
				value = previous
			}
			downloads = append(downloads, copyValue(value))
		}
		if customEntries, ok := get(ctx.parked, "rules_download_custom_entries", []any{}).([]any); ok {
			present := map[string]bool{}
			for _, entry := range downloads {
				present[pathKey(entry)] = true
			}
			for _, entry := range customEntries {
				if p, ok := downloadPath(entry); ok && !present[p] {
					downloads = append(downloads, copyValue(entry))
				}
			}
		}
		args.Set("downloads", downloads)
	}
	reload := ctx.plugin("rules_reload")
	if restoredReload {
		existing, ok := reload.Value("args").([]any)
		if !ok {
			fail(pipelineErrorMessage)
		}
		reload.Set("args", updateProviders(document, existing))
	}
	refresh := ctx.plugin("rules_refresh")
	if restoredRefresh && jsonx.Equal(refresh.Value("args"), []any{}) {
		refresh.Set("args", refreshSteps())
	}
	if refresh != nil && refresh.Value("type") == "sequence" && jsonx.Equal(refresh.Value("args"), downloadOnlySteps()) &&
		reload != nil && truthy(reload.Value("args")) {
		refresh.Set("args", refreshSteps())
	}
	defaults := DefaultSettings().GetObject("rule_update").GetObject("args")
	ctx.ensure("rules_download", jsonx.Obj("timeout", defaults.Value("timeout"), "downloads", desired), "download")
	ctx.ensure("rules_reload", updateProviders(document, []any{}), "reload_provider")
	ctx.ensure("rules_refresh", refreshSteps(), "sequence")
}

// managedJobs is _managed_jobs(cron).
func managedJobs(cron *jsonx.Object) []any {
	if cron == nil {
		return []any{}
	}
	args, ok := cron.Value("args").(*jsonx.Object)
	if cron.Value("type") != "cron" || !ok || args == nil {
		fail(pipelineErrorMessage)
	}
	list, ok := args.Value("jobs").([]any)
	if !ok {
		fail(pipelineErrorMessage)
	}
	jobs := []any{}
	for _, job := range list {
		obj, ok := job.(*jsonx.Object)
		if ok && obj != nil && (obj.Value("name") == "refresh_rules" || references(obj.Value("executors"), "rules_refresh")) {
			jobs = append(jobs, job)
		}
	}
	if len(jobs) > 1 {
		fail(pipelineErrorMessage)
	}
	for _, job := range jobs {
		obj := job.(*jsonx.Object)
		if obj.Value("name") != "refresh_rules" || !jsonx.Equal(obj.Value("executors"), []any{"$rules_refresh"}) {
			fail(pipelineErrorMessage)
		}
	}
	return jobs
}

// externalReferences is _external_references(ctx, targets, managed_jobs).
func externalReferences(ctx *commonContext, targets map[string]bool, jobs []any) bool {
	document := updatesDocument(ctx)
	if len(referenceLocations(document, nil, true)) > 0 {
		return true
	}
	external := withoutKey(document, "plugins")
	for tag := range targets {
		if references(external, tag) {
			return true
		}
	}
	for _, plugin := range pluginsOf(document) {
		tag := get(plugin, "tag", nil)
		hashable(tag)
		if text, ok := tag.(string); ok && targets[text] {
			continue
		}
		value := plugin
		if tag == "rules_cron" && isDict(get(plugin, "args", nil)) {
			copied := copyValue(plugin).(*jsonx.Object)
			kept := []any{}
			for _, job := range pyIter(get(item(plugin, "args"), "jobs", []any{})) {
				if listIndex(jobs, job) < 0 {
					kept = append(kept, job)
				}
			}
			copied.Value("args").(*jsonx.Object).Set("jobs", kept)
			value = copied
		}
		for tag := range targets {
			if references(value, tag) {
				return true
			}
		}
	}
	return false
}

// stopPipeline is _stop_pipeline(ctx, refresh_only).
func stopPipeline(ctx *commonContext, refreshOnly bool) {
	targets := map[string]bool{}
	for _, tag := range pipelineTags {
		if ctx.plugin(tag) != nil && (!refreshOnly || tag == "rules_refresh") {
			targets[tag] = true
		}
	}
	for _, tag := range pipelineTags {
		if targets[tag] && ctx.plugin(tag).Value("type") != pipelineKind[tag] {
			fail(pipelineErrorMessage)
		}
	}
	refresh := ctx.plugin("rules_refresh")
	if refresh != nil {
		args := refresh.Value("args")
		if !jsonx.Equal(args, refreshSteps()) && !jsonx.Equal(args, downloadOnlySteps()) && !jsonx.Equal(args, []any{}) {
			fail(pipelineErrorMessage)
		}
	}
	cron := ctx.plugin("rules_cron")
	jobs := managedJobs(cron)
	if externalReferences(ctx, targets, jobs) {
		fail(pipelineErrorMessage)
	}
	for _, job := range jobs {
		ctx.park("rules_cron_job", job)
		cronArgs := cron.Value("args").(*jsonx.Object)
		old := cronArgs.Value("jobs").([]any)
		ctx.replaceList(cronArgs, "jobs", old, listRemove(old, job))
	}
	if targets["rules_download"] {
		// Retain the options of withdrawn entries separately from the empty
		// parked plugin, so changing a disabled option cannot resurrect a
		// removed download.
		original := loadNativeYAML(item(ctx.settings, "native_config"), DefaultNativeLabel)
		var prior any = jsonx.NewObject()
		for _, plugin := range pluginsOf(original) {
			if get(plugin, "tag", nil) == "rules_download" && get(plugin, "type", nil) == "download" {
				prior = plugin
				break
			}
		}
		var entries any = []any{}
		if args := get(prior, "args", nil); isDict(args) {
			entries = get(args, "downloads", []any{})
		}
		saved, ok := get(ctx.parked, "rules_download_entries", []any{}).([]any)
		if !ok {
			saved = []any{}
		}
		all := append([]any{}, saved...)
		if list, ok := entries.([]any); ok {
			all = append(all, list...)
		}
		byPath := jsonx.NewObject()
		for _, entry := range all {
			if p, ok := downloadPath(entry); ok {
				byPath.Set(p, entry)
			}
		}
		if byPath.Len() > 0 {
			ctx.park("rules_download_entries", byPath.Values())
		}
	}
	for _, tag := range pipelineTags {
		if targets[tag] {
			ctx.parkPlugin(tag)
		}
	}
	if cron != nil && jsonx.Equal(cron.Value("args").(*jsonx.Object).Value("jobs"), []any{}) {
		ctx.parkPlugin("rules_cron")
	}
}

// removeEmptyReload is _remove_empty_reload(ctx).
func removeEmptyReload(ctx *commonContext) {
	refresh := ctx.plugin("rules_refresh")
	if refresh != nil && (refresh.Value("type") != "sequence" ||
		(!jsonx.Equal(refresh.Value("args"), refreshSteps()) && !jsonx.Equal(refresh.Value("args"), downloadOnlySteps()))) {
		fail(pipelineErrorMessage)
	}
	// The canonical refresh is the only reference that can safely be rewritten.
	document := updatesDocument(ctx)
	for _, plugin := range pluginsOf(document) {
		if !strIn(get(plugin, "tag", nil), "rules_reload", "rules_refresh") && references(plugin, "rules_reload") {
			fail(pipelineErrorMessage)
		}
	}
	if references(withoutKey(document, "plugins"), "rules_reload") {
		fail(pipelineErrorMessage)
	}
	ctx.parkPlugin("rules_reload")
	if refresh != nil {
		ctx.park("rules_refresh", refresh)
		refresh.Set("args", downloadOnlySteps())
	}
}

// finishRuleUpdatesImpl is finish_rule_updates(ctx, changes, config,
// base_url): suspend an empty owned pipeline without deleting independent
// user jobs.
func finishRuleUpdatesImpl(ctx *commonContext, changes *jsonx.Object, config any, baseURL string) {
	if !ruleUpdateRelevant(changes) {
		return
	}
	download := ctx.plugin("rules_download")
	reload := ctx.plugin("rules_reload")
	refresh := ctx.plugin("rules_refresh")
	downloadArgs, downloadDict := (*jsonx.Object)(nil), false
	if download != nil {
		downloadArgs, downloadDict = download.Value("args").(*jsonx.Object)
		downloadDict = downloadDict && downloadArgs != nil
	}
	emptyDownload := download != nil && download.Value("type") == "download" && downloadDict &&
		jsonx.Equal(downloadArgs.GetOr("downloads", []any{}), []any{})
	emptyRefresh := refresh != nil && refresh.Value("type") == "sequence" && jsonx.Equal(refresh.Value("args"), []any{})
	if emptyDownload || emptyRefresh {
		stopPipeline(ctx, emptyRefresh && !emptyDownload && reload != nil && truthy(reload.Value("args")))
		return
	}
	if reload != nil && reload.Value("type") == "reload_provider" && jsonx.Equal(reload.Value("args"), []any{}) {
		if download != nil && download.Value("type") == "download" && downloadDict && truthy(downloadArgs.Value("downloads")) {
			removeEmptyReload(ctx)
		} else {
			stopPipeline(ctx, false)
		}
		return
	}
	// A custom download may remain useful after its last managed provider was
	// removed. Restore only live provider references when resources return.
	saved, ok := get(ctx.parked, "rules_reload", nil).(*jsonx.Object)
	if download != nil && reload == nil && ok && saved != nil && saved.Value("tag") == "rules_reload" &&
		saved.Value("type") == "reload_provider" {
		existing, isList := saved.Value("args").([]any)
		if !isList {
			return
		}
		providers := updateProviders(updatesDocument(ctx), existing)
		if len(providers) > 0 {
			restorePipeline(ctx, "rules_reload")
			ctx.plugin("rules_reload").Set("args", providers)
			if refresh != nil && refresh.Value("type") == "sequence" && jsonx.Equal(refresh.Value("args"), downloadOnlySteps()) {
				refresh.Set("args", refreshSteps())
			}
		}
	}
}

// managedDownloads is _managed_downloads(document): the owned downloader's
// entries, ok false for None.
func managedDownloads(document any) ([]any, bool) {
	var downloader any
	found := false
	for _, plugin := range pluginsOf(document) {
		if get(plugin, "tag", nil) == "rules_download" {
			downloader, found = plugin, true
			break
		}
	}
	if !found || get(downloader, "type", nil) != "download" {
		return nil, false
	}
	args, ok := get(downloader, "args", nil).(*jsonx.Object)
	if !ok || args == nil {
		return nil, false
	}
	list, ok := args.Value("downloads").([]any)
	return list, ok
}

// proxyIdentity is _proxy_identity(config): known capability tokens and the
// profile rule-proxy endpoint; ok false without a token.
type proxyIdentity struct {
	tokens   map[string]bool
	endpoint string
}

func proxyIdentityOf(config *jsonx.Object) (proxyIdentity, bool) {
	system := get(config, "system_config", jsonx.NewObject())
	token, ok := get(system, "rule_proxy_token", nil).(string)
	if !ok || token == "" {
		return proxyIdentity{}, false
	}
	tokens := map[string]bool{token: true}
	if retired, ok := get(system, "retired_rule_proxy_tokens", []any{}).([]any); ok {
		for _, value := range retired {
			if text, ok := value.(string); ok && text != "" {
				tokens[text] = true
			}
		}
	}
	endpoint := repo.ProfileAPIPath(config, "/mosdns/rule-proxy")
	if endpoint == "/mosdns/rule-proxy" {
		endpoint = "/api" + endpoint
	}
	return proxyIdentity{tokens, endpoint}, true
}

type sourcePart struct{ source, part string }

// rulesetSources is _ruleset_sources(config, referenced): active ruleset
// sources by generated resource path (keys as resourceKey); referenced nil
// is None.
func rulesetSources(config *jsonx.Object, referenced map[string]bool) map[string]sourcePart {
	sources := map[string]sourcePart{}
	catalog := buildCatalog(config)
	for _, identity := range catalog.order {
		record := catalog.records[identity]
		source, ok := record.item.Value("url").(string)
		if record.suffix != "rulesets" || !ok || source == "" {
			continue
		}
		for part := range record.parts {
			path := resourceKey(relativeResource(rulePath(record.item, part)))
			if referenced == nil || referenced[path] {
				sources[path] = sourcePart{source, part}
			}
		}
	}
	return sources
}

// generatedSource is _generated_source(entry, identity, source, part): the
// origin of a generated rule-proxy URL built from source, ok false when
// the URL is a manual override.
func generatedSource(entry any, identity proxyIdentity, source, part string) (string, bool) {
	raw, ok := get(entry, "url", nil).(string)
	if !ok {
		return "", false
	}
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" ||
		parsed.User != nil || parsed.Fragment != "" || parsed.Opaque != "" {
		return "", false
	}
	path := parsed.EscapedPath()
	if !strings.HasSuffix(path, identity.endpoint) {
		return "", false
	}
	// Reading the port also rejects malformed authorities conservatively.
	if port := parsed.Port(); port != "" {
		value, err := strconv.Atoi(port)
		if err != nil || value < 1 || value > 65535 {
			return "", false
		}
	}
	var keys []string
	query := map[string]string{}
	for _, pair := range strings.Split(parsed.RawQuery, "&") {
		if pair == "" {
			continue
		}
		key, value, _ := strings.Cut(pair, "=")
		key, err1 := url.QueryUnescape(key)
		value, err2 := url.QueryUnescape(value)
		if err1 != nil || err2 != nil {
			return "", false
		}
		keys = append(keys, key)
		query[key] = value
	}
	if len(keys) != 3 || len(query) != 3 || !hasKeys(query, "url", "part", "token") ||
		!identity.tokens[query["token"]] || query["part"] != part {
		return "", false
	}
	origin := parsed.Scheme + "://" + parsed.Host + path[:len(path)-len(identity.endpoint)]
	want := source
	if strings.HasPrefix(source, "/") {
		want = origin + source
	}
	if query["url"] != want {
		return "", false
	}
	return origin, true
}

func hasKeys(m map[string]string, keys ...string) bool {
	for _, key := range keys {
		if _, ok := m[key]; !ok {
			return false
		}
	}
	return true
}

// withDownloads is _with_downloads(document, output).
func withDownloads(document *jsonx.Object, output []any) *jsonx.Object {
	rendered := copyValue(document).(*jsonx.Object)
	for _, plugin := range pyIter(item(rendered, "plugins")) {
		if get(plugin, "tag", nil) == "rules_download" {
			replaceDownloadEntries(plugin.(*jsonx.Object), output)
			return rendered
		}
	}
	raise(pyErr("StopIteration", ""))
	return nil
}

// refreshManagedRuleURLs is _refresh_managed_rule_urls(document, config,
// base_url): rebase proven generated downloads to the current origin.
func refreshManagedRuleURLs(document *jsonx.Object, config *jsonx.Object, baseURL string) *jsonx.Object {
	downloads, ok := managedDownloads(document)
	identity, hasIdentity := proxyIdentityOf(config)
	effective := baseOf(config, baseURL)
	if !ok || !hasIdentity || effective == "" {
		return document
	}
	referenced := map[string]bool{}
	for _, path := range resourceReferences(document) {
		referenced[resourceKey(relativeResource(path))] = true
	}
	sources := rulesetSources(config, referenced)
	output, changed := []any{}, false
	for _, entry := range downloads {
		output = append(output, entry)
		sp, found := sources[pathKey(entry)]
		if !found {
			continue
		}
		if _, generated := generatedSource(entry, identity, sp.source, sp.part); !generated {
			continue
		}
		currentSource := sp.source
		if strings.HasPrefix(currentSource, "/") {
			currentSource = effective + currentSource
		}
		proxied, err := mosdns.BuildRuleProxyURL(config, effective, currentSource, sp.part, nil)
		must(err)
		if obj := entry.(*jsonx.Object); obj.Value("url") != proxied {
			updated := obj.Clone()
			updated.Set("url", proxied)
			output[len(output)-1] = updated
			changed = true
		}
	}
	if changed {
		return withDownloads(document, output)
	}
	return document
}

// RebaseNativeRuleSources is rebase_native_rule_sources(before, after):
// follow a rule-library source edit in after's native draft, in place.
func RebaseNativeRuleSources(before, after *jsonx.Object) (changed bool, err error) {
	defer catch(&err)
	return rebaseNativeRuleSources(before, after), nil
}

func rebaseNativeRuleSources(before, after *jsonx.Object) bool {
	settings, ok := after.Value("oxidns").(*jsonx.Object)
	if !ok || settings == nil || settings.Value("config_mode") != "native" {
		return false
	}
	var document *jsonx.Object
	if tryExcept(func() {
		document = loadNativeYAML(settings.GetOr("native_config", ""), DefaultNativeLabel)
	}, ClassSettings) != nil {
		return false
	}
	downloads, hasDownloads := managedDownloads(document)
	identity, hasIdentity := proxyIdentityOf(before)
	if !hasDownloads || !hasIdentity {
		return false
	}
	previous, current := rulesetSources(before, nil), rulesetSources(after, nil)
	output, changed := []any{}, false
	for _, entry := range downloads {
		output = append(output, entry)
		path := pathKey(entry)
		old, inPrevious := previous[path]
		next, inCurrent := current[path]
		if !inPrevious || !inCurrent || old == next {
			continue
		}
		if old.part != next.part {
			continue
		}
		origin, generated := generatedSource(entry, identity, old.source, old.part)
		if !generated {
			continue
		}
		source := next.source
		if strings.HasPrefix(source, "/") {
			source = origin + source
		}
		proxied, err := mosdns.BuildRuleProxyURL(after, origin, source, old.part, nil)
		must(err)
		updated := entry.(*jsonx.Object).Clone()
		updated.Set("url", proxied)
		output[len(output)-1] = updated
		changed = true
	}
	if changed {
		settings.Set("native_config", dump(withDownloads(document, output)))
	}
	return changed
}

// PrepareNativeRuleUpdates is prepare_native_rule_updates(document,
// settings, config, base_url): render current routing and manual updates
// without changing the saved draft.
func PrepareNativeRuleUpdates(document, settings, config *jsonx.Object, baseURL string) (rendered *jsonx.Object, err error) {
	defer catch(&err)
	return prepareNativeRuleUpdates(document, settings, config, baseURL), nil
}

func prepareNativeRuleUpdates(document, settings, config *jsonx.Object, baseURL string) *jsonx.Object {
	document, settings = migrateDomainOnlyRouting(document, settings, config, baseURL)
	document = refreshManagedRuleURLs(document, config, baseURL)
	for _, plugin := range pluginsOf(document) {
		if strIn(get(plugin, "type", nil), "download", "reload_provider") {
			return document
		}
		tag := get(plugin, "tag", nil)
		hashable(tag)
		if text, ok := tag.(string); ok && pipelineKind[text] != "" {
			return document
		}
	}
	rendered := copyValue(document).(*jsonx.Object)
	current := copyValue(settings).(*jsonx.Object)
	current.Set("native_config", dump(rendered))
	prepareRuleUpdate(newCommonContext(rendered, current), jsonx.Obj("rule_update.enabled", false), config, baseURL)
	return rendered
}

// FinishRuleUpdates is finish_rule_updates(_Context(document, settings),
// changes, config, base_url) on a fresh context (document and settings are
// changed in place). The native apply module (C29) is in this package and
// uses stopPipeline, downloadPath, refreshSteps, downloadOnlySteps directly.
func FinishRuleUpdates(document, settings, changes *jsonx.Object, config any, baseURL string) (err error) {
	defer catch(&err)
	finishRuleUpdates(newCommonContext(document, settings), changes, config, baseURL)
	return nil
}
