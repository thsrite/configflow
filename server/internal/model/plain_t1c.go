// Plain-record codecs (O18 T1c) of the router, settings, resource and policy
// records: lenient UnmarshalJSON for stored data, plainWire so a record nested
// in another decodes with the enclosing decoder's options, strictInput so it
// is decoded strictly as input (see decodeStrict), MarshalJSON in declaration
// order.

package model

import (
	"bytes"
	"encoding/json"
)

type routersWire Routers

func (*Routers) plainRecord() {}

func (*Routers) strictInput() {}

// UnmarshalJSON decodes stored or captured Routers data leniently (DecodeStored).
func (r *Routers) UnmarshalJSON(data []byte) error { return decodePlain(data, (*routersWire)(r)) }

func (r *Routers) plainWire() any { return (*routersWire)(r) }

// MarshalJSON writes the Routers members in declaration order.
func (r Routers) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

// SetJSON strictly decodes data into a declared member (by JSON name); a value
// of another JSON type is a *MemberTypeError and leaves the record unchanged.
func (r *Routers) SetJSON(member string, data json.RawMessage) error {
	return decodeMember("routers", r, member, data)
}

// Clear sets a declared member (by JSON name) to its zero value.
func (r *Routers) Clear(member string) error { return clearMember(r, member) }

type routerWire Router

func (*Router) plainRecord() {}

func (*Router) strictInput() {}

// UnmarshalJSON decodes stored or captured Router data leniently (DecodeStored).
func (r *Router) UnmarshalJSON(data []byte) error { return decodePlain(data, (*routerWire)(r)) }

func (r *Router) plainWire() any { return (*routerWire)(r) }

// MarshalJSON writes the Router members in declaration order.
func (r Router) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

// SetJSON strictly decodes data into a declared member (by JSON name); a value
// of another JSON type is a *MemberTypeError and leaves the record unchanged.
func (r *Router) SetJSON(member string, data json.RawMessage) error {
	return decodeMember("router", r, member, data)
}

// Clear sets a declared member (by JSON name) to its zero value.
func (r *Router) Clear(member string) error { return clearMember(r, member) }

type routerDNSGuardWire RouterDNSGuard

func (*RouterDNSGuard) plainRecord() {}

func (*RouterDNSGuard) strictInput() {}

// UnmarshalJSON decodes stored or captured RouterDNSGuard data leniently (DecodeStored).
func (r *RouterDNSGuard) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*routerDNSGuardWire)(r))
}

func (r *RouterDNSGuard) plainWire() any { return (*routerDNSGuardWire)(r) }

// MarshalJSON writes the RouterDNSGuard members in declaration order, or a damaged
// stored guard as stored.
func (r RouterDNSGuard) MarshalJSON() ([]byte, error) {
	if r.Stored != nil {
		return bytes.Clone(r.Stored), nil
	}
	return marshalPlain(&r)
}

type routerPDGuardWire RouterPDGuard

func (*RouterPDGuard) plainRecord() {}

func (*RouterPDGuard) strictInput() {}

// UnmarshalJSON decodes stored or captured RouterPDGuard data leniently (DecodeStored).
func (r *RouterPDGuard) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*routerPDGuardWire)(r))
}

func (r *RouterPDGuard) plainWire() any { return (*routerPDGuardWire)(r) }

// MarshalJSON writes the RouterPDGuard members in declaration order, or a damaged
// stored guard as stored.
func (r RouterPDGuard) MarshalJSON() ([]byte, error) {
	if r.Stored != nil {
		return bytes.Clone(r.Stored), nil
	}
	return marshalPlain(&r)
}

type routerProbeWire RouterProbe

func (*RouterProbe) plainRecord() {}

func (*RouterProbe) strictInput() {}

// UnmarshalJSON decodes stored or captured RouterProbe data leniently (DecodeStored).
func (r *RouterProbe) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*routerProbeWire)(r))
}

func (r *RouterProbe) plainWire() any { return (*routerProbeWire)(r) }

// MarshalJSON writes the RouterProbe members in declaration order.
func (r RouterProbe) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type routerReleaseWire RouterRelease

func (*RouterRelease) plainRecord() {}

func (*RouterRelease) strictInput() {}

// UnmarshalJSON decodes stored or captured RouterRelease data leniently (DecodeStored).
func (r *RouterRelease) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*routerReleaseWire)(r))
}

