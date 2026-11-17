// Package subs ports the subscription parsing helpers of the Python backend:
// utils/subscription_parser (parse_uri_list and what it calls only), subscription_hosts, subscription_filter,
// subscription_dialers, proxy_chains, mihomo_listeners and proxy_server_dns.
// Comment-preserving listener edits use the shared yamlx.Edit facade.
//
// Node dictionaries are *jsonx.Object with the keys, key order and value
// types the Python parsers produce; they are stored and rendered into users'
// configs, so nothing here normalises them. Python exceptions that the
// callers catch map to Go errors or (nil, false) results with the same
// tolerance. Network access (fetching a subscription) is not part of this
// package: subscriptions are downloaded and converted by Sub-Store (#13);
// ParseURIList only parses the proxy_string of manually added nodes for the
// Surge and Loon converters.
package subs
