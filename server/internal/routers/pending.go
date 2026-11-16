package routers

// Collaborators of later routers modules (R02–R17) and other domains that
// ported modules call. Each is a package variable that fails closed with a
// *PendingError until its module is ported; the porter assigns the real
// function (or replaces the variable) and re-runs the routers replays
// (testdata/*_recorded.jsonl.gz), whose pending rows must drop to zero.
//
// Current list:
//   - RulesetContent (desired.go): routes.rules.get_ruleset_content (C32),
//     the default content loader of ComputeDesired.
//
// R01 ported the settings validators of dns_guard (R09) and ipv6_pd_guard
// (R11) early (guardsettings.go) because store.normalize always calls them;
// they are not collaborators.