func (r *RouterRelease) plainWire() any { return (*routerReleaseWire)(r) }

// MarshalJSON writes the RouterRelease members in declaration order.
func (r RouterRelease) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type routerResourceWire RouterResource

func (*RouterResource) plainRecord() {}

func (*RouterResource) strictInput() {}

// UnmarshalJSON decodes stored or captured RouterResource data leniently (DecodeStored).
func (r *RouterResource) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*routerResourceWire)(r))
}

func (r *RouterResource) plainWire() any { return (*routerResourceWire)(r) }

// MarshalJSON writes the RouterResource members in declaration order.
func (r RouterResource) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type rOSPolicyStateWire ROSPolicyState

func (*ROSPolicyState) plainRecord() {}

func (*ROSPolicyState) strictInput() {}

// UnmarshalJSON decodes stored or captured ROSPolicyState data leniently (DecodeStored).
func (r *ROSPolicyState) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*rOSPolicyStateWire)(r))
}

func (r *ROSPolicyState) plainWire() any { return (*rOSPolicyStateWire)(r) }

type rOSPolicySpecWire ROSPolicySpec

func (*ROSPolicySpec) plainRecord() {}

func (*ROSPolicySpec) strictInput() {}

// UnmarshalJSON decodes stored or captured ROSPolicySpec data leniently (DecodeStored).
func (r *ROSPolicySpec) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*rOSPolicySpecWire)(r))
}

func (r *ROSPolicySpec) plainWire() any { return (*rOSPolicySpecWire)(r) }

// MarshalJSON writes the ROSPolicySpec members in declaration order.
func (r ROSPolicySpec) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type dNSGuardStateWire DNSGuardState

func (*DNSGuardState) plainRecord() {}

func (*DNSGuardState) strictInput() {}

// UnmarshalJSON decodes stored or captured DNSGuardState data leniently (DecodeStored).
func (r *DNSGuardState) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*dNSGuardStateWire)(r))
}

func (r *DNSGuardState) plainWire() any { return (*dNSGuardStateWire)(r) }

type dNSGuardSpecWire DNSGuardSpec

func (*DNSGuardSpec) plainRecord() {}

func (*DNSGuardSpec) strictInput() {}

// UnmarshalJSON decodes stored or captured DNSGuardSpec data leniently (DecodeStored).
func (r *DNSGuardSpec) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*dNSGuardSpecWire)(r))
}

func (r *DNSGuardSpec) plainWire() any { return (*dNSGuardSpecWire)(r) }

// MarshalJSON writes the DNSGuardSpec members in declaration order.
func (r DNSGuardSpec) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type dNSGuardResourceWire DNSGuardResource

func (*DNSGuardResource) plainRecord() {}

func (*DNSGuardResource) strictInput() {}

// UnmarshalJSON decodes stored or captured DNSGuardResource data leniently (DecodeStored).
func (r *DNSGuardResource) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*dNSGuardResourceWire)(r))
}

func (r *DNSGuardResource) plainWire() any { return (*dNSGuardResourceWire)(r) }

// MarshalJSON writes the DNSGuardResource members in declaration order.
func (r DNSGuardResource) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type iPv6FakeIPStateWire IPv6FakeIPState

func (*IPv6FakeIPState) plainRecord() {}

func (*IPv6FakeIPState) strictInput() {}

// UnmarshalJSON decodes stored or captured IPv6FakeIPState data leniently (DecodeStored).
func (r *IPv6FakeIPState) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*iPv6FakeIPStateWire)(r))
}

func (r *IPv6FakeIPState) plainWire() any { return (*iPv6FakeIPStateWire)(r) }

type iPv6FakeIPSpecWire IPv6FakeIPSpec

func (*IPv6FakeIPSpec) plainRecord() {}

func (*IPv6FakeIPSpec) strictInput() {}

// UnmarshalJSON decodes stored or captured IPv6FakeIPSpec data leniently (DecodeStored).
func (r *IPv6FakeIPSpec) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*iPv6FakeIPSpecWire)(r))
}

func (r *IPv6FakeIPSpec) plainWire() any { return (*iPv6FakeIPSpecWire)(r) }

// MarshalJSON writes the IPv6FakeIPSpec members in declaration order.
func (r IPv6FakeIPSpec) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type iPv6RoutesStateWire IPv6RoutesState

func (*IPv6RoutesState) plainRecord() {}

func (*IPv6RoutesState) strictInput() {}

// UnmarshalJSON decodes stored or captured IPv6RoutesState data leniently (DecodeStored).
func (r *IPv6RoutesState) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*iPv6RoutesStateWire)(r))
}

func (r *IPv6RoutesState) plainWire() any { return (*iPv6RoutesStateWire)(r) }

type iPv6PDGuardStateWire IPv6PDGuardState

func (*IPv6PDGuardState) plainRecord() {}

func (*IPv6PDGuardState) strictInput() {}

// UnmarshalJSON decodes stored or captured IPv6PDGuardState data leniently (DecodeStored).
func (r *IPv6PDGuardState) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*iPv6PDGuardStateWire)(r))
}

func (r *IPv6PDGuardState) plainWire() any { return (*iPv6PDGuardStateWire)(r) }

type iPv6PDGuardSpecWire IPv6PDGuardSpec

func (*IPv6PDGuardSpec) plainRecord() {}

func (*IPv6PDGuardSpec) strictInput() {}

// UnmarshalJSON decodes stored or captured IPv6PDGuardSpec data leniently (DecodeStored).
func (r *IPv6PDGuardSpec) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*iPv6PDGuardSpecWire)(r))
}

func (r *IPv6PDGuardSpec) plainWire() any { return (*iPv6PDGuardSpecWire)(r) }

// MarshalJSON writes the IPv6PDGuardSpec members in declaration order.
func (r IPv6PDGuardSpec) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type pDGuardResourceWire PDGuardResource

func (*PDGuardResource) plainRecord() {}

func (*PDGuardResource) strictInput() {}

// UnmarshalJSON decodes stored or captured PDGuardResource data leniently (DecodeStored).
func (r *PDGuardResource) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*pDGuardResourceWire)(r))
}

func (r *PDGuardResource) plainWire() any { return (*pDGuardResourceWire)(r) }

// MarshalJSON writes the PDGuardResource members in declaration order.
func (r PDGuardResource) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type routerRuleSyncReceiptWire RouterRuleSyncReceipt

func (*RouterRuleSyncReceipt) plainRecord() {}

func (*RouterRuleSyncReceipt) strictInput() {}

// UnmarshalJSON decodes stored or captured RouterRuleSyncReceipt data leniently (DecodeStored).
func (r *RouterRuleSyncReceipt) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*routerRuleSyncReceiptWire)(r))
}

func (r *RouterRuleSyncReceipt) plainWire() any { return (*routerRuleSyncReceiptWire)(r) }

// MarshalJSON writes the RouterRuleSyncReceipt members in declaration order.
func (r RouterRuleSyncReceipt) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type routerSyncLogWire RouterSyncLog

func (*RouterSyncLog) plainRecord() {}

func (*RouterSyncLog) strictInput() {}

// UnmarshalJSON decodes stored or captured RouterSyncLog data leniently (DecodeStored).
func (r *RouterSyncLog) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*routerSyncLogWire)(r))
}

func (r *RouterSyncLog) plainWire() any { return (*routerSyncLogWire)(r) }

// MarshalJSON writes the RouterSyncLog members in declaration order.
func (r RouterSyncLog) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type routerSyncRuleWire RouterSyncRule

func (*RouterSyncRule) plainRecord() {}

func (*RouterSyncRule) strictInput() {}

// UnmarshalJSON decodes stored or captured RouterSyncRule data leniently (DecodeStored).
func (r *RouterSyncRule) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*routerSyncRuleWire)(r))
}

func (r *RouterSyncRule) plainWire() any { return (*routerSyncRuleWire)(r) }

// MarshalJSON writes the RouterSyncRule members in declaration order.
func (r RouterSyncRule) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type routerRuleSyncBaselineWire RouterRuleSyncBaseline

func (*RouterRuleSyncBaseline) plainRecord() {}

func (*RouterRuleSyncBaseline) strictInput() {}

// UnmarshalJSON decodes stored or captured RouterRuleSyncBaseline data leniently (DecodeStored).
func (r *RouterRuleSyncBaseline) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*routerRuleSyncBaselineWire)(r))
}

func (r *RouterRuleSyncBaseline) plainWire() any { return (*routerRuleSyncBaselineWire)(r) }

// MarshalJSON writes the RouterRuleSyncBaseline members in declaration order.
func (r RouterRuleSyncBaseline) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type routerSyncDesiredWire RouterSyncDesired

func (*RouterSyncDesired) plainRecord() {}

func (*RouterSyncDesired) strictInput() {}

// UnmarshalJSON decodes stored or captured RouterSyncDesired data leniently (DecodeStored).
func (r *RouterSyncDesired) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*routerSyncDesiredWire)(r))
}

func (r *RouterSyncDesired) plainWire() any { return (*routerSyncDesiredWire)(r) }

// MarshalJSON writes the RouterSyncDesired members in declaration order.
func (r RouterSyncDesired) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type routerCIDRWire RouterCIDR

func (*RouterCIDR) plainRecord() {}

func (*RouterCIDR) strictInput() {}

// UnmarshalJSON decodes stored or captured RouterCIDR data leniently (DecodeStored).
func (r *RouterCIDR) UnmarshalJSON(data []byte) error { return decodePlain(data, (*routerCIDRWire)(r)) }

func (r *RouterCIDR) plainWire() any { return (*routerCIDRWire)(r) }

// MarshalJSON writes the RouterCIDR members in declaration order.
func (r RouterCIDR) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type routerSyncNATWire RouterSyncNAT

func (*RouterSyncNAT) plainRecord() {}

func (*RouterSyncNAT) strictInput() {}

// UnmarshalJSON decodes stored or captured RouterSyncNAT data leniently (DecodeStored).
func (r *RouterSyncNAT) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*routerSyncNATWire)(r))
}

func (r *RouterSyncNAT) plainWire() any { return (*routerSyncNATWire)(r) }

// MarshalJSON writes the RouterSyncNAT members in declaration order.
func (r RouterSyncNAT) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type resourceRefWire ResourceRef

func (*ResourceRef) plainRecord() {}

func (*ResourceRef) strictInput() {}

// UnmarshalJSON decodes stored or captured ResourceRef data leniently (DecodeStored).
func (r *ResourceRef) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*resourceRefWire)(r))
}

func (r *ResourceRef) plainWire() any { return (*resourceRefWire)(r) }

// MarshalJSON writes the ResourceRef members in declaration order.
func (r ResourceRef) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type providerHealthCheckWire ProviderHealthCheck

func (*ProviderHealthCheck) plainRecord() {}

func (*ProviderHealthCheck) strictInput() {}

// UnmarshalJSON decodes stored or captured ProviderHealthCheck data leniently (DecodeStored).
func (r *ProviderHealthCheck) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*providerHealthCheckWire)(r))
}

func (r *ProviderHealthCheck) plainWire() any { return (*providerHealthCheckWire)(r) }

// MarshalJSON writes the ProviderHealthCheck members in declaration order.
func (r ProviderHealthCheck) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type nodeWire Node

func (*Node) plainRecord() {}

func (*Node) strictInput() {}

// UnmarshalJSON decodes stored or captured Node data leniently (DecodeStored).
func (r *Node) UnmarshalJSON(data []byte) error { return decodePlain(data, (*nodeWire)(r)) }

func (r *Node) plainWire() any { return (*nodeWire)(r) }

// MarshalJSON writes the Node members in declaration order.
func (r Node) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type subscriptionWire Subscription

func (*Subscription) plainRecord() {}

func (*Subscription) strictInput() {}

// UnmarshalJSON decodes stored or captured Subscription data leniently (DecodeStored).
func (r *Subscription) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*subscriptionWire)(r))
}

func (r *Subscription) plainWire() any { return (*subscriptionWire)(r) }

// MarshalJSON writes the Subscription members in declaration order.
func (r Subscription) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type aggregationWire Aggregation

func (*Aggregation) plainRecord() {}

func (*Aggregation) strictInput() {}

// UnmarshalJSON decodes stored or captured Aggregation data leniently (DecodeStored).
func (r *Aggregation) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*aggregationWire)(r))
}

func (r *Aggregation) plainWire() any { return (*aggregationWire)(r) }

// MarshalJSON writes the Aggregation members in declaration order.
func (r Aggregation) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type ruleLibraryItemWire RuleLibraryItem

func (*RuleLibraryItem) plainRecord() {}

