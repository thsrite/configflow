package fixedip

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"configflow/internal/routerdoc"
	"configflow/internal/routers/device"
	"configflow/internal/routers/ikuai"
	"configflow/internal/routers/ros"
	"configflow/internal/routers/spec"
)

// Binding is the portable ownership record. Undo is transient and is
// removed before persistence; optional vendor fields retain their presence.
type Binding struct {
	RouterID  string  `json:"router_id,omitempty"`
	Vendor    string  `json:"vendor"`
	ID        string  `json:"id"`
	IP        string  `json:"ip"`
	MAC       string  `json:"mac"`
	Comment   string  `json:"comment"`
	Server    *string `json:"server,omitempty"`
	Interface *string `json:"interface,omitempty"`
	Gateway   *string `json:"gateway,omitempty"`
	Undo      *Undo   `json:"undo,omitempty"`
}

// Undo proves the stable fields written by a single reservation job.
type Undo struct {
	Kind     string  `json:"kind,omitempty"`
	Expected ros.Row `json:"expected"`
	Original ros.Row `json:"original,omitempty"`
}

// Operation is immutable preview evidence, never trusted without rebuilding.
type Operation struct {
	Kind      string          `json:"kind"`
	IP        string          `json:"ip"`
	MAC       string          `json:"mac"`
	Name      *string         `json:"name,omitempty"`
	Vendor    string          `json:"vendor,omitempty"`
	ID        *string         `json:"id,omitempty"`
	Comment   string          `json:"comment"`
	Server    *string         `json:"server,omitempty"`
	Interface *string         `json:"interface,omitempty"`
	Gateway   *string         `json:"gateway,omitempty"`
	Scope     json.RawMessage `json:"scope,omitempty"`
	Expected  ros.Row         `json:"expected,omitempty"`
	Summary   string          `json:"summary"`
}

// MarshalJSON retains RouterOS add's explicit null expected field.
func (o Operation) MarshalJSON() ([]byte, error) {
	type plain Operation
	raw, err := json.Marshal(plain(o))
	if err != nil {
		return nil, err
	}
	if o.ID != nil && o.Kind != "delete" {
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(raw, &fields)
		fields["expected"] = device.JSON(o.Expected)
		return json.Marshal(fields)
	}
	return raw, nil
}

// Snapshot is a complete reservation snapshot. A malformed write table
// is rejected; read-only discovery's row-isolation rules do not grant ownership.
type Snapshot struct {
	Vendor string `json:"-"`
	// RouterOS tables.
	Servers   []ros.Row `json:"servers"`
	Networks  []ros.Row `json:"networks,omitempty"`
	Addresses []ros.Row `json:"addresses,omitempty"`
	Leases    []ros.Row `json:"leases"`
	// iKuai tables.
	ikuaiServers []ikuai.Row
	ikuaiStatic  []ikuai.Row
	ikuaiLeases  []ikuai.Row
}

// State separates control eligibility from evidence about existing leases.
type State struct {
	Eligible bool            `json:"eligible"`
	Reason   string          `json:"reason"`
	Fixed    bool            `json:"fixed"`
	Owned    bool            `json:"owned"`
	Dynamic  bool            `json:"dynamic"`
	Scope    json.RawMessage `json:"scope"`
	Lease    ros.Row         `json:"lease"`
	Binding  *Binding        `json:"binding,omitempty"`
}

// Device implements ownership-safe DHCP reservation writes and compensation.
type Device interface {
	Close()
	SupportsWrite() bool
	ReadFixedIP(context.Context) (*Snapshot, error)
	PreviewFixedIP(context.Context, string, string, string) (*Operation, error)
	ApplyFixedIP(context.Context, *Operation) (*Binding, error)
	RollbackFixedIP(context.Context, *Binding) error
	PreviewDeleteFixedIP(context.Context, *Binding) (*Operation, error)
	DeleteFixedIP(context.Context, *Operation) (*Binding, error)
	RestoreDeletedFixedIP(context.Context, *Binding) (*Binding, error)
	SeenFixedIP(context.Context, string, string) (bool, error)
}

type rosDevice struct{ *ros.Client }
type ikuaiDevice struct{ *ikuai.Client }

// NewROS wraps a RouterOS client as a fixed-IP reservation device.
func NewROS(c *ros.Client) Device { return rosDevice{c} }

// NewIkuai wraps an iKuai client as a fixed-IP reservation device.
func NewIkuai(c *ikuai.Client) Device { return ikuaiDevice{c} }

// Open constructs a fixed-IP reservation device for the configured router.
func Open(r *spec.Router) Device {
	if r == nil {
		return nil
	}
	switch r.Type {
	case "routeros":
		return NewROS(ros.NewClient(r.Endpoint, r.Username, r.Password, r.VerifyTLS))
	case "ikuai":
		return NewIkuai(ikuai.NewClient(r.Endpoint, r.Username, r.Password, r.VerifyTLS))
	default:
		return nil
	}
}

const fixedIPPrefix = "cf-client:"

func fixedMAC(value string) string {
	mac := routerdoc.NormalizeMAC(value)
	if len(mac) != 17 {
		return ""
	}
	bytes, err := hex.DecodeString(strings.ReplaceAll(mac, ":", ""))
	if err != nil || bytes[0]&1 != 0 || mac == "00:00:00:00:00:00" {
		return ""
	}
	return mac
}
func fixedNonce() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", fmt.Errorf("create reservation identity: %w", err)
	}
	return hex.EncodeToString(bytes[:]), nil
}
func fixedPointer(value string) *string { return &value }
func fixedText(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
func fixedOperationBinding(op *Operation, vendor string) *Binding {
	return &Binding{Vendor: vendor, ID: fixedText(op.ID), IP: op.IP, MAC: op.MAC, Comment: op.Comment, Server: op.Server, Interface: op.Interface, Gateway: op.Gateway}
}
func fixedDeleteOperation(b *Binding, expected ros.Row, summary string) *Operation {
	return &Operation{Kind: "delete", Vendor: b.Vendor, ID: fixedPointer(b.ID), IP: b.IP, MAC: b.MAC, Comment: b.Comment, Server: b.Server, Interface: b.Interface, Gateway: b.Gateway, Expected: expected, Summary: summary}
}
