package model

import (
	"bytes"
	"encoding/json"
)

// Router ownership manifests prove what a publication wrote to a device; the
// router guards refuse connection changes and deletions while one is stored.
// Their members (and those of their specs) are omitzero, so a stored {}
// (Python: falsy, no manifest) stays {}. A manifest stored with another JSON type still proves a
// publication to Python's truthiness guards, so it is not read as absent: the
// stored value is kept (Stored), reported as an issue and written back
// unchanged until the manifest is replaced (damagedKeeper).

// RouterResource pins a device ID and its full stable configuration; decoding is not proof validation.
type RouterResource struct {
	Path   string  `json:"path"`
	ID     string  `json:"id"`
	Config Dynamic `json:"config"`
}

// ROSPolicyState holds policy ownership; Python has no top-level version for this record.
type ROSPolicyState struct {
	Spec      ROSPolicySpec    `json:"spec,omitzero"`
	Resources []RouterResource `json:"resources,omitzero"`
	// Stored keeps a manifest stored with another JSON type; see damagedKeeper.
	Stored json.RawMessage `json:"-"`
}

// ROSPolicySpec pins the source, destination, gateway and naming/policy versions of a policy bundle.
type ROSPolicySpec struct {
	Sources       []string `json:"sources,omitzero"`
	Destinations  []string `json:"destinations,omitzero"`
	Gateway       string   `json:"gateway,omitzero"`
	NamingVersion Int      `json:"naming_version,omitzero"`
	PolicyVersion Int      `json:"policy_version,omitzero"`
}

// DNSGuardState holds DNS guard ownership and its exact published specification.
type DNSGuardState struct {
	Version   Int                `json:"version,omitzero"`
	Spec      DNSGuardSpec       `json:"spec,omitzero"`
	Resources []DNSGuardResource `json:"resources,omitzero"`
	// Stored keeps a manifest stored with another JSON type; see damagedKeeper.
	Stored json.RawMessage `json:"-"`
}

// DNSGuardSpec is the published LAN DNS interception specification. Exempt is a
// pointer because older specs lack it and readers then derive it from Bypass.
type DNSGuardSpec struct {
	Agent     string    `json:"agent,omitzero"`
	Interface string    `json:"interface,omitzero"`
	Network   string    `json:"network,omitzero"`
	Bypass    []string  `json:"bypass,omitzero"`
	BlockDoT  *bool     `json:"block_dot,omitzero"`
	Failover  *bool     `json:"failover,omitzero"`
	Exempt    *[]string `json:"exempt,omitzero"`
}

// DNSGuardResource adds a guard role to a device ID and full stable configuration.
type DNSGuardResource struct {
	Role   string  `json:"role"`
	Path   string  `json:"path"`
	ID     string  `json:"id"`
	Config Dynamic `json:"config"`
}

// IPv6FakeIPState retains the owned IPv6 fake-IP route and its specification.
type IPv6FakeIPState struct {
	Version   Int              `json:"version,omitzero"`
	Spec      IPv6FakeIPSpec   `json:"spec,omitzero"`
	Resources []RouterResource `json:"resources,omitzero"`
	// Stored keeps a manifest stored with another JSON type; see damagedKeeper.
	Stored json.RawMessage `json:"-"`
}

// IPv6FakeIPSpec pins the fake-IP network and gateway.
type IPv6FakeIPSpec struct {
	CIDR    string `json:"cidr,omitzero"`
	Gateway string `json:"gateway,omitzero"`
}

// IPv6RoutesState retains the gateway and full proofs of the owned IPv6 routes.
type IPv6RoutesState struct {
	Version   Int              `json:"version,omitzero"`
	Gateway   string           `json:"gateway,omitzero"`
	Resources []RouterResource `json:"resources,omitzero"`
	// Stored keeps a manifest stored with another JSON type; see damagedKeeper.
	Stored json.RawMessage `json:"-"`
}

// IPv6PDGuardState holds prefix-delegation ownership using its deliberately ID-only resources.
type IPv6PDGuardState struct {
	Version   Int               `json:"version,omitzero"`
	Spec      IPv6PDGuardSpec   `json:"spec,omitzero"`
	Resources []PDGuardResource `json:"resources,omitzero"`
	// Stored keeps a manifest stored with another JSON type; see damagedKeeper.
	Stored json.RawMessage `json:"-"`
}

// IPv6PDGuardSpec pins the resolved prefix-delegation installation parameters.
type IPv6PDGuardSpec struct {
	LAN             string `json:"lan,omitzero"`
	Pool            string `json:"pool,omitzero"`
	Client          string `json:"client,omitzero"`
	ClientInterface string `json:"client_interface,omitzero"`
	Hold            Int    `json:"hold,omitzero"`
}

// PDGuardResource is an ID-only ownership proof; it has no configuration member.
type PDGuardResource struct {
	Role string `json:"role"`
	Path string `json:"path"`
	ID   string `json:"id"`
}

func (r *ROSPolicyState) keepDamaged(raw []byte) { *r = ROSPolicyState{Stored: bytes.Clone(raw)} }

// MarshalJSON writes the manifest members, or a damaged stored value as stored.
func (r ROSPolicyState) MarshalJSON() ([]byte, error) {
	if r.Stored != nil {
		return bytes.Clone(r.Stored), nil
	}
	return marshalPlain(&r)
}

func (r *DNSGuardState) keepDamaged(raw []byte) { *r = DNSGuardState{Stored: bytes.Clone(raw)} }

// MarshalJSON writes the manifest members, or a damaged stored value as stored.
func (r DNSGuardState) MarshalJSON() ([]byte, error) {
	if r.Stored != nil {
		return bytes.Clone(r.Stored), nil
	}
	return marshalPlain(&r)
}

func (r *IPv6FakeIPState) keepDamaged(raw []byte) { *r = IPv6FakeIPState{Stored: bytes.Clone(raw)} }

// MarshalJSON writes the manifest members, or a damaged stored value as stored.
func (r IPv6FakeIPState) MarshalJSON() ([]byte, error) {
	if r.Stored != nil {
		return bytes.Clone(r.Stored), nil
	}
	return marshalPlain(&r)
}

func (r *IPv6RoutesState) keepDamaged(raw []byte) { *r = IPv6RoutesState{Stored: bytes.Clone(raw)} }

// MarshalJSON writes the manifest members, or a damaged stored value as stored.
func (r IPv6RoutesState) MarshalJSON() ([]byte, error) {
	if r.Stored != nil {
		return bytes.Clone(r.Stored), nil
	}
	return marshalPlain(&r)
}

func (r *IPv6PDGuardState) keepDamaged(raw []byte) { *r = IPv6PDGuardState{Stored: bytes.Clone(raw)} }

// MarshalJSON writes the manifest members, or a damaged stored value as stored.
func (r IPv6PDGuardState) MarshalJSON() ([]byte, error) {
	if r.Stored != nil {
		return bytes.Clone(r.Stored), nil
	}
	return marshalPlain(&r)
}