func (*RuleLibraryItem) strictInput() {}

// UnmarshalJSON decodes stored or captured RuleLibraryItem data leniently (DecodeStored).
func (r *RuleLibraryItem) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*ruleLibraryItemWire)(r))
}

func (r *RuleLibraryItem) plainWire() any { return (*ruleLibraryItemWire)(r) }

// MarshalJSON writes the RuleLibraryItem members in declaration order.
func (r RuleLibraryItem) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type proxyChainWire ProxyChain

func (*ProxyChain) plainRecord() {}

func (*ProxyChain) strictInput() {}

// UnmarshalJSON decodes stored or captured ProxyChain data leniently (DecodeStored).
func (r *ProxyChain) UnmarshalJSON(data []byte) error { return decodePlain(data, (*proxyChainWire)(r)) }

func (r *ProxyChain) plainWire() any { return (*proxyChainWire)(r) }

// MarshalJSON writes the ProxyChain members in declaration order.
func (r ProxyChain) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type proxyGroupWire ProxyGroup

func (*ProxyGroup) plainRecord() {}

func (*ProxyGroup) strictInput() {}

// UnmarshalJSON decodes stored or captured ProxyGroup data leniently (DecodeStored).
func (r *ProxyGroup) UnmarshalJSON(data []byte) error { return decodePlain(data, (*proxyGroupWire)(r)) }

func (r *ProxyGroup) plainWire() any { return (*proxyGroupWire)(r) }

// MarshalJSON writes the ProxyGroup members in declaration order.
func (r ProxyGroup) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

// SetJSON strictly decodes data into a declared member (by JSON name); a value
// of another JSON type is a *MemberTypeError and leaves the record unchanged.
func (r *ProxyGroup) SetJSON(member string, data json.RawMessage) error {
	return decodeMember("proxy_group", r, member, data)
}

// Clear sets a declared member (by JSON name) to its zero value.
func (r *ProxyGroup) Clear(member string) error { return clearMember(r, member) }

type ruleConfigWire RuleConfig

func (*RuleConfig) plainRecord() {}

func (*RuleConfig) strictInput() {}

// UnmarshalJSON decodes stored or captured RuleConfig data leniently (DecodeStored).
func (r *RuleConfig) UnmarshalJSON(data []byte) error { return decodePlain(data, (*ruleConfigWire)(r)) }

func (r *RuleConfig) plainWire() any { return (*ruleConfigWire)(r) }

// MarshalJSON writes the RuleConfig members in declaration order.
func (r RuleConfig) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type systemConfigWire SystemConfig

func (*SystemConfig) plainRecord() {}

func (*SystemConfig) strictInput() {}

// UnmarshalJSON decodes stored or captured SystemConfig data leniently (DecodeStored).
func (r *SystemConfig) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*systemConfigWire)(r))
}

func (r *SystemConfig) plainWire() any { return (*systemConfigWire)(r) }

// MarshalJSON writes the SystemConfig members in declaration order.
func (r SystemConfig) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

// SetJSON strictly decodes data into a declared member (by JSON name); a value
// of another JSON type is a *MemberTypeError and leaves the record unchanged.
func (r *SystemConfig) SetJSON(member string, data json.RawMessage) error {
	return decodeMember("system_config", r, member, data)
}

// Clear sets a declared member (by JSON name) to its zero value.
func (r *SystemConfig) Clear(member string) error { return clearMember(r, member) }

type nodeTestDNSConfigWire NodeTestDNSConfig

func (*NodeTestDNSConfig) plainRecord() {}

func (*NodeTestDNSConfig) strictInput() {}

// UnmarshalJSON decodes stored or captured NodeTestDNSConfig data leniently (DecodeStored).
func (r *NodeTestDNSConfig) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*nodeTestDNSConfigWire)(r))
}

func (r *NodeTestDNSConfig) plainWire() any { return (*nodeTestDNSConfigWire)(r) }

// MarshalJSON writes the NodeTestDNSConfig members in declaration order.
func (r NodeTestDNSConfig) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type managedDNSBindingWire ManagedDNSBinding

func (*ManagedDNSBinding) plainRecord() {}

func (*ManagedDNSBinding) strictInput() {}

