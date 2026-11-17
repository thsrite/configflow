package model

// RouterResource pins a device ID and its full stable configuration; decoding is not proof validation.
type RouterResource struct {
	Path   string  `json:"path"`
	ID     string  `json:"id"`
	Config Dynamic `json:"config"`
	Fields Fields
	raw    Raw
}

// UnmarshalJSON decodes RouterResource with stored-member preservation and R5 fallback.
func (r *RouterResource) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// MarshalJSON preserves unknown and unchanged RouterResource members.
func (r RouterResource) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// ROSPolicyState holds policy ownership; Python has no top-level version for this record.
type ROSPolicyState struct {
	Spec      ROSPolicySpec    `json:"spec"`
	Resources []RouterResource `json:"resources"`
	Fields    Fields
	raw       Raw
}

// UnmarshalJSON decodes ROSPolicyState with stored-member preservation and R5 fallback.
func (r *ROSPolicyState) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// MarshalJSON preserves unknown and unchanged ROSPolicyState members.
func (r ROSPolicyState) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// ROSPolicySpec pins the source, destination, gateway and naming/policy versions of a policy bundle.
type ROSPolicySpec struct {
	Sources       []string `json:"sources"`
	Destinations  []string `json:"destinations"`
	Gateway       string   `json:"gateway"`
	NamingVersion Int      `json:"naming_version"`
	PolicyVersion Int      `json:"policy_version"`
	Fields        Fields
	raw           Raw
}

// UnmarshalJSON decodes ROSPolicySpec with stored-member preservation and R5 fallback.
func (r *ROSPolicySpec) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// MarshalJSON preserves unknown and unchanged ROSPolicySpec members.
func (r ROSPolicySpec) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// DNSGuardState holds DNS guard ownership and its exact published specification.
type DNSGuardState struct {
	Version   Int                `json:"version"`
	Spec      DNSGuardSpec       `json:"spec"`
	Resources []DNSGuardResource `json:"resources"`
	Fields    Fields
	raw       Raw
}

// UnmarshalJSON decodes DNSGuardState with stored-member preservation and R5 fallback.
func (r *DNSGuardState) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// MarshalJSON preserves unknown and unchanged DNSGuardState members.
func (r DNSGuardState) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// DNSGuardSpec is the published LAN DNS interception specification. Exempt is a
// pointer because older specs lack it and readers then derive it from Bypass.
type DNSGuardSpec struct {
	Agent     string    `json:"agent"`
	Interface string    `json:"interface"`
	Network   string    `json:"network"`
	Bypass    []string  `json:"bypass"`
	BlockDoT  *bool     `json:"block_dot"`
	Failover  *bool     `json:"failover"`
	Exempt    *[]string `json:"exempt"`
	Fields    Fields
	raw       Raw
}

// UnmarshalJSON decodes DNSGuardSpec with stored-member preservation and R5 fallback.
func (r *DNSGuardSpec) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// MarshalJSON preserves unknown and unchanged DNSGuardSpec members.
func (r DNSGuardSpec) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// DNSGuardResource adds a guard role to a device ID and full stable configuration.
type DNSGuardResource struct {
	Role   string  `json:"role"`
	Path   string  `json:"path"`
	ID     string  `json:"id"`
	Config Dynamic `json:"config"`
	Fields Fields
	raw    Raw
}

// UnmarshalJSON decodes DNSGuardResource with stored-member preservation and R5 fallback.
func (r *DNSGuardResource) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// MarshalJSON preserves unknown and unchanged DNSGuardResource members.
func (r DNSGuardResource) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// IPv6FakeIPState retains the owned IPv6 fake-IP route and its specification.
type IPv6FakeIPState struct {
	Version   Int              `json:"version"`
	Spec      IPv6FakeIPSpec   `json:"spec"`
	Resources []RouterResource `json:"resources"`
	Fields    Fields
	raw       Raw
}

// UnmarshalJSON decodes IPv6FakeIPState with stored-member preservation and R5 fallback.
func (r *IPv6FakeIPState) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// MarshalJSON preserves unknown and unchanged IPv6FakeIPState members.
func (r IPv6FakeIPState) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// IPv6FakeIPSpec pins the fake-IP network and gateway.
type IPv6FakeIPSpec struct {
	CIDR    string `json:"cidr"`
	Gateway string `json:"gateway"`
	Fields  Fields
	raw     Raw
}

// UnmarshalJSON decodes IPv6FakeIPSpec with stored-member preservation and R5 fallback.
func (r *IPv6FakeIPSpec) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// MarshalJSON preserves unknown and unchanged IPv6FakeIPSpec members.
func (r IPv6FakeIPSpec) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// IPv6RoutesState retains the gateway and full proofs of the owned IPv6 routes.
type IPv6RoutesState struct {
	Version   Int              `json:"version"`
	Gateway   string           `json:"gateway"`
	Resources []RouterResource `json:"resources"`
	Fields    Fields
	raw       Raw
}

// UnmarshalJSON decodes IPv6RoutesState with stored-member preservation and R5 fallback.
func (r *IPv6RoutesState) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// MarshalJSON preserves unknown and unchanged IPv6RoutesState members.
func (r IPv6RoutesState) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// IPv6PDGuardState holds prefix-delegation ownership using its deliberately ID-only resources.
type IPv6PDGuardState struct {
	Version   Int               `json:"version"`
	Spec      IPv6PDGuardSpec   `json:"spec"`
	Resources []PDGuardResource `json:"resources"`
	Fields    Fields
	raw       Raw
}

// UnmarshalJSON decodes IPv6PDGuardState with stored-member preservation and R5 fallback.
func (r *IPv6PDGuardState) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// MarshalJSON preserves unknown and unchanged IPv6PDGuardState members.
func (r IPv6PDGuardState) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// IPv6PDGuardSpec pins the resolved prefix-delegation installation parameters.
type IPv6PDGuardSpec struct {
	LAN             string `json:"lan"`
	Pool            string `json:"pool"`
	Client          string `json:"client"`
	ClientInterface string `json:"client_interface"`
	Hold            Int    `json:"hold"`
	Fields          Fields
	raw             Raw
}

// UnmarshalJSON decodes IPv6PDGuardSpec with stored-member preservation and R5 fallback.
func (r *IPv6PDGuardSpec) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// MarshalJSON preserves unknown and unchanged IPv6PDGuardSpec members.
func (r IPv6PDGuardSpec) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// PDGuardResource is an ID-only ownership proof; it has no configuration member.
type PDGuardResource struct {
	Role   string `json:"role"`
	Path   string `json:"path"`
	ID     string `json:"id"`
	Fields Fields
	raw    Raw
}

// UnmarshalJSON decodes PDGuardResource with stored-member preservation and R5 fallback.
func (r *PDGuardResource) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// MarshalJSON preserves unknown and unchanged PDGuardResource members.
func (r PDGuardResource) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }
