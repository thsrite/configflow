package oxidns

import "configflow/internal/jsonx"

// Functions of later modules of the OxiDNS cluster that C05/C22 call. Each
// returns a *PendingError (raised) until its module is ported; the module's
// porter replaces the stub with the real function in its own file and
// deletes it here, then re-runs this package's replay
// (testdata/recorded.jsonl.gz), whose pending rows must drop to zero.

// validateExpandedDocument is oxidns_validation.validate_expanded_document
// (C23).
func validateExpandedDocument(document *jsonx.Object, sources *NativeSources) []any {
	pending("C23", "oxidns_validation.validate_expanded_document")
	return nil
}

// requireValidDocument is oxidns_validation.require_valid_document (C23).
func requireValidDocument(document any) []any {
	pending("C23", "oxidns_validation.require_valid_document")
	return nil
}

// collectReferenceIssues is
// oxidns_reference_validation.collect_reference_issues(document, catalog,
// dependency_graph=graph) (C23); graph receives tag -> referenced tags.
func collectReferenceIssues(document any, catalog *jsonx.Object, graph map[string][]string, order *[]string) []any {
	pending("C23", "oxidns_reference_validation.collect_reference_issues")
	return nil
}

// classicalIPLines is rule_ip_presence.classical_ip_lines(item, config)
// (routers domain; to be provided through a hook, coordinator decision 3):
// true/false for a classical set with known content, nil (None)
// otherwise.
func classicalIPLines(item any, config any) any {
	pending("routers", "rule_ip_presence.classical_ip_lines")
	return nil
}

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