// UnmarshalJSON decodes stored or captured ManagedDNSBinding data leniently (DecodeStored).
func (r *ManagedDNSBinding) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*managedDNSBindingWire)(r))
}

func (r *ManagedDNSBinding) plainWire() any { return (*managedDNSBindingWire)(r) }

// MarshalJSON writes the ManagedDNSBinding members in declaration order.
func (r ManagedDNSBinding) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type backupWire Backup

func (*Backup) plainRecord() {}

func (*Backup) strictInput() {}

// UnmarshalJSON decodes stored or captured Backup data leniently (DecodeStored).
func (r *Backup) UnmarshalJSON(data []byte) error { return decodePlain(data, (*backupWire)(r)) }

func (r *Backup) plainWire() any { return (*backupWire)(r) }

// MarshalJSON writes the Backup members in declaration order.
func (r Backup) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

// SetJSON strictly decodes data into a declared member (by JSON name); a value
// of another JSON type is a *MemberTypeError and leaves the record unchanged.
func (r *Backup) SetJSON(member string, data json.RawMessage) error {
	return decodeMember("backup", r, member, data)
}

// Clear sets a declared member (by JSON name) to its zero value.
func (r *Backup) Clear(member string) error { return clearMember(r, member) }

type mihomoSettingsWire MihomoSettings

func (*MihomoSettings) plainRecord() {}

func (*MihomoSettings) strictInput() {}

// UnmarshalJSON decodes stored or captured MihomoSettings data leniently (DecodeStored).
func (r *MihomoSettings) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*mihomoSettingsWire)(r))
}

func (r *MihomoSettings) plainWire() any { return (*mihomoSettingsWire)(r) }

// MarshalJSON writes the MihomoSettings members in declaration order.
func (r MihomoSettings) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

// SetJSON strictly decodes data into a declared member (by JSON name); a value
// of another JSON type is a *MemberTypeError and leaves the record unchanged.
func (r *MihomoSettings) SetJSON(member string, data json.RawMessage) error {
	return decodeMember("mihomo", r, member, data)
}

// Clear sets a declared member (by JSON name) to its zero value.
func (r *MihomoSettings) Clear(member string) error { return clearMember(r, member) }

type surgeSettingsWire SurgeSettings

func (*SurgeSettings) plainRecord() {}

func (*SurgeSettings) strictInput() {}

// UnmarshalJSON decodes stored or captured SurgeSettings data leniently (DecodeStored).
func (r *SurgeSettings) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*surgeSettingsWire)(r))
}

func (r *SurgeSettings) plainWire() any { return (*surgeSettingsWire)(r) }

// MarshalJSON writes the SurgeSettings members in declaration order.
func (r SurgeSettings) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type surgeSmartGroupWire SurgeSmartGroup

func (*SurgeSmartGroup) plainRecord() {}

func (*SurgeSmartGroup) strictInput() {}

// UnmarshalJSON decodes stored or captured SurgeSmartGroup data leniently (DecodeStored).
func (r *SurgeSmartGroup) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*surgeSmartGroupWire)(r))
}

func (r *SurgeSmartGroup) plainWire() any { return (*surgeSmartGroupWire)(r) }

// MarshalJSON writes the SurgeSmartGroup members in declaration order.
func (r SurgeSmartGroup) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type loonSettingsWire LoonSettings

func (*LoonSettings) plainRecord() {}

func (*LoonSettings) strictInput() {}

// UnmarshalJSON decodes stored or captured LoonSettings data leniently (DecodeStored).
func (r *LoonSettings) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*loonSettingsWire)(r))
}

func (r *LoonSettings) plainWire() any { return (*loonSettingsWire)(r) }

// MarshalJSON writes the LoonSettings members in declaration order.
func (r LoonSettings) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type easyTierSettingsWire EasyTierSettings

func (*EasyTierSettings) plainRecord() {}

func (*EasyTierSettings) strictInput() {}

// UnmarshalJSON decodes stored or captured EasyTierSettings data leniently (DecodeStored).
func (r *EasyTierSettings) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*easyTierSettingsWire)(r))
}

func (r *EasyTierSettings) plainWire() any { return (*easyTierSettingsWire)(r) }

// MarshalJSON writes the EasyTierSettings members in declaration order.
func (r EasyTierSettings) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type easyTierKeepaliveWire EasyTierKeepalive

func (*EasyTierKeepalive) plainRecord() {}

