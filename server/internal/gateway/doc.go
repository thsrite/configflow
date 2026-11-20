// Package gateway is the Linux gateway core (backend/common/linux_gateway.py):
// the opt-in, profile-scoped IPv4 selective takeover of a Linux Mihomo host.
//
// It owns the profile setting mihomo.linux_gateway (Settings), the IP rule
// extraction behind the gateway rule providers (ExtractCIDRs, ProviderPayload,
// ProviderYAML, SourceYAML), the patch of a generated Mihomo document (Apply:
// TUN with auto-route/auto-redirect, route-address-set from the fake-IP range
// and the selected rule sets, fake-ip DNS, rule mode, the
// x-configflow-linux-gateway marker), the editor state, selection validation
// and the overlap warnings against later DIRECT rules.
//
// Selections refer to rule_configs IDs, never to mutable rule-library names.
// The providers always include the fake-IP network, so an empty real-IP
// selection cannot turn Mihomo's route-address-set into unrestricted capture.
// With the setting off, Apply is an exact no-op.
//
// It does not talk to Agents or hosts (agents A20), keep drafts or restore
// points (A20), onboard or install gateways (A21/A22), or decide router
// split takeovers (internal/routersplit; Apply only calls its generation
// check). Rule contents that are not inline come from a ContentLoader or,
// by default, from the hook set with SetRuleSourceReader (config C32).
//
// The Mihomo documents it edits are *yaml.Node trees, and profile
// configurations arrive as typed *model.Config views.
package gateway
