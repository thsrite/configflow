package oxidns

import "configflow/internal/jsonx"

// Collaborators of later modules of the OxiDNS cluster that earlier modules
// call. Since C26 every one is real; they stay package variables because
// the C24/C25 replays (c24_test.go, c25_test.go) answer them with what
// Python's functions answered during the recorded call.

// prepareEditor is oxidns_editor.prepare_editor(settings, config,
// base_url) (C26, editor.go). A variable so the C24/C25 replays can answer
// it with Python's recorded results; the real function is assigned in init
// (a direct initializer would form an initialization cycle).
var prepareEditor func(settings, config any, baseURL string) *jsonx.Object

func init() {
	prepareEditor = prepareEditorImpl
	prepareRuleUpdate = prepareRuleUpdateImpl
	finishRuleUpdates = finishRuleUpdatesImpl
}

// The C25 collaborators of C24 (oxidns_common_routing, ported in
// routing.go). They stay variables so the C24 replay can answer them with
// Python's recorded results.
var (
	readRouting = func(document, settings *jsonx.Object, config any) *jsonx.Object {
		return readRoutingImpl(document, settings, config)
	}
	ownedChains  = ownedChainsImpl
	applyRouting = func(document, settings *jsonx.Object, config any, changes *jsonx.Object, baseURL string) (*jsonx.Object, *jsonx.Object) {
		return applyRoutingImpl(document, settings, config, changes, baseURL, nil, false, nil)
	}
	resourceDownloads = func(document, settings *jsonx.Object, config any, baseURL string) []any {
		return resourceDownloadsImpl(document, settings, config, baseURL, nil)
	}
	refreshRuleHelpers = refreshRuleHelpersImpl
)

// prepareRuleUpdate and finishRuleUpdates are
// oxidns_common_updates.prepare_rule_update / finish_rule_updates(ctx,
// changes, config, base_url) (C26, updates.go), variables for the C24/C25
// replays like prepareEditor.
var (
	prepareRuleUpdate func(ctx *commonContext, changes *jsonx.Object, config any, baseURL string)
	finishRuleUpdates func(ctx *commonContext, changes *jsonx.Object, config any, baseURL string)
)

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