func (*EasyTierKeepalive) strictInput() {}

// UnmarshalJSON decodes stored or captured EasyTierKeepalive data leniently (DecodeStored).
func (r *EasyTierKeepalive) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*easyTierKeepaliveWire)(r))
}

func (r *EasyTierKeepalive) plainWire() any { return (*easyTierKeepaliveWire)(r) }

// MarshalJSON writes the EasyTierKeepalive members in declaration order.
func (r EasyTierKeepalive) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type easyTierOptionsWire EasyTierOptions

func (*EasyTierOptions) plainRecord() {}

func (*EasyTierOptions) strictInput() {}

// UnmarshalJSON decodes stored or captured EasyTierOptions data leniently (DecodeStored).
func (r *EasyTierOptions) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*easyTierOptionsWire)(r))
}

func (r *EasyTierOptions) plainWire() any { return (*easyTierOptionsWire)(r) }

// MarshalJSON writes the EasyTierOptions members in declaration order.
func (r EasyTierOptions) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type mosDNSSettingsWire MosDNSSettings

func (*MosDNSSettings) plainRecord() {}

func (*MosDNSSettings) strictInput() {}

// UnmarshalJSON decodes stored or captured MosDNSSettings data leniently (DecodeStored).
func (r *MosDNSSettings) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*mosDNSSettingsWire)(r))
}

func (r *MosDNSSettings) plainWire() any { return (*mosDNSSettingsWire)(r) }

// MarshalJSON writes the MosDNSSettings members in declaration order.
func (r MosDNSSettings) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

// SetJSON strictly decodes data into a declared member (by JSON name); a value
// of another JSON type is a *MemberTypeError and leaves the record unchanged.
func (r *MosDNSSettings) SetJSON(member string, data json.RawMessage) error {
	return decodeMember("mosdns", r, member, data)
}

// Clear sets a declared member (by JSON name) to its zero value.
func (r *MosDNSSettings) Clear(member string) error { return clearMember(r, member) }

type domainDiscoveryWire DomainDiscovery

func (*DomainDiscovery) plainRecord() {}

func (*DomainDiscovery) strictInput() {}

// UnmarshalJSON decodes stored or captured DomainDiscovery data leniently (DecodeStored).
func (r *DomainDiscovery) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*domainDiscoveryWire)(r))
}

func (r *DomainDiscovery) plainWire() any { return (*domainDiscoveryWire)(r) }

// MarshalJSON writes the DomainDiscovery members in declaration order.
func (r DomainDiscovery) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type discoveryHistoryWire DiscoveryHistory

func (*DiscoveryHistory) plainRecord() {}

func (*DiscoveryHistory) strictInput() {}

// UnmarshalJSON decodes stored or captured DiscoveryHistory data leniently (DecodeStored).
func (r *DiscoveryHistory) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*discoveryHistoryWire)(r))
}

func (r *DiscoveryHistory) plainWire() any { return (*discoveryHistoryWire)(r) }

// MarshalJSON writes the DiscoveryHistory members in declaration order.
func (r DiscoveryHistory) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type discoveryRecheckWire DiscoveryRecheck

func (*DiscoveryRecheck) plainRecord() {}

func (*DiscoveryRecheck) strictInput() {}

// UnmarshalJSON decodes stored or captured DiscoveryRecheck data leniently (DecodeStored).
func (r *DiscoveryRecheck) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*discoveryRecheckWire)(r))
}

func (r *DiscoveryRecheck) plainWire() any { return (*discoveryRecheckWire)(r) }

// MarshalJSON writes the DiscoveryRecheck members in declaration order.
func (r DiscoveryRecheck) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type clientFixedIPWire ClientFixedIP

func (*ClientFixedIP) plainRecord() {}

func (*ClientFixedIP) strictInput() {}

// UnmarshalJSON decodes stored or captured ClientFixedIP data leniently (DecodeStored).
func (r *ClientFixedIP) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*clientFixedIPWire)(r))
}

func (r *ClientFixedIP) plainWire() any { return (*clientFixedIPWire)(r) }

// MarshalJSON writes the ClientFixedIP members in declaration order.
func (r ClientFixedIP) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

// Clear sets a declared member (by JSON name) to its zero value.
func (r *RuleConfig) Clear(member string) error { return clearMember(r, member) }
