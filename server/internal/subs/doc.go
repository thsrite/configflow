// Package subs reads subscription nodes and bounded source metadata, filters
// typed records, and composes hosts, DNS and relay/chain graphs for generators.
// Stored configuration uses model records; open engine sections use yamlx.
// Listener edits preserve user text through yamlx.Edit. Shared dialer validation
// and filtering are wired from their single convert owner, without a package
// cycle or private business copies. Network/cache/runtime ownership belongs to
// substore and its collaborators; this package never writes repository state.
package subs
