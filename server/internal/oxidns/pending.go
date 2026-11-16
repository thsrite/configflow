package oxidns

import "configflow/internal/jsonx"

// Functions of later modules of the OxiDNS cluster that earlier modules
// call. Each raises a *PendingError until its module is ported; the module's
// porter replaces the stub with the real function in its own file and
// deletes it here, then re-runs this package's replays
// (testdata/recorded.jsonl.gz, c23_recorded.jsonl.gz, c24_recorded.jsonl.gz),
// whose pending rows must drop to zero.
//
// The collaborators of oxidns_common (C24) are variables: the C24 replay
// (c24_test.go) answers them with what Python's functions answered during
// the recorded call. Their porters assign the real function (keep the
// variable, or turn it into a function and adapt c24_test.go's installer).

// prepareEditor is oxidns_editor.prepare_editor(settings, config,
// base_url) (C26).
var prepareEditor = func(settings, config any, baseURL string) *jsonx.Object {
	pending("C26", "oxidns_editor.prepare_editor")
	return nil
}

// readRouting is oxidns_common_routing.read_routing(document, settings,
// config) (C25): {"values", "capabilities"} of the routing controls.
var readRouting = func(document, settings *jsonx.Object, config any) *jsonx.Object {
	pending("C25", "oxidns_common_routing.read_routing")
	return nil
}

// ownedChains is oxidns_common_routing.owned_chains(document) (C25):
// {chain tag: action} of the pinned upstream chains of rule DNS.
var ownedChains = func(document *jsonx.Object) *jsonx.Object {
	pending("C25", "oxidns_common_routing.owned_chains")
	return nil
}

// applyRouting is oxidns_common_routing.apply_routing(document, settings,
// config, changes, base_url) (C25): the edited copies of document and
// settings.
var applyRouting = func(document, settings *jsonx.Object, config any, changes *jsonx.Object, baseURL string) (*jsonx.Object, *jsonx.Object) {
	pending("C25", "oxidns_common_routing.apply_routing")
	return nil, nil
}

// resourceDownloads is oxidns_common_routing.resource_downloads(document,
// settings, config, base_url) (C25).
var resourceDownloads = func(document, settings *jsonx.Object, config any, baseURL string) []any {
	pending("C25", "oxidns_common_routing.resource_downloads")
	return nil
}

// refreshRuleHelpers is oxidns_common_routing.refresh_rule_helpers(document)
// (C25): rebuilds the owned pinned upstream chains in place.
var refreshRuleHelpers = func(document *jsonx.Object) {
	pending("C25", "oxidns_common_routing.refresh_rule_helpers")
}

// prepareRuleUpdate is oxidns_common_updates.prepare_rule_update(ctx,
// changes, config, base_url) (C26). The early return for unrelated changes
// is ported; the rest is pending.
var prepareRuleUpdate = func(ctx *commonContext, changes *jsonx.Object, config any, baseURL string) {
	if !ruleUpdateRelevant(changes) {
		return
	}
	pending("C26", "oxidns_common_updates.prepare_rule_update")
}

// finishRuleUpdates is oxidns_common_updates.finish_rule_updates(ctx,
// changes, config, base_url) (C26). The early return for unrelated changes
// is ported; the rest is pending.
var finishRuleUpdates = func(ctx *commonContext, changes *jsonx.Object, config any, baseURL string) {
	if !ruleUpdateRelevant(changes) {
		return
	}
	pending("C26", "oxidns_common_updates.finish_rule_updates")
}

// ruleUpdateRelevant is oxidns_common_updates._relevant(changes).
func ruleUpdateRelevant(changes *jsonx.Object) bool {
	for _, field := range changes.Keys() {
		if RoutingFields[field] || hasAnyPrefix(field, "rule_update.", "adguard.") {
			return true
		}
	}
	return false
}

// classicalIPLines (rule_ip_presence.classical_ip_lines) moved to
// hooks_routers.go: a hook registered by routers R02 (internal/ippresence).

// migrateDomainOnlyRouting is
// oxidns_common_routing.migrate_domain_only_routing(document, settings,
// config, base_url) (C25).
func migrateDomainOnlyRouting(document, settings, config *jsonx.Object, baseURL string) (*jsonx.Object, *jsonx.Object) {
	pending("C25", "oxidns_common_routing.migrate_domain_only_routing")
	return nil, nil
}

// prepareNativeRuleUpdates is
// oxidns_common_updates.prepare_native_rule_updates(document, settings,
// config, base_url) (C26).
func prepareNativeRuleUpdates(document, settings, config *jsonx.Object, baseURL string) *jsonx.Object {
	pending("C26", "oxidns_common_updates.prepare_native_rule_updates")
	return nil
}

// patchDocument is oxidns_device_policy.patch_document(document, previous,
// rows) (C26).
func patchDocument(document *jsonx.Object, previous, rows []any) *jsonx.Object {
	pending("C26", "oxidns_device_policy.patch_document")
	return nil
}
